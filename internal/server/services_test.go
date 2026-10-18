package server

import (
	"context"
	"errors"
	"testing"

	"github.com/ch55secake/orbit/internal/client"
	"github.com/ch55secake/orbit/internal/config"
	"github.com/ch55secake/orbit/internal/tunnel"
)

func TestServiceReadsUseManagedTunnelAndPasswordWorkflow(t *testing.T) {
	target := sshServer()
	if _, _, err := ReadServices(context.Background(), target, nil, nil, client.ServiceQuery{}, ""); err == nil {
		t.Fatal("missing tunnel manager accepted")
	}
	tunnels := &fakeTunnelManager{ensureErr: tunnel.ErrPasswordRequired}
	_, _, err := ReadServices(context.Background(), target, tunnels, func(context.Context, config.Server) ([]byte, error) { return nil, context.Canceled }, client.ServiceQuery{}, "")
	if !errors.Is(err, context.Canceled) || tunnels.ensures != 1 {
		t.Fatalf("tunnel/password path not used: %v", err)
	}
}

func TestServiceLogReadsUseManagedTunnelAndPasswordWorkflow(t *testing.T) {
	target := sshServer()
	if _, err := ReadServiceLogs(context.Background(), target, nil, nil, "worker.service", client.ServiceLogQuery{}); err == nil {
		t.Fatal("missing tunnel manager accepted")
	}
	tunnels := &fakeTunnelManager{ensureErr: tunnel.ErrPasswordRequired}
	_, err := ReadServiceLogs(context.Background(), target, tunnels, func(context.Context, config.Server) ([]byte, error) { return nil, context.Canceled }, "worker.service", client.ServiceLogQuery{})
	if !errors.Is(err, context.Canceled) || tunnels.ensures != 1 {
		t.Fatalf("tunnel/password path not used: %v", err)
	}
}
