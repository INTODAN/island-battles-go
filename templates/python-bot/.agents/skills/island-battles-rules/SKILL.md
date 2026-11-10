---
name: island-battles-rules
description: The rules of an Island Battles game: turns (generations), castles, soldiers, growth, movement, clashes, capturing, upgrades and their costs, and how a game is won, lost or drawn. Load it before deciding what a bot should do, or when a result of the bot surprises you.
---

# Island Battles rules

Island Battles is a turn-based strategy game on a 2D map. Each team is played by **one player**, a person or a bot, and a game has two or more teams. A team has a `team_color` (1, 2, ...). Color `0` is **neutral**.

## A turn is a generation

The game is a list of **generations**, numbered from 1. In each one, every player sees the board and answers with commands (this is what `play(turn)` does). When all players have answered, the server computes the next generation. The steps run in this order:

1. **Your commands**, in the order you list them.
2. **Movement**: every soldier group moves, and enemy groups that meet clash.
3. **Growth**: every castle gains **1 soldier**. Neutral castles grow too; nothing is exempt.
4. **Arrival**: groups that reached their destination castle land.

So a group you send moves in the same turn, and a castle grows before the arrivals fight it.

## Castles and soldiers

A **castle** has an `id`, a `team_color`, `soldiers`, a position (`x`, `y`) and `upgrades` (its levels). Each team starts with one castle holding 30 soldiers. Maps may also have neutral castles. Ids never change or get reused.

`send_soldier_group(origin, destination, amount)` takes `amount` soldiers out of one of your castles (at least 1, at most what it has) and puts them in a **group** that walks in a straight line to the destination castle, which can be any other castle. The group copies the origin castle's upgrade levels at that moment and keeps them.

A group moves `speed` map units per turn and never overshoots. Distances are in map units (`castle.distance(other)`), not turns: at speed 0.5, 3 units take 6 turns.

## Clashes

Groups of different teams **clash** when their centers come within **0.25** units. Clashes resolve in time order within the turn. In a clash each team loses as many soldiers as all enemy groups in it have together (never more than it has); a group at 0 is gone.

Counting soldiers before the losses, if the smaller side has **half or more** of the larger side's soldiers, every group in the clash **stops** where they met, and stays still for its `clash_stop_generations` value (3, 2 or 1 turns, counting the clash turn). If the smaller side has less than half, the survivors keep walking. A group that reaches its castle in the same instant arrives instead of clashing.

## Arriving and capturing

A group that ends the turn at its destination lands, in group id order:

- **Your own castle**: its soldiers join the castle.
- **Any other castle** (enemy or neutral): if the group has **more** soldiers than the castle, the castle becomes yours with the difference as its soldiers (it keeps its upgrade levels). Otherwise the castle loses that many soldiers and stays as it was. An equal fight leaves the castle with 0 soldiers and its owner.

## Upgrades

`upgrade_castle(castle_id, type)` buys the **next level** for a castle you own, paid in that castle's soldiers (it may spend all of them). Level 1 is free and every castle starts there. You can buy several levels in one turn. Upgrade before you send: a group copies the levels at the moment it leaves.

| Type | Level | Value | Cost to reach it |
|---|---|---|---|
| `speed` (map units per turn) | 1 / 2 / 3 / 4 | 0.5 / 1 / 1.5 / 2 | 0 / 10 / 20 / 30 |
| `clash_stop_generations` (turns stopped) | 1 / 2 / 3 | 3 / 2 / 1 | 0 / 10 / 20 |

The costs are in `turn.upgrades`; read them there rather than hard-coding them.

## Winning, losing, drawing

A team is **out** when it has no castles and no groups. The game ends when:

- **One team is left**: it wins. Neutral castles do not need to be captured. If the last teams go out in the same turn, it is a **draw**.
- **Generation 300 is created**: the team with the most castles wins; equal counts are a draw. A game never lasts more than 300 generations.
- **A turn passes its deadline** before every player answered: each team with a late player **loses** and the game ends at once. The team with the most castles among the others wins.

The host sets the turn timeout: 30 seconds unless the lobby chose 5 to 600 seconds or no limit. `turn.turn_deadline_at` says when this turn ends (`None` for no limit). One slow answer loses the whole game, so answer fast.

## Rejected results

The server checks all commands against the board and rejects the **whole result** if one is invalid: soldiers a castle does not have (counting what earlier commands in the result spend), a castle that is not yours or does not exist, an upgrade past the maximum level, or sending to the same castle. The turn is not played. See `writing-a-bot`.
