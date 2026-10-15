package localasr

import (
	"context"
	"errors"
	"sync"
	"time"
)

// SetIdleTimeout changes the quiet period; zero disables automatic unloading.
func (m *Manager) SetIdleTimeout(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.idleTimeout = d
	m.armIdleLocked()
}

func (m *Manager) disarmIdleLocked() {
	m.idleGeneration++
	if m.idleTimer != nil {
		m.idleTimer.Stop()
		m.idleTimer = nil
	}
}

func (m *Manager) armIdleLocked() {
	m.disarmIdleLocked()
	if m.users != 0 || m.closed || m.stopping || m.state.Phase != "ready" || m.idleTimeout <= 0 {
		return
	}
	generation := m.idleGeneration
	m.idleTimer = time.AfterFunc(m.idleTimeout, func() {
		m.mu.Lock()
		if generation != m.idleGeneration || m.users != 0 || m.closed || m.stopping || m.state.Phase != "ready" {
			m.mu.Unlock()
			return
		}
		// Reserve shutdown under the same lock as Acquire, so a new session
		// either owns a live worker or waits for shutdown before restarting.
		m.stopping = true
		m.disarmIdleLocked()
		m.mu.Unlock()
		m.finishStop()
	})
}

// Acquire keeps the worker alive through a complete recognition session.
// The caller starts capturing audio first and releases after closing its stream.
func (m *Manager) Acquire(ctx context.Context) (func(), error) {
	ctx, cancel := context.WithTimeout(ctx, 150*time.Second)
	defer cancel()
	m.mu.Lock()
	m.users++
	m.disarmIdleLocked()
	m.mu.Unlock()
	var once sync.Once
	release := func() {
		once.Do(func() {
			m.mu.Lock()
			defer m.mu.Unlock()
			m.users--
			m.armIdleLocked()
		})
	}
	fail := func(err error) (func(), error) { release(); return nil, err }
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		m.mu.Lock()
		s, closed, stopping := m.state, m.closed, m.stopping
		m.mu.Unlock()
		if closed {
			return fail(errors.New("本地语音服务已关闭"))
		}
		if !s.Installed {
			return fail(errors.New("请先下载或复制本地语音模型"))
		}
		if !stopping {
			switch s.Phase {
			case "ready":
				return release, nil
			case "stopped":
				m.Start()
			case "error":
				return fail(errors.New(s.Detail))
			}
		}
		select {
		case <-ctx.Done():
			return fail(ctx.Err())
		case <-ticker.C:
		}
	}
}
