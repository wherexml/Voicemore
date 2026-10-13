package cloudasr

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/coder/websocket"

	"voiceinput/internal/asr"
)

// Alibaba Cloud Model Studio has two realtime protocols, and which one a
// model speaks is stated in its documents, not guessed from its name:
//
//   - Fun-ASR-Realtime (fun-asr-realtime): the "inference" protocol.
//     wss://…/api-ws/v1/inference; run-task, binary audio, finish-task;
//     task-started, result-generated, task-finished, task-failed.
//   - Qwen-ASR-Realtime (qwen3-asr-flash-realtime): the "realtime"
//     protocol. wss://…/api-ws/v1/realtime?model=…; session.update,
//     input_audio_buffer.append with base64 audio, session.finish.
//
// Both authenticate in the handshake with "Authorization: Bearer <key>".

// AliyunDefaultBase is the address the documents say still works; they
// recommend the workspace's own domain,
// wss://{WorkspaceId}.cn-beijing.maas.aliyuncs.com/api-ws/v1, which the
// user can enter instead.
const AliyunDefaultBase = "wss://dashscope.aliyuncs.com/api-ws/v1"

// Protocols of the Alibaba models this app can use.
const (
	AliyunInference = "inference"
	AliyunRealtime  = "realtime"
)

// AliyunProtocol is the protocol the documents give for a model, or "" for
// a model they do not list among the realtime ones.
func AliyunProtocol(model string) string {
	switch model {
	case "fun-asr-realtime":
		return AliyunInference
	case "qwen3-asr-flash-realtime":
		return AliyunRealtime
	}
	return ""
}

// Aliyun dials Alibaba Cloud's realtime recognition for one model.
type Aliyun struct {
	Base   string // wss://…/api-ws/v1
	APIKey string
	Model  string
}

func (a Aliyun) base() string {
	if b := strings.TrimRight(strings.TrimSpace(a.Base), "/"); b != "" {
		return b
	}
	return AliyunDefaultBase
}

func (a Aliyun) Label() string                 { return a.base() + " (" + a.Model + ")" }
func (a Aliyun) Hotwords() bool                { return false }
func (a Aliyun) Check(context.Context) error   { return nil } // nothing to ask without a session
func (a Aliyun) header() http.Header           { return http.Header{"Authorization": {"Bearer " + a.APIKey}} }

func (a Aliyun) Dial(ctx context.Context, _ *string) (asr.Stream, error) {
	switch AliyunProtocol(a.Model) {
	case AliyunInference:
		return a.dialInference(ctx)
	case AliyunRealtime:
		return a.dialRealtime(ctx)
	}
	return nil, fmt.Errorf("%w: no documented realtime protocol for model %s", ErrProtocol, a.Model)
}

// ---- the inference protocol (Fun-ASR-Realtime) ----

type inferenceStream struct {
	transcript
	outcome
	conn *websocket.Conn
	task string
}

type inferenceEvent struct {
	Header struct {
		Event        string `json:"event"`
		ErrorCode    string `json:"error_code"`
		ErrorMessage string `json:"error_message"`
	} `json:"header"`
	Payload struct {
		Output struct {
			Sentence struct {
				Text        string `json:"text"`
				Heartbeat   bool   `json:"heartbeat"`
				SentenceEnd bool   `json:"sentence_end"`
				SentenceID  int    `json:"sentence_id"`
			} `json:"sentence"`
		} `json:"output"`
	} `json:"payload"`
}

func (a Aliyun) dialInference(ctx context.Context) (asr.Stream, error) {
	conn, res, err := websocket.Dial(ctx, a.base()+"/inference", &websocket.DialOptions{HTTPHeader: a.header()})
	if err != nil {
		return nil, dialError(err, res)
	}
	conn.SetReadLimit(4 << 20)
	s := &inferenceStream{conn: conn, task: uuid()}
	s.done = make(chan struct{})
	run := map[string]any{
		"header": map[string]any{"action": "run-task", "task_id": s.task, "streaming": "duplex"},
		"payload": map[string]any{
			"task_group": "audio", "task": "asr", "function": "recognition", "model": a.Model,
			"parameters": map[string]any{"format": "pcm", "sample_rate": asr.SampleRate},
			"input":      map[string]any{},
		},
	}
	if err := writeJSON(ctx, conn, run); err != nil {
		conn.CloseNow()
		return nil, fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	// Audio may only be sent once the task has started.
	for {
		ev, err := s.read(ctx)
		if err != nil {
			conn.CloseNow()
			return nil, fmt.Errorf("%w: %w", ErrUnreachable, err)
		}
		if ev.Header.Event == "task-failed" {
			conn.CloseNow()
			return nil, &ServiceError{ev.Header.ErrorCode, ev.Header.ErrorMessage}
		}
		if ev.Header.Event == "task-started" {
			break
		}
	}
	go s.loop()
	return s, nil
}

func (s *inferenceStream) read(ctx context.Context) (inferenceEvent, error) {
	var ev inferenceEvent
	for {
		typ, data, err := s.conn.Read(ctx)
		if err != nil {
			return ev, err
		}
		if typ == websocket.MessageText {
			return ev, json.Unmarshal(data, &ev)
		}
	}
}

func (s *inferenceStream) loop() {
	defer close(s.done)
	for {
		ev, err := s.read(context.Background())
		if err != nil {
			s.fail(fmt.Errorf("%w: the connection ended early: %w", ErrUnreachable, err))
			return
		}
		switch ev.Header.Event {
		case "result-generated":
			sent := ev.Payload.Output.Sentence
			switch {
			case sent.Heartbeat: // a keep-alive, not a result
			case sent.SentenceEnd:
				s.setFinal(sent.SentenceID, sent.Text)
			default:
				s.setPending(sent.Text)
			}
		case "task-finished":
			s.finished()
			return
		case "task-failed":
			s.fail(&ServiceError{ev.Header.ErrorCode, ev.Header.ErrorMessage})
			return
		}
	}
}

func (s *inferenceStream) Send(ctx context.Context, pcm []byte) error {
	if len(pcm) == 0 {
		return nil
	}
	return s.conn.Write(ctx, websocket.MessageBinary, pcm)
}

func (s *inferenceStream) Finish(ctx context.Context) error {
	finish := map[string]any{
		"header":  map[string]any{"action": "finish-task", "task_id": s.task, "streaming": "duplex"},
		"payload": map[string]any{"input": map[string]any{}},
	}
	if err := writeJSON(ctx, s.conn, finish); err != nil {
		return err
	}
	return wait(ctx, &s.outcome)
}

func (s *inferenceStream) Close() { s.conn.Close(websocket.StatusNormalClosure, "") }

// ---- the realtime protocol (Qwen-ASR-Realtime) ----

type realtimeStream struct {
	transcript
	outcome
	conn *websocket.Conn
	n    int // completed items, in the order they came
}

type realtimeEvent struct {
	Type       string `json:"type"`
	Text       string `json:"text"`       // …transcription.text: the confirmed part
	Stash      string `json:"stash"`      // …transcription.text: the part that may change
	Transcript string `json:"transcript"` // …transcription.completed
	Error      struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (a Aliyun) dialRealtime(ctx context.Context) (asr.Stream, error) {
	target := a.base() + "/realtime?model=" + url.QueryEscape(a.Model)
	conn, res, err := websocket.Dial(ctx, target, &websocket.DialOptions{HTTPHeader: a.header()})
	if err != nil {
		return nil, dialError(err, res)
	}
	conn.SetReadLimit(4 << 20)
	s := &realtimeStream{conn: conn}
	s.done = make(chan struct{})
	// The server detects the ends of sentences (VAD mode), with the
	// values its documents recommend.
	update := map[string]any{
		"event_id": uuid(), "type": "session.update",
		"session": map[string]any{
			"modalities": []string{"text"}, "input_audio_format": "pcm", "sample_rate": asr.SampleRate,
			"turn_detection": map[string]any{"type": "server_vad", "threshold": 0.0, "silence_duration_ms": 400},
		},
	}
	if err := writeJSON(ctx, conn, update); err != nil {
		conn.CloseNow()
		return nil, fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	// session.created, then session.updated for the settings above.
	for {
		ev, err := s.read(ctx)
		if err != nil {
			conn.CloseNow()
			return nil, fmt.Errorf("%w: %w", ErrUnreachable, err)
		}
		if ev.Type == "error" {
			conn.CloseNow()
			return nil, &ServiceError{ev.Error.Code, ev.Error.Message}
		}
		if ev.Type == "session.updated" {
			break
		}
	}
	go s.loop()
	return s, nil
}

func (s *realtimeStream) read(ctx context.Context) (realtimeEvent, error) {
	var ev realtimeEvent
	for {
		typ, data, err := s.conn.Read(ctx)
		if err != nil {
			return ev, err
		}
		if typ == websocket.MessageText {
			return ev, json.Unmarshal(data, &ev)
		}
	}
}

func (s *realtimeStream) loop() {
	defer close(s.done)
	for {
		ev, err := s.read(context.Background())
		if err != nil {
			s.fail(fmt.Errorf("%w: the connection ended early: %w", ErrUnreachable, err))
			return
		}
		switch ev.Type {
		case "conversation.item.input_audio_transcription.text":
			s.setPending(ev.Text + ev.Stash)
		case "conversation.item.input_audio_transcription.completed":
			s.n++
			s.setFinal(s.n, ev.Transcript)
		case "conversation.item.input_audio_transcription.failed", "error":
			s.fail(&ServiceError{ev.Error.Code, ev.Error.Message})
			return
		case "session.finished":
			s.finished()
			return
		}
	}
}

func (s *realtimeStream) Send(ctx context.Context, pcm []byte) error {
	if len(pcm) == 0 {
		return nil
	}
	return writeJSON(ctx, s.conn, map[string]any{
		"event_id": uuid(), "type": "input_audio_buffer.append", "audio": base64.StdEncoding.EncodeToString(pcm),
	})
}

// Finish sends session.finish, as the documents require before closing:
// without it the sentence in progress would be dropped.
func (s *realtimeStream) Finish(ctx context.Context) error {
	if err := writeJSON(ctx, s.conn, map[string]any{"event_id": uuid(), "type": "session.finish"}); err != nil {
		return err
	}
	return wait(ctx, &s.outcome)
}

func (s *realtimeStream) Close() { s.conn.Close(websocket.StatusNormalClosure, "") }

func writeJSON(ctx context.Context, conn *websocket.Conn, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, data)
}

// wait waits for a session's reader to stop, or for the context.
func wait(ctx context.Context, o *outcome) error {
	select {
	case <-o.done:
		return o.Err()
	case <-ctx.Done():
		return fmt.Errorf("the provider did not finish in time: %w", ctx.Err())
	}
}
