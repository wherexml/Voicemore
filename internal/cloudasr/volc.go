package cloudasr

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/coder/websocket"

	"voiceinput/internal/asr"
)

// Volcengine's Doubao streaming recognition, as its document "大模型流式语音
// 识别API" describes it: a WebSocket to
// wss://openspeech.bytedance.com/api/v3/sauc/bigmodel_async (the
// bidirectional mode the document recommends), authenticated in the
// handshake, carrying frames of a binary protocol:
//
//	byte 0   protocol version (4 bits) | header size in 4-byte units (4 bits)
//	byte 1   message type (4 bits)     | type-specific flags (4 bits)
//	byte 2   serialization (4 bits)    | compression (4 bits)
//	byte 3   reserved
//	[4-byte sequence, in server responses whose flags have bit 0]
//	4-byte payload size, big-endian; payload
//
// The client sends one "full client request" (JSON, the audio's format
// and the options), then "audio only" frames, the last one flagged. The
// server answers with "full server responses" carrying the transcript so
// far, or with an error frame. Payloads are gzipped, as in the document's
// examples.
//
// The console's two generations authenticate differently: the new one
// with X-Api-Key alone, the old one with X-Api-App-Key (the App ID) and
// X-Api-Access-Key (the Access Token).

const volcURL = "wss://openspeech.bytedance.com/api/v3/sauc/bigmodel_async"

// Resource IDs of Doubao streaming ASR 2.0, by how the account is billed.
const (
	VolcHourly        = "volc.seedasr.sauc.duration"
	VolcConcurrent    = "volc.seedasr.sauc.concurrent"
	VolcClassicHourly = "volc.bigasr.sauc.duration"
)

const (
	volcFullRequest  = 0b0001
	volcAudioOnly    = 0b0010
	volcFullResponse = 0b1001
	volcError        = 0b1111

	volcFlagSequence = 0b0001 // a sequence number follows the header
	volcFlagLast     = 0b0010 // the last packet

	volcJSON = 0b0001
	volcRaw  = 0b0000
	volcGzip = 0b0001

	volcChunk = 6400 // 200 ms, the packet size the document recommends for this mode
)

// Volc dials Volcengine's streaming recognition.
type Volc struct {
	// APIKey is the new console's key. AppID and AccessToken are the old
	// console's pair. One or the other is set.
	APIKey, AppID, AccessToken string
	Resource                   string
}

func (v Volc) Label() string               { return volcURL }
func (v Volc) Hotwords() bool              { return false }
func (v Volc) Check(context.Context) error { return nil }

func (v Volc) Dial(ctx context.Context, _ *string) (asr.Stream, error) {
	resource := v.Resource
	if resource == "" {
		resource = VolcHourly
	}
	h := http.Header{}
	h.Set("X-Api-Resource-Id", resource)
	h.Set("X-Api-Connect-Id", uuid())
	h.Set("X-Api-Request-Id", uuid())
	h.Set("X-Api-Sequence", "-1")
	if v.APIKey != "" {
		h.Set("X-Api-Key", v.APIKey)
	} else {
		h.Set("X-Api-App-Key", v.AppID)
		h.Set("X-Api-Access-Key", v.AccessToken)
	}
	conn, res, err := websocket.Dial(ctx, volcURL, &websocket.DialOptions{HTTPHeader: h})
	if err != nil {
		return nil, dialError(err, res)
	}
	conn.SetReadLimit(4 << 20)
	s := &volcStream{conn: conn}
	s.done = make(chan struct{})
	request, _ := json.Marshal(map[string]any{
		"user":  map[string]any{"uid": "voice-input"},
		"audio": map[string]any{"format": "pcm", "codec": "raw", "rate": asr.SampleRate, "bits": 16, "channel": 1},
		"request": map[string]any{
			"model_name": "bigmodel", "enable_itn": true, "enable_punc": true,
			"show_utterances": true, "result_type": "full",
		},
	})
	if err := s.write(ctx, volcFullRequest, 0, volcJSON, request); err != nil {
		conn.CloseNow()
		return nil, fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	// The server answers the request before any audio: an error here is
	// about the request or the account, and is reported as the dial's.
	first, err := s.read(ctx)
	if err != nil {
		conn.CloseNow()
		return nil, err
	}
	s.apply(first)
	go s.loop()
	return s, nil
}

type volcStream struct {
	transcript
	outcome
	conn *websocket.Conn
	held []byte // audio not sent yet: the last packet must be flagged
}

// volcFrame is a decoded server frame.
type volcFrame struct {
	last    bool
	payload []byte
}

func frame(msgType, flags, serialization byte, payload []byte) ([]byte, error) {
	var zipped bytes.Buffer
	zw := gzip.NewWriter(&zipped)
	if _, err := zw.Write(payload); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	out := []byte{0x11, msgType<<4 | flags, serialization<<4 | volcGzip, 0x00}
	out = binary.BigEndian.AppendUint32(out, uint32(zipped.Len()))
	return append(out, zipped.Bytes()...), nil
}

func (s *volcStream) write(ctx context.Context, msgType, flags, serialization byte, payload []byte) error {
	data, err := frame(msgType, flags, serialization, payload)
	if err != nil {
		return err
	}
	return s.conn.Write(ctx, websocket.MessageBinary, data)
}

// parseFrame decodes a server frame: a response with its payload, or an
// error frame, which becomes a ServiceError.
func parseFrame(data []byte) (volcFrame, error) {
	var f volcFrame
	if len(data) < 4 {
		return f, fmt.Errorf("%w: a frame of %d bytes", ErrProtocol, len(data))
	}
	header := int(data[0]&0x0f) * 4
	msgType, flags, compression := data[1]>>4, data[1]&0x0f, data[2]&0x0f
	if header < 4 || len(data) < header {
		return f, fmt.Errorf("%w: a header of %d bytes", ErrProtocol, header)
	}
	rest := data[header:]
	take := func() (uint32, bool) {
		if len(rest) < 4 {
			return 0, false
		}
		v := binary.BigEndian.Uint32(rest)
		rest = rest[4:]
		return v, true
	}
	switch msgType {
	case volcFullResponse:
		if flags&volcFlagSequence != 0 {
			if _, ok := take(); !ok {
				return f, fmt.Errorf("%w: a response without its sequence", ErrProtocol)
			}
		}
		f.last = flags&volcFlagLast != 0
	case volcError:
		code, ok := take()
		if !ok {
			return f, fmt.Errorf("%w: an error frame without a code", ErrProtocol)
		}
		size, _ := take()
		msg := rest[:min(int(size), len(rest))]
		if text, err := gunzip(msg, compression); err == nil {
			msg = text
		}
		return f, &ServiceError{fmt.Sprint(code), string(msg)}
	default:
		return f, fmt.Errorf("%w: message type %d", ErrProtocol, msgType)
	}
	size, ok := take()
	if !ok || int(size) > len(rest) {
		return f, fmt.Errorf("%w: a payload shorter than its size", ErrProtocol)
	}
	payload, err := gunzip(rest[:size], compression)
	if err != nil {
		return f, fmt.Errorf("%w: %w", ErrProtocol, err)
	}
	f.payload = payload
	return f, nil
}

func gunzip(data []byte, compression byte) ([]byte, error) {
	if compression != volcGzip || len(data) == 0 {
		return data, nil
	}
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return io.ReadAll(io.LimitReader(zr, 8<<20))
}

func (s *volcStream) read(ctx context.Context) (volcFrame, error) {
	for {
		typ, data, err := s.conn.Read(ctx)
		if err != nil {
			return volcFrame{}, fmt.Errorf("%w: %w", ErrUnreachable, err)
		}
		if typ == websocket.MessageBinary {
			return parseFrame(data)
		}
	}
}

// volcResult is the transcript so far. Utterances marked definite no
// longer change.
type volcResult struct {
	Text       string `json:"text"`
	Utterances []struct {
		Text     string `json:"text"`
		Definite bool   `json:"definite"`
	} `json:"utterances"`
}

// apply shows a response's transcript. The document's table calls
// "result" a list and its example shows an object: both are read.
func (s *volcStream) apply(f volcFrame) {
	if len(f.payload) == 0 {
		return
	}
	var one struct {
		Result volcResult `json:"result"`
	}
	if err := json.Unmarshal(f.payload, &one); err != nil {
		var many struct {
			Result []volcResult `json:"result"`
		}
		if json.Unmarshal(f.payload, &many) != nil || len(many.Result) == 0 {
			return
		}
		one.Result = many.Result[0]
	}
	r := one.Result
	if f.last || len(r.Utterances) == 0 {
		// The last response is the whole, final transcript.
		if f.last {
			s.replace(r.Text, "")
		} else {
			s.replace("", r.Text)
		}
		return
	}
	final, pending := "", ""
	for _, u := range r.Utterances {
		if u.Definite && pending == "" {
			final += u.Text
		} else {
			pending += u.Text
		}
	}
	s.replace(final, pending)
}

func (s *volcStream) loop() {
	defer close(s.done)
	for {
		f, err := s.read(context.Background())
		if err != nil {
			s.fail(err)
			return
		}
		s.apply(f)
		if f.last {
			s.finished()
			return
		}
	}
}

// Send sends the audio in packets of 200 ms, keeping the newest back:
// the last packet of a session must carry the "last" flag, and which one
// that is is only known at Finish.
func (s *volcStream) Send(ctx context.Context, pcm []byte) error {
	s.held = append(s.held, pcm...)
	for len(s.held) > volcChunk {
		if err := s.write(ctx, volcAudioOnly, 0, volcRaw, s.held[:volcChunk]); err != nil {
			return err
		}
		s.held = s.held[volcChunk:]
	}
	return nil
}

func (s *volcStream) Finish(ctx context.Context) error {
	if err := s.write(ctx, volcAudioOnly, volcFlagLast, volcRaw, s.held); err != nil {
		return err
	}
	s.held = nil
	return wait(ctx, &s.outcome)
}

func (s *volcStream) Close() { s.conn.Close(websocket.StatusNormalClosure, "") }
