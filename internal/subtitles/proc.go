package subtitles

import (
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// proc is an owned child process in a process group of its own. Exactly
// one goroutine calls wait; stop may be called any number of times, from
// any goroutine, and returns once the child is gone or the bound passed.
type proc struct {
	cmd    *exec.Cmd
	once   sync.Once
	waited chan struct{}
	err    error
}

func newProc(cmd *exec.Cmd) *proc {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return &proc{cmd: cmd, waited: make(chan struct{})}
}

// wait reaps the child; its result is p.err once p.waited is closed.
func (p *proc) wait() error {
	p.err = p.cmd.Wait()
	close(p.waited)
	return p.err
}

func (p *proc) exited() bool {
	select {
	case <-p.waited:
		return true
	default:
		return false
	}
}

// stop ends the group: SIGTERM, and SIGCONT so that a stopped child can
// take it, then SIGKILL after grace. A child that already exited gets no
// signal, so a reused PID is never hit.
func (p *proc) stop(grace time.Duration) {
	p.once.Do(func() {
		if p.cmd.Process == nil || p.exited() {
			return
		}
		pid := p.cmd.Process.Pid
		syscall.Kill(-pid, syscall.SIGTERM)
		syscall.Kill(-pid, syscall.SIGCONT)
		select {
		case <-p.waited:
			return
		case <-time.After(grace):
		}
		syscall.Kill(-pid, syscall.SIGKILL)
		select {
		case <-p.waited:
		case <-time.After(2 * time.Second):
		}
	})
}
