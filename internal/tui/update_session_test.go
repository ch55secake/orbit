//go:build linux || darwin

package tui

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/ch55secake/orbit/internal/config"
	"github.com/charmbracelet/x/ansi"
)

func TestUpdateRunsInTUIAndKeepsOutputAfterCompletion(t *testing.T) {
	model, fake := testApp([]config.Server{updateServer("alpha")})
	model.tunnels = &fakeTunnelController{}
	fake.set("http://alpha.test", metricsResult{response: testMetrics(42)})
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 32})
	model.updateCommand = func(context.Context, config.Server, string) (*exec.Cmd, error) {
		return exec.Command("sh", "-c", "stty -echo; printf 'sudo password: '; IFS= read -r password; stty echo; printf '\\nupdate authorized\\n'"), nil
	}
	model.Update(keyPress("u"))
	_, wait := model.Update(keyPress("y"))
	if model.shellSession == nil || !model.updateRunning || wait == nil {
		t.Fatal("update did not start in an embedded terminal")
	}
	session := model.shellSession
	t.Cleanup(model.closeShell)

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for !strings.Contains(ansi.Strip(session.Render()), "sudo password:") {
		select {
		case <-session.Events():
			_, wait = model.Update(shellEventMsg{session: session})
		case <-timer.C:
			t.Fatalf("in-TUI password prompt did not render: %q", ansi.Strip(session.Render()))
		}
	}
	model.Update(tea.PasteMsg{Content: "not-visible-secret"})
	model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	for model.updateRunning {
		select {
		case <-session.Events():
			_, wait = model.Update(shellEventMsg{session: session})
		case <-timer.C:
			t.Fatalf("update process did not return; terminal output: %q", ansi.Strip(session.Render()))
		}
	}
	if wait == nil {
		t.Fatal("update completion did not schedule result handling")
	}
	_, refresh := model.Update(wait())
	if refresh == nil {
		t.Fatal("update completion did not refresh metrics")
	}
	updateModel(t, model, refresh())
	view := ansi.Strip(model.View().Content)
	if model.shellSession != session || !model.shellUpdate || model.noticeError || !strings.Contains(view, "UPDATE · alpha") || !strings.Contains(view, "update authorized") || strings.Contains(view, "not-visible-secret") || !strings.Contains(model.notice, "updated on alpha") {
		t.Fatalf("update result was not retained in the TUI: %q", view)
	}
	if fake.callCount() != 1 {
		t.Fatal("metrics were not refreshed after the update")
	}
	_, cmd := model.Update(keyPress("ctrl+]"))
	if cmd != nil || model.shellSession != nil {
		t.Fatal("completed update pane did not return to the dashboard")
	}
	if !strings.Contains(ansi.Strip(model.View().Content), "Stardust updated on alpha") {
		t.Fatal("update result notice disappeared when leaving the terminal pane")
	}
}
