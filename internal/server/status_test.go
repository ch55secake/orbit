package server

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ch55secake/orbit/internal/client"
	"github.com/ch55secake/orbit/internal/config"
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
	ensures      int
	reconnects   int
	ensureErr    error
	reconnectErr error
}

func (f *fakeTunnelManager) Ensure(context.Context, config.Server) error {
	f.ensures++
	return f.ensureErr
}

func (f *fakeTunnelManager) Reconnect(context.Context, config.Server) error {
	f.reconnects++
	return f.reconnectErr
}

func sshServer() config.Server {
	return config.Server{
		Name:    "remote",
		Address: "http://127.0.0.1:18787",
		SSH:     &config.SSHConfig{Host: "prod", RemotePort: 8787},
	}
}
