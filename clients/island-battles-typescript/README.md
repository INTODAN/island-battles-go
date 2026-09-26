# Island Battles TypeScript SDK

A Node.js 24+ player SDK using contract-generated OpenAPI operations and models, `openapi-fetch` for typed HTTP calls, and the official `centrifuge` JavaScript client for realtime subscriptions. The handwritten runtime implements the shared [SDK behavior specification](../../docs/sdk/behavior.md).

## Install and verify

From this directory:

```sh
npm ci
npm run build
npm test
npm run typecheck
npm run check
npm run generate:check
```

`npm run check` runs pinned Biome 2.5.14 formatting, recommended lint rules, and import organization, treating warnings as failures. `npm run check:fix` applies safe fixes; `npm run format` only formats. Generated source, build output, dependencies, and the npm lockfile are excluded from Biome so it cannot modify generator-owned artifacts. The generation drift check verifies generated source separately.

`npm test` executes every case in the shared JSON conformance suite through the real runtime and generated transport, plus model and lifecycle tests. Same-process Node test execution keeps the deterministic scheduling adapter visible to the runner. A smaller real-backend/Centrifugo test is orchestrated by the repository's integration suite using `test/live-player.mjs`.

## Run a player

Keep a generic lobby client joined and running throughout gameplay so that it continues renewing its membership lease. Obtain its guest credential and lobby ID, then supply them to this SDK. The game client does not create guests, join lobbies, own heartbeats, or leave membership when closed.

```js
import { IslandBattlesClient } from "@island-battles/client";

const client = new IslandBattlesClient({
  baseUrl: "http://localhost:8080",
  credential: guestCredential,
  lobbyId,
  playerFactory: (teamColor) => ({
    teamColor,
    async onGeneration(generation, commands, signal) {
      const origin = generation.castles.find(
        (castle) => castle.teamColor === teamColor && castle.soldiers > 10,
      );
      const destination = generation.castles.find(
        (castle) => castle.teamColor !== teamColor,
      );
      if (origin && destination && !signal.aborted) {
        await commands.sendSoldierGroup(origin.id, destination.id, 5);
      }
      // Successful return completes this generation, including an empty turn.
    },
  }),
});

await client.run(); // Start, wait for errors/close, and release resources.
```

For a runnable environment-based example, build and run `node examples/player.mjs` with `ISLAND_BASE_URL`, `ISLAND_GUEST_CREDENTIAL`, and `ISLAND_LOBBY_ID`.

`start()` waits for subscription acknowledgment and authoritative HTTP reconciliation. `generation` exposes the latest public snapshot; `refresh()` explicitly reconciles it. `wait()` surfaces background strategy, completion, authentication, and permanent reconciliation failures. `run()` closes in a `finally` block. `close()` is idempotent and can also be called by your shutdown handler. Polling is disabled unless `refreshIntervalMs` is provided.

Callbacks run serially and receive a generation-scoped command receiver. Every send is immediate; transport retries reuse its request UUID and payload. A new send gets a new UUID, even when its arguments match an earlier send. Successful callbacks complete their turn automatically; exceptions and cancellation leave it open. A receiver expires when its callback exits.

Use the callback's `AbortSignal` for long-running or external asynchronous work. Closing aborts active requests and callbacks, prevents completion, unsubscribes, and disconnects. JavaScript cannot forcibly stop arbitrary user code; a strategy must honor its signal to stop its own work. The runtime stops waiting for a cancelled callback and expires its receiver regardless. An injected `fetch` remains caller-owned, must honor each request's signal, and is not disposed; a factory-created realtime connection is owned and closed by the game client. Native fetch uses Node's shared connection pool.

Snapshot records and arrays are frozen. Helpers include `distance`, `compareTeam`, and `distanceLeft`; each group resolves its `destinationCastle` against the current snapshot by persistent ID. Generated JSON Schema validation rejects malformed values without coercing strings or booleans. All integer wire fields must be JavaScript safe integers (at most `Number.MAX_SAFE_INTEGER`); larger server integers are rejected instead of silently losing precision.

`GameError.kind` distinguishes `authentication`, `transport`, `protocol`, `closed`, `validation`, and other `http` errors. HTTP errors retain their status and problem detail; raw response bodies are not included. Strategy exceptions propagate unchanged.

## Regenerate the transport contract

Go/Huma registration is the wire source of truth. Regenerate the public artifacts from the repository root after changing it:

```sh
go run ./cmd/export-game-openapi \
  --output api/game/openapi.json \
  --events-output api/game/events.json
```

Then run in this package:

```sh
npm run generate
npm run generate:check
```

Both commands first export a fresh contract to a temporary directory and compare it with the checked-in OpenAPI and event artifacts. Generation uses pinned `openapi-typescript` to produce `src/generated/api.ts`, containing every operation, path, request, response, and registered event model. `openapi-fetch` consumes those generated operations for path parameters, bodies, authentication headers, serialization, and responses. The accompanying generated `schemas.json` contains the same schema components with local references; Ajv validates actual HTTP and realtime data against them at runtime. No generated files are edited by hand.

`generate:check` also regenerates and compares the entire generated file set without changing tracked output. `package-lock.json` pins transitive dependencies. The handwritten files contain player scheduling, command identity/retries, recovery, cancellation, error mapping, and public model helpers.

Generator references: [openapi-typescript](https://openapi-ts.dev/introduction), [openapi-fetch](https://openapi-ts.dev/openapi-fetch/), [Centrifugo JavaScript SDK](https://centrifugal.dev/docs/transports/client_sdk).
