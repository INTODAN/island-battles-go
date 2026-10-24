---
name: backend-development
description: Apply Island Battles conventions when adding or changing Go backend Huma endpoints, service logic, or GORM persistence.
---

# Backend development

## API and service boundaries

- Register Huma v2 operations in `backend/routes/`; keep HTTP handlers in `backend/handlers/`, game rules in `backend/simulation/` (pure functions over `simulation.GameState`, no database), service logic in `backend/services/`, and persistence in `backend/dal/`. Persistence holds no game rules.
- When an endpoint's public behavior changes, update its Huma `Summary` and `Description` to match. Internal refactors do not require documentation edits.
- Player API types live in `backend/models/generation_input.go` and are published without their `View` suffix. After changing them, run `go run ./backend/cmd/export-openapi --output backend/api/openapi.json` and regenerate the Python models with `clients/island-battles-python/scripts/generate.py`.
- Breaking the API or schema is preferred over compatibility code; follow the [breaking-changes skill](../breaking-changes/SKILL.md). GORM `AutoMigrate` adds columns but never renames them, so a rename strands existing game data; reset it rather than migrating it. Lobby tables and map templates are the exception: change them only through versioned migrations that keep existing rows.
- Use descriptive domain variable names, such as `soldierGroup` rather than `sg`, following the [repository conventions](../../../README.md#code-philosophies).

## GORM conventions

- Use model structs for query filters and GORM `clause` expressions for ordering instead of handwritten SQL strings. See [game DAL methods](../../../backend/dal/game.go) for existing patterns, including `.Where(&models.Game{LobbyID: &lobbyID})`.
- Prefer `Limit(1).Find(&slice)` for optional single-row lookups. Check the query error and slice length before accessing a result; preserve the DAL method's existing not-found contract.
- Keep database access in the DAL. Reuse its loading and preload helpers when retrieving game generations.

For database-backed game scenarios or graph debugging, use the `game-state-testing` skill.
