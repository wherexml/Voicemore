// Package asr is the client of the existing local ASR service, as
// docs/asr-integration.md describes it. It sends no Origin header, and
// leaves voice detection and the rejection of silence to the service.
package asr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/coder/websocket"
)

const (
	SampleRate = 16000
	ChunkBytes = 3200  // 100 ms, as the existing client sends
	maxFrame   = 32000 // the service's limit for one frame
)

type Health struct {
	Ready               bool `json:"ready"`
	Busy                bool `json:"busy"`
	DefaultHotwordCount int  `json:"default_hotword_count"`
	// Languages are the speech languages the worker takes; empty for a
	// worker older than the setting, which only recognises Chinese.
	Languages []string `json:"languages,omitempty"`
}

// ErrLanguage: the running worker predates the speech-language setting.
var ErrLanguage = errors.New("the local ASR service does not support this speech language; restart it")

// CheckHealth asks the service whether it can take a recording.
func CheckHealth(ctx context.Context, base string) (Health, error) {
	var h Health
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/health", nil)
	if err != nil {
		return h, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return h, fmt.Errorf("the local ASR service is not reachable at %s: %w", base, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return h, fmt.Errorf("the local ASR service answered %s", res.Status)
	}
	return h, json.NewDecoder(res.Body).Decode(&h)
}

// Info is what the service's /health says about itself. The model is the
// one the service has loaded: a client cannot choose another.
type Info struct {
	Health
	Model string // empty when the service does not name it
}

// Inspect reads /health in full, for the settings' connection check. It
// starts no session and sends no audio.
func Inspect(ctx context.Context, base string) (Info, error) {
	var info Info
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return info, ErrBadURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(strings.TrimSpace(base), "/")+"/health", nil)
	if err != nil {
		return info, ErrBadURL
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return info, ctx.Err()
		}
		return info, fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return info, fmt.Errorf("%w: %s", ErrNotASR, res.Status)
	}
	var raw map[string]any
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&raw); err != nil {
		return info, ErrNotASR
	}
	ready, ok := raw["ready"].(bool)
	if !ok {
		// Another kind of server answered: this is not the ASR service.
		return info, ErrNotASR
	}
	info.Ready = ready
	info.Busy, _ = raw["busy"].(bool)
	if n, ok := raw["default_hotword_count"].(float64); ok {
		info.DefaultHotwordCount = int(n)
	}
	info.Model = modelName(raw)
	return info, nil
}

var (
	ErrBadURL      = errors.New("the ASR URL is not a valid http(s) address")
	ErrUnreachable = errors.New("the ASR service is not reachable")
	ErrNotASR      = errors.New("the address did not answer as the ASR service does")
)

// modelName finds the model's name in the health document, whose model
// metadata is a string or an object depending on the service's version.
func modelName(raw map[string]any) string {
	for _, k := range []string{"model", "model_id", "model_name", "asr_model"} {
		switch v := raw[k].(type) {
		case string:
			if v != "" {
				return v
			}
		case map[string]any:
			for _, kk := range []string{"name", "id", "model", "path", "repo"} {
				if s, ok := v[kk].(string); ok && s != "" {
					return s[strings.LastIndex(s, "/")+1:]
				}
			}
		}
	}
	return ""
}

// Message is one message of the service.
type Message struct {
	Type         string  `json:"type"`
	Segment      int     `json:"segment"`
	Text         string  `json:"text"`
	AudioS       float64 `json:"audio_s"`
	DecodeS      float64 `json:"decode_s"`
	ElapsedS     float64 `json:"elapsed_s"`
	Message      string  `json:"message"`
	SampleRate   int     `json:"sample_rate"`
	HotwordCount int     `json:"hotword_count"`
}

// Transcript gathers the segments of a session: a partial replaces the
// earlier partial of its segment, and a final replaces its partials.
type Transcript struct {
	mu       sync.Mutex
	finals   map[int]string
	partials map[int]string
}

func (t *Transcript) add(m Message) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.finals == nil {
		t.finals, t.partials = map[int]string{}, map[int]string{}
	}
	if m.Type == "final" {
		t.finals[m.Segment] = m.Text
		delete(t.partials, m.Segment)
	} else if _, done := t.finals[m.Segment]; !done {
		t.partials[m.Segment] = m.Text
	}
}

// Final is the final segments in order, each once.
func (t *Transcript) Final() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return concat(t.finals)
}

// Pending is the text of the segments not final yet, which may still change.
func (t *Transcript) Pending() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return concat(t.partials)
}

func concat(segments map[int]string) string {
	keys := make([]int, 0, len(segments))
	for k := range segments {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var b strings.Builder
	for _, k := range keys {
		s := segments[k]
		if s == "" {
			continue
		}
		// Segments are concatenated as they are; only two Latin words
		// meeting at the seam get a space between them.
		if prev := []rune(b.String()); len(prev) > 0 {
			if a, z := prev[len(prev)-1], []rune(s)[0]; a < unicode.MaxASCII && z < unicode.MaxASCII &&
				(unicode.IsLetter(a) || unicode.IsDigit(a)) && (unicode.IsLetter(z) || unicode.IsDigit(z)) {
				b.WriteByte(' ')
			}
		}
		b.WriteString(s)
	}
	return b.String()
}

// Session is one recording session of the service.
type Session struct {
	conn         *websocket.Conn
	HotwordCount int
	Transcript   Transcript

	// OnUpdate is called after every partial and final message.
	OnUpdate func()

	done   chan struct{} // closed when the reader ends
	mu     sync.Mutex
	err    error
	isDone bool // the service sent "done"
	carry  []byte
}

// Dial opens a session and waits until the service is ready for audio.
// hotwords nil leaves the service's own default terms in use; a value,
// even an empty one, replaces them.
func Dial(ctx context.Context, base string, hotwords *string) (*Session, error) {
	return DialLanguage(ctx, base, hotwords, "")
}

// DialLanguage is Dial with the worker's speech language ("zh", "auto" or
// "en"). Empty sends none, and the worker keeps its Chinese default.
func DialLanguage(ctx context.Context, base string, hotwords *string, language string) (*Session, error) {
	u, err := url.Parse(strings.TrimRight(base, "/") + "/ws")
	if err != nil {
		return nil, err
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	}
	q := url.Values{}
	if hotwords != nil {
		q.Set("hotwords", *hotwords)
	}
	if language != "" {
		q.Set("language", language)
	}
	u.RawQuery = q.Encode()
	conn, _, err := websocket.Dial(ctx, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("could not connect to the local ASR service: %w", err)
	}
	conn.SetReadLimit(4 << 20)
	s := &Session{conn: conn, done: make(chan struct{})}
	for {
		m, err := s.read(ctx)
		if err != nil {
			conn.CloseNow()
			return nil, fmt.Errorf("the local ASR service did not become ready: %w", err)
		}
		if m.Type == "error" {
			conn.CloseNow()
			return nil, fmt.Errorf("the local ASR service refused the session: %s", m.Message)
		}
		if m.Type == "ready" {
			if m.SampleRate != 0 && m.SampleRate != SampleRate {
				conn.CloseNow()
				return nil, fmt.Errorf("the ASR expects %d Hz audio; this app records %d Hz", m.SampleRate, SampleRate)
			}
			s.HotwordCount = m.HotwordCount
			break
		}
	}
	go s.loop()
	return s, nil
}

func (s *Session) read(ctx context.Context) (Message, error) {
	var m Message
	for {
		typ, b, err := s.conn.Read(ctx)
		if err != nil {
			return m, err
		}
		if typ != websocket.MessageText {
			continue
		}
		return m, json.Unmarshal(b, &m)
	}
}

func (s *Session) loop() {
	defer close(s.done)
	for {
		m, err := s.read(context.Background())
		if err != nil {
			s.mu.Lock()
			if !s.isDone && s.err == nil {
				s.err = fmt.Errorf("the connection to the ASR ended early: %w", err)
			}
			s.mu.Unlock()
			return
		}
		switch m.Type {
		case "partial", "final":
			s.Transcript.add(m)
			if s.OnUpdate != nil {
				s.OnUpdate()
			}
		case "error":
			s.mu.Lock()
			s.err = errors.New("ASR error: " + m.Message)
			s.mu.Unlock()
			return
		case "done":
			s.mu.Lock()
			s.isDone = true
			s.mu.Unlock()
			return
		}
	}
}

// Send sends PCM16 mono 16 kHz audio in even frames of at most 100 ms. An
// odd trailing byte waits for the next call.
func (s *Session) Send(ctx context.Context, pcm []byte) error {
	s.carry = append(s.carry, pcm...)
	for len(s.carry) >= ChunkBytes {
		if err := s.conn.Write(ctx, websocket.MessageBinary, s.carry[:ChunkBytes]); err != nil {
			return err
		}
		s.carry = s.carry[ChunkBytes:]
	}
	return nil
}

// Finish flushes the audio, asks the service to stop and waits for it to
// finalize every segment. The transcript keeps what was received even when
// it fails.
func (s *Session) Finish(ctx context.Context) error {
	if n := len(s.carry) &^ 1; n > 0 && n <= maxFrame {
		if err := s.conn.Write(ctx, websocket.MessageBinary, s.carry[:n]); err != nil {
			return err
		}
	}
	s.carry = nil
	if err := s.conn.Write(ctx, websocket.MessageText, []byte(`{"type":"stop"}`)); err != nil {
		return err
	}
	select {
	case <-s.done:
	case <-ctx.Done():
		return fmt.Errorf("the ASR did not finish in time: %w", ctx.Err())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Err is the error that ended the session early, if one did.
func (s *Session) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Close ends the session. Closing before Finish returns cancels the
// delivery of the remaining results, which is what a cancel wants.
func (s *Session) Close() {
	s.conn.Close(websocket.StatusNormalClosure, "")
}

// TranscribePCM runs recorded audio through a new session, for the
// history's retranscription. It sends faster than real time, pausing
// briefly between chunks.
func TranscribePCM(ctx context.Context, base string, hotwords *string, pcm []byte) (string, error) {
	s, err := Dial(ctx, base, hotwords)
	if err != nil {
		return "", err
	}
	defer s.Close()
	for len(pcm) > 0 {
		n := min(len(pcm), ChunkBytes)
		if err := s.Send(ctx, pcm[:n]); err != nil {
			return s.Transcript.Final(), err
		}
		pcm = pcm[n:]
		select {
		case <-ctx.Done():
			return s.Transcript.Final(), ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	err = s.Finish(ctx)
	return s.Transcript.Final(), err
}

// Stream is a recognition session in progress, whatever service it is on:
// the local one above, or a cloud provider's (internal/cloudasr). Audio is
// PCM16 mono 16 kHz. Final text no longer changes; pending text may.
type Stream interface {
	Send(ctx context.Context, pcm []byte) error
	// Finish says the audio is complete and waits for the last results.
	Finish(ctx context.Context) error
	// Err is the error that ended the session early, if one did.
	Err() error
	// Close ends the session; before Finish returns, it cancels it.
	Close()
	Final() string
	Pending() string
	// SetOnUpdate sets what is called after every change of the text.
	SetOnUpdate(fn func())
}

// Dialer opens sessions on one service.
type Dialer interface {
	// Label names the service in messages: an address, never a credential.
	Label() string
	// Check says whether the service can take a recording now, where
	// that can be asked without starting one.
	Check(ctx context.Context) error
	// Dial opens a session and returns once the service takes audio.
	// hotwords is nil where the service has no such list.
	Dial(ctx context.Context, hotwords *string) (Stream, error)
	// Hotwords says the service takes this app's list of terms.
	Hotwords() bool
}

// ErrBusy: the service is serving another recording.
var ErrBusy = errors.New("the speech recognition service is busy with another recording")

func (s *Session) Final() string         { return s.Transcript.Final() }
func (s *Session) Pending() string       { return s.Transcript.Pending() }
func (s *Session) SetOnUpdate(fn func()) { s.OnUpdate = fn }

type localDialer struct{ base, language string }

// Local is the dialer of a service speaking this app's own protocol.
func Local(base string) Dialer { return localDialer{base: strings.TrimSpace(base)} }

// LocalLanguage is Local with the speech language of the app-managed
// worker. A language other than Chinese is refused by a worker that does
// not list it, rather than silently recognised as Chinese.
func LocalLanguage(base, language string) Dialer {
	if language == "zh" {
		language = ""
	}
	return localDialer{base: strings.TrimSpace(base), language: language}
}

func (d localDialer) Label() string  { return d.base }
func (d localDialer) Hotwords() bool { return true }
func (d localDialer) Check(ctx context.Context) error {
	h, err := CheckHealth(ctx, d.base)
	if err != nil {
		return err
	}
	if h.Busy {
		return ErrBusy
	}
	if d.language != "" && !slices.Contains(h.Languages, d.language) {
		return ErrLanguage
	}
	return nil
}
func (d localDialer) Dial(ctx context.Context, hotwords *string) (Stream, error) {
	if d.language != "" {
		// Retranscription dials without Check: ask here too.
		if h, err := CheckHealth(ctx, d.base); err == nil && !slices.Contains(h.Languages, d.language) {
			return nil, ErrLanguage
		}
	}
	s, err := DialLanguage(ctx, d.base, hotwords, d.language)
	if err != nil {
		return nil, err // a nil *Session must not become a non-nil Stream
	}
	return s, nil
}

// Transcribe runs recorded audio through a new session of a service, for
// the history's retranscription. It sends faster than real time, pausing
// briefly between chunks.
func Transcribe(ctx context.Context, d Dialer, hotwords *string, pcm []byte) (string, error) {
	raw, _, err := TranscribeResult(ctx, d, hotwords, pcm)
	return raw, err
}

// TranscribeResult also preserves an integrated service's paid result.
func TranscribeResult(ctx context.Context, d Dialer, hotwords *string, pcm []byte) (string, *ProcessedResult, error) {
	s, err := d.Dial(ctx, hotwords)
	if err != nil {
		return "", nil, err
	}
	defer s.Close()
	for len(pcm) > 0 {
		n := min(len(pcm), ChunkBytes)
		if err := s.Send(ctx, pcm[:n]); err != nil {
			return s.Final(), nil, err
		}
		pcm = pcm[n:]
		select {
		case <-ctx.Done():
			return s.Final(), nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	err = s.Finish(ctx)
	if p, ok := s.(ProcessedStream); ok && err == nil {
		return s.Final(), p.Processed(), nil
	}
	return s.Final(), nil, err
}
