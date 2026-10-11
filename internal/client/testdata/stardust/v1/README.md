# Stardust API v1 fixtures

These fixtures document the JSON contract consumed by Orbit. Timestamps use
RFC 3339 UTC (`Z`). CPU, memory, and disk percentages are in the range 0–100;
`used_bytes`, `total_bytes`, and network byte counters are bytes; packet fields
are packet counts; `uptime_seconds` is seconds. Partial metric responses keep
the available values and describe unavailable collectors in `data.errors`.

The files are served by `httptest.Server` in the client integration tests so
contract changes fail in CI rather than relying on copied inline JSON.
`health-v2.json` is a negative fixture used to verify that an incompatible
response version is reported clearly.
