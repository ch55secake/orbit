# Orbit

Orbit is terminal-native mission control for your servers. It is a local-first
Go CLI/TUI that reads a small YAML configuration and talks to Stardust agents
over HTTP, normally through SSH port forwarding.

## Features

- `orbit`: dark terminal dashboard with keyboard navigation and refresh
- `orbit version`: print the application version
- `orbit servers list`: list configured servers
- `orbit servers add`: register a Stardust endpoint locally
- `orbit servers edit`: update a registered Stardust endpoint
- `orbit servers remove`: remove a registered Stardust endpoint
- `orbit status`: fetch CPU, memory, disk, and uptime metrics
- No remote command execution and no hosted service required

## Install and run

Requirements: Go 1.26 or newer.

```sh
make build
./bin/orbit version
```

Orbit stores configuration in the platform user config directory. On Linux
this is normally `~/.config/orbit/config.yaml`; on macOS it follows
`os.UserConfigDir`. Set `ORBIT_CONFIG` or pass `--config` to override it.

Add a local or SSH-forwarded Stardust agent:

```sh
orbit servers add \
  --name server-01 \
  --address http://127.0.0.1:8787 \
  --description "Production web server"
orbit servers edit \
  --name server-01 \
  --address https://127.0.0.1:8787 \
  --description "Production web server (TLS)"
orbit servers list
orbit status
orbit
```

Server addresses must be absolute `http://` or `https://` URLs with a host.
Credentials, query strings, and fragments are not accepted. Remove a server
with `orbit servers remove --name server-01`.

For a remote agent, keep Stardust bound to loopback and create a tunnel:

```sh
ssh -N -L 8787:127.0.0.1:8787 user@server-01
```

An example configuration is in `examples/config.yaml`.

## Dashboard controls

- `↑`/`k`, `↓`/`j`: move through servers
- `r`: refresh all metrics
- `q` or `Ctrl-C`: exit

The dashboard reports loading and connection errors without hiding the rest of
the configured servers.

## Stardust API contract

Orbit consumes Stardust's `/api/v1/health` and `/api/v1/metrics` endpoints.
Responses include an `api_version` and RFC 3339 UTC `timestamp`. Metric units
are explicit: byte fields are bytes, percentages are 0-100, and uptime is
seconds. Partial metric availability is represented by `data.errors`; an HTTP
503 means the complete collection exceeded its timeout. API errors use:

```json
{
  "api_version": "v1",
  "error": {"code": "not_found", "message": "endpoint not found"}
}
```

## Development

```sh
make fmt
make test
make build
make lint
```

The release workflow creates a prerelease tag (`v0.0.0-main.<run>`) and
publishes automatically for pushes to `main`. Explicit version tags matching
`v*` are also released, and the workflow can be started manually from GitHub
Actions.
