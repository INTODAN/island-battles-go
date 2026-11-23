---
name: headless-matches
description: "Play whole Island Battles games between bots in memory, without the database, the lobby or a browser, with backend/cmd/headless-match: the schedule format, the result lines, the demos, what differs from a lobby's game, and how to change the runner. Load it before playing bots against each other, measuring a bot, or changing backend/services/headless_match.go or backend/cmd/headless-match."
---

# Headless matches

`go run ./backend/cmd/headless-match` plays games between bots in one process: no PostgreSQL, lobby, Centrifugo or HTTP. Use it whenever the question is "how does this bot play", and the live stack (`./scripts/dev.sh`, `island-battles-bot run`) only when the question is about the lobby, deadlines, real-time turns or the network.

The bot league, [INTODAN/island-battles-rsi-bot](https://github.com/INTODAN/island-battles-rsi-bot), is a project of its own that plays its tournaments with the runner, with a result cache and ratings on top. Its bots are written in Go, so it depends on the Go SDK's worker.

## Build it from outside

A project outside this repository builds the runner from a whole checkout at a commit, at the checkout's root, and writes the schedules itself:

```sh
git clone https://github.com/INTODAN/island-battles-go.git && git -C island-battles-go checkout <commit>
go -C island-battles-go build -o "$PWD/headless-match" ./backend/cmd/headless-match
```

`go install ...@<commit>` doesn't work: the module is named `islandBattles`, and `go.mod` has a `replace`. That `replace` points the Go SDK at `./clients/island-battles-go`, so `backend/` alone isn't enough either.

## Run it

From the repository's root, this schedule runs as written: the bot template's bot through this repository's Python SDK, and the Go SDK's example bot, each against default level 3 on the repository's `crossing` map.

```sh
go run ./backend/cmd/headless-match schedule.json > results.jsonl     # or the schedule on stdin
```

```json
{
  "players": {
    "level3": {"default_level": 3},
    "python": {"worker": ["uv", "run", "--project", "../../clients/island-battles-python", "--locked", "python", "-m", "island_battles_client.worker", "bot:play"],
               "directory": "templates/python-bot"},
    "go": {"worker": ["go", "run", "./clients/island-battles-go/examples/firstbot", "worker"]}
  },
  "maps": {"crossing": "backend/cmd/headless-match/maps/crossing.json"},
  "matches": [
    {"id": "crossing-python", "map": "crossing", "seats": ["python", "level3"]},
    {"id": "crossing-go", "map": "crossing", "seats": ["go", "level3"]}
  ],
  "max_generations": 300,
  "turn_timeout_milliseconds": 2000,
  "parallel": 8,
  "demo_directory": "demos",
  "compress_demos": true
}
```

- **A player** is a default bot (`default_level`) or a **worker**: any command that speaks the SDKs' worker protocol, JSON Lines on stdin and stdout. For Python, `python -m island_battles_client.worker module:function` run where the module imports, with a Python that has the SDK (`uv run --project <the bot's project> python ...`, or that environment's interpreter). A Go bot's program has a `worker` command, built or through `go run`. `directory` is the worker's working directory, where its command's relative paths start (hence `../../` above); `environment` adds variables for the worker.
- **A map** is a map file, the `MapTemplate` schema, by path. `seats[index]` plays team colour `index + 1`, and a match needs as many seats as its map has teams. Play each pair from both sides: maps are fair, turn order is not always.
- **Output:** one JSON line per match, in the order the matches end: `id`, `map`, `players`, `outcome` (`reason`, `winner_team_color`, `summary`), `winner` (a player's name, or null for a draw), `generations`, `seats` and `demo`. A match that couldn't be played has `error` instead. A seat has `castles`, `soldiers`, `eliminated_at_turn`, `turns_played`, `commands`, `failed_turns`, `failures` (the first five, with the turn and the message, a Python traceback included) and `total_turn_milliseconds`, `slowest_turn_milliseconds`, `slowest_turn`.
- **Demos** are the files that Save demo writes, with every team's log lines. `compress_demos` packs them with gzip; the demo viewer opens the unpacked file.

## What it shares with a lobby's game, and what it doesn't

The same: the `BotTurn` a bot gets (turn number, the game's last generation, the upgrade table, its memory), the check of its answer (`checkBotResult`, which a default bot's answer gets in a lobby's game: `validateCommands`, the memory and log limits), `simulation.Simulate` and `simulation.Outcome`. A bot whose result the server would reject is rejected here.

Different:

- **No deadline.** `turn.turn_deadline_at` is always null. A bot that raises, answers something the rules reject, or takes longer than `turn_timeout_milliseconds` **passes the turn** and gets an error line in its game log, as a failed default bot does; the seat's `failed_turns` counts it. In a lobby's game a late answer loses the game. Treat any failed turn as a bug in the bot.
- **No real-time turns, departures or resignations.**
- **Teams answer in colour order**, each seeing the board as its turn started.
- **Generation IDs start at 5538**, not at 1, as on the server, where every game shares the IDs: a bot that reads `generation.id` as the turn number plays badly here too. The turn's number is `turn.turn_number`.

## Speed

A worker process is started once and plays any number of games, since a bot keeps nothing between turns: a match borrows a process per worker seat and gives it back. Starting a worker is never part of a turn's time: a worker that stops, or whose turn times out, is stopped and replaced between that turn and the seat's next one (`HeadlessRecoverer`), so one failure costs one turn. A replacement that doesn't start is not tried again in that match. `parallel` is how many matches play at once, the number of CPUs by default. A Python bot's turn costs about 1 ms, mostly the SDK reading the turn; a default bot's 0.3 ms.

Bots are deterministic, so a match with the same runner, map, seats, bots and settings always plays the same game, unless a turn timed out, which depends on the machine's load. Playing it twice measures nothing; add maps instead.

## Changing the runner

- The engine is `services.PlayHeadlessMatch` in `backend/services/headless_match.go`; it must keep using the service's own functions (`checkBotResult`, `goBotGeneration`, the simulation) rather than copies of them, so a rule changes in both places at once. The loop around them is its own: when `playTurn` or `playGoBots` in `go_bots.go` changes what a turn does, change it here too. Its tests are `backend/services/headless_match_test.go`, which need no database.
- The command, the schedule and the worker processes are `backend/cmd/headless-match`; its tests use the test binary itself as a worker, and cover a worker that is late, one that exits, one that stops reading its requests and one that can't start again. Run them with `-race`: a seat's turns must never overlap.
- `go test ./backend/services -run Headless` takes about a second and `go test ./backend/cmd/headless-match` about 10 seconds. `TestGoBotTournament` plays the default bots on the same in-memory board.
- Those tests write the worker protocol by hand. `scripts/check-headless-match.sh`, in the checks' Python job, plays the real workers: the bot template's bot through this repository's Python SDK and `clients/island-battles-go/examples/firstbot` built from the checkout, each against default level 3 from both sides on `backend/cmd/headless-match/maps/crossing.json`. It fails on a runner error, a match `error` or any failed turn. Run it after changing either SDK's worker or how the runner talks to one.
- A rule change (see the `game-rules` skill) needs nothing here, but it changes every bot's results. Say so in the pull request: a project that measures bots with the runner, such as the bot league, has to play its games again.
