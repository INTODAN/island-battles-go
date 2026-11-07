# Island Battles web client

A 3D browser client for Island Battles, written in TypeScript with [Babylon.js](https://www.babylonjs.com/). It will let people watch a game's gameplay. Today the `src/scene/` layer draws any one generation as a 3D board with placeholder art (see [The 3D board](#the-3d-board)); the page itself is still the scaffold that later issues build on. See [#94](https://github.com/INTODAN/island-battles-go/issues/94) for the plan.

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

Open **http://localhost:3000/**. The dev server hot-reloads and proxies `/lobby/v1/*`, `/game/*`, `/generation/*`, `/map/*`, `/bots/*`, `/healthz` and `/openapi.json` to the backend on `http://localhost:8888`, so the browser talks to one origin and needs no CORS (the backend sends none). The Centrifugo WebSocket goes straight to `:8000`, whose allowed origins already include `http://localhost:3000`. The proxied paths live in [`proxy.ts`](proxy.ts).

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

## The 3D board

`src/scene/` draws one `Generation` (the `Generation` type from `src/api/generated/game.ts`) and knows nothing about the server. `renderGeneration(scene, generation, teams)` builds or updates meshes by castle, group and clash ID, so a later call moves and relabels what is already there rather than rebuilding it. Everything is placeholder art made from Babylon primitives in `art.ts`; replace those builders to change the look.

- `coordinates.ts`: the one map-to-world mapping. One map unit is one world unit; the map's `(x, y)` becomes `(x, 0, -y)`, so the board is laid out as the 2D replay is.
- `palette.ts`: the team palette, checked against `backend/services/replay.html` by a test.
- `board.ts` and `camera.ts`: the board fits the castles with a minimum of 8 by 6 units; the camera is a 3/4 `ArcRotateCamera` with tilt, zoom and pan limits. Double-click a castle to focus it, `F` to refit.
- `islands.ts`: island shapes are seeded by castle ID and never change; castles closer than 1.5 units share a landmass.
- `labelLayer.ts` and `labels.ts`: Babylon GUI labels linked to meshes, spread each frame so none covers another.
- `headingLines.ts`: the heading lines of [#89](https://github.com/INTODAN/island-battles-go/issues/89). Every style choice (casing, arrowhead, lane offset, tint) is a value in `headingStyle`, since the owner has not yet picked treatments.
- `picking.ts`: hover highlight and tooltip, double-click focus and the `F` key.

**Dev page.** With `bun run dev`, open `http://localhost:3000/?fixture=busy-turn.json` (add `&frame=3` to start on the third frame). It draws a saved game from [`test/fixtures/`](test/fixtures/) with a frame slider and needs no backend. The fixtures are demo files: `generated-map.json` (the generated map's first turns), `busy-turn.json` (a bot match with clashes and upgrades) and `six-teams.json`. Regenerate them by playing seeded bots with the backend's own simulation: from the repository root, run `WEB_CLIENT_FIXTURES_DIR=<absolute path to clients/island-battles-web/test/fixtures> go test ./backend/simulation -run TestWriteWebClientFixtures`. Screenshots of each frame type are in [`docs/screenshots/`](docs/screenshots/). The slider and arrows jump to a frame with no animation; **Play** plays the game on a turn at a time (`&play=1` starts playing, `&speed=2` sets the pace, `&turn=800` sets the length of a turn in milliseconds).

### Turn animation

`src/scene/animation/` animates the turn between two generations. A generation holds end-of-turn positions only, so the animation is rebuilt from the clashes' `time` and contact positions and from the turn's rules (backend/simulation).

- `plan.ts`: `planTurn(previous, next)` is pure (no Babylon) and returns tracks over `t` in [0, 1]: a group's path (to the contact position at `clash.time` first), launches from the origin castle's edge, arrivals to the destination castle's edge and into it, sinking after `clash.time` for `soldiers_after == 0`, clash effects, counts that roll at the clash, castle counts and team flips when a capture lands, and a castle display-level change at the start of the turn (a later castle pop hooks on it). `sampleTurn(plan, t)` gives every pose at `t`; at `t = 1` it equals the next generation exactly. A castle's count replays the turn's arithmetic (sends and upgrade cost at `t = 0`, growth by one, landings in group ID order); a group sent and defeated within one turn is in neither generation and is placed by the soldiers missing from its castle.
- `player.ts`: `TurnPlayer` has `play(previous, next)`, `seek(generation)` (no animation, drops the queue, never leaves anything half-way), `setSpeed` and `scrub` (a frozen mid-turn still). A turn takes 1.5 s at 1x. Turns that arrive while one plays are queued and play faster as the queue grows, 1x with nothing waiting up to 4x; more than 3 waiting and it skips to the newest. It drives a `Stage`, so tests run it with a fake stage and fake time.
- `stage.ts`: `SceneStage` applies samples to the meshes `renderGeneration` built, by castle and group ID; boats that leave the board during the turn get meshes of their own until it ends. `createScenePlayer(scene, teams)` advances with the render loop.
- `effects.ts`: the placeholder clash flash, sink tween and castle pop behind the small `TurnEffects` interface, for the art pass to replace.

## Layout

- `index.html`, `src/main.ts`, `src/styles.css`: the page and the Babylon scene. `src/main.ts` starts the dev page when the URL has `?fixture=`. `@babylonjs/core` is imported from its ES module paths so the bundle tree-shakes; add the side-effect imports a feature needs (for example `StandardMaterial`) instead of importing the package root.
- `src/realtime/`: the Centrifugo session, described above.
- `src/scene/`: the 3D board, described above.
- `dev.ts` and `proxy.ts`: the dev server and its proxy rules.
- `build.ts`: the production build, served under `/play/`.

CI runs `bun install --frozen-lockfile`, `bun run check` and `bun run build` in [`.github/workflows/web-client.yml`](../../.github/workflows/web-client.yml). It deploys nothing.
