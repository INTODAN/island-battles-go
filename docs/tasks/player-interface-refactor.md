# Player interface refactor

## Contract

- `Player` is an abstract base class with `async on_generation(generation: Generation, commands: CommandReceiver) -> None` marked abstract. Its shared `__init__(team_color)` assigns `self.team_color` directly; subclasses inherit this initializer and must implement the callback. No networking, command methods, or inherited lifecycle.
- `IslandBattlesClient(base_url, credential, lobby_id, *, player_factory: Callable[[int], Player], ...)` owns authentication, subscriptions, reconciliation, and turn completion. After the first authorized state read it calls the factory once with the frozen team color. A player that adds its own constructor calls `super().__init__(team_color)`.
- The public `Generation` has only `id`, `castles`, and `soldier_groups`. Transport identity and submission metadata remain in an internal envelope. `client.generation`, `start()`, and `refresh()` expose the lean model; `client.player` exposes the constructed strategy.
- All public snapshot models and internal wire models are frozen Pydantic models. Nested collections are tuples; IDs and counts reject booleans/string coercion.
- A fresh `CommandReceiver` is supplied per callback. Its async `send_soldier_group(origin_castle_id, destination_castle_id, soldier_amount)` method returns `None` and sends one group for that callback's original generation. The SDK owns request UUID creation and retries; no public order model, batch method, or request-ID parameter is needed. Receivers reject use after the callback ends. The player has no `end_turn`; successful return completes once, while exceptions/cancellation do not complete.
- Keep the existing HTTP and Centrifugo wire protocol, authorization, UUID retries, serial callback execution, reconnect recovery, and no default polling.

## Subtasks and ownership

1. Core SDK architecture (primary; requires stronger architectural judgment).
   - Add the abstract player base with shared initialization and the public turn-scoped receiver.
   - Replace dataclasses/manual parsing with frozen Pydantic models and internal validation.
   - Move callback dispatch to the constructed strategy; remove subclass callback/command API from the runtime.
   - Keep envelope validation and immutable team identity internal; preserve generation pinning and lifecycle/error behavior.
2. Regression tests (Python SDK agent; requires concurrency and protocol reasoning).
   - Convert test players to composition and factory initialization.
   - Preserve push/reconnect/authorization/retry/close coverage.
   - Cover automatic completion after orders or an empty callback, no completion on failure/cancel, original-generation pinning, expired receiver rejection, one-time factory/team assignment, lean callback fields, frozen nested models, and malformed wire input.
   - Update the real two-player acceptance scenario to return from callbacks through the receiver.
3. Example and documentation (docs agent; bounded mechanical/API migration).
   - Subclass `Player` to inherit team-color initialization and implement only the strategy callback.
   - Keep the one-soldier strategy with `NEUTRAL_TEAM_COLOR`, typed `Generation`, and `CommandReceiver`.
   - Document factory startup, model shape/immutability, receiver lifetime, and automatic completion.
   - Update current backend/frontend documentation references; leave historical issue plan as history.
4. Integration and verification (primary).
   - Update dependency lock; inspect the final public API and migration for stale names.
   - Run SDK tests, Ruff/format/type checks, and the focused live acceptance test.
   - Confirm the independent browser demo uses the current server API field names.

## Verification

- Completed the SDK, example, documentation, and test migrations.
- SDK regression suite: 30 tests passed after simplifying commands to direct single-group calls, including frozen nested models, immutable team identity, receiver expiry, callback completion, generation pinning, generated request-ID retries, parameter validation, and reconnects.
- Ruff, formatting, and ty checks passed for the SDK and example; SDK test lint/format checks also passed.
- Live acceptance: 17 existing cases passed on the first run. After correcting fixture registration in the two migrated gameplay tests, their focused rerun passed both cases against real PostgreSQL and Centrifugo.
- The host acceptance runner uses the SDK's locked integration dependency group so both clients and Pydantic are installed. The shared lobby package and browser wire protocol are unchanged.
- The single-group API passed the focused live two-player generation test; request IDs are generated only inside the SDK and reused unchanged on transport retries.
