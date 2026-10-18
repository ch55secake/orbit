package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
