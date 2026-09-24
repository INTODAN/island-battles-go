# Island Battles lobby integration

Run `./scripts/test-lobby-acceptance.sh` from the monorepo root. It invokes the shared runner in `lobby/` with the Island Battles host and this suite.

These tests own game-specific assertions: automatic game creation on Island Battles lobby start, idempotent `/game/from-lobby` retries, shared member-only spectator results, other games creating no Island Battles state, and schema/replay persistence across restart. Generic lobby behavior is tested under `lobby/integration/`.
