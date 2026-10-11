//go:build linux || darwin

package tui

import (
	"os/exec"
	"strings"
	"testing"

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
