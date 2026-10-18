package server

import (
	"context"
	"errors"
	"time"

	"github.com/ch55secake/orbit/internal/client"
	"github.com/ch55secake/orbit/internal/config"
)

// ReadServices uses the normal managed tunnel/password workflow, never a remote
// shell command. The dedicated client keeps credentials scoped to this server.
func ReadServices(ctx context.Context, target config.Server, tunnels TunnelManager, prompt PasswordPrompt, query client.ServiceQuery, unit string) (client.ServicePage, *client.ServiceUnit, error) {
	api, err := client.NewServices(target.Address, target.ServiceTokenFile, 5*time.Second)
	if err != nil {
		return client.ServicePage{}, nil, err
	}
	if target.SSH != nil {
		if tunnels == nil {
			return client.ServicePage{}, nil, errors.New("SSH tunnel manager is unavailable")
		}
		if err := prepareTunnel(ctx, target, tunnels, prompt, false); err != nil {
			return client.ServicePage{}, nil, err
		}
	}
	if unit != "" {
		detail, err := api.Detail(ctx, unit)
		if err != nil {
			return client.ServicePage{}, nil, err
		}
		return client.ServicePage{}, &detail, nil
	}
	page, err := api.List(ctx, query)
	return page, nil, err
}

func ReadServiceLogs(ctx context.Context, target config.Server, tunnels TunnelManager, prompt PasswordPrompt, unit string, query client.ServiceLogQuery) (client.ServiceLogPage, error) {
	api, err := client.NewServices(target.Address, "", 5*time.Second)
	if err != nil {
		return client.ServiceLogPage{}, err
	}
	if target.SSH != nil {
		if tunnels == nil {
			return client.ServiceLogPage{}, errors.New("SSH tunnel manager is unavailable")
		}
		if err := prepareTunnel(ctx, target, tunnels, prompt, false); err != nil {
			return client.ServiceLogPage{}, err
		}
	}
	return api.Logs(ctx, target.ServiceLogTokenFile, unit, query)
}
