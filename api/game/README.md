# Island Battles player contract

`openapi.json` describes the public game SDK surface: authorized current-generation reads, idempotent command batches, and the shared lobby connection-token operation. `events.json` is a standalone JSON Schema (2020-12) for `game.next_generation`, with the same `GenerationState` definition used by HTTP. Replays, map administration, lobby management, and trusted Centrifugo proxy endpoints are excluded.

Export from the repository root without a running server, database, or environment configuration:

```sh
go run ./cmd/export-game-openapi --output api/game/openapi.json --events-output api/game/events.json
```

Check checked-in artifacts without modifying them:

```sh
go run ./cmd/export-game-openapi --output api/game/openapi.json --events-output api/game/events.json --check
```

The exporter calls the server's `routes.RegisterPlayerRoutes` and `httpapi.RegisterConnectionToken`; Go/Huma registrations and DTOs are the source of truth. The reusable lobby's independent exports remain in `lobby/api/`.

`PublicGenerationCommand.parameters` references `SendSoldierGroupParameters`: both castle IDs are required nonnegative integers (zero is valid), and `soldier_amount` is a positive integer. Only `send_soldier_group` is public. Huma validates this typed schema before the handler, returning 422 for unsupported command types or malformed parameters. Ownership failures remain 403 and reservation conflicts remain 409. The service retains raw JSON internally to validate non-HTTP callers and hash the original submitted payload for idempotency; SDKs receive fully typed generated models.

Required state collections are nonnullable arrays. `clashes` is optional, nonnullable when present, and interpreted as empty when absent. `complete` is optional and defaults to true in the service; clients send an explicit boolean. Game routes use Huma's `ErrorModel` problem responses, while the shared token route retains the generic lobby `Problem` response including its stable error code. Bearer credentials identify the guest; connection tokens authenticate Centrifugo and are not interchangeable.

Generated HTTP code does not implement the runtime protocol. See [shared behavior](../../docs/sdk/behavior.md), [conformance scenarios](../../clients/conformance/), and [SDK inventory and verification](../../docs/sdk/README.md). Generated SDKs must validate incoming data and implement the shared callback, reconciliation, retry, and lifecycle rules.
