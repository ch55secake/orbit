//go:build linux || darwin

package tui

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/ch55secake/orbit/internal/config"
	"github.com/ch55secake/orbit/internal/shell"
	"github.com/ch55secake/orbit/internal/tui/views"
)

func selectionApp(t *testing.T) *app {
	t.Helper()
	m, _ := testApp([]config.Server{updateServer("alpha")})
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	bounds := views.TerminalBounds(m.dashboardData())
	session, err := shell.Start(exec.Command("sh", "-c", "printf 'hello world'; read answer; printf '\\r\\nlater output'; exec sleep 60"), bounds.Width, bounds.Height)
	if err != nil {
		t.Fatal(err)
	}
	m.shellSession, m.shellBounds, m.shellServer = session, bounds, "alpha"
	t.Cleanup(m.closeShell)
	waitSelectionOutput(t, m, "hello world")
	return m
}

func waitSelectionOutput(t *testing.T, m *app, text string) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for !strings.Contains(m.shellSession.Render(), text) {
		select {
		case <-m.shellSession.Events():
		case <-timer.C:
			t.Fatal("timed out waiting for local shell output")
		}
	}
}

func TestMouseSelectionIsConfinedToShellText(t *testing.T) {
	m := selectionApp(t)
	b := m.shellBounds
	if m.View().MouseMode != tea.MouseModeCellMotion {
		t.Fatal("embedded shell does not capture selection mouse events")
	}
	for _, mouse := range []tea.Mouse{{X: b.X - 1, Y: b.Y, Button: tea.MouseLeft}, {X: b.X, Y: b.Y - 1, Button: tea.MouseLeft}, {X: b.X + 20, Y: b.Y, Button: tea.MouseLeft}} {
		m.Update(tea.MouseClickMsg(mouse))
		if m.shellSelection != nil {
			t.Fatal("frame, dashboard, or blank padding started selection")
		}
	}
	m.Update(tea.MouseClickMsg{X: b.X, Y: b.Y, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: b.X + 4, Y: b.Y, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: b.X + 4, Y: b.Y, Button: tea.MouseLeft})
	if m.shellSelection == nil || m.shellSelection.content(false) != "hello" {
		t.Fatal("drag did not select shell text")
	}
	if m.View().Cursor != nil {
		t.Fatal("live cursor remains visible over frozen selection")
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
	if cmd == nil || fmt.Sprint(cmd()) != "hello" {
		t.Fatal("copy command did not contain only selected plain text")
	}
	m.shellSession.Paste("\n")
	waitSelectionOutput(t, m, "later output")
	if strings.Contains(m.dashboardData().ShellContent, "later output") {
		t.Fatal("live output changed the selection snapshot")
	}
	m.Update(arrowKey(tea.KeyEscape))
	if m.shellSelection != nil || !strings.Contains(m.dashboardData().ShellContent, "later output") {
		t.Fatal("clearing selection did not restore live shell")
	}
	m.Update(tea.MouseClickMsg{X: b.X, Y: b.Y, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: b.X + b.Width + 10, Y: b.Y + b.Height + 10, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: b.X + b.Width + 10, Y: b.Y + b.Height + 10, Button: tea.MouseLeft})
	if text := m.shellSelection.content(false); strings.Contains(text, "alpha") || strings.Contains(text, "SSH") || strings.Contains(text, "╭") {
		t.Fatalf("outside drag copied dashboard/frame text: %q", text)
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	if m.shellSelection != nil {
		t.Fatal("resize retained stale selection coordinates")
	}
	m.Update(keyPress("ctrl+]"))
	if m.View().MouseMode != tea.MouseModeNone {
		t.Fatal("closing shell did not restore default terminal mouse behavior")
	}
}

func TestShellInputClearsTextSelection(t *testing.T) {
	m := selectionApp(t)
	b := m.shellBounds
	m.Update(tea.MouseClickMsg{X: b.X, Y: b.Y, Button: tea.MouseLeft})
	m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if m.shellSelection != nil {
		t.Fatal("typing retained frozen selection")
	}
	m.Update(tea.MouseClickMsg{X: b.X, Y: b.Y, Button: tea.MouseLeft})
	m.Update(tea.PasteMsg{Content: "pasted"})
	if m.shellSelection != nil {
		t.Fatal("paste retained frozen selection")
	}
	m.Update(tea.MouseClickMsg{X: b.X, Y: b.Y, Button: tea.MouseLeft})
	m.Update(tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})
	if m.shellSelection != nil {
		t.Fatal("outside click retained frozen selection")
	}
}

func TestShellSelectionDoesNotChangePaneGeometry(t *testing.T) {
	m := selectionApp(t)
	for _, width := range []int{120, 80, 50} {
		m.Update(tea.WindowSizeMsg{Width: width, Height: 24})
		bounds := m.shellBounds
		m.Update(tea.MouseClickMsg{X: bounds.X, Y: bounds.Y, Button: tea.MouseLeft})
		m.Update(tea.MouseReleaseMsg{X: bounds.X + 4, Y: bounds.Y, Button: tea.MouseLeft})
		if m.shellSelection == nil {
			t.Fatal("text selection did not start")
		}
		if views.TerminalBounds(m.dashboardData()) != bounds {
			t.Fatal("selection menu changed PTY geometry")
		}
		view := m.View()
		if lipgloss.Width(view.Content) > width || lipgloss.Height(view.Content) > 24 {
			t.Fatalf("selection overflows %dx24", width)
		}
	}
}
