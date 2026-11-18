---
name: game-rules
description: Keep the 3D client's Game rules page true when the game's rules change. Use for any change to what a turn does or allows - a new or changed upgrade type, level, value or cost, a new player command, a change to production, movement, clashes, capturing, bursts, the turn order, the turn limit or how a game ends - in backend/simulation, backend/models/upgrades.go or the game's constants.
---

# Game rules page

The 3D client has a **Game rules** page that players read: a dialog opened by the **Game rules** button on the start screen, in the lobby room and in the game view's top bar. Its text is written by hand in [`clients/island-battles-web/src/ui/rules/rules.ts`](../../../clients/island-battles-web/src/ui/rules/rules.ts), and [`rulesDialog.ts`](../../../clients/island-battles-web/src/ui/rules/rulesDialog.ts) draws it. Whenever a pull request changes a rule, it changes this page too, in the same pull request.

## What counts as a rule change

Anything a player would play differently after, for example:

- an upgrade type added, removed or renamed, or a level's value or cost changed (`upgradeLevels` and `upgradeDescriptions` in `backend/models/upgrades.go`);
- a player command added or changed (`backend/simulation/*.go`, `validateCommands` in `backend/services/generation_input.go`, `PublicGenerationCommand` in the OpenAPI document);
- production, bursts and debt (`increase_soldier_amount.go`, `burst_castle.go`);
- movement and clashes: speed, the clash radius, who loses what, when groups stop (`calculate_initial_positions.go`, `calculate_clash_positions.go`, `DefaultClashRadius`);
- arriving and capturing (`remove_arrived_soldier_groups.go`);
- the order of a turn's steps (`EndOfTurnCommands` in `commands.go`);
- how a game ends: elimination, draws, the generation cap, turn timeouts (`outcome.go`, `DefaultMaxGenerations` and its limits in `backend/models/lobby_settings.go`, `DefaultTurnTimeoutSeconds`).

A refactor that leaves every result the same is not a rule change.

## Updating the page

1. Read the changed rule in the Go code, not only its doc comment, and work out what a player sees: what they can do, what it costs, what happens and when.
2. Change `rules.ts`:
   - **An upgrade:** its entry in `upgradeRules` (a short `name`, the `effect` in a sentence or two, the `unit` its values count, and every level's `value` and `cost`, level 1 first with cost 0) and its place in `upgradeOrder`. The table is drawn from these.
   - **A command:** its entry in `commandRules` and its place in `commandOrder`.
   - **A step of the turn:** `turnSteps`, whose `step` keys after `commands` are the command types `EndOfTurnCommands` runs, in its order.
   - **A number:** `defaultGameLength`, `defaultTurnSeconds` or `clashRadius`, or a new exported constant if the text needs another one.
   - **Anything else:** the sentences of the section it belongs to in `rulesSections`. Read the whole section again, and the others that mention the same thing (a capture, for example, appears under "Reaching a castle" and under Burst).
3. If the API changed, regenerate the client's types first (`bun run generate` in `clients/island-battles-web`). `upgradeRules` and `commandRules` are keyed by the generated `UpgradeType` and command types, so a missing entry fails `tsc`.
4. Run `bun run check` in `clients/island-battles-web`. [`rules.test.ts`](../../../clients/island-battles-web/src/ui/rules/rules.test.ts) compares the upgrade levels and costs, the upgrade and command lists, the turn's steps and the numbers with the Go sources and `backend/api/openapi.json`. When a rule it can't read changes, it still passes, so don't rely on it alone. When it fails, fix the page, not the test, unless the Go code it reads moved: then make the test read the new place.
5. Open the page and read it as a player: `bun run dev`, then **Game rules** on the start screen (see the [browser-testing skill](../browser-testing/SKILL.md)). Check the table at phone width too.

## How to write it

The page is for players, not for bot authors: no field names, endpoints or error types. Say what happens, in short sentences, in the words the game shows ("turn", "castle", "soldiers", "group"; "generations" only for the game's length, as the outcome names it). Give numbers where a player needs them. Say "can't" plainly for a refused order, and what happens instead. Keep each section to what it is about; one rule is said once, and other sections point to it in a few words.

## The other descriptions of the rules

The page is not the only place the rules are written. A rule change usually also touches:

- the Python bot template's rules skill, [`templates/python-bot/.agents/skills/island-battles-rules/SKILL.md`](../../../templates/python-bot/.agents/skills/island-battles-rules/SKILL.md), which bot authors and their coding agents read;
- the doc strings of the API (the Huma route summaries and `doc:` tags), then the OpenAPI export and the generated clients, as the [breaking-changes skill](../breaking-changes/SKILL.md) says;
- the SDK READMEs (`clients/island-battles-python/`, `clients/island-battles-go/`) and `backend/README.md`, where they describe the rule.

Change them in the same pull request, so that they and the page say the same thing.
