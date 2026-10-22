---
name: breaking-changes
description: Apply when changing the player or lobby API, Centrifugo event payloads, stored data or columns, or the client SDKs. Island Battles breaks compatibility rather than carrying code for old clients or old data.
---

# Break compatibility instead of carrying it

Clean, compact code matters more than compatibility. When the API, an event, or the schema should change, change it directly and have users upgrade their clients. Old clients and existing data are not a reason to keep extra code.

## Do

- Rename, retype or remove fields, endpoints and columns outright, in a single change.
- Update the server and every in-repository client together: export `backend/api/openapi.json` (and `lobby/api/openapi.json` for lobby changes), regenerate the Python models, update the conformance adapters and fixtures, the browser demo and the docs.
- Say in the pull request description and the affected client README that the change is breaking and that clients must upgrade.
- Reset development data when a schema change strands it: `docker compose down -v`, or remove the `island-battles-dev-postgres` Podman volume.

## Don't

- Add aliases, deprecated fields, fallback decoders, dual-read paths or version branches for older clients.
- Write data migrations, schema upgrades or backfills to keep existing rows working.
- Keep readers or default values only for old saved formats, such as replays or queued notifications.
- Propose compatibility shims as follow-up fixes. Mention the break, and leave it.

Compatibility that costs no code is fine, such as generated clients ignoring unknown response fields. Add compatibility code only when the user explicitly asks for it.
