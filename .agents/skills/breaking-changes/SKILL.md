---
name: breaking-changes
description: Apply when changing the player or lobby API, Centrifugo event payloads, stored data or columns, or the client SDKs. Island Battles breaks compatibility rather than carrying code for old clients or old game data, but preserves lobby data and map templates.
---

# Break compatibility instead of carrying it

Clean, compact code matters more than compatibility. When the API, an event, or the schema should change, change it directly and have users upgrade their clients. Old clients and existing game data are not a reason to keep extra code.

## Data that must survive

Two kinds of stored data are never reset:

- **Lobby data:** every `lobby_*` table. The lobby is shared by other games, so resetting or breaking its data would break them too.
- **Map templates:** `map_templates` and `castle_templates`. Default maps are kept forever.

Change their schema only through a new versioned migration that keeps existing rows working, with a data migration or backfill where needed. Add the next numbered `NNNN_description.sql` file to `lobby/postgres/migrations/` for lobby tables, or to `backend/dal/migrations/` for map templates, and update the GORM record or model in the same change. Never edit an applied migration. Everything below about resetting data applies only to game data.

## Do

- Rename, retype or remove fields, endpoints and columns outright, in a single change.
- Update the server and every in-repository client together: export `backend/api/openapi.json` (and `lobby/api/openapi.json` for lobby changes), regenerate the Python models, update the web client (`clients/island-battles-web/`), update the conformance adapters and fixtures, the browser demo and the docs.
- Say in the pull request description and the affected client README that the change is breaking and that clients must upgrade.
- Reset game data when a schema change strands it: games, their maps, castles, generations, soldier groups, commands, player inputs, requests, notifications, team bindings, frozen rosters and replays. In development, `docker compose down -v` or removing the `island-battles-dev-postgres` Podman volume does this, but it also wipes lobby data and map templates, so only do it locally.

## Don't

- Add aliases, deprecated fields, fallback decoders, dual-read paths or version branches for older clients.
- Write data migrations, schema upgrades or backfills to keep existing game rows working.
- Keep readers or default values only for old saved game formats, such as replays or queued notifications.
- Propose compatibility shims as follow-up fixes. Mention the break, and leave it.

Compatibility that costs no code is fine, such as generated clients ignoring unknown response fields. Add compatibility code only when the user explicitly asks for it.
