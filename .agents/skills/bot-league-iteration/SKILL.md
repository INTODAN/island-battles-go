---
name: bot-league-iteration
description: "One iteration of improving the bot league's bots (league/): measure the population, study the champion's lost games and logs on the training maps, write candidates with one idea each, gate and select them against the training and holdout maps, and record it in the ledger. Load it when asked to improve the league's bots, run a league iteration, 'do RSI' on the bots, or find a better champion."
---

# A league iteration

An iteration takes the league from one measured state to the next: it ends with a tournament that says what improved, a ledger entry, and nothing half-done. Read the `bot-league` skill first for the commands, how to read standings and games, and the rules for a bot.

Run several iterations in a row when asked to; finish each one completely before starting the next. Each is one commit when the session was asked to commit.

## 0. Start from the ledger

Read [league/ledger/iterations.md](../../../league/ledger/iterations.md): the **Lessons so far**, and the last two or three entries. They say who the champion is, where it loses, and which ideas were already tried and rejected. Don't try a rejected idea again unless you can say what is different this time. The new iteration's number is the last entry's plus one.

Then `uv run --project league league roster` and `league/scripts/check.sh`. A league that doesn't pass its checks is fixed before anything is measured.

## 1. Measure

```sh
uv run --project league league tournament --run-id iteration-N-before
```

It plays the active and retired bots and default levels 1 to 3 on the training maps; most of it comes from the cache. The **champion** is the top-rated active bot. Note its rating, its row of the pair table and its row of the map table.

## 2. Study

```sh
uv run --project league league tournament --focus CHAMPION --run-id iteration-N-champion
```

lists the games the champion didn't win. Study at least the three shortest losses, one draw if there is one, and the opponent or map where its row is worst; then `digest` each, and the deciding turns with `--turns` and both bots' logs, as `bot-league` describes. Look at a long win too: a game the champion won in 200 turns that should have taken 60 hides a weakness that a better opponent will use.

Write each finding down before writing any code:

> **Observation:** `islands`, as team 2 against `expander`, turns 18 to 31: the champion's home held 41 soldiers while three neutral castles 2.5 away stayed neutral. **Cause:** it buys production until it can't, and only then looks at targets. **Idea:** a neutral castle that pays back within 15 turns comes before the next production level.

Aim for three to five findings, from different games. Prefer a cause that explains several losses over one that explains one. If the logs don't say why the bot did something, that is a finding too: the first change is a better log line.

When the bots under study lose nothing worth reading (the champion wins nearly everything), the population is too weak to teach it: go to **founders** below.

## 3. Vary

One candidate per idea, each copied from the bot the idea is about (usually the champion, but a fix for `rusher` starts from `rusher`):

```sh
uv run --project league league new economist_neutrals --parent economist --iteration N --idea "A neutral castle that pays back within 15 turns comes before the next production level."
```

Three kinds of candidates:

- **Fix:** the smallest change that answers one finding.
- **Graft:** a mechanism another bot has and this one lacks, copied into the candidate (`sentinel`'s bursts into `economist`). The pair table says which: graft from the bot that beats the champion.
- **Founder:** a new strategy, not a descendant, written from the rules and the Lessons. Add one when the last two iterations promoted nothing, or when the champion loses to nobody. A founder's parent is the bot whose scaffolding it starts from; say in its idea that it is a new strategy.

Rules:

- **One idea per candidate.** Two changes in one candidate give a result that says nothing about either. Two ideas are two candidates, and a third that has both if both win.
- Change **only the candidate's directory**. Never the parent, another bot, the maps or the tools: a tool that needs changing is a separate change, made before the iteration and measured with a full tournament.
- Keep the logging rule: the changed decision shows in the turn's log line.
- Three to six candidates per iteration is the useful range. Fewer wastes the tournament; more than six is usually the same idea with different numbers.
- **A number is not an idea.** Don't make candidates that only differ in a constant. If an idea needs a constant tuned, tune it inside the candidate on the training maps, and count that as the one idea.

Candidates don't share files, so they can be written in parallel: one subagent per candidate, each given the finding with its evidence (the digest lines and log lines), the parent's name, the candidate's name, and the instruction to read the `bot-league` skill and the template's rules skills and to touch nothing outside `league/bots/<candidate>/`. Each subagent finishes by running `league/scripts/check.sh` and `league tournament --focus <candidate> --strict`, and reports the standings. Do the selection yourself.

## 4. Gate

A candidate plays only when all of this holds:

- `league/scripts/check.sh` passes.
- `uv run --project league league tournament --focus CANDIDATES --strict --run-id iteration-N-candidates` exits 0: no match unplayed, no failed turn.
- Its slowest turn is under 200 ms.

Fix a candidate that fails the gate, or drop it. A failed turn is never "close enough".

## 5. Select

On the **training maps**, from the candidates' tournament, a candidate **passes** when both hold:

1. Head to head against its parent it has more than half the points.
2. Its rating is above its parent's.

Then, for the candidates that passed only, play the **holdout maps**:

```sh
uv run --project league league tournament --maps holdout --focus CANDIDATES --run-id iteration-N-holdout
```

A candidate is **promoted** when its holdout rating is not below its parent's. Read only the standings and the tables of that run. **Never digest a holdout game, and never change a candidate after seeing its holdout numbers:** a candidate that fails the holdout is rejected, and its idea may come back next iteration in another form, measured again from the start.

Then:

- **Promoted:** `league status NAME active`.
- **Its parent** is `retired` when the candidate does better than it against every other player, or does as well: check the pair table's two rows. Otherwise both stay active: they are good at different things, which the population needs.
- **Rejected:** delete the directory. The ledger keeps the idea and the numbers.
- **More than six active bots:** retire the lowest-rated one whose row of the pair table another active bot matches or beats everywhere. **More than six retired:** delete the lowest-rated.

Finish with the whole population on all maps, which becomes the ledger's standings:

```sh
uv run --project league league tournament --maps all --strict --run-id iteration-N
```

## 6. Record

Append an entry to `league/ledger/iterations.md`, in the shape of the entries before it:

- the date, the maps and the number of matches;
- the findings, each with its game and turns;
- every candidate: its parent, its idea, its training numbers (rating, score against its parent), its holdout rating if it got there, and **promoted** or **rejected and why**;
- the standings on the training maps and on the holdout maps;
- the champion, and its rating next to last iteration's;
- where the champion still loses, as the next iteration's starting point.

Then update **Lessons so far** at the top: add what this iteration showed about the game in general, and correct a lesson that a game contradicted. A lesson is about the game ("a group that lands one turn after a burst loses to it"), not about a bot.

Run `league/scripts/check.sh` once more. The iteration is done when the checks pass, the roster has no candidate left, and the ledger entry is written.

## When the numbers stop moving

- **Nothing promoted twice in a row:** add a founder, and add maps. `league new-map NAME --seed N --set train` and one for `holdout`; new maps change every rating, so play `--maps all` before the iteration's measurements and say in the ledger that the map set grew.
- **The champion wins everything:** the opponents are too weak. Add founders built to beat it: read its logs for what it never does (never defends, never upgrades speed, never attacks before turn 40) and write the bot that punishes that.
- **Training rating up, holdout rating down, more than once:** the bots are learning the maps. Add training maps, and prefer ideas stated without map geometry.
- **Two bots that trade the top place every iteration** are a cycle, not progress: the measure of progress is the rating against the default bots and the retired bots, which don't move.

## What an iteration never does

- Change a bot that isn't a candidate, or a candidate after its holdout run.
- Read or digest a holdout game.
- Change the runner, the rules, a map or the tests to make a bot pass.
- Keep a candidate "for later". At the end every candidate is active or deleted.
- Report an improvement that the final tournament doesn't show.
