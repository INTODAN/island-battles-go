# Issue #6: player commands and generation turns

Source: https://github.com/INTODAN/island-battles-go/issues/6

Worktree: `issue-6-player-generation-commands`; branch: `feature/6-player-generation-commands`.

## Outcome and fixed decisions

Starting an Island Battles lobby initializes generation one. Every player in the frozen start roster can register commands for the current generation, then explicitly complete their turn. The last completion advances the game exactly once and persists a next-generation notification for every roster player. A Python subclass receives `next_generation` callbacks and submits commands over authenticated HTTP.

1. Identity comes from the existing guest bearer credential. Team comes from the frozen roster and persisted team/color binding; no caller-supplied player or team is trusted.
2. Read/write game access also requires a current authorized lobby snapshot. Frozen roster, rather than current connections, defines the turn barrier. Disconnection does not silently skip a player.
3. Commands may arrive incrementally. `complete: true` seals that player's turn; an empty array is valid. An omitted `complete` defaults to true for a convenient one-batch turn. Python `send_soldier_groups` uses false; `end_turn` uses true.
4. Reject an invalid batch atomically with an indicative HTTP error; none of its commands are stored or executed and it does not complete the turn. Previously accepted batches remain intact.
5. A request UUID identifies one immutable batch for one player and generation. Exact retries are successful without appending or advancing twice; changed payloads under the same UUID conflict. Stale new requests and commands after completion conflict.
6. `send_soldier_group` is initially the only public command. Internal movement, production, and cleanup remain server-only. Validate parameter presence/types, positive count, distinct existing persistent castles, ownership, and cumulative available soldiers, including reservations by teammates.
7. Serialize submissions and migration with a database lock on the game row. Submission, readiness, new generation, and notification rows commit together. Engine errors roll everything in that attempt back.
8. The initial state and every migrated state have durable notifications. Publish to existing authenticated `player:<player_uuid>` channels so the generic lobby protocol stays unchanged. Delivery is at least once; client deduplicates by game and generation.
9. The SDK subscribes before HTTP reconciliation and periodically refreshes the authorized current state to recover missing pushes or restarts. It invokes callbacks serially and does not repeat the callback for a generation.
10. Remove public raw generation-create/migrate routes to close the permission/barrier bypass. Preserve internal simulation helpers for trusted tests. Old saved spectator replays remain readable; new games present their initialized/current replay history if available.

## Shared wire contract (agents must coordinate changes)

`GET /game/from-lobby/{lobby_id}/generation` authenticates a current roster player and returns `GenerationState` directly as the JSON body:

```json
{"game_id":1,"lobby_id":"uuid","generation_id":1,"player_id":"uuid","team_color":1,"channel":"player:uuid","submitted":false,"generation":{"castles":[{"id":0,"team_color":1,"soldiers":30,"x":-2,"y":0}],"soldier_groups":[]}}
```

Castle/group `id` values are persistent IDs (zero is valid). Soldier group views also include origin/destination persistent castle IDs and clash radius when useful. Never expose pending opponents' orders in state.

`POST /generation/commands`, operation ID `registerGenerationCommand`:

```json
{"game_id":1,"generation_id":1,"request_id":"uuid","complete":true,"commands":[{"command_type":"send_soldier_group","parameters":{"origin_persistent_castle_id":0,"destination_persistent_castle_id":1,"soldier_amount":5}}]}
```

Response: current `GenerationState`. On the last completion this is the new generation with `submitted:false`. Exact retries can return the latest authorized state. Errors use Huma problem JSON with meaningful detail and status: 401 authentication; 404 inaccessible/missing game; 403 forbidden command/ownership; 409 stale generation, sealed turn, UUID payload conflict or insufficient reserved soldiers; 422 invalid parameters.

Push JSON: `{"schema_version":1,"type":"game.next_generation","state":<GenerationState>}`. Every payload is addressed to `state.player_id` and its personal channel. `submitted` in a newly generated event is false. HTTP remains authoritative after reconnect.

Persistence contract for the delivery worker: exported `models.GenerationNotification` with `ID uint` primary key, `GameID uint`, `GenerationID uint`, `PlayerID string`, `Payload []byte` JSONB, `PublishedAt *time.Time`, and creation time. Unique `(generation_id, player_id)`. Core writes these in the transaction; the worker selects unpublished rows, publishes, and acknowledges after success. Duplicate delivery across workers/crash boundaries is acceptable. No network I/O in game transactions.

## Detailed task breakdown and agent allocation

### T01 — isolate work and inspect contracts (lead; complete)
- Fetch origin/main, create the branch/worktree, confirm no existing user's work is touched.
- Read issue, README conventions, backend-development and game-state-testing skills.
- Trace lobby authentication/start, game binding, migration, notifications and Python transport.
- Deliver this plan before implementation or delegation.

### T02 — add persisted turn records (strong agent; backend core)
- Own new `backend/models/generation_input.go`, `backend/dal/generation_input.go`, schema additions.
- Store frozen player/color records if needed so future roster snapshots cannot change ownership/barrier.
- Add per-generation/player command accumulation and completion with database uniqueness.
- Add UUID request ledger and generation notification model described above.
- Add all models to startup AutoMigrate; use struct query predicates and clause ordering.
- Acceptance: restart preserves pending commands, completion, request deduplication and unsent events.

### T03 — initialize one playable generation (strong agent; backend core; after T02)
- Change `RunLobbyGame` to initialize once, under the game lock, from occupied frozen teams.
- Preserve castle placement, 30 starting soldiers and stable team color mappings.
- Persist roster and initial notifications atomically with generation initialization.
- Repeated/concurrent start reuses the existing state. Failed initialization leaves no partial map/roster/generation/event data.
- Adapt automatic-battle-specific binding tests to the new one-generation behavior.

### T04 — define public state and strict command conversion (strong agent; backend core)
- Use snake_case transport DTOs separate from legacy engine structs.
- Include persistent castle IDs despite the legacy model hiding those IDs in JSON.
- Include enough castle and soldier-group information for a strategy to choose valid orders.
- Reject server-only/unknown types, malformed parameters, missing IDs (zero is valid), zero soldiers and self-targeting.
- Convert validated public sends to existing engine CommandFields only internally.

### T05 — implement authorization and reservation validation (strong agent; backend core; after T04)
- Authenticate in handler and pass the verified principal to service.
- Load game, require its Island Battles lobby snapshot, started phase and frozen roster membership.
- Derive team color server-side, validate origin belongs to that color in the requested current generation.
- Aggregate reservations from every accepted player's commands, including same-team players.
- Reject a whole incoming batch without marking complete if any element is invalid.
- Test foreign game/generation/castle, outsider, same-team overbooking and forbidden types.

### T06 — implement turn barrier and atomic migration (strong agent; backend core; after T02/T05)
- Under the game lock, check UUID retry before stale-generation rejection, then check current generation and completion.
- Append validated commands, optionally seal the player's turn, count completion against all frozen players.
- On the last completion, order accumulated commands deterministically and call trusted migration once.
- Queue one full next-state event per player in the same transaction.
- Return current state with the requesting player's current submitted flag.
- Test concurrent final completions, duplicate retries before/after migration and transaction rollback on injected failure.

### T07 — HTTP routes and application wiring (strong agent; backend core; after T06)
- Own new handler/routes and changes to existing routes and lobby start descriptions.
- Register authenticated read and registerGenerationCommand operations, request constraints and public error mapping.
- Remove public raw create/migrate operations; retain internal Go helpers used by tests.
- Wire game handlers in main; coordinate with lead, who owns delivery worker lines in main.
- Test HTTP auth/status/body behavior and route bypass removal.

### T08 — durable notification publisher (lead; independent after T02 contract)
- Own `backend/services/generation_delivery.go` and `backend/dal/generation_delivery.go` plus tests.
- Read unpublished notification rows in deterministic bounded batches, publish to canonical player channels using gocent.
- Acknowledge only on success; publication failure leaves a durable retry. Bound network calls and stop promptly on context cancellation.
- Register worker lifecycle in main alongside existing lobby publisher/sweeper.
- Test payload/channel, failure retries, ack behavior and cancellation with a fake publisher and PostgreSQL.

### T09 — Python package and HTTP client (standard agent; independent, frozen wire contract)
- Own new `clients/island-battles-python/` package, pyproject/lock, README/example and tests.
- Reuse httpx and centrifuge-python dependencies; keep game-specific behavior out of generic lobby_client.
- Provide `IslandBattlesClient(base_url, credential, lobby_id, ...)`, `send_soldier_groups`, singular alias if useful, and `end_turn`.
- Each method immediately sends authenticated registration requests with UUID, captured generation and proper complete flag.
- Raise an indicative game exception retaining HTTP status/detail. Retain an identical request UUID/payload through automatic transport retries; allow caller explicit request IDs.
- Reject sends before initial synchronization and serialize local requests so callback/turn races cannot target the wrong generation.

### T10 — Python realtime callbacks and recovery (standard agent; after T09)
- Provide async overridable `next_generation(self, generation)` and an async start/run/close/context-manager lifecycle.
- Obtain connection tokens from the existing lobby endpoint; use SDK-owned refresh/reconnect.
- Subscribe to the player's returned channel, await acknowledgment, then fetch current generation again.
- Filter by event type/schema/game/lobby/player, reconcile pushes through authorized HTTP and deduplicate generation IDs.
- Invoke callback in a separate serial worker, allowing callback methods to make HTTP calls without deadlock.
- Periodically fetch state; fail/propagate authentication errors and cleanly cancel all tasks/subscriptions/HTTP transports on close.
- Unit tests with fake HTTP/SDK: callback starts once, new event once, duplicate/foreign events ignored, lost push reconciliation, HTTP permission errors, empty end_turn, shutdown and retry behavior.

### T11 — documentation migration (lighter agent; independent)
- Own root README, backend README and integration/island_battles/README.md only.
- Replace claims that lobby start completes an automatic battle with one-generation initialization and all-player turns.
- Document endpoint examples, empty completion, incremental commands, forbidden commands, retry UUIDs, error behavior, team reservation and disconnected-player waiting.
- Link the new Python package and task plan; explain recovery and player-channel events without changing generic lobby docs.
- Do not claim verification until lead records actual results.

### T12 — integration checks and user-visible compatibility (lead; after core/SDK)
- Update host acceptance schema expectations and automatic-game assertions for new models and one initial generation.
- Exercise two real players: register/complete, verify first does not advance, second advances once, both receive full next-generation event.
- Cover empty turn, forbidden operation/ownership and stale generation; prove rejection causes no state change.
- Ensure quiz-demo, existing lobby behavior and saved replay authorization remain covered.
- Inspect temporary frontend wording and adapt any obsolete automatic-battle messages if necessary.

### T13 — review and verification (lead; after all code)
- Run gofmt, focused Go tests, Python unit tests, then full Go tests/vet and live host acceptance as available.
- Use disposable PostgreSQL/Podman fixtures, never the user's developer database.
- Review authorization, GORM zero-value predicates, command arithmetic, transaction boundaries, UUID replay behavior and event/callback races.
- Fix cross-agent interface mismatches and failed checks; record exact outcomes here.
- Final report includes branch/worktree, plan path, resulting behavior and tests/limitations.

## Assignment rationale and file ownership

The backend core requires a stronger agent: authorization, multi-player reservations, database locking, idempotency and atomic migration interact and must be reasoned about together. Use GPT-6 Astra with high reasoning for T02–T07. The Python client is a bounded implementation against the fixed contract; GPT-6 Sol with high reasoning handles T09–T10. Documentation is mechanical once contracts are fixed; GPT-6 Luna handles T11. The lead owns T08/T12/T13, integration, review and final verification. Agents must not edit each other's owned files; communicate any contract changes before applying them.

## Verification results

All tasks T01–T13 are complete. Implemented by the assigned Astra backend agent, Sol Python agent, Luna documentation agent and lead, with shared-contract review and integration fixes by the lead.

- Full Go suite passed: `go test -p 2 ./... ./lobby/...`.
- Go static checks passed: `go vet ./... ./lobby/...`.
- Concurrent turn/binding and delivery checks passed with the race detector: `go test -race ./backend/services ./backend/lobbyadapter -run 'TestGeneration|TestBindLobbyConcurrent' -count=1`.
- Additional final rollback checks passed for failure during actual soldier-group creation, notification insertion and initial generation creation. Each verifies no partial transition survives, and retry succeeds.
- Existing generic Python suite: **67 passed**.
- New Island Battles Python SDK suite: **9 passed**, including late-GET completion regression, callback generation pinning, transport UUID retry, authorization, lost publication/reconnect, temporary HTTP failure recovery and shutdown.
- Real PostgreSQL/Centrifugo host acceptance: **19 passed**; rerun after SDK recovery/shutdown changes also passed. Includes both players' callbacks with a 60-second polling interval, frozen-player barrier, forbidden commands/ownership, removed mutation bypass routes, request replay after backend restart and schema/replay persistence.
- Generic OpenAPI/event artifact check passed unchanged.
- Dependency lock check and `git diff --check` passed.

Database tests ran against disposable PostgreSQL containers through a temporary Podman API socket. Live acceptance owned and cleaned up its PostgreSQL and Centrifugo containers. No developer database was used.

Final behavior notes: new games save their initial replay frame and append a frame per migration. The SDK first reads authorized state to discover its personal channel, subscribes, then reconciles again after acknowledgment. A gameplay client must keep its companion lobby client joined and heartbeating. Every frozen roster player remains required after disconnect; no automatic timeout/pass rule was added.
