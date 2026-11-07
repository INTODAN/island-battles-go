# Player client conformance

This suite verifies an Island Battles player client in any language. The client is driven through a small adapter process that speaks JSON Lines on stdin and stdout, so the tests never import client code. The [Python adapter](../island-battles-python/conformance/adapter.py) is the reference implementation.

## Writing a client

Generate the data models and request types from [`backend/api/openapi.json`](../../backend/api/openapi.json). It contains the five player operations, including the upgrade cost table and the game log read, the `GenerationEvent` and `GameEndedEvent` pushed over Centrifugo, and every constraint the server applies. Do not hand-write wire types. Treat unknown response fields as allowed so newer servers stay compatible.

Hand-write only the behavior below. Each rule has live tests in [`tests/live/test_turns.py`](tests/live/test_turns.py), rule 11 in [`tests/live/test_upgrades.py`](tests/live/test_upgrades.py), and rule 12 in [`tests/live/test_logs.py`](tests/live/test_logs.py).

The client's strategy is a **function** from `BotTurn` to `BotTurnResult` (both generated), sync or async. It is stateless: the client stores the memory it returns on the server.

1. **Start.** Request a connection token, connect to Centrifugo, read `GET /game/from-lobby/{lobby_id}/generation` to learn your `player:{player_id}` channel and team color, subscribe, wait for the subscription acknowledgment, then read the generation again.
2. **Turns.** Call the bot once per generation, serially, with a `BotTurn`: the state's `game_id`, `generation`, `team_color`, `memory` and `turn_deadline_at`. Never call it for a generation that is already submitted, including after a restart.
3. **Results.** The bot returns a `BotTurnResult`: `commands`, `logs` and `memory`. Build commands from the generated `SendSoldierGroupParameters` and `UpgradeCastleParameters`, so invalid arguments fail inside the bot before any request. Reject a result that is not valid, and a `memory` of more than 64 KiB as compact JSON, before any request (error kind `memory_too_large`). Map the server's problem types `urn:island-battles:problem:castle-not-found`, `castle-not-owned`, `upgrade-max-level`, `insufficient-soldiers` and `bot-memory-too-large` to their own errors, for both commands.
4. **Completion.** When the bot returns, post **one** batch with `complete: true`, a new request UUID, the result's `commands`, `logs` and `memory`. The server stores the memory with the completing batch and rejects the whole batch if any part is invalid, which leaves the turn open; the client then stops, after sending the error and the turn's remaining log lines in a last batch with `commands: []` and `complete: false`. When the bot raises, or the client closes during the turn, leave the turn open.
5. **Retries.** When a request fails without a response, retry the identical body, including its request UUID; the server applies it once. Report HTTP error responses instead of retrying them: the turn stays open and the client stops.
6. **Events.** Accept a publication only if it validates as `GenerationEvent` (`schema_version` 1, type `game.next_generation`) or `GameEndedEvent` (type `game.ended`, with an `outcome` in its state) and its game, lobby, player, channel and team match the client. Ignore everything else.
7. **State.** Install a state only if its generation is newer, or if it is the same generation and does not undo `submitted: true`. A late read must not reopen a completed turn.
8. **Recovery.** Read the generation again after every connect, reconnect or resubscribe, and retry failed reads with backoff. The player channel keeps no history, so this read is the only way to recover a missed push. Do not poll while idle.
9. **End.** A state with an `outcome`, from `game.ended` or from HTTP, is the final generation of a finished game. Do not invoke the strategy for it or send any command; report the outcome and the final board to the strategy once, then stop. A client that starts on a finished game reports the end the same way. The server rejects commands for a finished game with 409 and problem type `urn:island-battles:problem:game-ended`.
10. **Utilities.** Implement `distance(other)`, `compare_team(other)` and `distance_left()` as methods on the generated models. Their rules are defined by [`island_conformance/reference.py`](island_conformance/reference.py), not by any client. A generation whose group's destination castle is missing is invalid.
11. **Upgrades.** A result buys levels with `upgrade_castle` commands, in order, together with its sends. The server counts each level bought against the castle's soldiers; the client sends the commands as they are and reports a rejection as the problem type's error.
12. **Logs.** Give the strategy a logger in the language's own logging library. Lines it logs while the bot plays go to the game log, besides any console or file output the developer chooses; lines logged outside a turn don't. The lines in the result's `logs` follow the lines it logged. The turn's completing request carries them all as `logs`, and a retry resends the identical lines. Map levels onto `debug`, `info`, `warning` and `error`, and name the logger. Apply the server's limits before sending, and never raise into the strategy for them:
    - Cut a line longer than 500 code points so that, ending in `… (cut at 500 characters)`, it is 500 code points.
    - Replace NUL characters and unpaired surrogates, which stored text can't hold, with U+FFFD.
    - Send at most 100 lines per generation, counting from the generation state's `log_lines_stored`, the lines an earlier run of the same player already stored. Hold back the line that would be the 100th until the turn's last request: send it if no more lines came, and otherwise send, in its place, the error `game log limit reached for generation <id>: 100 lines per generation; <n> lines were not stored` at level `error` from the SDK's own logger, counting every dropped line. Write the error to the console and file outputs once, when the first line is dropped. When 100 lines are already stored, send no lines at all.
    - When the bot raises, log the error with its traceback at `error`, cut to the limit, and send the remaining lines in a last batch with `commands: []` and `complete: false`, then stop. The turn stays open.
    - If the server still rejects a batch with 422 and problem type `urn:island-battles:problem:game-log-limit`, for example because another client of the same player logged meanwhile, drop that batch's lines, write the limit error to the console and file outputs, send no more lines for the generation, and send the batch's commands and completion again without `logs`, under a new request UUID. The strategy never sees this error.

    The server stores the lines with the generation, and `GET /game/from-lobby/{lobby_id}/logs?generation_id=N` returns them, with only the caller's team visible until the game ends.

## Adapter protocol

Each line is one JSON object with a `type`. Replies and events can interleave, so the runner matches messages by type. Write logs to stderr only.

Messages from the runner:

| Type | Fields | Effect |
| --- | --- | --- |
| `evaluate` | `id`, `generation` (wire `Generation`) | Parse it with the client's public model and reply `evaluation`. Needs no server. |
| `start` | `base_url`, `credential`, `lobby_id`, optional `log_file` | Start the client, with its log written to `log_file` when given, and to no stdout output. The bot reports each turn and waits for `log`, `finish` or `fail`. |
| `log` | `level` (`debug`, `info`, `warning` or `error`), `message` | Log the message with the bot's logger during the current turn, or the next one when none runs, and reply `logged`. |
| `finish` | optional `commands`, `logs`, `memory` | Return a result from the current turn. Each command is `{"type": "send_soldier_group", "origin_castle_id", "destination_castle_id", "soldier_amount"}` or `{"type": "upgrade_castle", "castle_id", "upgrade_type"}`, built with the client's helpers with the values as given, which may be invalid or missing. Each log is `{"level", "message"}`; `memory` is a JSON object, or absent for none. |
| `fail` | | Raise from the current turn. |
| `close` | | Close the client without completing an open turn, reply `closed`, and exit. |

Messages from the adapter:

| Type | Fields |
| --- | --- |
| `evaluation` | `id` and `ok`. When `ok` is true, also `generation` (the public model serialized with wire field names), and `distances` and `relations` matrices over the castles followed by the soldier groups (`relations[i][j]` is `entity[i].compare_team(entity[j])`), and `distance_left` per soldier group. |
| `started` | `team_color` |
| `generation` | `generation`, serialized like `evaluation.generation`, and the `BotTurn`'s `memory`, `team_color`, `game_id` and `turn_deadline_at` |
| `ended` | `outcome` (wire `GameOutcome`) and the final `generation`, serialized like `evaluation.generation` |
| `logged` | |
| `stopped` | `error`: `kind`, `status` (HTTP status, or null when no request answered) and `detail` |
| `closed` | |

Error kinds are `invalid_argument` (a helper rejected the arguments inside the bot, before any request), `castle_not_found`, `not_owned`, `max_level` and `insufficient_soldiers` (from the server's problem type), `memory_too_large` (over 64 KiB, with status null before any request, or the server's 422), `rejected` (any other 4xx but 401), `authentication`, `transport`, `protocol`, `closed`, and `player` (the bot raised). `stopped` reports a client that ended on its own, for example after a failed turn or a rejected credential.

## Running

The offline tier needs only the adapter. It checks parsing and the utilities in two ways, both against the reference implementation:

- [`fixtures/models.json`](fixtures/models.json) holds hand-picked valid and invalid boards. It is generated by `island_conformance/fixtures.py`, and a test fails when it no longer matches the reference. After changing a board or a rule, run `uv run --project clients/conformance python -m island_conformance.fixtures`.
- 300 random boards from a fixed seed cover shared positions, groups at their destination, neutral castles, whole-number coordinates, upgrade levels, unknown fields, and missing destinations. A failure prints the seed, board number and board. Set `ISLAND_BATTLES_CONFORMANCE_SEED` or `ISLAND_BATTLES_CONFORMANCE_BOARDS` to explore further.

Run it with:

```sh
uv run --project clients/conformance --locked pytest clients/conformance/tests/test_models.py
```

The live tier builds the backend and starts disposable PostgreSQL and Centrifugo containers. The client under test plays one seat behind a proxy that drops responses, injects server errors and delays reads; the runner plays the other seat over HTTP and stops Centrifugo or publishes foreign events where a test needs it. Both tiers run with:

```sh
./scripts/test-client-conformance.sh                       # the Python client
./scripts/test-client-conformance.sh "node my-adapter.mjs" # another client
```

Set `ISLAND_BATTLES_CLIENT_COMMAND` to use another adapter with the offline command. The live tier needs Go, uv, Python 3.14 and Linux Docker or Podman, like the [lobby acceptance suite](../../lobby/integration/README.md).
