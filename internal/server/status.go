package server

import (
	"context"
	"fmt"

	"github.com/ch55secake/orbit/internal/client"
	"github.com/ch55secake/orbit/internal/config"
)

const (
	TunnelConnected    = "connected"
	TunnelDisconnected = "disconnected"
)

type Status struct {
	Server      config.Server
	Metrics     *client.MetricsResponse
	TunnelState string
	Error       error
}

type MetricsClient interface {
	Metrics(context.Context, string) (client.MetricsResponse, error)
}

type TunnelManager interface {
	Ensure(context.Context, config.Server) error
	Reconnect(context.Context, config.Server) error
}

func FetchMetrics(ctx context.Context, target config.Server, apiClient MetricsClient, tunnels TunnelManager) Status {
	status := Status{Server: target}
	if target.SSH != nil {
		status.TunnelState = TunnelDisconnected
		if tunnels == nil {
			status.Error = fmt.Errorf("SSH tunnel manager is unavailable")
			return status
		}
		if err := tunnels.Ensure(ctx, target); err != nil {
			status.Error = fmt.Errorf("start SSH tunnel: %w", err)
			return status
		}
		status.TunnelState = TunnelConnected
	}

	metrics, err := apiClient.Metrics(ctx, target.Address)
	if err != nil && target.SSH != nil {
		if reconnectErr := tunnels.Reconnect(ctx, target); reconnectErr != nil {
			status.TunnelState = TunnelDisconnected
			status.Error = fmt.Errorf("metrics request failed: %v; SSH tunnel reconnect failed: %w", err, reconnectErr)
			return status
		}
		metrics, err = apiClient.Metrics(ctx, target.Address)
		if err != nil {
			status.Error = fmt.Errorf("metrics request failed after SSH tunnel reconnect: %w", err)
			return status
		}
	} else if err != nil {
		status.Error = err
		return status
	}
	status.Metrics = &metrics
	return status
}
