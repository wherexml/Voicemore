package subtitles

import "sync"

// Gate says whether microphone dictation in this app is busy. Optional
// subtitle work (Qwen correction and its retranslation) yields to it; the
// capture, sherpa and live translation keep running. The app sets it from
// its dictation phase; subtitles only read it. A nil Gate is never busy.
type Gate struct {
	mu   sync.Mutex
	busy bool
	flip chan struct{} // closed at the next change
}

func NewGate() *Gate { return &Gate{flip: make(chan struct{})} }

// Set records the state and reports whether it changed.
func (g *Gate) Set(busy bool) bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.busy == busy {
		return false
	}
	g.busy = busy
	close(g.flip)
	g.flip = make(chan struct{})
	return true
}

// State is whether dictation is busy now, and a channel closed at the next
// change (nil, never closed, for a nil Gate).
func (g *Gate) State() (bool, <-chan struct{}) {
	if g == nil {
		return false, nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.busy, g.flip
}
