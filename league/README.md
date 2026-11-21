# The bot league

The league is where Island Battles bots are **written and improved by AI agents alone**. A population of Python bots, each a `play(turn)` function on the [Python SDK](../clients/island-battles-python/README.md), plays round robins in the [headless runner](../backend/README.md#headless-matches). An agent studies the games and the bots' logs, changes the bots, and keeps a change only when the next tournament says it is better. The best bot of the league is its **champion**, the bot this work is for.

People change the tools, the maps and the rules of the loop. **Only agents change `bots/`.**

## Layout

| Path | What it is |
|---|---|
| `bots/<name>/bot.py` | A bot: `play(turn)`. Its directory is a package of its own and imports nothing from another bot. |
| `bots/<name>/bot.toml` | The bot's status (`candidate`, `active` or `retired`), its parent, the iteration that made it and its idea in a sentence. |
| `maps/train/` | The maps whose games an agent may study. `claw` is the official game's default map, rebuilt from the first board of a real game. |
| `maps/holdout/` | The maps that only give numbers: nobody reads their games, so a bot can't be fitted to them. |
| `league_tools/` | The `league` command: tournaments, ratings, the game digest, the roster and the map maker. |
| `ledger/iterations.md` | What every iteration tried, measured, kept and learned. The league's memory between sessions. |
| `tests/`, `scripts/check.sh` | The checks every bot and tool passes before it plays. |
| `runs/` (not in git) | Results, the result cache and the demos of every match played here. |

## How a game is played

`league tournament` plays every pair of players on every chosen map **from both sides**, in one run of `go run ./backend/cmd/headless-match`. That command plays whole games in memory with the server's own rules: the same turn (`BotTurn`), the same command checks as `POST /generation/commands`, the same simulation and end rule, and no database, lobby or network. Each bot runs in the SDK's worker (`python -m island_battles_client.worker bots.<name>.bot:play`), as an uploaded bot does on the server. The default bots, levels 1 to 3, play in the runner itself as `level1`, `level2` and `level3`.

A headless game differs from a lobby's in what it leaves out: no turn deadline (a bot that fails or takes over 2 seconds passes the turn, and the result counts it), no real-time turns, nobody leaves. The backend README's [Headless matches](../backend/README.md#headless-matches) has the details.

**Speed.** On a 32-thread desktop, the 504 matches of the whole league (4 bots and the 3 default bots, 14 maps, both sides, 123 turns a game on average) take 5 seconds; a league bot's turn costs about 1 ms, the SDK's reading of the turn included. Bots are deterministic, so a match is decided by the runner's build, the map, the two bots' code, the SDK and the settings. `league tournament` keys its cache on those and on the players' names, and **doesn't play the same match twice**: after a change to one bot, only that bot's matches are played. The runner is built without the commit's stamp, so a commit that leaves the backend alone keeps the cache. A result with a failed turn is never kept, since a turn can time out because the machine was busy: it is played again next time. The timings in a cached result are those of the run that played it; `--no-cache` plays everything again.

**Every match leaves a demo** in `runs/cache/`, the file **Save demo** writes, with both bots' log lines for every turn. `league digest` prints it as text (any demo, a real game's **Save demo** file too), and `league unpack` unpacks it for the demo viewer (`/game/demo-viewer/` of a running backend).

## Ratings

A tournament's ratings are a Bradley-Terry fit of its games, shown on the Elo scale, with **`level3` fixed at 1000**. The default bots never change, so a rating means the same in every iteration, and the champion's rating over the iterations is the league's progress. A fit doesn't depend on the order of the games.

A rating says something only when the player met the whole field. `tournament --focus NAME` plays NAME's pairs alone: use it to find NAME's lost games quickly, never to compare ratings. `tournament --add NAME` adds NAME to the whole round robin, which costs the same thanks to the cache, and its ratings count.

Deterministic bots give one game per pair, map and side, so the number of maps is the sample size. More maps make a rating mean more; `league new-map` makes fair ones from a seed.

## The loop

One **iteration** is one pass, and ends as one commit. The [bot-league-iteration skill](../.agents/skills/bot-league-iteration/SKILL.md) is the procedure; in short:

1. **Measure.** Play the population and the default bots on the training maps. Most of it comes from the cache.
2. **Study.** Take the champion's lost and drawn games, and its slow wins, on the training maps. Read the digest, then the turns that decided the game with both bots' logs. Write each finding as: what happened (map, turn), why, and the change that would fix it.
3. **Vary.** One candidate per idea: `league new` copies a parent, and the agent changes only that directory. Three kinds of ideas: a **fix** of something a game showed, a **graft** of a mechanism another bot has, and, when two iterations promoted nothing, a new **founder** with a different strategy.
4. **Gate.** `scripts/check.sh` passes, no failed turn, no turn slower than 200 ms.
5. **Select.** In a round robin of the whole population with the candidates added, a candidate is promoted when it beats its parent head to head, rates higher than it on the training maps, and doesn't rate lower than it on the holdout maps. A rejected candidate is deleted; the ledger keeps what it tried and its numbers.
6. **Record.** The standings, the ideas, the decisions and what was learned about the game go in `ledger/iterations.md`.

What keeps the loop honest:

- **Holdout maps** catch a bot that learned the training maps instead of the game. Their games are never read.
- **The default bots and the retired bots stay in every tournament**, so a new bot must still beat the old ones, and a strategy that only beats the current champion doesn't win.
- **One idea per candidate**, so a result says something about the idea.
- **The population stays varied**: at most six active bots, and a bot is retired only when another does everything it does better.

## Rules for a bot

The [Python bot template's skills](../templates/python-bot/.agents/skills/) describe the game's rules and how a bot is written; they apply here unchanged. On top of them, `tests/test_bots.py` checks for every bot:

- **Self-contained**: it imports only the SDK, a short list of pure standard modules (`math`, `dataclasses`, `collections`, `itertools`, `functools`, `heapq`, `bisect`, `enum`, `operator`, `typing`) and its own package.
- **Pure**: no globals, no value at module or class level that a turn could change, no clock, randomness, file or network. The same turn gives the same result.
- **It logs every turn** what it decided and why, in a line or two. A game whose bots say nothing can't be studied.
- **Every result is one the server accepts**, on six boards: the SDK's fixture, under attack, with burst debt and top levels, with empty castles, without enemy castles and without castles.

The tests don't measure speed. The tournament does: its **Slowest turn** column should stay well under 200 ms; a real-time game gives a bot one second.

## Commands

Run them from the repository root; `uv run --project league` installs what they need, and the first tournament builds the runner with Go.

```sh
uv run --project league league roster
uv run --project league league tournament                    # the population and levels 1-3 on the training maps
uv run --project league league tournament --focus NAME       # only NAME's matches: its lost games and longest wins
uv run --project league league tournament --add NAME         # NAME, a candidate too, in the whole round robin
uv run --project league league tournament --maps holdout     # or: all, or map names
uv run --project league league digest DEMO                   # the timeline, the logistics and the events of a game
uv run --project league league digest DEMO --turns 40-55     # those turns whole, with both bots' logs
uv run --project league league new NAME --parent PARENT --iteration N --idea "..."
uv run --project league league status NAME active            # or: candidate, retired
uv run --project league league new-map NAME --seed N --set train
uv run --project league league unpack DEMO                   # a plain .json for the demo viewer
league/scripts/check.sh                                      # format, lint, types and tests
```

`tournament --strict` exits with status 1 when a match wasn't played, a player failed a turn or there was no match; the pull request checks run it on one map.

## What is not here yet

- **The champion plays nowhere but here.** Shipping it means `island-battles-bot package` and an upload, a hosted bot, or a rated bot name on the leaderboard. Before that it must play real lobby games with `island-battles-bot run`, where turns have deadlines and a network.
- **Two teams only.** The runner plays any number of teams; the tournament pairs two.
- **Official maps come by hand.** The game lists its default maps but doesn't serve their map files (INTODAN/island-battles-go#390). `claw` was rebuilt from a saved demo's first board; a new default map needs the same.
- **No real opponents.** The league has read one real game (the ledger's lessons from it) and played none: every opponent here is a league bot or a default bot.
