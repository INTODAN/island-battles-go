---
name: bot-league-iteration
description: "One iteration of improving the bot league's bots (league/), from the branch to the final report: measure the population, study the champion's lost games and logs on the training maps, write candidates with one idea each, gate and select them against the training and holdout maps, and record it in the ledger. Load it when asked to run a league iteration, improve the league's bots, 'do RSI' on the bots, or find a better champion; the request needs to say nothing more."
---

# A league iteration

An iteration takes the league from one measured state to the next: it ends with a tournament that says what improved, a ledger entry, and nothing half-done.

"Run a league iteration" is the whole request: everything else is here. Asked for several, finish each one completely before starting the next.

## Before you start

**Read these first, in full**, even if you think you know them:

1. [league/README.md](../../../league/README.md): what the league is.
2. The [`bot-league`](../bot-league/SKILL.md) skill: the commands, reading standings and games, the rules for a bot.
3. [island-battles-rules](../../../templates/python-bot/.agents/skills/island-battles-rules/SKILL.md) and [writing-a-bot](../../../templates/python-bot/.agents/skills/writing-a-bot/SKILL.md): the game and the SDK.
4. [league/ledger/iterations.md](../../../league/ledger/iterations.md): where the league stands.

**Branch.** An iteration is its own branch, `claude/league-iteration-N`, cut from `main`, or from the branch that holds the ledger's last entry while that one isn't merged yet. Never work on another pull request's branch.

**Commit, push, pull request.** Only what the request asks for. Asked to commit, make one commit when the iteration is done, whose message names the iteration and what was promoted and rejected. Don't push or open a pull request unless asked.

**What you may change:** the directories of the candidates you create under `league/bots/`, the `bot.toml` of a bot whose status changes, and `league/ledger/iterations.md`. Nothing else: not another bot's code, the tools, the tests, an existing map, the runner or these skills. New map files are allowed only as **When the numbers stop moving** says, and only before the iteration's first measurement.

**When something doesn't work, stop.** A tool that crashes, a command from a skill that fails, a check that failed before you changed anything, or a result that can't be right (a bot that wins every game after a small change, a tournament with no matches): stop, and report what you ran and what it printed. Don't work around it, and don't fix the tool inside the iteration. A wrong measurement is worse than no iteration.

## 0. Start from the ledger

In the ledger, read the **Lessons so far**, and the last two or three entries. They say who the champion is, where it loses, and which ideas were already tried and rejected. Don't try a rejected idea again unless you can say what is different this time. The new iteration's number is the last entry's plus one.

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

lists the games the champion didn't win, and its longest wins. (Its ratings table is from the champion's pairs alone; ignore it.) Study at least the three shortest losses, one draw if there is one, and the opponent or map where its row is worst; then `digest` each, and the deciding turns with `--turns` and both bots' logs, as `bot-league` describes. Read a long win too: a game the champion won in 200 turns that should have taken 60 hides a weakness that a better opponent will use.

Check each game against the ledger's lessons from real games as well: a habit that a real opponent punished there (long walks, trickled attacks, a border castle left thin) is a finding even in a game the champion won.

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
- **Three candidates** unless the request names another number. More than six is usually the same idea with different numbers.
- **A number is not an idea.** Don't make candidates that only differ in a constant. If an idea needs a constant tuned, tune it inside the candidate on the training maps, and count that as the one idea.

Write the candidates yourself, one after the other, measuring each as it is done. When the request asks for subagents, or for more than three candidates, they can be written in parallel, since candidates don't share files: one subagent per candidate, each given the finding with its evidence (the digest lines and log lines), the parent's name, the candidate's name, and the instruction to read the `bot-league` skill and the template's rules skills and to touch nothing outside `league/bots/<candidate>/`. Each subagent finishes by running `uv run --project league league tournament --focus <candidate> --strict --run-id iteration-N-<candidate>`, and reports its lost games. It doesn't run `league/scripts/check.sh`, which checks every bot and so fails on another subagent's half-written candidate: run it yourself once all of them are done. Do the selection yourself.

## 4. Gate

A candidate plays only when all of this holds:

- `league/scripts/check.sh` passes.
- `uv run --project league league tournament --focus CANDIDATES --strict --run-id iteration-N-candidates` exits 0: no match unplayed, no failed turn.
- Its slowest turn is under 200 ms.

Fix a candidate that fails the gate, or drop it. A failed turn is never "close enough". A turn that ran out of time while the machine was busy with something else is the one exception: run the same command again, which plays that match again, and believe the second run.

## 5. Select

Select from a round robin of **the whole population with the candidates in it**, never from a `--focus` run, whose ratings come from the candidates' pairs alone:

```sh
uv run --project league league tournament --add CANDIDATES --run-id iteration-N-select
```

The cache already holds every pair without a candidate, so it plays little more than the gate did. On the **training maps**, a candidate **passes** when both hold:

1. Head to head against its parent it has more than half the points (the pair table).
2. Its rating is above its parent's (the standings).

Then, for the candidates that passed only, play the **holdout maps**:

```sh
uv run --project league league tournament --maps holdout --add CANDIDATES --run-id iteration-N-select-holdout
```

A candidate is **promoted** when its holdout rating is not below its parent's. Read only the standings and the tables of that run; it lists no games. **Never digest a holdout game, and never change a candidate after seeing its holdout numbers:** a candidate that fails the holdout is rejected, and its idea may come back next iteration in another form, measured again from the start.

**Promoting nothing is a result.** Most ideas don't work, and an iteration that rejects all three and says why has done its job. Never promote a candidate that missed a rule by a little, and never loosen a rule to get a promotion.

Then:

- **Promoted:** `league status NAME active`.
- **Its parent** is `retired` when the candidate does better than it against every other player, or does as well: check the pair table's two rows. Otherwise both stay active: they are good at different things, which the population needs.
- **Rejected:** delete the directory. The ledger keeps the idea and the numbers.
- **More than six active bots:** retire the lowest-rated one whose row of the pair table another active bot matches or beats everywhere. **More than six retired:** delete the lowest-rated.

Finish with the population as it now is, once on each set of maps. These two tables are the ledger's standings:

```sh
uv run --project league league tournament --maps train --strict --run-id iteration-N-train
uv run --project league league tournament --maps holdout --strict --run-id iteration-N-holdout
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

## 7. Report

End with a report to whoever asked, short enough to read in a minute:

- the findings, one line each, with the map and turns;
- each candidate: its idea, its training rating and its points against its parent, its holdout rating if it got there, and **promoted** or **rejected**, with the rule it missed;
- the standings on the training maps and on the holdout maps, as tables;
- the champion and its rating, before and after;
- what you didn't do or couldn't verify, and anything that looked wrong on the way.

Say what the numbers say. "No candidate was promoted; the champion is unchanged at 1093" is a complete report.

## When the numbers stop moving

- **Nothing promoted twice in a row:** make one of the next iteration's candidates a founder, and start that iteration by adding maps: `league new-map NAME --seed N --set train` and one for `holdout`. New maps change every rating, so add them before step 1, never in the middle of an iteration, and say in the ledger that the map set grew.
- **The champion wins everything:** the opponents are too weak. Add founders built to beat it: read its logs for what it never does (never defends, never upgrades speed, never attacks before turn 40) and write the bot that punishes that.
- **Training rating up, holdout rating down, more than once:** the bots are learning the maps. Start the next iteration by adding training maps, and prefer ideas stated without map geometry.
- **A real game arrives** (someone gives you a demo saved from a lobby's game): digest it before the next iteration and add what it shows to **Lessons so far**. A new default map in it goes into `league/maps/train/` as a map file built from its first board, before step 1.
- **Two bots that trade the top place every iteration** are a cycle, not progress: the measure of progress is the rating against the default bots and the retired bots, which don't move.

## What an iteration never does

- Change a bot that isn't a candidate, or a candidate after its holdout run.
- Read or digest a holdout game.
- Change the runner, the rules, an existing map or the tests, for any reason.
- Go on after a tool failed or a result looked impossible.
- Keep a candidate "for later". At the end every candidate is active or deleted.
- Report an improvement that the final tournament doesn't show.
