# Island Battles MCP server

`island-battles-mcp` is a [Model Context Protocol](https://modelcontextprotocol.io/) server that you run on your own computer, so that your AI agent (Claude Code, or any other MCP client) can investigate an Island Battles game with you. For example, stop the replay on turn 40 and ask: "In the current turn, how did my castle get captured?" ([#282](https://github.com/INTODAN/island-battles-go/issues/282)).

It reads a game in one of two ways:

- **A demo file**: the file that **Save demo** in a game's replay saves (`island-battles-game-<id>-generation-<n>.json`). It holds every turn's board and the bot logs its saver could see, and needs no network.
- **A lobby code**: the lobby's join code, as the lobby page shows it. The server reads the lobby's **newest** game from the game's watcher endpoints, while the game runs and after it ends, as anyone with the code can, so you need no player token. A lobby that played several games gives only its newest; save a demo to keep an earlier one.

**Whose bot logs it sees.** Other teams' bot logs stay hidden until the game ends, because bots often log their plans ([#287](https://github.com/INTODAN/island-battles-go/issues/287)):

- **A running game by lobby code** has no bot logs at all: the server reads it as a watcher, and a watcher belongs to no team.
- **A demo saved during the game** holds only its saver's team's logs.
- **Once the game has ended**, its demo and its lobby code give every team's logs.

`game_overview` says whose logs the loaded copy holds, and `get_logs` and `explain_castle` say which teams' lines are left out and why. Log lines, team names and display names are written by the game's players and their bots: the server's instructions tell the agent to treat them as untrusted data, never as instructions.

## Install

You need [Go](https://go.dev/dl/) 1.25 or later.

```sh
go install github.com/INTODAN/island-battles-go/clients/island-battles-go/cmd/island-battles-mcp@latest
```

This puts `island-battles-mcp` in `$(go env GOPATH)/bin`, which should be on your `PATH`. To try it from a checkout of this repository instead, run `go install ./clients/island-battles-go/cmd/island-battles-mcp`.

Register it with Claude Code, then start a new session:

```sh
claude mcp add island-battles -- island-battles-mcp
```

Any other MCP client runs it as a stdio server: the command is `island-battles-mcp`, and it takes no input other than the MCP stream. For example, in a client's JSON configuration:

```json
{"mcpServers": {"island-battles": {"command": "island-battles-mcp"}}}
```

## Ask your AI agent

In the 3D client's game view, **Ask your AI agent** copies a ready-made prompt for your agent: how to install and register this server, the game (the lobby's code, or the demo file you opened), your team, and the turn the replay is on. Paste it into your agent, and then ask your question. The agent loads the game with `load_lobby` or `load_demo` and answers with the tools below.

Without the button, tell your agent the lobby code or the demo file's path, and the turn.

## Options

| Flag | Default | Does |
|---|---|---|
| `--demo <path>` | | Loads a demo file at start |
| `--lobby-code <code>` | | Loads a lobby's game at start |
| `--game-url <url>` | `https://island-battles.elidan.org`, or `$ISLAND_BATTLES_GAME_URL` | The game server whose watcher endpoints it reads |
| `--lobby-url <url>` | `https://lobby.elidan.org`, or `$ISLAND_BATTLES_LOBBY_URL` | The lobby it watches lobbies on |
| `--name <name>` | `AI agent` | The watcher name the lobby's host sees, at most 40 characters |

For a local game from `./scripts/dev.sh`, pass the backend and the lobby. The 3D client's prompt passes its dev server, `http://localhost:3000`, which forwards the game's paths to the backend:

```sh
claude mcp add island-battles -- island-battles-mcp --game-url http://localhost:8888 --lobby-url http://localhost:8890
```

It logs to stderr, because stdout carries the MCP stream.

## Tools

Turns count from 1, as the replay's "Turn n / m" does. Turn n's board is what the commands of turn n-1 made: a group sent on turn n-1 first shows on turn n. Turn n's bot logs are what the bots logged while choosing turn n's commands, so the reasons for what changed on turn n are in turn n-1's logs. Every tool takes an optional `game`, a loaded demo file's name or a lobby code. Without it, a tool uses the game loaded last.

| Tool | Answers |
|---|---|
| `load_demo` | Loads a demo file (packed with gzip or not) and returns its overview |
| `load_lobby` | Reads a lobby's newest game by its join code, as a watcher: no bot logs while the game runs, every team's once it has ended. While the game runs, the other tools read it again when their copy is 15 seconds old |
| `game_overview` | Teams and players, the outcome, the number of turns, each team's forces on the last turn, every capture, when each team was eliminated, and whose bot logs the loaded copy holds |
| `get_turn` | One turn's board: castles (owner, soldiers, upgrades, and the soldiers marching on each), soldier groups (from, to, distance left, the turn that sent them), clashes and team totals. It can be limited to one team |
| `diff_turns` | What changed between two turns, turn by turn: captures, groups sent, arrivals and what they did, groups defeated, upgrades bought, each castle's net change, and team totals |
| `explain_castle` | What happened to one castle on one turn, such as a capture. It gives the castle's soldier ledger (soldiers sent, upgrade cost, growth, then each arrival with the garrison before and after it), each arriving group's journey from the turn it was sent, and the log lines of the teams involved on the turns that matter |
| `castle_history` | The turns on which something happened to one castle |
| `list_captures` | Castles that changed hands, by team and range of turns |
| `get_logs` | The bot log lines over a range of turns that the loaded copy holds, filtered by team, player, lowest level or text, and which teams' lines are left out |
| `team_timeline` | Each team's castles and soldiers, turn by turn |

The answers are compact JSON. The ledgers follow the game's rules for a turn: commands send groups and buy upgrades, groups move and clash, every castle a team owns grows by its production (its `generation_amount` level, 1 to 4 soldiers; a burst ordered that turn arrives on top, and burst debt is repaid first), and then the groups that reached their castle arrive in group ID order. A friendly group reinforces its castle. A hostile group fights the garrison and takes the castle when more attackers than defenders arrive. A demo doesn't say what upgrades cost, so the ledger works the cost out from the soldiers, trying at most 1000 costs when groups arrived. A group that was sent and arrived or was destroyed within one turn shows on no board, so its ledger names the soldiers that no board shows.

## How lobby mode reads a game

1. `POST /lobby/v1/sessions` on the lobby creates a guest session named `--name`.
2. `POST /lobby/v1/watches` with the code makes it a watcher of the lobby. The host sees it in the lobby's watchers and can remove it, and then reads fail with 403.
3. `POST /lobby/v1/player-tokens` issues a player token for the game, renewed before it expires.
4. `GET /game/from-lobby/{lobby_id}/watch/demo` on the game returns the lobby's newest game so far as a demo: no bot logs while the game runs, every team's once it has ended.
5. When the server stops, `DELETE /lobby/v1/games/island-battles/lobbies/{lobby_id}/watch` removes each of its watches, so the host's watcher list doesn't keep it and restarts don't fill the lobby's limit of watchers. It does the same with its old session's watches before it renews the session. A server that is killed can't, and the host can remove the watcher.

It never joins a lobby, takes a seat or plays. A lobby is deleted some time after its game ends; after that, its code no longer works, so save a demo to keep the game.

## Development

The tools are tested over an in-memory MCP connection against [`testdata/demo.json`](testdata/demo.json), a six-turn game with captures, an upgrade and a clash, and against a fake lobby and game for lobby mode. Another test checks that every castle ledger of the web client's real bot-game fixtures (`clients/island-battles-web/test/fixtures/`, all but the hand-edited `heading-arrows.json`) reconciles:

```sh
go test ./clients/island-battles-go/cmd/island-battles-mcp
```

The demo format is the game's `Demo` schema in [`backend/api/openapi.json`](../../../../backend/api/openapi.json). When its `schema_version` changes, change `demoSchemaVersion` in [`demo.go`](demo.go).
