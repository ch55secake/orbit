//go:build linux || darwin

package shell

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

func await(t *testing.T, s *Session, check func() bool) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for !check() {
		select {
		case <-s.Events():
		case <-timer.C:
			t.Fatalf("timed out; screen: %q", s.Render())
		}
	}
}

func TestPTYInputResizeAndExit(t *testing.T) {
	s, err := Start(exec.Command("sh", "-c", "printf 'ready\\n'; read answer; stty size; printf 'answer:%s\\n' \"$answer\""), 40, 8)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	await(t, s, func() bool { return strings.Contains(s.Render(), "ready") })
	if err := s.Resize(50, 10); err != nil {
		t.Fatal(err)
	}
	s.Paste("héllo")
	s.SendKey(uv.KeyPressEvent{Code: uv.KeyEnter})
	await(t, s, func() bool { exited, _ := s.Result(); return exited })
	_, err = s.Result()
	if err != nil {
		t.Fatal(err)
	}
	screen := ansi.Strip(s.Render())
	if !strings.Contains(screen, "10 50") || !strings.Contains(screen, "answer:héllo") {
		t.Fatalf("PTY input/size lost: %q", screen)
	}
}

func TestEmulatorHandlesControlSequencesAndAlternateScreen(t *testing.T) {
	s, err := Start(exec.Command("sh", "-c", "printf '\033[2J\033[Horiginal\033[?1049halternate\033[?1049l\033[2;1Hsecond'; exit 7"), 30, 5)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	await(t, s, func() bool { exited, _ := s.Result(); return exited })
	_, err = s.Result()
	if err == nil {
		t.Fatal("exit failure lost")
	}
	screen := ansi.Strip(s.Render())
	if !strings.Contains(screen, "original") || !strings.Contains(screen, "second") || strings.Contains(screen, "alternate") {
		t.Fatalf("terminal control sequences not emulated: %q", screen)
	}
}

func TestCloseTerminatesAndReapsProcess(t *testing.T) {
	s, err := Start(exec.Command("sh", "-c", "printf ready; exec sleep 60"), 30, 5)
	if err != nil {
		t.Fatal(err)
	}
	await(t, s, func() bool { return strings.Contains(s.Render(), "ready") })
	s.Close()
	s.Close()
	await(t, s, func() bool { exited, _ := s.Result(); return exited })
}

func TestModifiedNavigationReachesPTY(t *testing.T) {
	s, err := Start(exec.Command("sh", "-c", "stty raw -echo; printf ready; dd bs=1 count=6 2>/dev/null | od -An -tx1"), 40, 8)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	await(t, s, func() bool { return strings.Contains(s.Render(), "ready") })
	if !s.SendKey(uv.KeyPressEvent{Code: uv.KeyRight, Mod: uv.ModCtrl}) {
		t.Fatal("key was not queued")
	}
	await(t, s, func() bool { exited, _ := s.Result(); return exited })
	if !strings.Contains(strings.Join(strings.Fields(s.Render()), " "), "1b 5b 31 3b 35 43") {
		t.Fatalf("modified arrow did not reach PTY: %q", s.Render())
	}
}
