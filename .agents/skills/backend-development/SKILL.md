---
name: backend-development
description: Apply Island Battles conventions when adding or changing Go backend Huma endpoints, service logic, or GORM persistence.
---

# Backend development

## API and service boundaries

- Register Huma v2 operations in `backend/routes/`; keep HTTP handlers in `backend/handlers/`, game logic in `backend/services/`, and persistence in `backend/dal/`.
- When an endpoint's public behavior changes, update its Huma `Summary` and `Description` to match. Internal refactors do not require documentation edits.
- Use descriptive domain variable names, such as `soldierGroup` rather than `sg`, following the [repository conventions](../../../README.md#code-philosophies).

## GORM conventions

- Use model structs for query filters and GORM `clause` expressions for ordering instead of handwritten SQL strings. See [game DAL methods](../../../backend/dal/game.go) for existing patterns, including `.Where(&models.Game{LobbyID: &lobbyID})`.
- Prefer `Limit(1).Find(&slice)` for optional single-row lookups. Check the query error and slice length before accessing a result; preserve the DAL method's existing not-found contract.
- Keep database access in the DAL. Reuse its loading and preload helpers when retrieving game generations.

For database-backed game scenarios or graph debugging, use the `game-state-testing` skill.
