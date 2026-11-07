# Island Battles web client

A 3D browser client for Island Battles, written in TypeScript with [Babylon.js](https://www.babylonjs.com/). It will let people watch a game's gameplay; today it is the scaffold: a full-window Babylon scene (engine, camera, light, ground) that later issues build on. See [#94](https://github.com/INTODAN/island-battles-go/issues/94) for the plan.

The tooling is [Bun](https://bun.sh/) (runtime, package manager, bundler, dev server and test runner) and [Biome](https://biomejs.dev/) (lint, format and import sorting). Bun does not type-check, so `tsc` does.

## Prerequisites

- [Bun](https://bun.sh/) 1.4.4, the version pinned in `packageManager` in `package.json`. CI reads the same field.
- Docker with Compose (or Podman) and Go, to run the backend with `./scripts/dev.sh`.

## Run it locally

From the repository root, start the backend, Postgres and Centrifugo:

```sh
./scripts/dev.sh
```

Then, in this directory:

```sh
bun install
bun run dev
```

Open **http://localhost:3000/**. The dev server hot-reloads and proxies `/lobby/v1/*`, `/game/*`, `/generation/*`, `/map/*`, `/healthz` and `/openapi.json` to the backend on `http://localhost:8888`, so the browser talks to one origin and needs no CORS (the backend sends none). The Centrifugo WebSocket goes straight to `:8000`, whose allowed origins already include `http://localhost:3000`. The proxied paths live in [`proxy.ts`](proxy.ts).

## Commands

| Command | Does |
|---|---|
| `bun run dev` | Dev server with hot reload on `:3000` |
| `bun run build` | Production build to `dist/` with hashed asset names, for the `/play/` base path |
| `bun run generate` | Regenerates `src/api/generated/` from the API contract |
| `bun run generate --check` | Regenerates into a temporary directory and fails on any difference |
| `bun run check` | `biome ci`, then `generate --check`, then `tsc --noEmit`, then `bun test` |

`bunx biome check --write .` fixes formatting, lint and import order. Commit `bun.lock`; CI installs with `--frozen-lockfile`.

## API types and client

`src/api/generated/` is generated, never edited by hand, from the server's checked-in contract: `lobby/api/openapi.json` and `backend/api/openapi.json` with [openapi-typescript](https://openapi-ts.dev/), and `lobby/api/events.json` with [json-schema-to-typescript](https://github.com/bcherny/json-schema-to-typescript). After any change to those files, run `bun run generate` and commit the result. `bun run check` (and so CI, which also triggers on `lobby/api/**` and `backend/api/**`) fails while the generated files are stale. The generators live in the `tools/codegen` workspace because they need TypeScript 5, while the client itself uses TypeScript 7.

`src/api/client.ts` exports `lobbyApi` and `gameApi` ([openapi-fetch](https://openapi-ts.dev/openapi-fetch/)) for the page's own origin; `createApiClients({ baseUrl })` builds clients for another base URL. Call `setAccessToken` after creating a session: every request except `POST /lobby/v1/sessions` then sends `Authorization: Bearer <token>`. Calls resolve with `{ data }` and reject with `ApiError` (`src/api/errors.ts`): `kind` is `problem` (with `code` for lobby, `problemType` for game, `title`, `detail`), `http` (error status without a problem body) or `transport` (no response). Responses are not validated, so unknown fields such as `$schema` are ignored.

## Realtime session

`src/realtime/` keeps one [centrifuge-js](https://github.com/centrifugal/centrifuge-js) connection (pinned at an exact version) and follows a lobby and its game. It has no DOM or Babylon imports, so `bun test` runs it. `createRealtime()` returns four typed stores with a tiny `get()` / `subscribe(listener)` API: `session`, `lobby` (`Snapshot | null`), `game` (latest `GenerationState | null`) and `connection` (`connecting | connected | disconnected`), plus `problem` for a session, lobby or game that ended.

- `signIn(name)` creates a guest session, or reuses the one saved in `sessionStorage` until `expires_at`. A 401 from any call, or from `getToken` (which re-calls `POST /lobby/v1/connection-token` when the 5-minute connection token expires), ends the session.
- `followLobby({ gameKey, lobbyId })` (or `adoptSnapshot(snapshot)` with a snapshot an HTTP call returned) reads the snapshot, subscribes to its channel and re-reads it on `subscribed` and on each `lobby.changed` with a newer `version`. `followGame(lobbyId)` subscribes to `player:<id>` and reads `GET /game/from-lobby/{lobby_id}/generation` on `subscribed`.
- Install rules (`install.ts`): a generation older than the one held, a duplicate, another game's state and anything after the outcome are ignored. Results of reads that finished after `leaveLobby()` / `leaveGame()` are dropped by an epoch counter.
- Every `connected`, `subscribed` and `unsubscribed` event asks for a reconciling read. Requests close together share one read, a failed read is retried with backoff, and nothing polls while idle.

Tests inject a fake Centrifugo client (`createClient`), fake `fetch` and fake storage. The turn deadline call (`POST /game/from-lobby/{lobby_id}/deadline`) is not part of this module.

## Layout

- `index.html`, `src/main.ts`, `src/styles.css`: the page and the Babylon scene. `@babylonjs/core` is imported from its ES module paths so the bundle tree-shakes; add the side-effect imports a feature needs (for example `StandardMaterial`) instead of importing the package root.
- `src/realtime/`: the Centrifugo session, described above.
- `dev.ts` and `proxy.ts`: the dev server and its proxy rules.
- `build.ts`: the production build, served under `/play/`.

CI runs `bun install --frozen-lockfile`, `bun run check` and `bun run build` in [`.github/workflows/web-client.yml`](../../.github/workflows/web-client.yml). It deploys nothing.
