# Live lobby acceptance

From the repository root:

```sh
./scripts/test-lobby-acceptance.sh
```

Requires Go, Python 3.14+, uv and Linux Docker or Podman. The runner uses pinned
PostgreSQL 18-alpine and Centrifugo 6.9.6 images, unique container names, temporary
ports and generated server secrets. It builds the actual backend and runs the
locked Python SDK/wrapper against it. No configured developer database is used.

Tests cover two-player teams/start/game binding, an unrelated generic game,
real socket loss and Centrifugo history loss, reconciliation during publication
failure, outbox retry, the subscribe/snapshot race, shared-identity leases,
explicit leave, persisted expiry after a backend process restart, fresh-schema
startup and restart, protocol authorization and minimal invalidation payloads.
Go database tests additionally cover exact deadline/concurrency boundaries.

The runner starts from a disposable empty database. Test-only Unix signals
stop/restart the backend through the runner; they are not production control
endpoints. Containers and processes are cleaned on success or failure.
Failure logs are retained in a printed `/tmp/lobby-acceptance-*` directory.
`LOBBY_CONTAINER_RUNTIME` can select a specific Docker-compatible executable.

The tests intentionally fail when required services cannot start; they do not
skip infrastructure checks. Generator reproducibility is checked separately:

```sh
uv run --project clients/lobby-python/tools --locked \
  python clients/lobby-python/scripts/generate.py --check
```
