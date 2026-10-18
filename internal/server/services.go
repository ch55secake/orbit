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

// ReadServiceRestartAccess validates the advertised restart capability and the
// exact unit's restart authorization using the read-only inventory credential.
func ReadServiceRestartAccess(ctx context.Context, target config.Server, tunnels TunnelManager, prompt PasswordPrompt, unit string) (client.ServiceCapabilities, client.ServiceUnit, error) {
	api, err := newServiceRestartAPI(ctx, target, tunnels, prompt)
	if err != nil {
		return client.ServiceCapabilities{}, client.ServiceUnit{}, err
	}
	capabilities, err := api.Capabilities(ctx)
	if err != nil {
		return capabilities, client.ServiceUnit{}, err
	}
	if !capabilities.Supported {
		return capabilities, client.ServiceUnit{}, &client.ServiceError{Code: "restart_unsupported"}
	}
	if !capabilities.Restart {
		return capabilities, client.ServiceUnit{}, &client.ServiceError{Code: "restart_disabled"}
	}
	if capabilities.RestartAuthentication != "bearer" {
		return capabilities, client.ServiceUnit{}, errors.New("unsupported service restart authentication scheme; upgrade Orbit/Stardust")
	}
	unitInfo, err := api.Detail(ctx, unit)
	if err != nil {
		return capabilities, client.ServiceUnit{}, err
	}
	if !unitInfo.RestartAllowed {
		return capabilities, unitInfo, &client.ServiceError{Code: "restart_forbidden"}
	}
	return capabilities, unitInfo, nil
}

// StartServiceRestart rechecks capability and per-unit authorization directly
// before submitting the mutation. The supplied idempotency key must be reused
// for any retry of this one user-confirmed operation.
func StartServiceRestart(ctx context.Context, target config.Server, tunnels TunnelManager, prompt PasswordPrompt, unit, key string) (client.ServiceRestartOperation, error) {
	if _, _, err := ReadServiceRestartAccess(ctx, target, tunnels, prompt, unit); err != nil {
		return client.ServiceRestartOperation{}, err
	}
	api, err := newServiceRestartAPI(ctx, target, tunnels, prompt)
	if err != nil {
		return client.ServiceRestartOperation{}, err
	}
	return api.Restart(ctx, target.ServiceRestartTokenFile, unit, key)
}

func ReadServiceRestartStatus(ctx context.Context, target config.Server, tunnels TunnelManager, prompt PasswordPrompt, operationID string) (client.ServiceRestartOperation, error) {
	api, err := newServiceRestartAPI(ctx, target, tunnels, prompt)
	if err != nil {
		return client.ServiceRestartOperation{}, err
	}
	return api.RestartStatus(ctx, target.ServiceRestartTokenFile, operationID)
}

func newServiceRestartAPI(ctx context.Context, target config.Server, tunnels TunnelManager, prompt PasswordPrompt) (*client.ServicesClient, error) {
	api, err := client.NewServices(target.Address, target.ServiceTokenFile, 8*time.Second)
	if err != nil {
		return nil, err
	}
	if target.SSH != nil {
		if tunnels == nil {
			return nil, errors.New("SSH tunnel manager is unavailable")
		}
		if err := prepareTunnel(ctx, target, tunnels, prompt, false); err != nil {
			return nil, err
		}
	}
	return api, nil
}
