package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/ch55secake/orbit/internal/client"
	"github.com/ch55secake/orbit/internal/config"
	"github.com/ch55secake/orbit/internal/tunnel"
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
	EnsureWithPassword(context.Context, config.Server, []byte) error
	Reconnect(context.Context, config.Server) error
}

type PasswordPrompt func(context.Context, config.Server) ([]byte, error)

func FetchMetrics(ctx context.Context, target config.Server, apiClient MetricsClient, tunnels TunnelManager) Status {
	return FetchMetricsWithPrompt(ctx, target, apiClient, tunnels, nil)
}

func FetchMetricsWithPrompt(ctx context.Context, target config.Server, apiClient MetricsClient, tunnels TunnelManager, prompt PasswordPrompt) Status {
	status := Status{Server: target}
	if target.SSH != nil {
		status.TunnelState = TunnelDisconnected
		if tunnels == nil {
			status.Error = fmt.Errorf("SSH tunnel manager is unavailable")
			return status
		}
		if err := prepareTunnel(ctx, target, tunnels, prompt, false); err != nil {
			status.Error = fmt.Errorf("start SSH tunnel: %w", err)
			return status
		}
		status.TunnelState = TunnelConnected
	}

	metrics, err := apiClient.Metrics(ctx, target.Address)
	if err != nil && target.SSH != nil {
		if reconnectErr := prepareTunnel(ctx, target, tunnels, prompt, true); reconnectErr != nil {
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

func prepareTunnel(ctx context.Context, target config.Server, tunnels TunnelManager, prompt PasswordPrompt, reconnect bool) error {
	var err error
	if reconnect {
		err = tunnels.Reconnect(ctx, target)
	} else {
		err = tunnels.Ensure(ctx, target)
	}
	for attempts := 0; errors.Is(err, tunnel.ErrPasswordRequired); attempts++ {
		if prompt == nil {
			return err
		}
		if attempts == 3 {
			return fmt.Errorf("SSH authentication failed after %d password attempts: %w", attempts, err)
		}
		password, promptErr := prompt(ctx, target)
		if promptErr != nil {
			clear(password)
			return fmt.Errorf("request SSH password: %w", promptErr)
		}
		if password == nil {
			return errors.New("SSH password prompt returned no password")
		}
		err = tunnels.EnsureWithPassword(ctx, target, password)
		clear(password)
	}
	return err
}
