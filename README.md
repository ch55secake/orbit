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
- `orbit ssh <server>`: open an SSH shell using the configured connection
- No hosted service required; remote shells are opened only on explicit request

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

Managed tunnels start OpenSSH in forwarding-only mode (`ssh -N -L`); they
do not invoke a shell or send a remote command. Configure key-based SSH
authentication through an identity file, an agent, or your OpenSSH config.
If key/agent authentication is rejected, the TUI prompts for a concealed SSH
password, and interactive `orbit status` does the same. Passwords are kept only
in memory for that connection attempt and are never written to the server
configuration. Non-interactive `orbit status` requires key/agent authentication.

An example configuration is in `examples/config.yaml`.

Open a shell with `orbit ssh mele` or press `s` in the dashboard. OpenSSH handles
host-key checks and password prompts using the existing host, user, port, and
identity settings. Shell sessions are separate from metrics tunnels and do not
inherit SSH forwarding rules. On Linux and macOS, the dashboard opens a framed
terminal in place of the resource cards, keeping the fleet visible. Authentication
prompts, shell input, pasted text, and `Ctrl+C` stay inside that terminal. It resizes
with the dashboard and supports terminal applications with an alternate screen.
Press `Ctrl+]` to disconnect and restore metrics. When the shell exits, its final
output and exit status remain visible until you close the frame. Dashboard shortcuts
are inactive while the shell has focus; sessions are not saved and there is no
scrollback or mouse forwarding to remote applications. The CLI shell remains a
normal full-terminal session.

Drag with the left mouse button to select text inside the SSH pane. The frame,
padding, and other dashboard panels are excluded. Press `Ctrl+Y` to copy the
selection (requires terminal OSC 52 clipboard support), or Esc to clear it.
`Ctrl+Shift+C` also works if your terminal forwards that shortcut to Orbit. The
displayed shell snapshot freezes while selected; the remote process keeps running.
Typing, pasting, or resizing clears the selection and restores live output. Native
terminal selection overrides, such as Shift-drag, remain controlled by your terminal.

If the window becomes too small for the shell frame, Orbit shows a compact resize
warning and keeps the session at its last usable size. Shell input is paused until
you enlarge the window; `Ctrl+]` remains available to disconnect.

## Update a remote Stardust service

```sh
orbit stardust update mele --yes
orbit stardust update mele --version v1.2.3 --yes
```

Updates require an existing Linux `stardust.service` or macOS system LaunchDaemon
`com.ch55secake.stardust`, SSH access, and root or sudo privileges. OpenSSH and
sudo handle passwords interactively; Orbit never stores them. `--yes` explicitly
confirms binary replacement and service restart. The agent must run on the SSH
host's loopback interface, not a different machine behind that host.

In the dashboard, press `u` on an SSH-configured server, then `y` to confirm an
update to the latest release. Enter, `n`, or Esc cancels without connecting.
SSH/sudo prompts run in the terminal; Orbit returns to the dashboard, refreshes
metrics, and displays the outcome afterward. Use the CLI to select a specific tag.

The updater detects the remote OS/architecture, downloads a published release,
verifies its SHA-256 checksum, atomically replaces the binary used by the existing
service, and checks `/api/v1/health` on the configured remote port. Listener ports,
service arguments, and service accounts are preserved. Restart or health failures
restore the previous binary and attempt to restart it; rollback failures report
the retained backup path. Missing services, custom executable names, and
package-managed symlinks are rejected instead of overwritten. curl, tar, Bash,
and sha256sum or shasum must be available remotely. No local/source-build uploads
or automatic background updates are performed.

## Dashboard controls

Press **v** to browse the selected server's authorized systemd services, or use
`orbit services list <server>` / `orbit services show <server> <unit.service>`.
See [Browsing systemd services](docs/services.md) for setup and controls. This
requires opted-in Stardust inventory and a private per-server `service_token_file`;
it never opens an SSH shell or restarts services.

- `↑`/`k`, `↓`/`j`: move through servers
- `PgUp`/`PgDown`: move a page; `Home`/`End`: jump to the first/last server
- `r`: refresh all metrics
- `s`: open the selected server's SSH shell inside the resource pane
- `Ctrl+]`: close the embedded SSH shell and restore metrics
- `u`: confirm an update of the selected server's Stardust service to latest
- `?`: toggle help and the active configuration path
- `q` or `Ctrl-C`: exit

The dashboard reports loading and connection errors without hiding the rest of
the configured servers.
Wide terminals place the server list beside the selected server's metric cards;
smaller terminals stack the sections and keep full metrics in the detail view.
Taller terminals expand the metric cards, with ASCII Saturn artwork when space
allows, and show cumulative network counters plus the agent's sample time.
Short terminals keep the compact layout; network counters are totals, not
transfer rates.
On wide, tall terminals the fleet sits beside Saturn, rather than below it.
The fleet summary shows the selected server's position in the list.

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

Versioned response fixtures and client compatibility tests are in
`internal/client/testdata/stardust/v1`.

The dashboard also accepts the proposed optional `data.swap` object with
`total_bytes`, `used_bytes`, `free_bytes`, and `used_percent`. When supplied, it
shows used/total capacity and percentage below the resource cards (next to memory
in the compact summary). Zero capacity means "not configured"; a `swap` collector
error or invalid sample is "unavailable". Omitted/null swap data without an error
is hidden, preserving older agents' display. Agent-side support is tracked in
[stardust#28](https://github.com/ch55secake/stardust/issues/28) and is not yet
implemented; the new swap fixtures model the proposed extension.

The optional proposed `data.filesystems` array adds `mount_point`, optional
`filesystem`/`device` identity, `used_bytes`, `total_bytes`, `available_bytes`, and
`used_percent` without replacing the existing `disk` metric. The dashboard shows
up to two mounts, prioritizing the fullest valid samples, with used/total capacity
in wide layouts; additional mounts are counted. Paths are truncated to keep the
list bounded. Absent/null/empty
lists preserve older agents' display. Invalid samples show "unavailable"; a
`filesystems` collector error marks supplied samples as partial without hiding
healthy mounts or unrelated metrics. Available space may exclude reserved blocks,
so used plus available need not equal total. Agent-side collection/filtering is
still tracked in [stardust#27](https://github.com/ch55secake/stardust/issues/27);
the filesystem fixtures model the proposed extension, not a released-agent payload.

When CPU and other supplemental metrics compete for limited space, filesystem
details compact to a single row showing the fullest mount and a count of omitted
entries. Enlarge the window to restore the bounded mount list.

Orbit also accepts the proposed optional `data.network.interfaces` array. Each
entry has `name`, `bytes_received`, `bytes_sent`, `packets_received`, and
`packets_sent`. The selected-server detail view shows RX/TX byte totals for the
first two supplied interfaces and a count of additional entries, keeping virtual
interface lists from pushing other metrics off-screen. Missing, null, or empty
lists leave older agents' display unchanged; a network collector error marks
interface readings unavailable. Counters are cumulative, can reset, and may count
the same traffic on multiple interfaces; they are not transfer rates and are not
summed by Orbit. Agent-side support remains tracked in
[stardust#29](https://github.com/ch55secake/stardust/issues/29); the interface
fixture models the proposed extension, not a released-agent payload.

The proposed CPU extension adds `data.cpu.per_core_usage_percent` (an array whose
indices identify logical cores) and `data.cpu.load_average` with `one_minute`,
`five_minutes`, and `fifteen_minutes`. The aggregate CPU reading is unchanged.
The detail view shows the busiest four cores (two in narrow panes), preserving
core indices and counting additional entries. Missing/null samples and invalid
percentages are shown as `—`, not idle cores; omitted/empty optional fields add
no display. Load averages are nonnegative scheduling-pressure values, not
percentages, and can exceed 100. Orbit displays agent samples without deriving
them from aggregate usage or resampling them. When space is tight, aggregate
network telemetry and interface details may be compacted or hidden to retain
CPU details and the core resource readings; enlarge the window for more detail.
Producer-side sampling/platform support remains tracked in
[stardust#26](https://github.com/ch55secake/stardust/issues/26) and is not yet
implemented; the CPU fixtures model this proposed extension.

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
