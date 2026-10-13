// Package cloudasr has the speech recognition adapters of the cloud
// providers: Alibaba Cloud Model Studio and Volcengine. Each speaks its
// provider's own protocol, as the official documents describe it (their
// addresses are in docs/implementation.md), and presents it as an
// asr.Stream, so that a dictation runs the same whatever recognizes it.
//
// Audio is what the microphone captures: PCM16, mono, 16 kHz. Credentials
// are passed in by the caller, used in the handshake and kept nowhere; no
// error or label of this package contains one.
//
// These adapters were written from the documents without credentials to
// run them against the services. Until that is done on the user's
// accounts they are unverified, however carefully they follow the text.
package cloudasr

import (
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
)

var (
	// ErrAuth: the service refused the credentials in the handshake.
	ErrAuth = errors.New("the provider refused the credentials")
	// ErrUnreachable wraps a failure to connect at all.
	ErrUnreachable = errors.New("the provider cannot be reached")
	// ErrProtocol: the service answered with something the adapter does
	// not understand.
	ErrProtocol = errors.New("the provider answered in an unexpected way")
)

// ServiceError is an error the provider reported, in its own words.
type ServiceError struct{ Code, Message string }

func (e *ServiceError) Error() string { return "the provider reported " + e.Code + ": " + e.Message }

// dialError turns a failed WebSocket handshake into one of the errors
// above. res is the HTTP response, when there was one.
func dialError(err error, res *http.Response) error {
	if res != nil {
		switch res.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return fmt.Errorf("%w (%s)", ErrAuth, res.Status)
		}
		return fmt.Errorf("%w: %s", ErrUnreachable, res.Status)
	}
	return fmt.Errorf("%w: %w", ErrUnreachable, err)
}

// uuid is a random UUID (version 4), for task and connection IDs.
func uuid() string {
	var b [16]byte
	rand.Read(b[:])
	b[6], b[8] = b[6]&0x0f|0x40, b[8]&0x3f|0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// transcript gathers a session's sentences: those that are final, by
// their number, and the text that may still change.
type transcript struct {
	mu       sync.Mutex
	finals   map[int]string
	pending  string
	onUpdate func()
}

func (t *transcript) setFinal(n int, text string) {
	t.mu.Lock()
	if t.finals == nil {
		t.finals = map[int]string{}
	}
	t.finals[n] = text
	t.pending = ""
	fn := t.onUpdate
	t.mu.Unlock()
	if fn != nil {
		fn()
	}
}

func (t *transcript) setPending(text string) {
	t.mu.Lock()
	t.pending = text
	fn := t.onUpdate
	t.mu.Unlock()
	if fn != nil {
		fn()
	}
}

// replace sets the whole transcript at once, for services that send it
// whole each time.
func (t *transcript) replace(final, pending string) {
	t.mu.Lock()
	t.finals, t.pending = map[int]string{0: final}, pending
	fn := t.onUpdate
	t.mu.Unlock()
	if fn != nil {
		fn()
	}
}

func (t *transcript) Final() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	keys := make([]int, 0, len(t.finals))
	for k := range t.finals {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(t.finals[k])
	}
	return b.String()
}

func (t *transcript) Pending() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.pending
}

func (t *transcript) SetOnUpdate(fn func()) {
	t.mu.Lock()
	t.onUpdate = fn
	t.mu.Unlock()
}

// outcome is how a session ended: done is closed when its reader stops,
// with err set if it stopped on an error.
type outcome struct {
	done chan struct{}
	mu   sync.Mutex
	err  error
	ok   bool // the service said it had finished
}

func (o *outcome) fail(err error) {
	o.mu.Lock()
	if o.err == nil && !o.ok {
		o.err = err
	}
	o.mu.Unlock()
}

func (o *outcome) finished() {
	o.mu.Lock()
	o.ok = true
	o.mu.Unlock()
}

func (o *outcome) Err() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.err
}
