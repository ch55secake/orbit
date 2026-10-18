# Browsing systemd services

Orbit uses Stardust's read-only service inventory and journal APIs, not
`systemctl` or `journalctl` over SSH. The managed SSH tunnel/password workflow
can carry API traffic; no interactive shell is opened. Restart controls remain a
separate feature.

## Setup

Use a Stardust version with the service and logs APIs. On the agent, explicitly
configure its private service policy, random inventory token, separate random
log-read token, and **exact unit allowlists**, following the
[producer contract](https://github.com/ch55secake/stardust/blob/main/docs/services-api.md).
Both capabilities are disabled by default. They support systemd Linux, not macOS
or non-systemd Linux. Only units in each respective allowlist are visible.

On the machine running Orbit, put each token in its own private regular file
owned by your user, mode `0400` or `0600`. A trailing newline is accepted; JSON,
symlinks, group-readable files, and tokens containing spaces are not. Protect the
parent directories. Never put either token itself in arguments, YAML, URLs,
source control, screenshots, or logs. Reference the files for the intended
server:

```sh
orbit servers edit --name production --service-token-file /absolute/private/path/service-token
orbit servers edit --name production --service-log-token-file /absolute/private/path/service-log-token
```

The flags also work when adding servers. YAML uses `service_token_file` and
`service_log_token_file`: absolute **paths**, not secrets. Each empty flag clears
its reference. Editing a server's API address clears both old references unless
they are explicitly provided again, preventing accidental credential reuse with
another server. Keep the tokens separate and matched to the agent's respective
policies.

Service credentials require HTTPS or numeric-loopback HTTP (for example,
`http://127.0.0.1:18787` through a trusted SSH tunnel). Plain remote HTTP is
refused. Redirects are **never followed**, including another port on the same
host. Tokens are not sent to capabilities, metrics, or health, nor shared across
servers. Orbit reloads each token file for every request. Inventory permission
does not grant log access; log credentials are never sent to inventory requests.
Both credentials grant **read access only**.

## CLI

```sh
orbit services list production
orbit services list production --filter worker --limit 10
orbit services list production --filter worker --limit 10 --cursor worker@blue.service
orbit services show production worker@blue.service
orbit services logs production worker@blue.service --limit 100
orbit services logs production worker@blue.service --since 2026-10-18T12:00:00Z --follow
```

`list` prints one bounded page (1–50 units, default 20). Echo the returned cursor
as `--cursor` with the same filter; do not infer cursor values. Filtering is a
case-sensitive unit-name substring. `show` reads current canonical identity,
description, load/active/sub states, unit-file state, main PID, and optional
process name/effective UID.

`logs` reads a bounded page (1–100 entries) in oldest-to-newest order. Use the
printed `--before` opaque cursor to page older history, keeping the same time
filters. `--follow` polls with bounded requests until Ctrl+C; `--after` resumes
from a returned opaque cursor. Empty logs are reported explicitly. Logs may
contain application secrets; Orbit does not persist, export, or copy them.

## TUI

Select a fleet server and press **v**. The service browser retains fleet/server
context; tall layouts retain the Saturn masthead. Controls:

| Key | Action |
| --- | --- |
| Up/Down or k/j; Home/End | Select a unit within the page |
| Enter | Fetch live details for the selected unit |
| g | Open the selected unit's logs |
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
background service polling or automatic action. The log view bounds retained
entries and message bytes, supports paging, scrolling, follow/pause, refresh, and
cancellation when leaving or switching servers. Log content is sanitized and
never automatically copied or exported. In the log view, PgUp loads older pages;
f toggles follow, p pauses/resumes it, r refreshes, Tab/Shift+Tab switches
servers, and Esc returns to the service browser.

## State and errors

- Missing configured units remain in lists with `load_state: not-found`. Detail
  lookup can return `unit_not_found`; refresh after removal.
- A zero main PID means **no main process**. States/PIDs are live observations,
  not stable handles or application health checks. Process identity may be
  absent when a process exits during collection.
- Disabled inventory, rejected credentials, a forbidden unit, insufficient
  systemd permissions, unavailable backends, and timeouts have distinct messages.
  Check agent configuration rather than granting Orbit broader access.
- Log access has its own credential and unit allowlist. Unsupported journal
  capability, disabled access, rejected credentials, forbidden/missing units,
  expired cursors, insufficient journal permissions, and unavailable backends
  are distinguished. Refresh from recent history after an expired cursor. There
  is no broader-access request or SSH fallback.
- Older agents and unsupported hosts show actionable capability messages. The
  dashboard and existing SSH features remain available. There is no SSH-command
  fallback.
- Remote text is untrusted: terminal controls and record separators are
  neutralized. Credentials reflected in service data are redacted; raw remote
  error messages are never printed.

Tests use local HTTP servers and mocks only; no real services are changed.
