# Adding and maintaining SDKs

Generate the wire boundary; implement runtime behavior against one shared specification. The [behavior specification](behavior.md) describes observable guarantees, and [conformance scenarios](../../clients/conformance/README.md) supply shared inputs and expected outcomes. [The public game export](../../api/game/README.md) comes from the server's actual Go/Huma registrations, including typed commands and the named realtime event.

## SDK inventory

| SDK | Scope | Generated boundary | Handwritten behavior | Shared adapter |
| --- | --- | --- | --- | --- |
| [Island Battles Python](../../clients/island-battles-python/README.md) | Full game player | `generated/island_game_api_client`, pinned `openapi-python-client` | asyncio runtime, strict contract validation, public player/models/helpers | `tests/test_conformance.py` |
| [Island Battles TypeScript](../../clients/island-battles-typescript/README.md) | Full game player | `openapi-typescript` types with `openapi-fetch` transport | realtime runtime, contract validation, public player/models/helpers | `test/conformance.test.mjs` |
| [Generic lobby Python](../../lobby/clients/lobby-python/README.md) | Generic lobby management | `generated/lobby_api_client`, pinned `openapi-python-client` | credentials, leases, heartbeat, reconciliation | Existing lifecycle/reconciliation tests; game scenarios do not apply |

`lobby/clients/main.go` and the browser demo are examples, not additional full SDK packages. Keep affected examples working when contracts change. Game players require a running lobby client to maintain their lease; the game SDK does not join or heartbeat for them.

## Regeneration

From the repository root, export Go/Huma contracts first:

```sh
go run ./cmd/export-game-openapi --output api/game/openapi.json --events-output api/game/events.json
go run ./lobby/cmd/export-lobby-openapi --output lobby/api/openapi.json --events-output lobby/api/events.json
```

Generate the game clients with pinned tools:

```sh
uv run --project clients/island-battles-python/tools --locked python clients/island-battles-python/scripts/generate.py
npm ci --prefix clients/island-battles-typescript
npm --prefix clients/island-battles-typescript run generate
```

The Python generator writes typed endpoint functions and wire models. TypeScript uses generated path/model types with a typed fetch client rather than generated endpoint functions. Both consume the same export and validate incoming data against that contract. Realtime SDKs provide socket mechanics; handwritten runtime code provides generation ordering, reconciliation, and callback/command lifecycle. Do not edit generated files.

Each generator has a non-mutating check that also verifies the checked-in OpenAPI against a fresh offline export. The generic lobby retains its own export/generation commands, described in its README. Do not mix game DTOs into the reusable lobby contract.

## Verification

Install Node dependencies once with the `npm ci` command above. Go, uv with Python 3.14, and Node 24 are needed for the complete check:

```sh
./scripts/check-sdks.sh
```

This checks both exports, all generated clients, schema/HTTP boundary tests, Python game/lobby suites, Ruff and ty across the Python game project, Biome lint/format/import checks and TypeScript compilation, and shared conformance cases. The repository's local pre-commit SDK hook invokes the same check for relevant changes. It fails on missing tooling or dependencies rather than skipping a language.

For real PostgreSQL/backend/Centrifugo validation on Linux with Docker or Podman:

```sh
./scripts/test-lobby-acceptance.sh
```

The integration suite complements deterministic conformance tests; fake transports cannot establish real proxy authorization, WebSocket recovery, or database idempotency.

## New language checklist

1. Read the behavior specification and run existing conformance tests. Select a generator that supports the exported OpenAPI dialect, pin its tools/configuration, and add regeneration plus drift checking. Generate operations/models and event types without a running server.
2. Implement the runtime using generated wire types and the language's Centrifugo SDK. Keep language-specific conveniences outside generated output. Validate actual wire data; compile-time types alone do not reject malformed payloads.
3. Implement a thin adapter for every shared game scenario. Add focused public model/helper tests and real transport integration for the new language. Do not skip scenarios because a runtime feature is missing.
4. Add the SDK to this inventory and `scripts/check-sdks.sh`, document installation/lifecycle/examples, and run the complete check. A generated transport package is not yet a full player SDK.

For feature changes, update the shared specification and scenarios first, then the Go/Huma contract if needed, regenerate and update every affected SDK. The repository skills `sdk-implementation` and `sdk-behavior-change` encode these workflows.
