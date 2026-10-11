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
- managed SSH local port forwarding for remote Stardust agents
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

Add a directly reachable Stardust agent:

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

For a remote agent, keep Stardust bound to loopback and let Orbit manage the
forward. The address is the local loopback endpoint; `--ssh-host` is an SSH
host name or `ssh_config` alias, and `--ssh-remote-port` is the Stardust port
on the remote host:

```sh
orbit servers add \
  --name server-01 \
  --address http://127.0.0.1:18787 \
  --ssh-host server-01 \
  --ssh-user ubuntu \
  --ssh-remote-port 8787 \
  --ssh-identity-file ~/.ssh/id_ed25519
orbit status
orbit
```

SSH settings can also be stored under each server's `ssh` key in YAML. The SSH
username defaults to the local OpenSSH configuration, the SSH port defaults to
22, and the forwarded Stardust host defaults to `127.0.0.1`. `orbit status`
starts the tunnel for that command and stops it afterward; the TUI keeps it
open until exit and reconnects on refresh after a disconnect. Tunnel addresses
must use a loopback IP and each managed tunnel needs a unique local port. Use
`orbit servers edit --name server-01 --direct` to remove the tunnel settings.

Orbit starts OpenSSH in non-interactive forwarding-only mode (`ssh -N -L`); it
does not invoke a shell or send a remote command. Configure key-based SSH
authentication through an identity file, an agent, or your OpenSSH config.

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

The shared release workflow automatically publishes a semantic-versioned
release on pushes to `main` and can also be started manually from GitHub
Actions. It selects the next patch version, starting at `v1.0.0` when no
release exists.
