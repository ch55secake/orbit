# Browsing systemd services

Orbit uses Stardust's service inventory, journal, and explicitly authorized
restart APIs, not `systemctl` or `journalctl` over SSH. The managed SSH
tunnel/password workflow can carry API traffic; no interactive shell is opened.
Inventory and logs remain read-only; restarts are a separate, opt-in mutation.

## Setup

Use a Stardust version with the service, logs, and restart APIs. On the agent,
explicitly configure its private service policy, random inventory token, separate
random log-read token, optional separate restart token, and **exact unit
allowlists**, following the
[producer contract](https://github.com/ch55secake/stardust/blob/main/docs/services-api.md).
Inventory and logs are disabled by default. Restarts additionally require a
separate restart policy and allowlist; these capabilities support systemd Linux,
not macOS or non-systemd Linux. Only units in each respective allowlist are
visible or restartable.

On the machine running Orbit, put each token in its own private regular file
owned by your user, mode `0400` or `0600`. A trailing newline is accepted; JSON,
symlinks, group-readable files, and tokens containing spaces are not. Protect the
parent directories. Never put any token itself in arguments, YAML, URLs,
source control, screenshots, or logs. Reference the files for the intended
server:

```sh
orbit servers edit --name production --service-token-file /absolute/private/path/service-token
orbit servers edit --name production --service-log-token-file /absolute/private/path/service-log-token
orbit servers edit --name production --service-restart-token-file /absolute/private/path/service-restart-token
```

The flags also work when adding servers. YAML uses `service_token_file`,
`service_log_token_file`, and `service_restart_token_file`: absolute **paths**,
not secrets. Each empty flag clears its reference. Editing a server's API address
clears old references unless
they are explicitly provided again, preventing accidental credential reuse with
another server. Keep the tokens separate and matched to the agent's respective
policies. Inventory permission does not grant restart permission; the restart
credential is never sent on read requests, and the read credentials are never
sent to the restart API. Keep all three credentials distinct. Restart permission
remains subject to Stardust's server-side allowlist and least-privilege systemd
policy; Orbit's `restart_allowed` inventory flag is only a UI preflight hint.

Service credentials require HTTPS or numeric-loopback HTTP (for example,
`http://127.0.0.1:18787` through a trusted SSH tunnel). Plain remote HTTP is
refused. Redirects are **never followed**, including another port on the same
host. Tokens are not sent to capabilities, metrics, or health, nor shared across
servers. Orbit reloads each token file for every request. Inventory permission
does not grant log access; log credentials are never sent to inventory requests.
Inventory and log credentials grant **read access only**. The restart token is
management-sensitive and authorizes only operations allowed by Stardust.

## CLI

```sh
orbit services list production
orbit services list production --filter worker --limit 10
orbit services list production --filter worker --limit 10 --cursor worker@blue.service
orbit services show production worker@blue.service
orbit services logs production worker@blue.service --limit 100
orbit services logs production worker@blue.service --since 2026-10-18T12:00:00Z --follow
orbit services restart production nginx.service
orbit services restart production nginx.service --yes
orbit services operation production <operation_id>
```

Restart requires the exact server and unit to be explicitly authorized. The
interactive command asks you to type `restart <server> <unit.service>`; in
noninteractive scripts, `--yes` is required deliberately. Cancellation sends no
mutation. Orbit tracks the Stardust operation and refreshes systemd details after
completion. A timed-out or disconnected operation may have an unknown outcome.
If Stardust returned an operation ID, inspect it with `services operation`; if the
submission response was lost, inspect the service and do not create a new restart
until you understand the prior result.

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
| R | Request restart for a unit marked restart-authorized; confirm the exact server and unit |
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
unsolicited background service polling or automatic restart. The log view bounds retained
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
- Restart controls are shown only when Stardust advertises restart support and
  the selected unit's `restart_allowed` field is true. A restart confirmation
  identifies the exact server/unit and warns about interruption. Duplicate input
  is ignored while an operation is pending; ambiguous requests reuse the same
  idempotency identity, and known operations are reconciled by operation ID.
  Systemd's active state is not an application health check. Failed, interrupted,
  or timed-out jobs are surfaced without automatic retries using a new identity.
- Older agents and unsupported hosts show actionable capability messages. The
  dashboard and existing SSH features remain available. There is no SSH-command
  fallback.
- Remote text is untrusted: terminal controls and record separators are
  neutralized. Credentials reflected in service data are redacted; raw remote
  error messages are never printed.

Tests use local HTTP servers and mocks only; no real services are changed.
