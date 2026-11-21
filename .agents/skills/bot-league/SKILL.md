---
name: bot-league
description: "The bot league in league/: Python bots that only AI agents write and improve, the league command (tournament, digest, roster, new, status, new-map), how to read standings and study a game from its demo and logs, and the rules every league bot keeps. Load it before touching anything under league/, playing league bots, or reading their results."
---

# The bot league

`league/` holds a population of Python bots that play each other in the headless runner, and the tools around them. [league/README.md](../../../league/README.md) is the design; read it once. To improve the bots, follow the `bot-league-iteration` skill. The runner itself is the `headless-matches` skill.

**Only agents change `league/bots/`**, and only through the loop: a change to a bot that no tournament measured doesn't belong in a pull request. Tools, maps and tests are ordinary code.

## Commands

From the repository root. The first run installs the Python project and builds the runner with Go.

| Command | Use it to |
|---|---|
| `uv run --project league league roster` | List the bots with status, parent, iteration and idea. |
| `uv run --project league league tournament` | Play every active and retired bot and default levels 1 to 3 on the training maps, from both sides, and print the standings. |
| `... tournament --focus A,B` | Play only the pairs with A or B (candidates too), and list the games they didn't win with their demos. The quick way to measure a change. |
| `... tournament --maps holdout` | The holdout maps. Also `all`, or map names such as `strait,circle`. |
| `... tournament --bots A,B --anchors 3` | Choose the bots and the default levels (`--anchors ''` for none). |
| `... tournament --strict` | Exit with status 1 when a match wasn't played or a player failed a turn. |
| `uv run --project league league digest DEMO` | A game as text: a timeline and every event. |
| `... digest DEMO --turns 40-55 [--logs NAME]` | Those turns whole: the board, the events and the bots' log lines. |
| `uv run --project league league new NAME --parent P --iteration N --idea "..."` | Copy bot P to a new candidate. |
| `uv run --project league league status NAME active` | Change a bot's status: `candidate`, `active` or `retired`. |
| `uv run --project league league new-map NAME --seed N --set train` | Make a fair two-team map. |
| `uv run --project league league unpack DEMO` | Write the demo as plain JSON for the demo viewer, and print its path. |
| `league/scripts/check.sh` | Format, lint, types and tests of the tools and every bot. Run it before a tournament counts. |

Results go to `league/runs/<run id>/` (`results.jsonl`, `summary.json`, `summary.md`, `workers.log` with the bots' stderr), which git ignores. `--run-id` names the directory.

## Reading the standings

- **Rating** is on the Elo scale with `level3` at 1000, so it compares across tournaments that include `level3`. Compare two bots only within tournaments on the same maps.
- **Score** is points per game (a win 1, a draw 0.5).
- **Failed turns** must be 0. A failed turn is a turn the bot passed because it raised, was too slow, or sent something the server rejects; the table's last section quotes the first failure of each game. Fix it before reading anything else from that bot's results.
- **Slowest turn** should stay under 200 ms. A real-time game gives a bot one second, with a network in between.
- The **pair table** shows who beats whom; the population is not a ladder, and a bot that beats the champion may lose to everyone else.
- The **map table** shows where a bot is weak. A bot at 12/12 on most maps and 2/12 on one has a hole worth an iteration.

A draw at the generation limit (`generation_cap`) between two bots that stopped attacking is common. It is half a point, and usually a sign that a bot doesn't know how to finish.

## Studying a game

The numbers say where to look; the game says what to change.

1. `tournament --focus NAME` lists NAME's lost and drawn games, shortest first, each with its demo path.
2. `digest DEMO` shows the timeline (castles / soldiers in castles / soldiers in groups / production, per team), who had the most soldiers from which turn, and the events: every send with its size, target and distance, every clash, capture, upgrade, burst and elimination.
3. Find the turn where the game turned: the capture that was never answered, the send that left a castle empty, the stack that arrived one turn late. Then `digest DEMO --turns A-B` for the turns before it, with both bots' logs, to see what the loser knew and decided.
4. Say the cause as a rule the bot broke or lacks, such as "it counted the castle's soldiers when it sent, not when the group lands". That is the finding; a change follows from it.

Read games on the **training maps only**. The holdout maps exist to give numbers nobody has fitted a bot to: never digest a holdout game, and never write a bot's code around a holdout map.

## Rules for a league bot

The game's rules and the SDK are in the bot template's skills: [island-battles-rules](../../../templates/python-bot/.agents/skills/island-battles-rules/SKILL.md), [writing-a-bot](../../../templates/python-bot/.agents/skills/writing-a-bot/SKILL.md) and [bot-logging](../../../templates/python-bot/.agents/skills/bot-logging/SKILL.md). Read the first two before writing bot code. On top of them:

- A bot is `league/bots/<name>/`: `__init__.py`, `bot.py` with `play(turn)`, `bot.toml`, and any other modules of its own. **It imports nothing from another bot.** To use another bot's idea, copy the code in.
- It imports only the SDK and the pure standard modules that `league/tests/test_bots.py` allows. No globals, clocks, randomness, files or network: the same turn always gives the same result. Everything it remembers is in `memory`.
- It counts what its commands spend. One rejected command rejects the whole turn.
- **It logs one or two lines every turn**: what it saw that mattered and what it decided, with the numbers (`logger.info`). The next agent studies its games from those lines. Not more: the game log keeps 100 lines a turn, and long logs make demos slow to read.
- Its strategy constants sit at the top of `bot.py`, each with a comment that says what it does.
- Names are lower-case with digits and `_`. `levelN` names a default bot. A descendant's name says what it tries (`economist_guard`), not a version number.

## Statuses

- `candidate`: new and unmeasured. Tournaments leave it out unless `--focus` or `--bots` names it.
- `active`: in the population, and a possible parent.
- `retired`: no longer changed, kept as an opponent so that old strengths still have to be beaten.

Deleting a bot is deleting its directory; git keeps it. Never edit an `active` or `retired` bot's code in place: its results in the ledger would stop being true. Copy it to a candidate.
