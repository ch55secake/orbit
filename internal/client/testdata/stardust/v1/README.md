# Stardust API v1 fixtures

`services-states.json` mirrors the implemented producer fixture in
[Stardust PR #46](https://github.com/ch55secake/stardust/pull/46), including active,
failed, inactive, and missing units plus optional process name/effective UID.
Service client tests serve it through local HTTP alongside capability, permission,
authentication, pagination, redirect, and response-boundary cases. These are
constructed contract examples, not captured production responses.

These fixtures document the JSON contract consumed by Orbit. Timestamps use
RFC 3339 UTC (`Z`). CPU, memory, and disk percentages are in the range 0–100;
`used_bytes`, `total_bytes`, and network byte counters are bytes; packet fields
are packet counts; `uptime_seconds` is seconds. Partial metric responses keep
the available values and describe unavailable collectors in `data.errors`.

The files are served by `httptest.Server` in the client integration tests so
contract changes fail in CI rather than relying on copied inline JSON.
`health-v2.json` is a negative fixture used to verify that an incompatible
response version is reported clearly.

`metrics-host-unavailable.json` models the implemented Stardust host collector
failure: `data.errors` names `host`, and host metadata/uptime retain zero values.
Consumers must not interpret the zero uptime as a healthy reading. The payload
matches the collector contract at Stardust commit
`254c08e7ec29d242982ae1c384856b1d58675428`.

`metrics-current.json` mirrors the documented metrics example from Stardust
commit `254c08e7ec29d242982ae1c384856b1d58675428`, including implemented swap and
per-interface network counters. It is a documented contract example, not a
captured response from a running server.

## Swap fixtures

`metrics-complete.json` and `metrics-partial.json` retain the legacy payloads.
The swap fixtures model the additive `data.swap` object implemented in
[stardust#28](https://github.com/ch55secake/stardust/issues/28). They cover populated,
zero-capacity, and unavailable samples and match the implemented collector's field
names and units. These fixtures are constructed examples, not captured responses.

The object uses `total_bytes`, `used_bytes`, `free_bytes`, and `used_percent`.
An omitted/null object is absent; an all-zero object means no swap is configured.
A `swap` entry in `data.errors` marks the sample unavailable even if absent.

## Proposed filesystem extension fixtures

`metrics-filesystems.json` and `metrics-filesystems-partial.json` model the
optional `data.filesystems` array proposed in
[stardust#27](https://github.com/ch55secake/stardust/issues/27). Producer-side
collection and filtering are not implemented yet; these are proposed payloads,
not captured output from a released agent. The legacy `disk` field is unchanged.

Each entry has `mount_point`, optional `filesystem` and `device`, and byte/percent
fields `used_bytes`, `total_bytes`, `available_bytes`, and `used_percent`. Available
bytes may exclude reserved blocks. The partial fixture retains a healthy mount
and an unavailable zero-capacity sample, with a `filesystems` collector error.
Missing/null/empty lists remain compatible with older agents.

## Network interface fixtures

`metrics-interfaces.json` models the additive `data.network.interfaces` array
implemented in [stardust#29](https://github.com/ch55secake/stardust/issues/29).
It retains the legacy aggregate
counters and adds two interface records with a `name` and the same four
byte/packet counter fields. All counters are cumulative and may reset; traffic
can be observed on multiple interfaces and the list must not be summed to derive
aggregate totals. Missing/null/empty arrays are compatible with older agents.

## Proposed CPU extension fixtures

`metrics-cpu-detail.json` and `metrics-cpu-detail-partial.json` model the proposed
extension in [stardust#26](https://github.com/ch55secake/stardust/issues/26), not
output from a released agent. Producer-side sampling and platform availability
are not implemented yet. Existing aggregate CPU fields are retained.

`data.cpu.per_core_usage_percent` is a nullable array of nullable percentage
samples, indexed by logical core. `data.cpu.load_average` is an optional object
with nullable `one_minute`, `five_minutes`, and `fifteen_minutes` samples. Null
or missing values differ from valid zero. Load values are nonnegative numbers,
not percentages. The partial fixture exercises absent/null samples and invalid
per-core percentages without failing unrelated metrics.
