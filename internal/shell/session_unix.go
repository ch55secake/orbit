//go:build linux || darwin

package shell

import (
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

type input struct {
	key   uv.KeyPressEvent
	paste *string
}

// Session confines a real PTY to a virtual screen. Neither terminal output nor
// input is logged, saved, or passed through as host-terminal escape sequences.
type Session struct {
	terminal  *vt.SafeEmulator
	inputPipe io.Closer
	pty       *os.File
	command   *exec.Cmd
	events    chan struct{}
	inputs    chan input
	stop      chan struct{}
	once      sync.Once
	mu        sync.Mutex
	// screenMu serializes PTY output with scrollback rendering and resizing.
	screenMu sync.RWMutex
	exited   bool
	reaped   bool
	err      error
	scroll   int
	visible  atomic.Bool
}

func Start(command *exec.Cmd, width, height int) (*Session, error) {
	terminal := vt.NewSafeEmulator(width, height)
	terminal.SetScrollbackSize(vt.DefaultScrollbackSize)
	s := &Session{terminal: terminal, command: command, events: make(chan struct{}, 1), inputs: make(chan input, 128), stop: make(chan struct{})}
	// Close the pipe rather than Emulator.Close: its closed flag is not
	// synchronized with concurrent Read, even in SafeEmulator.
	s.inputPipe = terminal.InputPipe().(io.Closer)
	s.visible.Store(true)
	terminal.SetCallbacks(vt.Callbacks{CursorVisibility: func(visible bool) { s.visible.Store(visible) }})
	command.Env = append(command.Environ(), "TERM=xterm-256color", "SSH_ASKPASS_REQUIRE=never")
	tty, err := pty.StartWithSize(command, &pty.Winsize{Cols: uint16(width), Rows: uint16(height)})
	if err != nil {
		_ = s.inputPipe.Close()
		return nil, err
	}
	s.pty = tty
	go func() { _, _ = io.Copy(tty, terminal) }()
	go func() {
		for {
			select {
			case in := <-s.inputs:
				if in.paste != nil {
					terminal.Paste(*in.paste)
				} else {
					sendKey(terminal, in.key)
				}
			case <-s.stop:
				return
			}
		}
	}()
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		buffer := make([]byte, 8192)
		for {
			n, err := tty.Read(buffer)
			if n > 0 {
				s.screenMu.Lock()
				oldScrollback := terminal.ScrollbackLen()
				_, _ = terminal.Write(buffer[:n])
				newScrollback := terminal.ScrollbackLen()
				if terminal.IsAltScreen() {
					s.scroll = 0
				} else if s.scroll > 0 && newScrollback > oldScrollback {
					s.scroll = min(s.scroll+newScrollback-oldScrollback, newScrollback)
				}
				s.screenMu.Unlock()
				s.notify()
			}
			if err != nil {
				return
			}
		}
	}()
	go func() {
		err := command.Wait()
		s.mu.Lock()
		s.reaped = true
		s.mu.Unlock()
		// Let the PTY reader consume final output before closing descriptors.
		select {
		case <-readDone:
		case <-time.After(250 * time.Millisecond):
		}
		_ = tty.Close()
		_ = s.inputPipe.Close()
		s.mu.Lock()
		s.exited = true
		s.err = err
		s.mu.Unlock()
		s.Close()
		s.notify()
	}()
	return s, nil
}

func (s *Session) notify() {
	select {
	case s.events <- struct{}{}:
	default:
	}
}
func (s *Session) Events() <-chan struct{} { return s.events }

// Render returns the visible terminal viewport, including local scrollback when selected.
func (s *Session) Render() string {
	s.screenMu.RLock()
	defer s.screenMu.RUnlock()
	if s.scroll == 0 || s.terminal.IsAltScreen() {
		return s.terminal.Render()
	}

	history := s.terminal.Scrollback()
	if history == nil || history.Len() == 0 {
		return s.terminal.Render()
	}
	current := strings.Split(s.terminal.Render(), "\n")
	if len(current) == 0 {
		return ""
	}
	end := history.Len() + len(current) - min(s.scroll, history.Len())
	start := max(end-len(current), 0)
	lines := make([]string, 0, len(current))
	for row := start; row < end; row++ {
		if row < history.Len() {
			lines = append(lines, history.Line(row).Render())
		} else {
			lines = append(lines, current[row-history.Len()])
		}
	}
	return strings.Join(lines, "\n")
}
func (s *Session) Cursor() (uv.Position, bool)       { return s.terminal.CursorPosition(), s.visible.Load() }
func (s *Session) Result() (bool, error)             { s.mu.Lock(); defer s.mu.Unlock(); return s.exited, s.err }
func (s *Session) SendKey(key uv.KeyPressEvent) bool { return s.enqueue(input{key: key}) }
func (s *Session) Paste(text string) bool            { return s.enqueue(input{paste: &text}) }

// Scroll moves the local viewport; positive values reveal older output.
func (s *Session) Scroll(lines int) int {
	s.screenMu.Lock()
	defer s.screenMu.Unlock()
	if s.terminal.IsAltScreen() {
		s.scroll = 0
		return 0
	}
	s.scroll = min(max(s.scroll+lines, 0), s.terminal.ScrollbackLen())
	return s.scroll
}

// ScrollOffset reports how many lines above the live screen the viewport is.
func (s *Session) ScrollOffset() int {
	s.screenMu.RLock()
	defer s.screenMu.RUnlock()
	return s.scroll
}

// ScrollbackLen reports the number of lines retained before the live screen.
func (s *Session) ScrollbackLen() int {
	s.screenMu.RLock()
	defer s.screenMu.RUnlock()
	return s.terminal.ScrollbackLen()
}
func (s *Session) enqueue(in input) bool {
	select {
	case <-s.stop:
		return false
	default:
	}
	select {
	case s.inputs <- in:
		return true
	default:
		return false
	}
}
func (s *Session) Resize(width, height int) error {
	s.screenMu.Lock()
	defer s.screenMu.Unlock()
	s.terminal.Resize(width, height)
	return pty.Setsize(s.pty, &pty.Winsize{Cols: uint16(width), Rows: uint16(height)})
}
func (s *Session) Close() {
	s.once.Do(func() {
		s.mu.Lock()
		if !s.reaped {
			_ = syscall.Kill(-s.command.Process.Pid, syscall.SIGKILL)
		}
		s.mu.Unlock()
		close(s.stop)
		_ = s.pty.Close()
		_ = s.inputPipe.Close()
		s.notify()
	})
}
