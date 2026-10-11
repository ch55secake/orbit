package server

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ch55secake/orbit/internal/client"
	"github.com/ch55secake/orbit/internal/config"
	"github.com/ch55secake/orbit/internal/tunnel"
)

func TestFetchMetricsStartsTunnelAndReconnectsAfterRequestFailure(t *testing.T) {
	target := sshServer()
	api := &fakeMetricsClient{results: []metricsResult{
		{err: errors.New("connection reset")},
		{response: client.MetricsResponse{Data: client.MetricsData{Hostname: "remote"}}},
	}}
	tunnels := &fakeTunnelManager{}

	status := FetchMetrics(context.Background(), target, api, tunnels)
	if status.Error != nil || status.Metrics == nil || status.TunnelState != TunnelConnected {
		t.Fatalf("status = %+v, want connected tunnel and metrics", status)
	}
	if api.calls != 2 || tunnels.ensures != 1 || tunnels.reconnects != 1 {
		t.Fatalf("calls: metrics=%d ensure=%d reconnect=%d", api.calls, tunnels.ensures, tunnels.reconnects)
	}
}

func TestFetchMetricsReportsTunnelStartupAndReconnectErrors(t *testing.T) {
	t.Run("startup", func(t *testing.T) {
		tunnels := &fakeTunnelManager{ensureErr: errors.New("key rejected")}
		status := FetchMetrics(context.Background(), sshServer(), &fakeMetricsClient{}, tunnels)
		if status.TunnelState != TunnelDisconnected || status.Error == nil || !strings.Contains(status.Error.Error(), "key rejected") {
			t.Fatalf("startup status = %+v", status)
		}
	})

	t.Run("reconnect", func(t *testing.T) {
		api := &fakeMetricsClient{results: []metricsResult{{err: errors.New("connection reset")}}}
		tunnels := &fakeTunnelManager{reconnectErr: errors.New("port is occupied")}
		status := FetchMetrics(context.Background(), sshServer(), api, tunnels)
		if status.TunnelState != TunnelDisconnected || status.Error == nil || !strings.Contains(status.Error.Error(), "port is occupied") {
			t.Fatalf("reconnect status = %+v", status)
		}
		if api.calls != 1 {
			t.Fatalf("metrics calls = %d, want no retry after failed reconnect", api.calls)
		}
	})
}

func TestFetchMetricsKeepsDirectHTTPBehavior(t *testing.T) {
	api := &fakeMetricsClient{results: []metricsResult{{err: errors.New("connection refused")}}}
	tunnels := &fakeTunnelManager{}
	status := FetchMetrics(context.Background(), config.Server{Name: "direct", Address: "http://127.0.0.1:8787"}, api, tunnels)
	if status.Error == nil || status.TunnelState != "" || tunnels.ensures != 0 || tunnels.reconnects != 0 {
		t.Fatalf("direct status = %+v; tunnel calls ensure=%d reconnect=%d", status, tunnels.ensures, tunnels.reconnects)
	}
}

func TestFetchMetricsPromptsOnlyAfterAuthenticationIsRejected(t *testing.T) {
	target := sshServer()
	api := &fakeMetricsClient{results: []metricsResult{{response: client.MetricsResponse{Data: client.MetricsData{Hostname: "remote"}}}}}
	tunnels := &fakeTunnelManager{ensureErr: tunnel.ErrPasswordRequired, passwordErrs: []error{tunnel.ErrPasswordRequired, nil}}
	promptCalls := 0
	var supplied []byte
	prompt := func(context.Context, config.Server) ([]byte, error) {
		promptCalls++
		supplied = []byte("temporary secret")
		return supplied, nil
	}

	status := FetchMetricsWithPrompt(context.Background(), target, api, tunnels, prompt)
	if status.Error != nil || status.Metrics == nil || promptCalls != 2 || tunnels.passwordCalls != 2 {
		t.Fatalf("status=%+v prompt calls=%d password attempts=%d", status, promptCalls, tunnels.passwordCalls)
	}
	if string(supplied) != strings.Repeat("\x00", len(supplied)) {
		t.Fatal("password buffer was not cleared after tunnel authentication")
	}
}

func TestFetchMetricsPasswordPromptCancellationAndAttemptLimit(t *testing.T) {
	t.Run("cancelled", func(t *testing.T) {
		tunnels := &fakeTunnelManager{ensureErr: tunnel.ErrPasswordRequired}
		api := &fakeMetricsClient{}
		status := FetchMetricsWithPrompt(context.Background(), sshServer(), api, tunnels, func(context.Context, config.Server) ([]byte, error) {
			return nil, context.Canceled
		})
		if status.Error == nil || !strings.Contains(status.Error.Error(), "context canceled") || api.calls != 0 {
			t.Fatalf("cancelled prompt status=%+v metrics calls=%d", status, api.calls)
		}
	})

	t.Run("bounded wrong passwords", func(t *testing.T) {
		tunnels := &fakeTunnelManager{ensureErr: tunnel.ErrPasswordRequired, passwordErrs: []error{tunnel.ErrPasswordRequired, tunnel.ErrPasswordRequired, tunnel.ErrPasswordRequired}}
		api := &fakeMetricsClient{}
		promptCalls := 0
		status := FetchMetricsWithPrompt(context.Background(), sshServer(), api, tunnels, func(context.Context, config.Server) ([]byte, error) {
			promptCalls++
			return []byte("wrong"), nil
		})
		if status.Error == nil || !strings.Contains(status.Error.Error(), "after 3 password attempts") || promptCalls != 3 || api.calls != 0 {
			t.Fatalf("bounded retry status=%+v prompt calls=%d metrics calls=%d", status, promptCalls, api.calls)
		}
	})
}

func TestFetchMetricsReconnectCanPromptForPassword(t *testing.T) {
	target := sshServer()
	api := &fakeMetricsClient{results: []metricsResult{
		{err: errors.New("connection reset")},
		{response: client.MetricsResponse{Data: client.MetricsData{Hostname: "remote"}}},
	}}
	tunnels := &fakeTunnelManager{reconnectErr: tunnel.ErrPasswordRequired}
	promptCalls := 0
	status := FetchMetricsWithPrompt(context.Background(), target, api, tunnels, func(context.Context, config.Server) ([]byte, error) {
		promptCalls++
		return []byte("temporary"), nil
	})
	if status.Error != nil || status.Metrics == nil || promptCalls != 1 || tunnels.reconnects != 1 || tunnels.passwordCalls != 1 {
		t.Fatalf("status=%+v prompts=%d reconnects=%d password attempts=%d", status, promptCalls, tunnels.reconnects, tunnels.passwordCalls)
	}
}

type metricsResult struct {
	response client.MetricsResponse
	err      error
}

type fakeMetricsClient struct {
	results []metricsResult
	calls   int
}

func (f *fakeMetricsClient) Metrics(context.Context, string) (client.MetricsResponse, error) {
	index := f.calls
	f.calls++
	if index >= len(f.results) {
		return client.MetricsResponse{}, errors.New("unexpected metrics request")
	}
	return f.results[index].response, f.results[index].err
}

type fakeTunnelManager struct {
	ensures       int
	reconnects    int
	passwordCalls int
	ensureErr     error
	reconnectErr  error
	passwordErrs  []error
}

func (f *fakeTunnelManager) Ensure(context.Context, config.Server) error {
	f.ensures++
	return f.ensureErr
}

func (f *fakeTunnelManager) Reconnect(context.Context, config.Server) error {
	f.reconnects++
	return f.reconnectErr
}

func (f *fakeTunnelManager) EnsureWithPassword(_ context.Context, _ config.Server, password []byte) error {
	f.passwordCalls++
	if len(password) == 0 {
		return errors.New("empty password")
	}
	index := f.passwordCalls - 1
	if index < len(f.passwordErrs) {
		return f.passwordErrs[index]
	}
	return nil
}

func sshServer() config.Server {
	return config.Server{
		Name:    "remote",
		Address: "http://127.0.0.1:18787",
		SSH:     &config.SSHConfig{Host: "prod", RemotePort: 8787},
	}
}
