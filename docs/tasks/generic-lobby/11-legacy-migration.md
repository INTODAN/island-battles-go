# Task 11 — Additive legacy archive and cutover migration

Status: implemented; see [verification evidence](implementation-status.md). Prerequisites: task 02 schema exists and task 04 persistence conventions are frozen.
Read [contracts](contracts.md), [architecture](architecture.md), and [schema task](02-schema-fixtures.md).
Read `backend/models/game.go`, `backend/dependencies/database.go`, `backend/dal/game.go`, and `backend/internal/testutils/test_database.go`.
Apply both repository backend-development and game-state-testing skills.
The migration preserves legacy game state; it cannot infer a user identity from old player IDs or room names.

## Exclusive ownership

- New `backend/migrations/` implementation, tests, SQL resources if needed, and cutover README.
- Do not edit backend model/DAL files owned by task 12 or startup files owned by task 13.
- Do not edit lobby records/schema owned by task 02; agree on archive shape through that owner.
- Do not run migrations against the user's real Compose database while implementing tests.

## Required outcome

Archive original `players` rows and old `games.room_name` values in lobby-owned JSON records.
Keep every legacy source table/column and all game simulation IDs and references.
Leave old games detached with `lobby_id=NULL`; create no fake active lobby/player/host/membership.
Make deprecated `games.room_name` nullable so new game inserts can omit it safely.
Expose an explicit, repeatable, error-returning migration entry point for task 13's startup wiring.

## Algorithm and cutover

1. Freeze a migration version/key and source identity convention before implementing; use `(source_table, source_primary_key)`.
2. Build legacy fixtures using frozen old-schema structs or fixed DDL, not the future `backend/models.Game` type.
3. Cover `players` fields ID, game_id, team_color, created_at, updated_at, and deleted_at.
4. Archive game identity plus original room_name and available source timestamps; preserve null versus empty string distinctly.
5. Use JSON records with stable source IDs; do not deserialize big integer IDs through float64.
6. Establish a canonical checksum encoding, sorted by source key, that preserves timestamp/null/numeric values deterministically.
7. Document the exact fields and encoding used for source/archive count and checksum comparison.
8. Acquire a migration lock and use a transaction so two startup attempts cannot partially interleave an import.
9. Require the old application to be stopped during cutover; a one-time archive cannot safely cover continuing legacy writes.
10. Schema initialization is generic and task 02-owned; migration should check required tables rather than unexpectedly create the full app schema.
11. On fresh databases without legacy tables, record/document a safe no-legacy path without fabricating source rows.
12. Add nullable UUID `games.lobby_id` and unique non-null link support in coordination with task 12's model.
13. Keep legacy games' link NULL; do not assign lobby IDs or infer registered game keys from room names.
14. Alter deprecated `games.room_name` to allow NULL; remove a legacy default if one exists that would populate new inserts.
15. Keep existing non-null legacy room names and their uniqueness constraint; PostgreSQL allows multiple new NULL values.
16. Read old players unscoped, including soft-deleted rows; default GORM queries must not silently exclude archived history.
17. Import every original row with source-key uniqueness; archive source IDs as data rather than reusing them as guest IDs.
18. Import all legacy game room-name records even when there are no player rows for that game.
19. Existing identical archive record is a no-op; conflicting checksum/content must fail with a clear report, never overwrite history.
20. Compute source/archive counts and checksums inside the consistent migration transaction before marking migration complete.
21. Include soft-deleted source rows in those counts/checksums and describe their ordering.
22. Leave source data untouched except additive link column and room_name nullability/default changes.
23. Do not delete `players`, remove `games.room_name`, renumber games, or recreate any generation/simulation table.
24. Do not add identities, credential hashes, active membership, guest sessions, or claimant endpoints for archived records.
25. Store successful completion metadata separately from source content in the agreed archive schema or migration metadata mechanism.
26. After completion, reruns must not import newly created game rows with NULL room_name as if they were legacy data.
27. Freeze the cutover source set/high-water marks or use the completion marker to make successful reruns strict no-ops.
28. A failure before commit must allow a clean retry; transactional rollback must remove partial archive/DDL/marker changes.
29. Retain actionable migration errors for logs while avoiding dumping whole source records or future secrets.
30. Use fixed SQL for precise DDL/legacy extraction if required; never interpolate request or source values into SQL text.
31. Provide a dry verification/report function or documented SQL checks for counts/checksums after cutover.
32. Document backup, stop-old-app, migrate, verify, start-new-app sequencing for task 13.
33. Document rollback boundary: old app cannot safely resume against new detached games without an explicit rollback plan.
34. Do not implement a destructive automatic down migration; schema removal remains a future task.

## Required tests and assertions

- `TestLegacyMigrationPreservesGameGraph`: seed game/generation/map/castle/command/soldier state; every ID, FK, and gameplay value survives.
- `TestLegacyMigrationArchivesEveryPlayerField`: archive IDs, game references, negative/duplicate team colors, and original timestamps exactly.
- `TestLegacyMigrationIncludesSoftDeletedRows`: deleted games/players contribute to archive count/checksum with original deletion timestamps.
- `TestLegacyMigrationArchivesRoomsWithoutPlayers`: a named legacy game without players still gets its room-name archive.
- `TestLegacyMigrationDoesNotInventIdentity`: lobby_players/sessions/rooms/memberships stay empty and old games retain NULL lobby_id.
- `TestLegacyMigrationRetainsSourceRows`: original players and game names are byte/value equivalent after import.
- `TestLegacyMigrationAllowsMultipleDetachedGames`: insert two new games omitting room_name; both persist with NULL room_name/lobby_id.
- `TestLegacyMigrationIsRepeatable`: run twice; archive counts, payloads, checksums, source IDs, and link values do not change.
- `TestLegacyMigrationIgnoresPostCutoverGames`: add a new NULL-name game after success; rerun does not archive it as legacy.
- `TestLegacyMigrationRejectsConflictingArchive`: seed a different archive payload for an existing source key; migration fails without overwrite.
- `TestLegacyMigrationRollsBackOnVerificationFailure`: inject a count/checksum mismatch; no completed marker or partial import remains.
- `TestLegacyMigrationRetryAfterFailure`: remove injected failure and retry; exactly one complete archive set results.
- `TestLegacyMigrationConcurrentStartup`: two independent DB connections run migration; one complete result, no duplicates or partial state.
- `TestLegacyMigrationFreshDatabase`: absent legacy sources take the documented empty path and generic schema remains usable.
- `TestLegacyArchiveCannotAuthorizeMembership`: old row ID, room name, or player `0` cannot resolve to a new guest/member through migration output.

Use disposable PostgreSQL and fixed old-schema seed data. Do not depend on task 12's new model to represent the old schema.
Prefer externally packaged tests if imports would otherwise produce a backend fixture/service cycle.
No wall-clock sleeps; use synchronization hooks/barriers for concurrent startup and injected failures.

## Verification and handoff

```sh
go test ./backend/migrations -count=1
go test -race ./backend/migrations -count=1
go vet ./backend/migrations
```

Handoff migration entry point/order, archive key/JSON/checksum format, marker behavior, and cutover README to tasks 12/13.
Record exact fixture/schema coverage, commands/results, and limitations. Do not claim a production database was migrated.
Done requires repeatable rollback-safe import, preserved game graph, nullable room names, and tested archive verification.
