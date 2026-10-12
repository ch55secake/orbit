//go:build linux || darwin

package tui

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/ch55secake/orbit/internal/config"
	"github.com/ch55secake/orbit/internal/shell"
	"github.com/ch55secake/orbit/internal/tui/views"
	"github.com/charmbracelet/x/ansi"
)

func TestEmbeddedShellLayoutAndResize(t *testing.T) {
	model, _ := testApp([]config.Server{updateServer("alpha"), updateServer("beta")})
	session, err := shell.Start(exec.Command("sh", "-c", "exec sleep 60"), 30, 5)
	if err != nil {
		t.Fatal(err)
	}
	model.shellSession = session
	model.shellServer = "alpha"
	t.Cleanup(model.closeShell)
	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 80, Height: 40}, {Width: 120, Height: 40}, {Width: 120, Height: 24}} {
		model.Update(size)
		view := model.View()
		if lipgloss.Width(view.Content) > size.Width || lipgloss.Height(view.Content) > size.Height {
			t.Fatalf("shell overflows %dx%d (%dx%d):\n%s", size.Width, size.Height, lipgloss.Width(view.Content), lipgloss.Height(view.Content), ansi.Strip(view.Content))
		}
		if !strings.Contains(ansi.Strip(view.Content), "SSH · alpha") || !strings.Contains(ansi.Strip(view.Content), "close SSH") {
			t.Fatal("shell frame or close hint missing")
		}
		bounds := views.TerminalBounds(model.dashboardData())
		if bounds != model.shellBounds {
			t.Fatal("PTY geometry not updated")
		}
		if view.Cursor == nil || view.Cursor.X != bounds.X || view.Cursor.Y != bounds.Y {
			t.Fatalf("cursor not inside frame: %+v; bounds %+v", view.Cursor, bounds)
		}
	}
	model.Update(keyPress("j"))
	model.Update(keyPress("ctrl+c"))
	model.Update(tea.PasteMsg{Content: "q"})
	if model.table.Cursor() != 0 || model.shellSession == nil {
		t.Fatal("shell input reached dashboard")
	}
}

func TestEmbeddedShellExitAndStaleEvents(t *testing.T) {
	model, _ := testApp([]config.Server{updateServer("alpha")})
	model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	session, err := shell.Start(exec.Command("sh", "-c", "printf final; exit 7"), 30, 5)
	if err != nil {
		t.Fatal(err)
	}
	model.shellSession = session
	model.shellServer = "alpha"
	t.Cleanup(model.closeShell)
	cmd := waitShell(session)
	for cmd != nil {
		_, cmd = model.Update(cmd())
	}
	if !strings.Contains(model.shellStatus, "exit status 7") || !strings.Contains(model.View().Content, "final") {
		t.Fatal("final output/exit status lost")
	}
	model.Update(keyPress("ctrl+]"))
	_, cmd = model.Update(shellEventMsg{session: session})
	if cmd != nil || model.shellSession != nil {
		t.Fatal("stale session event reopened shell")
	}
}

func TestEmbeddedShellShrinkAndExpand(t *testing.T) {
	model, _ := testApp([]config.Server{updateServer("alpha"), updateServer("beta")})
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	bounds := views.TerminalBounds(model.dashboardData())
	session, err := shell.Start(exec.Command("sh", "-c", "printf ready; read answer; printf '\\nreceived:%s\\n' \"$answer\""), bounds.Width, bounds.Height)
	if err != nil {
		t.Fatal(err)
	}
	model.shellSession, model.shellBounds, model.shellServer = session, bounds, "alpha"
	t.Cleanup(model.closeShell)
	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 12}, {Width: 120, Height: 8}, {Width: 10, Height: 6}, {Width: 1, Height: 1}, {Width: 80, Height: 2}} {
		model.Update(size)
		view := model.View()
		if lipgloss.Width(view.Content) > size.Width || lipgloss.Height(view.Content) > size.Height {
			t.Fatalf("warning overflows %dx%d: %q", size.Width, size.Height, view.Content)
		}
		if view.Cursor != nil {
			t.Fatal("undersized frame exposes off-screen cursor")
		}
		if model.shellSession != session || model.shellBounds != bounds {
			t.Fatal("shrinking changed session or last usable PTY geometry")
		}
		if size.Width == 80 && !strings.Contains(ansi.Strip(view.Content), "Ctrl+] close SSH") {
			t.Fatal("resize warning hides close action")
		}
		model.Update(keyPress("q"))
		model.Update(tea.PasteMsg{Content: "hidden"})
	}
	model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if view := model.View(); view.Cursor == nil || !strings.Contains(view.Content, "SSH · alpha") || strings.Contains(view.Content, "Resize terminal") {
		t.Fatal("expanding failed to restore shell frame/cursor")
	}
	if model.shellBounds != views.TerminalBounds(model.dashboardData()) {
		t.Fatal("expansion failed to resize PTY")
	}
	model.Update(tea.PasteMsg{Content: "visible"})
	model.Update(arrowKey(tea.KeyEnter))
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		if exited, _ := session.Result(); exited {
			break
		}
		select {
		case <-session.Events():
		case <-timer.C:
			t.Fatalf("shell did not receive restored input: %q", session.Render())
		}
	}
	if screen := session.Render(); !strings.Contains(screen, "received:visible") || strings.Contains(screen, "hidden") {
		t.Fatalf("input during resize warning leaked into shell: %q", screen)
	}
}

func TestEmbeddedShellShortSplitLayoutFits(t *testing.T) {
	model, _ := testApp([]config.Server{updateServer("alpha"), updateServer("beta")})
	session, err := shell.Start(exec.Command("sh", "-c", "exec sleep 60"), 30, 5)
	if err != nil {
		t.Fatal(err)
	}
	model.shellSession, model.shellServer = session, "alpha"
	t.Cleanup(model.closeShell)
	for _, height := range []int{12, 14, 16, 20, 24, 40} {
		model.Update(tea.WindowSizeMsg{Width: 120, Height: height})
		view := model.View()
		if lipgloss.Height(view.Content) > height || lipgloss.Width(view.Content) > 120 {
			t.Fatalf("split shell overflows 120x%d: %dx%d", height, lipgloss.Width(view.Content), lipgloss.Height(view.Content))
		}
	}
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	_, cmd := model.Update(keyPress("ctrl+]"))
	if cmd != nil || model.shellSession != nil {
		t.Fatal("undersized warning prevented closing shell")
	}
}
