package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/ch55secake/orbit/internal/client"
	"github.com/ch55secake/orbit/internal/config"
	"github.com/charmbracelet/x/ansi"
)

func serviceUnits() []client.ServiceUnit {
	return []client.ServiceUnit{
		{Name: "a.service", ID: "canonical.service", Description: "Running worker", LoadState: "loaded", ActiveState: "active", SubState: "running", UnitFileState: "enabled", MainPID: 123, MainProcess: &client.ServiceProcess{Name: "worker", EffectiveUID: 1001}},
		{Name: "b.service", ID: "b.service", LoadState: "loaded", ActiveState: "failed", SubState: "failed", UnitFileState: "disabled"},
	}
}

func finishServiceCommand(t *testing.T, model *app, command tea.Cmd) {
	t.Helper()
	if command == nil {
		t.Fatal("expected service command")
	}
	updateModel(t, model, command())
}

func TestServiceBrowserNavigationPagingFilteringAndLiveDetails(t *testing.T) {
	model, _ := testApp([]config.Server{testServer("alpha"), testServer("beta")})
	model.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	var query client.ServiceQuery
	pid := uint32(400)
	model.serviceLoader = func(_ context.Context, _ config.Server, q client.ServiceQuery, name string) (client.ServicePage, *client.ServiceUnit, error) {
		query = q
		if name != "" {
			unit := serviceUnits()[0]
			unit.Name = name
			pid++
			unit.MainPID = pid
			return client.ServicePage{}, &unit, nil
		}
		if q.Cursor != "" {
			return client.ServicePage{Units: []client.ServiceUnit{{Name: "z.service", ID: "z.service", ActiveState: "inactive", SubState: "dead"}}}, nil, nil
		}
		return client.ServicePage{Units: serviceUnits(), NextCursor: "b.service"}, nil, nil
	}
	_, command := model.Update(keyPress("v"))
	finishServiceCommand(t, model, command)
	view := ansi.Strip(model.View().Content)
	for _, want := range []string{"SERVICES", "alpha", "beta", "a.service", "active/running", "UID 1001"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q:\n%s", want, view)
		}
	}
	model.Update(arrowKey(tea.KeyDown))
	if model.table.Cursor() != 0 || model.services.cursor != 1 {
		t.Fatal("service navigation changed fleet selection")
	}
	if view = ansi.Strip(model.View().Content); !strings.Contains(view, "failed/failed") || !strings.Contains(view, "Main process: none") {
		t.Fatal(view)
	}
	model.Update(arrowKey(tea.KeyUp))
	_, command = model.Update(arrowKey(tea.KeyEnter))
	finishServiceCommand(t, model, command)
	if model.services.detail == nil || model.services.detail.MainPID != 401 {
		t.Fatal("detail fetch did not refresh PID")
	}
	_, command = model.Update(keyPress("r"))
	finishServiceCommand(t, model, command)
	if model.services.detail.MainPID != 402 {
		t.Fatal("refresh did not fetch live detail")
	}
	_, command = model.Update(arrowKey(tea.KeyRight))
	finishServiceCommand(t, model, command)
	if query.Cursor != "b.service" || len(model.services.previous) != 1 || model.services.page.Units[0].Name != "z.service" {
		t.Fatal("next page failed")
	}
	_, command = model.Update(arrowKey(tea.KeyLeft))
	finishServiceCommand(t, model, command)
	if query.Cursor != "" || len(model.services.previous) != 0 {
		t.Fatal("previous page failed")
	}
	model.Update(keyPress("/"))
	model.Update(keyPress("worker"))
	_, command = model.Update(arrowKey(tea.KeyEnter))
	finishServiceCommand(t, model, command)
	if query.Filter != "worker" || query.Cursor != "" || model.services.editing {
		t.Fatal("filter failed")
	}
	model.Update(keyPress("/"))
	model.Update(keyPress("x"))
	model.Update(arrowKey(tea.KeyEscape))
	if model.services.query.Filter != "worker" {
		t.Fatal("filter cancellation changed query")
	}
	model.Update(arrowKey(tea.KeyEscape))
	if model.services != nil || !strings.Contains(model.View().Content, "ORBIT") {
		t.Fatal("browser did not return to dashboard")
	}
}

func TestServiceBrowserCancelsAndIgnoresStaleServerResults(t *testing.T) {
	model, _ := testApp([]config.Server{testServer("alpha"), testServer("beta")})
	var contexts []context.Context
	started := make(chan struct{})
	model.serviceLoader = func(ctx context.Context, target config.Server, _ client.ServiceQuery, _ string) (client.ServicePage, *client.ServiceUnit, error) {
		contexts = append(contexts, ctx)
		if target.Name == "alpha" {
			close(started)
			<-ctx.Done()
		}
		return client.ServicePage{Units: []client.ServiceUnit{{Name: target.Name + ".service"}}}, nil, nil
	}
	_, first := model.Update(keyPress("v"))
	result := make(chan tea.Msg, 1)
	go func() { result <- first() }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("request did not start")
	}
	_, second := model.Update(arrowKey(tea.KeyTab))
	var oldMessage tea.Msg
	select {
	case oldMessage = <-result:
	case <-time.After(time.Second):
		t.Fatal("server switch did not cancel the in-flight read")
	}
	if contexts[0].Err() == nil {
		t.Fatal("previous context not canceled")
	}
	model.Update(oldMessage)
	if model.table.Cursor() != 1 || len(model.services.page.Units) != 0 || !model.services.loading {
		t.Fatal("stale response applied to a different server")
	}
	finishServiceCommand(t, model, second)
	if model.services.page.Units[0].Name != "beta.service" {
		t.Fatal("wrong server inventory")
	}
	_, pending := model.Update(keyPress("r"))
	model.Update(arrowKey(tea.KeyEscape))
	model.Update(pending())
	if model.services != nil || contexts[len(contexts)-1].Err() == nil {
		t.Fatal("leaving browser did not cancel/ignore pending request")
	}
}

func TestServiceBrowserErrorRecoveryAndFilterPasteBounds(t *testing.T) {
	model, _ := testApp([]config.Server{testServer("alpha")})
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	fail := true
	model.serviceLoader = func(context.Context, config.Server, client.ServiceQuery, string) (client.ServicePage, *client.ServiceUnit, error) {
		if fail {
			return client.ServicePage{}, nil, errors.New("permission denied\x1b[31m\nretry")
		}
		return client.ServicePage{Units: serviceUnits()}, nil, nil
	}
	_, command := model.Update(keyPress("v"))
	finishServiceCommand(t, model, command)
	if view := model.View().Content; strings.Contains(view, "\x1b[31m") || !strings.Contains(ansi.Strip(view), "permission denied retry") {
		t.Fatal("unsafe error display")
	}
	fail = false
	_, command = model.Update(keyPress("r"))
	finishServiceCommand(t, model, command)
	if model.services.err != "" || len(model.services.page.Units) != 2 {
		t.Fatal("refresh did not recover")
	}
	model.Update(keyPress("/"))
	model.Update(tea.PasteMsg{Content: strings.Repeat("界", 200) + "\x1b[31m\n"})
	if len(model.services.input) > 255 || strings.ContainsAny(model.services.input, "\x1b\n") {
		t.Fatal("filter paste not bounded/sanitized")
	}
	model.Update(arrowKey(tea.KeyBackspace))
	if len(model.services.input) > 252 {
		t.Fatal("unicode backspace failed")
	}
}

func TestServiceBrowserLayoutsRetainContextAndBoundRemoteText(t *testing.T) {
	model, _ := testApp([]config.Server{testServer("alpha"), testServer("beta")})
	units := serviceUnits()
	units[0].Description = "\x1b[31mRED\x1b[0m\n" + strings.Repeat("界", 200)
	model.services = &serviceBrowser{page: client.ServicePage{Units: units}}
	for _, width := range []int{1, 20, 40, 64, 80, 100, 120, 180} {
		for _, height := range []int{1, 5, 10, 24, 40, 56} {
			t.Run(fmt.Sprintf("%dx%d", width, height), func(t *testing.T) {
				model.Update(tea.WindowSizeMsg{Width: width, Height: height})
				view := model.View().Content
				if lipgloss.Width(view) > width || lipgloss.Height(view) > height {
					t.Fatalf("service view overflow:\n%s", ansi.Strip(view))
				}
				plain := ansi.Strip(view)
				if width == 120 && height == 40 {
					t.Log("\n" + plain)
				}
				if width >= 80 && height >= 24 && (!strings.Contains(plain, "alpha") || !strings.Contains(plain, "a.service") || !strings.Contains(plain, "Fleet") && !strings.Contains(plain, "FLEET") && !strings.Contains(plain, "Servers")) {
					t.Fatalf("lost fleet/server/service context:\n%s", plain)
				}
				if strings.Contains(view, "\x1b[31m") {
					t.Fatal("remote escape sequence rendered")
				}
			})
		}
	}
}

func TestServiceBrowserKeepsConsistentLeftInset(t *testing.T) {
	model, _ := testApp([]config.Server{testServer("alpha")})
	model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	model.services = &serviceBrowser{page: client.ServicePage{Units: serviceUnits()}}
	view := ansi.Strip(model.View().Content)
	lines := strings.Split(view, "\n")
	if len(lines) < 3 || !strings.HasPrefix(lines[0], "  SERVICES") || !strings.HasPrefix(lines[2], "  Fleet") {
		t.Fatalf("services view lost the shared left inset:\n%s", view)
	}
	if width := lipgloss.Width(view); width > 80 {
		t.Fatalf("services view width = %d, terminal = 80", width)
	}
}

func TestServiceRestartConfirmationTrackingAndRefresh(t *testing.T) {
	model, _ := testApp([]config.Server{testServer("alpha"), testServer("beta")})
	model.ctx = context.Background()
	model.Update(tea.WindowSizeMsg{Width: 90, Height: 24})
	model.services = &serviceBrowser{page: client.ServicePage{
		Units:        []client.ServiceUnit{{Name: "worker.service", ID: "worker.service", ActiveState: "active", SubState: "running", RestartAllowed: true}},
		Capabilities: client.ServiceCapabilities{Restart: true},
	}}
	if view := ansi.Strip(model.View().Content); !strings.Contains(view, "R restart") || !strings.Contains(view, "Restart access: authorized") {
		t.Fatalf("authorized restart action/access was not distinguished:\n%s", view)
	}
	var starts atomic.Int32
	var key string
	model.serviceRestartStart = func(_ context.Context, target config.Server, unit, requestKey string) (client.ServiceRestartOperation, error) {
		starts.Add(1)
		if target.Name != "alpha" || unit != "worker.service" {
			t.Errorf("restart target changed: %s/%s", target.Name, unit)
		}
		if len(requestKey) != 64 {
			t.Errorf("invalid operation identity length: %d", len(requestKey))
		}
		key = requestKey
		return client.ServiceRestartOperation{ID: "0123456789abcdef0123456789abcdef", Unit: unit, State: client.RestartRunning}, nil
	}
	var statusCalls atomic.Int32
	model.serviceRestartStatus = func(_ context.Context, target config.Server, id string) (client.ServiceRestartOperation, error) {
		statusCalls.Add(1)
		if target.Name != "alpha" || id != "0123456789abcdef0123456789abcdef" {
			t.Errorf("operation tracking changed target: %s/%s", target.Name, id)
		}
		return client.ServiceRestartOperation{ID: id, Unit: "worker.service", State: client.RestartSucceeded, JobResult: "succeeded", ActiveState: "active", SubState: "running"}, nil
	}
	var refreshes atomic.Int32
	model.serviceLoader = func(_ context.Context, target config.Server, _ client.ServiceQuery, unit string) (client.ServicePage, *client.ServiceUnit, error) {
		refreshes.Add(1)
		if target.Name != "alpha" || unit != "worker.service" {
			t.Errorf("detail refresh changed target: %s/%s", target.Name, unit)
		}
		detail := client.ServiceUnit{Name: unit, ID: unit, ActiveState: "active", SubState: "running", RestartAllowed: true, RestartCapability: true}
		return client.ServicePage{}, &detail, nil
	}

	model.Update(keyPress("R"))
	if model.serviceRestart == nil || model.serviceRestart.phase != "confirm" {
		t.Fatal("authorized service did not open confirmation")
	}
	confirm := ansi.Strip(model.View().Content)
	for _, want := range []string{"SYSTEMD SERVICE RESTART", "Server: alpha", "Unit: worker.service", "may interrupt", "Press y to confirm"} {
		if !strings.Contains(confirm, want) {
			t.Fatalf("confirmation missing %q:\n%s", want, confirm)
		}
	}
	model.Update(tea.WindowSizeMsg{Width: 70, Height: 20})
	model.Update(arrowKey(tea.KeyTab))
	if model.table.Cursor() != 0 || model.serviceRestart.server.Name != "alpha" {
		t.Fatal("confirmation target changed after resize/server navigation")
	}

	_, submit := model.Update(keyPress("y"))
	if submit == nil || model.serviceRestart.phase != "submitting" {
		t.Fatal("confirmed restart did not submit asynchronously")
	}
	_, duplicate := model.Update(keyPress("y"))
	if duplicate != nil {
		t.Fatal("duplicate confirmation triggered another command")
	}
	started := submit()
	_, poll := model.Update(started)
	if starts.Load() != 1 || poll == nil {
		t.Fatal("restart submission was duplicated or operation was not tracked")
	}
	polled := poll()
	_, refresh := model.Update(polled)
	if statusCalls.Load() != 1 || model.serviceRestart.phase != "complete" || refresh == nil {
		t.Fatal("completed operation was not reflected or detail refresh was not started")
	}
	model.Update(refresh())
	if refreshes.Load() != 1 || model.services.detail == nil || model.services.detail.ActiveState != "active" {
		t.Fatal("service details were not refreshed after restart")
	}
	result := ansi.Strip(model.View().Content)
	for _, want := range []string{"succeeded", "worker.service", "does not report application health"} {
		if !strings.Contains(result, want) {
			t.Fatalf("restart result missing %q:\n%s", want, result)
		}
	}
	if key == "" {
		t.Fatal("operation identity was not retained for the confirmed intent")
	}
	model.Update(arrowKey(tea.KeyEscape))
	if model.serviceRestart != nil {
		t.Fatal("completed restart modal did not close")
	}
}

func TestServiceRestartCancellationAuthorizationAndSameIdentityReconcile(t *testing.T) {
	t.Run("cancel does not submit", func(t *testing.T) {
		model, _ := testApp([]config.Server{testServer("alpha")})
		model.services = &serviceBrowser{page: client.ServicePage{Units: []client.ServiceUnit{{Name: "worker.service", RestartAllowed: true}}, Capabilities: client.ServiceCapabilities{Restart: true}}}
		var starts atomic.Int32
		model.serviceRestartStart = func(context.Context, config.Server, string, string) (client.ServiceRestartOperation, error) {
			starts.Add(1)
			return client.ServiceRestartOperation{}, nil
		}
		model.Update(keyPress("R"))
		model.Update(keyPress("n"))
		if model.serviceRestart != nil || starts.Load() != 0 {
			t.Fatal("cancel submitted or retained a restart")
		}
	})

	for _, test := range []struct {
		name string
		page client.ServicePage
	}{
		{name: "global capability missing", page: client.ServicePage{Units: []client.ServiceUnit{{Name: "worker.service", RestartAllowed: true}}}},
		{name: "unit is read-only", page: client.ServicePage{Units: []client.ServiceUnit{{Name: "worker.service"}}, Capabilities: client.ServiceCapabilities{Restart: true}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			model, _ := testApp([]config.Server{testServer("alpha")})
			model.services = &serviceBrowser{page: test.page}
			view := ansi.Strip(model.View().Content)
			if strings.Contains(view, "R restart") {
				t.Fatal("restart action was advertised without both global and per-unit authorization")
			}
			model.Update(keyPress("R"))
			if model.serviceRestart != nil {
				t.Fatal("restart confirmation opened without both capability and unit authorization")
			}
		})
	}

	t.Run("unknown submission retries same key", func(t *testing.T) {
		model, _ := testApp([]config.Server{testServer("alpha")})
		model.ctx = context.Background()
		model.services = &serviceBrowser{page: client.ServicePage{Units: []client.ServiceUnit{{Name: "worker.service", RestartAllowed: true}}, Capabilities: client.ServiceCapabilities{Restart: true}}}
		var starts atomic.Int32
		var keys []string
		model.serviceRestartStart = func(_ context.Context, _ config.Server, unit, key string) (client.ServiceRestartOperation, error) {
			keys = append(keys, key)
			if starts.Add(1) == 1 {
				return client.ServiceRestartOperation{}, &client.ServiceError{Code: "restart_transport"}
			}
			return client.ServiceRestartOperation{ID: "0123456789abcdef0123456789abcdef", Unit: unit, State: client.RestartSucceeded}, nil
		}
		model.serviceLoader = func(context.Context, config.Server, client.ServiceQuery, string) (client.ServicePage, *client.ServiceUnit, error) {
			return client.ServicePage{}, nil, nil
		}
		model.Update(keyPress("R"))
		_, submit := model.Update(keyPress("y"))
		_, unknown := model.Update(submit())
		if unknown != nil || model.serviceRestart.phase != "unknown" {
			t.Fatal("ambiguous submission was not surfaced as unknown")
		}
		_, retry := model.Update(keyPress("r"))
		if retry == nil {
			t.Fatal("same-intent reconciliation did not retry")
		}
		_, refresh := model.Update(retry())
		if starts.Load() != 2 || len(keys) != 2 || keys[0] == "" || keys[0] != keys[1] {
			t.Fatalf("reconciliation generated a new operation identity: %#v", keys)
		}
		if refresh != nil {
			model.Update(refresh())
		}
		if model.serviceRestart.phase != "complete" {
			t.Fatal("same-identity retry did not resolve the operation")
		}
	})
}

func TestServiceRestartPromptFitsSmallAndLargeTerminals(t *testing.T) {
	model, _ := testApp([]config.Server{testServer("production-east-2")})
	model.services = &serviceBrowser{}
	model.serviceRestart = &serviceRestartState{server: model.cfg.Servers[0], unit: "worker@blue.service", phase: "confirm"}
	for _, size := range [][2]int{{1, 1}, {16, 5}, {40, 10}, {80, 24}, {140, 40}} {
		model.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := model.View().Content
		if lipgloss.Width(view) > size[0] || lipgloss.Height(view) > size[1] {
			t.Fatalf("restart prompt overflowed %dx%d:\n%s", size[0], size[1], ansi.Strip(view))
		}
	}
}

func TestServiceRestartDisplaysFailedJobWithoutRetry(t *testing.T) {
	model, _ := testApp([]config.Server{testServer("alpha")})
	model.ctx = context.Background()
	model.services = &serviceBrowser{page: client.ServicePage{Units: []client.ServiceUnit{{Name: "worker.service", RestartAllowed: true}}, Capabilities: client.ServiceCapabilities{Restart: true}}}
	var starts atomic.Int32
	model.serviceRestartStart = func(_ context.Context, _ config.Server, unit, _ string) (client.ServiceRestartOperation, error) {
		starts.Add(1)
		return client.ServiceRestartOperation{ID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Unit: unit, State: client.RestartFailed, JobResult: "failed", ErrorCode: "restart_failed"}, nil
	}
	model.serviceLoader = func(context.Context, config.Server, client.ServiceQuery, string) (client.ServicePage, *client.ServiceUnit, error) {
		detail := client.ServiceUnit{Name: "worker.service", ActiveState: "failed", SubState: "failed"}
		return client.ServicePage{}, &detail, nil
	}
	model.Update(keyPress("R"))
	_, submit := model.Update(keyPress("y"))
	_, refresh := model.Update(submit())
	if starts.Load() != 1 || refresh == nil {
		t.Fatal("failed restart job was retried or service detail was not refreshed")
	}
	model.Update(refresh())
	view := ansi.Strip(model.View().Content)
	for _, want := range []string{"State: failed", "Systemd job: failed", "Systemd reported a failed restart job", "does not report application health"} {
		if !strings.Contains(view, want) {
			t.Fatalf("failed restart result missing %q:\n%s", want, view)
		}
	}
}
