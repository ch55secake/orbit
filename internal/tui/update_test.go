package tui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/ch55secake/orbit/internal/config"
	"github.com/charmbracelet/x/ansi"
)

func updateServer(name string) config.Server {
	target := testServer(name)
	target.SSH = &config.SSHConfig{Host: "prod", RemotePort: 8787}
	return target
}

func TestUpdateRequiresConfirmationAndPinsTarget(t *testing.T) {
	model, _ := testApp([]config.Server{updateServer("alpha"), updateServer("beta")})
	if !strings.Contains(ansi.Strip(model.View().Content), "u update") {
		t.Fatal("update menu option missing")
	}
	model.showHelp = true
	_, cmd := model.Update(keyPress("u"))
	if cmd != nil || model.updateTarget == nil {
		t.Fatal("update started before confirmation")
	}
	if model.showHelp {
		t.Fatal("confirmation should replace the expanded help")
	}
	if model.updateTarget.SSH == model.cfg.Servers[0].SSH {
		t.Fatal("confirmation target must snapshot SSH settings")
	}
	model.Update(keyPress("j"))
	if model.table.Cursor() != 0 || model.updateTarget.Name != "alpha" {
		t.Fatal("confirmation allowed switching target")
	}
	if view := model.View().Content; !strings.Contains(view, "Update Stardust on alpha?") || !strings.Contains(view, "restart the service") {
		t.Fatalf("missing confirmation consequences: %q", view)
	}
	_, cmd = model.Update(keyPress("y"))
	if cmd == nil || model.updateTarget != nil {
		t.Fatal("confirmation did not launch the terminal process")
	}
}

func TestUpdateCancellationDoesNotStartProcess(t *testing.T) {
	for _, key := range []string{"esc", "n", "N", "enter"} {
		t.Run(key, func(t *testing.T) {
			model, _ := testApp([]config.Server{updateServer("alpha")})
			model.Update(keyPress("u"))
			_, cmd := model.Update(keyPress(key))
			if cmd != nil || model.updateTarget != nil {
				t.Fatal("cancelled update started a process")
			}
		})
	}
}

func TestUpdateUnavailableWhileLoadingOrWithoutLocalSSH(t *testing.T) {
	for _, target := range []config.Server{testServer("direct"), updateServer("remote")} {
		if target.SSH != nil {
			target.SSH.RemoteHost = "10.0.0.2"
		}
		model, _ := testApp([]config.Server{target})
		_, cmd := model.Update(keyPress("u"))
		if cmd != nil || model.updateTarget != nil || model.message == "" {
			t.Fatal("unsupported updater target accepted")
		}
		if strings.Contains(ansi.Strip(model.View().Content), "u update") {
			t.Fatal("unsupported target advertises updating")
		}
	}
	model, _ := testApp([]config.Server{updateServer("alpha")})
	model.loading = true
	model.Update(keyPress("u"))
	if model.updateTarget != nil {
		t.Fatal("update interrupted metrics authentication")
	}
}

func TestUpdateShortcutRemainsPasswordInput(t *testing.T) {
	model, _ := testApp([]config.Server{updateServer("alpha")})
	request := &passwordRequest{server: model.cfg.Servers[0], response: make(chan passwordResponse, 1)}
	model.Update(passwordRequestMsg{request: request})
	model.Update(keyPress("u"))
	if model.updateTarget != nil || string(model.passwordInput) != "u" {
		t.Fatal("update shortcut intercepted password input")
	}
	model.cancelPasswordPrompt()
}

func TestUpdateConfirmationCanQuitAndYieldsToPasswordPrompt(t *testing.T) {
	model, _ := testApp([]config.Server{updateServer("alpha")})
	model.Update(keyPress("u"))
	_, cmd := model.Update(keyPress("ctrl+c"))
	if cmd == nil || model.updateTarget != nil {
		t.Fatal("confirmation did not allow quitting")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("confirmation quit started another command")
	}
	model.Update(keyPress("u"))
	request := &passwordRequest{server: model.cfg.Servers[0], response: make(chan passwordResponse, 1)}
	model.Update(passwordRequestMsg{request: request})
	if model.updateTarget != nil || model.passwordRequest == nil {
		t.Fatal("password prompt did not take priority")
	}
	model.cancelPasswordPrompt()
}

func TestUpdateReturnRefreshesMetricsAndPreservesResult(t *testing.T) {
	for _, failure := range []bool{false, true} {
		model, fake := testApp([]config.Server{updateServer("alpha")})
		model.tunnels = &fakeTunnelController{}
		fake.set("http://alpha.test", metricsResult{response: testMetrics(42)})
		var err error
		if failure {
			err = errors.New("exit status 1")
		}
		_, cmd := model.Update(updateEndedMsg{server: "alpha", err: err})
		if cmd == nil || !model.loading || model.noticeError != failure {
			t.Fatal("update return did not refresh/report outcome")
		}
		updateModel(t, model, cmd())
		if fake.callCount() != 1 || model.loading || !strings.Contains(model.View().Content, model.notice) || model.notice == "" {
			t.Fatal("metrics refresh lost update result")
		}
	}
}

func TestUpdateDialogAndOutcomeFitDashboard(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 80, Height: 40}, {Width: 120, Height: 40}} {
		model, _ := testApp([]config.Server{updateServer("alpha"), updateServer("beta")})
		model.statuses[0].Metrics = metricsPtr(testMetrics(42))
		model.Update(size)
		model.Update(keyPress("u"))
		if view := model.View().Content; lipgloss.Width(view) > size.Width || lipgloss.Height(view) > size.Height {
			t.Fatalf("confirmation overflows %dx%d:\n%s", size.Width, size.Height, ansi.Strip(view))
		}
		model.Update(keyPress("esc"))
		model.Update(updateEndedMsg{server: "alpha", err: errors.New("exit status 1")})
		if view := model.View().Content; lipgloss.Width(view) > size.Width || lipgloss.Height(view) > size.Height {
			t.Fatalf("outcome overflows %dx%d:\n%s", size.Width, size.Height, ansi.Strip(view))
		}
	}
}
