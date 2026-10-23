package cloudasr

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"voiceinput/internal/asr"
	"voiceinput/internal/textstyle"
)

const Sub2APIEndpoint = "https://47.94.244.74:8443"
const Sub2APIModel = "doubao-asr-2.0-cleanup"

// Sub2API runs recognition and cleanup as one idempotent, paid task.
// Never retry audio automatically: only retrieve the original task's result.
type Sub2API struct {
	Base, APIKey, TaskID string
	TextOptions          *VoiceTextOptions
}
type VoiceTextOptions struct {
	Version             int                    `json:"version"`
	Format              textstyle.Options      `json:"format"`
	PolishRules         string                 `json:"polish_rules"`
	Hotwords            []string               `json:"hotwords,omitempty"`
	Corrections         []textstyle.Correction `json:"corrections,omitempty"`
	TranslationLanguage string                 `json:"translation_language,omitempty"`
}

// WithTranslation freezes the target in this task's options and digest.
func (v Sub2API) WithTranslation(language string) asr.Dialer {
	o := VoiceTextOptions{Version: 1}
	if v.TextOptions != nil {
		o = *v.TextOptions
	}
	o.TranslationLanguage = language
	v.TextOptions = &o
	return v
}

func (v Sub2API) translationLanguage() string {
	if v.TextOptions == nil {
		return ""
	}
	return v.TextOptions.TranslationLanguage
}

func (o *VoiceTextOptions) digest() string {
	b, _ := json.Marshal(o)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func (v Sub2API) base() (string, error) {
	b := strings.TrimRight(strings.TrimSpace(v.Base), "/")
	if b == "" {
		b = Sub2APIEndpoint
	}
	u, e := url.Parse(b)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost"))) {
		return "", errors.New("sub2api requires an HTTPS origin without credentials, path or query")
	}
	return b, nil
}
func (v Sub2API) Label() string                   { b, _ := v.base(); return b }
func (v Sub2API) Hotwords() bool                  { return true }
func (v Sub2API) Check(context.Context) error     { _, e := v.base(); return e }
func (v Sub2API) WithTaskID(id string) asr.Dialer { v.TaskID = id; return v }

type taskError struct{ Code, ID string }

func (e *taskError) Error() string { return fmt.Sprintf("sub2api %s (task %s)", e.Code, e.ID) }

type voiceEvent struct {
	TextOptionsDigest string               `json:"text_options_digest"`
	Type              string               `json:"type"`
	Text              string               `json:"text"`
	Result            *asr.ProcessedResult `json:"result"`
	Error             struct {
		Code string `json:"code"`
	} `json:"error"`
}

// WithCorrections freezes confirmed rules for this paid task.
func (v Sub2API) WithCorrections(rules []textstyle.Correction) asr.Dialer {
	o := VoiceTextOptions{Version: 1}
	if v.TextOptions != nil {
		o = *v.TextOptions
	}
	o.Hotwords = append([]string(nil), o.Hotwords...)
	o.Corrections = make([]textstyle.Correction, len(rules))
	for i, r := range rules {
		o.Corrections[i] = r
		o.Corrections[i].Context = append([]string(nil), r.Context...)
	}
	v.TextOptions = &o
	return v
}

func (v Sub2API) Dial(ctx context.Context, words *string) (asr.Stream, error) {
	switch v.translationLanguage() {
	case "", "en", "hans", "hant", "ja", "ko", "fr", "de", "es":
	default:
		return nil, errors.New("unsupported translation language")
	}
	o := VoiceTextOptions{Version: 1}
	if v.TextOptions != nil {
		o = *v.TextOptions
	}
	o.Hotwords = append([]string(nil), o.Hotwords...)
	if words != nil {
		o.Hotwords = nil
		for _, w := range strings.Split(*words, ",") {
			if w = strings.TrimSpace(w); w != "" {
				o.Hotwords = append(o.Hotwords, w)
			}
		}
	}
	v.TextOptions = &o

	b, e := v.base()
	if e != nil {
		return nil, e
	}
	if v.TaskID == "" {
		v.TaskID = uuid()
	}
	h := http.Header{"Authorization": {"Bearer " + v.APIKey}, "Idempotency-Key": {v.TaskID}}
	ws := "wss" + strings.TrimPrefix(b, "https") + "/v1/voice/stream"
	if strings.HasPrefix(b, "http:") {
		ws = "ws" + strings.TrimPrefix(b, "http") + "/v1/voice/stream"
	}
	c, res, e := websocket.Dial(ctx, ws, &websocket.DialOptions{HTTPHeader: h})
	if e != nil {
		return nil, dialError(e, res)
	}
	c.SetReadLimit(2 << 20)
	s := &sub2Stream{conn: c, api: v, done: make(chan struct{})}
	start := map[string]any{"type": "start", "format": "pcm_s16le", "rate": 16000, "channels": 1}
	if v.TextOptions != nil {
		start["text_options"] = v.TextOptions
	}
	payload, _ := json.Marshal(start)
	if e = c.Write(ctx, websocket.MessageText, payload); e == nil {
		var m voiceEvent
		m, e = s.read(ctx)
		if e == nil && m.Type == "error" {
			e = &taskError{m.Error.Code, v.TaskID}
		}
		if e == nil && v.TextOptions != nil && m.TextOptionsDigest != v.TextOptions.digest() {
			e = errors.New("sub2api 服务尚未支持当前文字设置，请更新服务端")
		}
		if e == nil && m.Type != "accepted" {
			e = ErrProtocol
		}
	}
	if e != nil {
		c.CloseNow()
		return nil, e
	}
	go s.loop()
	return s, nil
}

type sub2Stream struct {
	transcript
	conn       *websocket.Conn
	api        Sub2API
	done       chan struct{}
	mu         sync.Mutex
	err        error
	result     *asr.ProcessedResult
	carry      []byte // Send and Finish have one writer, as required by asr.Stream.
	total      int
	lastSend   time.Time
	asrFinalAt time.Time
}

func (s *sub2Stream) read(ctx context.Context) (voiceEvent, error) {
	var m voiceEvent
	t, b, e := s.conn.Read(ctx)
	if e == nil && t != websocket.MessageText {
		e = ErrProtocol
	}
	if e == nil {
		e = json.Unmarshal(b, &m)
	}
	return m, e
}
func (s *sub2Stream) loop() {
	defer close(s.done)
	for {
		m, e := s.read(context.Background())
		if e != nil {
			s.mu.Lock()
			s.err = e
			s.mu.Unlock()
			return
		}
		switch m.Type {
		case "asr.partial":
			s.replace("", m.Text)
		case "asr.final":
			s.mu.Lock()
			s.asrFinalAt = time.Now()
			s.mu.Unlock()
			s.replace(m.Text, "")
		case "cleanup.started": // Final remains the original ASR, never the cleaned text.
		case "completed":
			e = s.complete(m.Result)
			s.mu.Lock()
			s.err = e
			s.mu.Unlock()
			return
		case "error":
			s.mu.Lock()
			s.err = &taskError{m.Error.Code, s.api.TaskID}
			s.mu.Unlock()
			return
		default:
			s.mu.Lock()
			s.err = ErrProtocol
			s.mu.Unlock()
			return
		}
	}
}
func (s *sub2Stream) complete(r *asr.ProcessedResult) error {
	if r == nil || r.ID != s.api.TaskID || r.Model != Sub2APIModel || strings.TrimSpace(r.Text) == "" || r.UsageLogID <= 0 {
		return ErrProtocol
	}
	if r.TranslationLanguage != s.api.translationLanguage() {
		return ErrProtocol
	}
	s.mu.Lock()
	r.ASRFinalAt = s.asrFinalAt
	s.result = r
	s.err = nil
	s.mu.Unlock()
	s.replace(r.RawText, "")
	return nil
}
func (s *sub2Stream) Processed() *asr.ProcessedResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.result == nil {
		return nil
	}
	r := *s.result
	return &r
}
func (s *sub2Stream) Err() error { s.mu.Lock(); defer s.mu.Unlock(); return s.err }
func (s *sub2Stream) send(ctx context.Context, b []byte) error {
	// Live capture already supplies 200 ms packets at recording speed.
	// Pacing that stream at 1x again preserves all audio queued during Dial
	// until after Stop. Let ready packets catch up, with a 100 ms safety
	// floor even after a scheduler stall. Never drop or rewrite PCM.
	if !s.lastSend.IsZero() {
		if wait := time.Until(s.lastSend.Add(100 * time.Millisecond)); wait > 0 {
			t := time.NewTimer(wait)
			defer t.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-t.C:
			}
		}
	}
	if e := s.conn.Write(ctx, websocket.MessageBinary, b); e != nil {
		return e
	}
	s.lastSend = time.Now()
	return nil
}
func (s *sub2Stream) Send(ctx context.Context, pcm []byte) error {
	s.total += len(pcm)
	if s.total > 180*32000 {
		return errors.New("sub2api recordings must be at most 180 seconds")
	}
	s.carry = append(s.carry, pcm...)
	for len(s.carry) >= 6400 {
		if e := s.send(ctx, s.carry[:6400]); e != nil {
			return e
		}
		s.carry = s.carry[6400:]
	}
	return nil
}
func (s *sub2Stream) Finish(ctx context.Context) error {
	if len(s.carry)%2 != 0 {
		return errors.New("sub2api requires complete PCM16 samples")
	}
	var e error
	if len(s.carry) > 0 {
		e = s.send(ctx, s.carry)
		s.carry = nil
	}
	if e == nil {
		e = s.conn.Write(ctx, websocket.MessageText, []byte(`{"type":"finish"}`))
	}
	if e == nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.done:
			e = s.Err()
		}
	}
	if e == nil {
		return nil
	}
	var terminal *taskError
	if ctx.Err() != nil || (errors.As(e, &terminal) && terminal.Code != "BILLING_STATUS_UNKNOWN") {
		return e
	}
	// A response can disappear after the transaction commits. GET is safe
	// even with an exhausted balance; resending audio with a new ID is not.
	r, recoverErr := s.api.Result(ctx, s.api.TaskID)
	if recoverErr != nil {
		return fmt.Errorf("%w; retrieve original task %s: %v", ErrUnreachable, s.api.TaskID, recoverErr)
	}
	return s.complete(r)
}
func (s *sub2Stream) Close() { s.conn.CloseNow() }

// Result retrieves a paid result or terminal failure. It never creates a task.
func (v Sub2API) Result(ctx context.Context, id string) (*asr.ProcessedResult, error) {
	b, e := v.base()
	if e != nil {
		return nil, e
	}
	for {
		req, e := http.NewRequestWithContext(ctx, "GET", b+"/v1/voice/tasks/"+url.PathEscape(id), nil)
		if e != nil {
			return nil, e
		}
		req.Header.Set("Authorization", "Bearer "+v.APIKey)
		res, e := http.DefaultClient.Do(req)
		if e != nil {
			return nil, fmt.Errorf("%w: task %s", ErrUnreachable, id)
		}
		body, e := io.ReadAll(io.LimitReader(res.Body, 2<<20))
		res.Body.Close()
		if e != nil {
			return nil, e
		}
		if res.StatusCode == 200 {
			var r asr.ProcessedResult
			if e := json.Unmarshal(body, &r); e != nil {
				return nil, ErrProtocol
			}
			if r.ID != id || r.Model != Sub2APIModel || strings.TrimSpace(r.Text) == "" || r.UsageLogID <= 0 {
				return nil, ErrProtocol
			}
			if r.TranslationLanguage != v.translationLanguage() {
				return nil, ErrProtocol
			}
			return &r, nil
		}
		var m voiceEvent
		if json.Unmarshal(body, &m) != nil || m.Error.Code == "" {
			return nil, fmt.Errorf("sub2api HTTP %d (task %s)", res.StatusCode, id)
		}
		if m.Error.Code != "TASK_IN_PROGRESS" && m.Error.Code != "BILLING_STATUS_UNKNOWN" {
			return nil, &taskError{m.Error.Code, id}
		}
		t := time.NewTimer(500 * time.Millisecond)
		select {
		case <-ctx.Done():
			t.Stop()
			return nil, ctx.Err()
		case <-t.C:
		}
	}
}
