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

## The 3D board

`src/scene/` draws one `Generation` (the `Generation` type from `src/api/generated/game.ts`) and knows nothing about the server. `renderGeneration(scene, generation, teams)` builds or updates meshes by castle, group and clash ID, so a later call moves and relabels what is already there rather than rebuilding it. Everything is placeholder art made from Babylon primitives in `art.ts`; replace those builders to change the look.

- `coordinates.ts`: the one map-to-world mapping. One map unit is one world unit; the map's `(x, y)` becomes `(x, 0, -y)`, so the board is laid out as the 2D replay is.
- `palette.ts`: the team palette, checked against `backend/services/replay.html` by a test.
- `board.ts` and `camera.ts`: the board fits the castles with a minimum of 8 by 6 units; the camera is a 3/4 `ArcRotateCamera` with tilt, zoom and pan limits. Double-click a castle to focus it, `F` to refit.
- `islands.ts`: island shapes are seeded by castle ID and never change; castles closer than 1.5 units share a landmass.
- `labelLayer.ts` and `labels.ts`: Babylon GUI labels linked to meshes, spread each frame so none covers another.
- `headingLines.ts`: the heading lines of [#89](https://github.com/INTODAN/island-battles-go/issues/89). Every style choice (casing, arrowhead, lane offset, tint) is a value in `headingStyle`, since the owner has not yet picked treatments.
- `picking.ts`: hover highlight and tooltip, double-click focus and the `F` key.

**Dev page.** With `bun run dev`, open `http://localhost:3000/?fixture=busy-turn.json` (add `&frame=3` to start on the third frame). It draws a saved game from [`test/fixtures/`](test/fixtures/) with a frame slider and needs no backend. The fixtures are demo files: `generated-map.json` (the generated map's first turns), `busy-turn.json` (a bot match with clashes and upgrades) and `six-teams.json`. Regenerate them by playing seeded bots with the backend's own simulation: from the repository root, run `WEB_CLIENT_FIXTURES_DIR=<absolute path to clients/island-battles-web/test/fixtures> go test ./backend/simulation -run TestWriteWebClientFixtures`. Screenshots of each frame type are in [`docs/screenshots/`](docs/screenshots/).

## Layout

- `index.html`, `src/main.ts`, `src/styles.css`: the page and the Babylon scene. `src/main.ts` starts the dev page when the URL has `?fixture=`. `@babylonjs/core` is imported from its ES module paths so the bundle tree-shakes; add the side-effect imports a feature needs (for example `StandardMaterial`) instead of importing the package root.
- `dev.ts` and `proxy.ts`: the dev server and its proxy rules.
- `build.ts`: the production build, served under `/play/`.

CI runs `bun install --frozen-lockfile`, `bun run check` and `bun run build` in [`.github/workflows/web-client.yml`](../../.github/workflows/web-client.yml). It deploys nothing.
