# Island Battles lobby integration

Run `./scripts/test-lobby-acceptance.sh` from the monorepo root. It invokes the shared runner in `lobby/` with the Island Battles host and this suite.

These tests own game-specific assertions: generation one initializes on Island Battles lobby start; `/game/from-lobby` retries return the same binding; commands are authorized against the frozen roster and team ownership; incremental batches and empty completed turns work; one player's completion waits for every roster player; final completion advances once and emits each player's next-generation event; rejected commands do not partially apply; and other game keys create no Island Battles state. Previously saved spectator replays remain available. Generic lobby behavior is tested under `lobby/integration/`.

The HTTP and event contract, including request UUID retries, error statuses, reservations, disconnected-player waiting and client recovery, is documented in the [Issue #6 task plan](../../docs/tasks/issue-6-player-generation-commands.md). The gameplay SDK is documented in [`clients/island-battles-python`](../../clients/island-battles-python/README.md).
