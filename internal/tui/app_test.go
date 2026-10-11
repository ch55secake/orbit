package tui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/ch55secake/orbit/internal/client"
	"github.com/ch55secake/orbit/internal/config"
	"github.com/ch55secake/orbit/internal/server"
)

func TestInitShowsLoadingAndLoadsMetrics(t *testing.T) {
	model, fake := testApp([]config.Server{testServer("alpha")})
	fake.set("http://alpha.test", metricsResult{response: testMetrics(42)})

	cmd := model.Init()
	if cmd == nil {
		t.Fatal("Init returned no command for a configured server")
	}
	if !model.loading {
		t.Fatal("model is not loading after Init")
	}
	if view := model.View().Content; !strings.Contains(view, "Loading metrics") {
		t.Fatalf("initial view does not show loading state: %q", view)
	}

	updateModel(t, model, cmd())
	if model.loading {
		t.Fatal("model remained loading after metrics completed")
	}
	if model.statuses[0].Metrics == nil || model.statuses[0].Metrics.Data.CPU.UsagePercent != 42 {
		t.Fatalf("loaded status = %+v", model.statuses[0])
	}
	if got := model.table.Rows()[0][1]; got != "online" {
		t.Fatalf("server status = %q, want online", got)
	}
	if view := model.View().Content; !strings.Contains(view, "test-host") || !strings.Contains(view, "42.0%") {
		t.Fatalf("successful metrics are not rendered in the selected-server view: %q", view)
	}
	if got := fake.callCount(); got != 1 {
		t.Fatalf("metrics calls = %d, want 1", got)
	}
}

func TestInitShowsEmptyConfiguration(t *testing.T) {
	model, fake := testApp(nil)

	if cmd := model.Init(); cmd != nil {
		t.Fatal("Init returned a command with no configured servers")
	}
	if model.loading {
		t.Fatal("empty configuration entered loading state")
	}
	if view := model.View().Content; !strings.Contains(view, "No servers configured") {
		t.Fatalf("empty view does not explain configuration state: %q", view)
	}
	if got := fake.callCount(); got != 0 {
		t.Fatalf("metrics calls = %d, want 0", got)
	}
}

func TestOfflineMetricsShowErrorAndOfflineRow(t *testing.T) {
	model, fake := testApp([]config.Server{testServer("alpha")})
	fake.set("http://alpha.test", metricsResult{err: errors.New("connection refused")})

	updateModel(t, model, model.Init()())
	if got := model.table.Rows()[0][1]; got != "offline" {
		t.Fatalf("server status = %q, want offline", got)
	}
	if !strings.Contains(model.message, "could not be reached") {
		t.Fatalf("offline message = %q", model.message)
	}
	if view := model.View().Content; !strings.Contains(view, "connection refused") {
		t.Fatalf("offline view does not include actionable error: %q", view)
	}
}

func TestPartialMetricErrorsKeepServerOnlineAndShowWarning(t *testing.T) {
	model, fake := testApp([]config.Server{testServer("alpha")})
	fake.set("http://alpha.test", metricsResult{response: testMetrics(25, client.MetricError{
		Metric:  "disk",
		Message: "permission denied",
	})})

	updateModel(t, model, model.Init()())
	if got := model.table.Rows()[0][1]; got != "online" {
		t.Fatalf("server status = %q, want online", got)
	}
	if model.message != "Some metrics are unavailable" {
		t.Fatalf("partial metrics message = %q", model.message)
	}
	if view := model.View().Content; !strings.Contains(view, model.message) {
		t.Fatalf("partial-metrics warning missing from view: %q", view)
	}
}

func TestSSHTunnelStateIsShownAndStartupErrorsAreActionable(t *testing.T) {
	target := testServer("remote")
	target.SSH = &config.SSHConfig{Host: "prod", RemotePort: 8787}
	t.Run("connected", func(t *testing.T) {
		model, fake := testApp([]config.Server{target})
		model.tunnels = &fakeTunnelController{}
		fake.set(target.Address, metricsResult{response: testMetrics(25)})
		updateModel(t, model, model.Init()())
		if model.statuses[0].TunnelState != server.TunnelConnected {
			t.Fatalf("tunnel state = %q, want connected", model.statuses[0].TunnelState)
		}
		if view := model.View().Content; !strings.Contains(view, "SSH tunnel: connected") {
			t.Fatalf("connected tunnel state missing from dashboard: %q", view)
		}
	})

	t.Run("startup failure", func(t *testing.T) {
		model, fake := testApp([]config.Server{target})
		model.tunnels = &fakeTunnelController{ensureErr: errors.New("private key rejected")}
		updateModel(t, model, model.Init()())
		if model.statuses[0].TunnelState != server.TunnelDisconnected {
			t.Fatalf("tunnel state = %q, want disconnected", model.statuses[0].TunnelState)
		}
		if view := model.View().Content; !strings.Contains(view, "SSH tunnel: disconnected") || !strings.Contains(view, "private key rejected") {
			t.Fatalf("tunnel failure is not actionable in dashboard: %q", view)
		}
		if got := fake.callCount(); got != 0 {
			t.Fatalf("metrics calls = %d, want no request after tunnel startup fails", got)
		}
	})
}

func TestRefreshClearsErrorAndFetchesAgain(t *testing.T) {
	model, fake := testApp([]config.Server{testServer("alpha")})
	fake.set("http://alpha.test", metricsResult{err: errors.New("connection refused")})
	updateModel(t, model, model.Init()())
	if model.message == "" {
		t.Fatal("expected initial offline error")
	}

	fake.set("http://alpha.test", metricsResult{response: testMetrics(60)})
	_, cmd := model.Update(keyPress("r"))
	if cmd == nil {
		t.Fatal("refresh returned no metrics command")
	}
	if !model.loading {
		t.Fatal("refresh did not enter loading state")
	}
	if model.message != "" {
		t.Fatalf("refresh did not clear old error: %q", model.message)
	}

	updateModel(t, model, cmd())
	if model.loading || model.message != "" {
		t.Fatalf("state after successful refresh: loading=%t message=%q", model.loading, model.message)
	}
	if got := model.table.Rows()[0][2]; got != "60.0%" {
		t.Fatalf("refreshed CPU = %q, want 60.0%%", got)
	}
	if got := fake.callCount(); got != 2 {
		t.Fatalf("metrics calls = %d, want 2", got)
	}
}

func TestKeyboardNavigationAndSelectedServer(t *testing.T) {
	servers := []config.Server{testServer("alpha"), testServer("beta"), testServer("gamma")}
	model, _ := testApp(servers)
	model.statuses = []server.Status{
		{Server: servers[0], Metrics: metricsPtr(testMetrics(10))},
		{Server: servers[1], Metrics: metricsPtr(testMetrics(20))},
		{Server: servers[2], Metrics: metricsPtr(testMetrics(30))},
	}

	for _, input := range []struct {
		key  tea.KeyPressMsg
		want int
	}{
		{keyPress("j"), 1},
		{arrowKey(tea.KeyDown), 2},
		{keyPress("k"), 1},
		{arrowKey(tea.KeyUp), 0},
	} {
		model.Update(input.key)
		if got := model.table.Cursor(); got != input.want {
			t.Fatalf("cursor after %q = %d, want %d", input.key.String(), got, input.want)
		}
	}

	model.Update(keyPress("j"))
	if view := model.View().Content; !strings.Contains(view, "beta") || !strings.Contains(view, "20.0%") {
		t.Fatalf("selected-server details are not rendered: %q", view)
	}
}

func TestQuitKeyReturnsQuitCommand(t *testing.T) {
	model, _ := testApp(nil)
	_, cmd := model.Update(keyPress("q"))
	if cmd == nil {
		t.Fatal("q returned no quit command")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("q command returned %T, want tea.QuitMsg", msg)
	}
}

func TestPasswordPromptMasksAndSubmitsSecret(t *testing.T) {
	model, _ := testApp(nil)
	request := &passwordRequest{server: testServer("remote"), response: make(chan passwordResponse, 1)}
	model.Update(passwordRequestMsg{request: request})
	model.Update(keyPress("sëqret"))
	view := model.View().Content
	if strings.Contains(view, "sëqret") || !strings.Contains(view, "Password: ••••••") {
		t.Fatalf("password prompt did not conceal input: %q", view)
	}

	model.Update(keyPress("enter"))
	response := <-request.response
	if response.err != nil || string(response.password) != "sëqret" {
		t.Fatalf("password response = %q, %v", response.password, response.err)
	}
	if len(model.passwordInput) != 0 || model.passwordRequest != nil {
		t.Fatal("password prompt retained its input after submission")
	}
	clear(response.password)
}

func TestPasswordPromptCanBeCancelled(t *testing.T) {
	model, _ := testApp(nil)
	request := &passwordRequest{server: testServer("remote"), response: make(chan passwordResponse, 1)}
	model.Update(passwordRequestMsg{request: request})
	model.Update(keyPress("secret"))
	model.Update(keyPress("esc"))
	response := <-request.response
	if !errors.Is(response.err, context.Canceled) {
		t.Fatalf("cancel response error = %v, want context cancellation", response.err)
	}
	if len(model.passwordInput) != 0 || model.passwordRequest != nil {
		t.Fatal("cancelled password prompt retained its input")
	}
}

type metricsResult struct {
	response client.MetricsResponse
	err      error
}

type fakeMetricsClient struct {
	mu        sync.Mutex
	responses map[string]metricsResult
	calls     []string
}

type fakeTunnelController struct {
	ensureErr    error
	reconnectErr error
}

func (f *fakeTunnelController) Ensure(context.Context, config.Server) error {
	return f.ensureErr
}

func (*fakeTunnelController) EnsureWithPassword(context.Context, config.Server, []byte) error {
	return nil
}

func (f *fakeTunnelController) Reconnect(context.Context, config.Server) error {
	return f.reconnectErr
}

func (f *fakeMetricsClient) Metrics(_ context.Context, address string) (client.MetricsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, address)
	result := f.responses[address]
	return result.response, result.err
}

func (f *fakeMetricsClient) set(address string, result metricsResult) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.responses[address] = result
}

func (f *fakeMetricsClient) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func testApp(servers []config.Server) (*app, *fakeMetricsClient) {
	fake := &fakeMetricsClient{responses: make(map[string]metricsResult)}
	model := newApp(config.Config{Version: 1, Servers: servers}, "test-config.yaml", "test")
	model.apiClient = fake
	return model, fake
}

func testServer(name string) config.Server {
	return config.Server{Name: name, Address: "http://" + name + ".test"}
}

func testMetrics(cpuUsage float64, metricErrors ...client.MetricError) client.MetricsResponse {
	return client.MetricsResponse{Data: client.MetricsData{
		Hostname: "test-host",
		OS:       client.OSInfo{Name: "Test OS", PlatformVersion: "1"},
		CPU:      client.CPU{UsagePercent: cpuUsage, LogicalCores: 4},
		Memory:   client.Memory{UsedBytes: 512, TotalBytes: 1024, UsedPercent: 50},
		Disk:     client.Disk{UsedBytes: 1024, TotalBytes: 2048, UsedPercent: 50},
		Uptime:   3661,
		Errors:   metricErrors,
	}}
}

func metricsPtr(response client.MetricsResponse) *client.MetricsResponse {
	return &response
}

func updateModel(t *testing.T, model *app, msg tea.Msg) {
	t.Helper()
	if msg == nil {
		t.Fatal("command returned no message")
	}
	model.Update(msg)
}

func keyPress(text string) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Text: text, Code: rune(text[0])})
}

func arrowKey(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: code})
}
