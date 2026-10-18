# Browsing systemd services

Orbit uses Stardust's read-only service inventory API, not `systemctl` over SSH.
The managed SSH tunnel/password workflow can carry API traffic; no interactive
shell is opened. Logs and restart controls remain separate features.

## Setup

Use a Stardust version with the inventory API (introduced in
[Stardust #46](https://github.com/ch55secake/stardust/pull/46)). On the agent,
explicitly configure its private service policy, random service read token, and
**exact unit allowlist**, following the
[producer contract](https://github.com/ch55secake/stardust/blob/main/docs/services-api.md).
Inventory is disabled by default and supports systemd Linux, not macOS or
non-systemd Linux. Only allowlisted units are visible.

On the machine running Orbit, put **only the token** in a private regular file
owned by your user, mode `0400` or `0600`. A trailing newline is accepted; JSON,
symlinks, group-readable files, and tokens containing spaces are not. Protect its
parent directory. Never put the token itself in arguments, YAML, URLs, source
control, screenshots, or logs. Reference the file for the intended server:

```sh
orbit servers edit --name production --service-token-file /absolute/private/path/service-token
```

The flag also works when adding servers. YAML uses `service_token_file`: an
absolute **path**, not a secret. An empty flag clears it. Editing a server's API
address clears the old reference unless `--service-token-file` is explicitly
provided again, preventing accidental reuse with another server. If manually
editing YAML or changing SSH routing, keep credentials matched to the agent.

Service credentials require HTTPS or numeric-loopback HTTP (for example,
`http://127.0.0.1:18787` through a trusted SSH tunnel). Plain remote HTTP is
refused. Redirects are **never followed**, including another port on the same
host. Tokens are not sent to capabilities, metrics, or health, nor shared across
servers. Orbit reloads the token file for each read, allowing rotation alongside
the agent policy. This token grants service **read access only**.

## CLI

```sh
orbit services list production
orbit services list production --filter worker --limit 10
orbit services list production --filter worker --limit 10 --cursor worker@blue.service
orbit services show production worker@blue.service
```

`list` prints one bounded page (1–50 units, default 20). Echo the returned cursor
as `--cursor` with the same filter; do not infer cursor values. Filtering is a
case-sensitive unit-name substring. `show` reads current canonical identity,
description, load/active/sub states, unit-file state, main PID, and optional
process name/effective UID.

## TUI

Select a fleet server and press **v**. The service browser retains fleet/server
context; tall layouts retain the Saturn masthead. Controls:

| Key | Action |
| --- | --- |
| Up/Down or k/j; Home/End | Select a unit within the page |
| Enter | Fetch live details for the selected unit |
| r | Refresh the page, or the inspected unit |
| Right/Left, l/h, PageDown/PageUp | Next/previous page |
| / | Edit a name filter; Enter applies, Esc cancels |
| Tab / Shift+Tab | Next/previous fleet server |
| Esc or v | Return to the existing metrics dashboard |
| q / Ctrl+C | Quit |

Rows/details are bounded and truncated in smaller terminals; selection remains
visible while navigating. For a long description, use CLI detail output.
Filtering resets pagination. Only the current page and a bounded cursor history
are retained. Switching servers, refreshing, or leaving cancels pending reads;
late responses cannot overwrite another server's inventory. There is no
background service polling or automatic action.

## State and errors

- Missing configured units remain in lists with `load_state: not-found`. Detail
  lookup can return `unit_not_found`; refresh after removal.
- A zero main PID means **no main process**. States/PIDs are live observations,
  not stable handles or application health checks. Process identity may be
  absent when a process exits during collection.
- Disabled inventory, rejected credentials, a forbidden unit, insufficient
  systemd permissions, unavailable backends, and timeouts have distinct messages.
  Check agent configuration rather than granting Orbit broader access.
- Older agents and unsupported hosts show actionable capability messages. The
  dashboard and existing SSH features remain available. There is no SSH-command
  fallback.
- Remote text is untrusted: terminal controls and record separators are
  neutralized. Credentials reflected in service data are redacted; raw remote
  error messages are never printed.

Tests use local HTTP servers and mocks only; no real services are changed.
