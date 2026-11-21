# The league's ledger

One entry per iteration, newest last. An entry says what was measured, what was tried, what was kept and what was learned, so that the next session starts from here and doesn't try a rejected idea again. Ratings have `level3` at 1000.

## Lessons so far

Short, general statements about the game that the games have shown. Add to them, and correct them when a later game says otherwise.

- On `circle` (two castles 4 apart, no neutral castles) a bot that spends its first soldiers on upgrades loses to one that sends everything at once: 32 soldiers arrive on turn 11, before production level 3 has paid for itself.
- Production upgrades pay on most maps with neutral castles: the economist, which does nothing else cleverly, beats default level 3 in 24 of 28 games. `islands` is the exception so far (4 of 12 points there).

From a real game between two players' bots on the official map, `claw` (2026-11-21, 195 turns; read with `league digest`, not played here):

- **Production is the entry fee, not an edge.** By turn 100 seven of the eight castles were at production level 4 and the eighth at 3, and the game stood at four castles each from turn 62 to turn 149. The league's founders, the economist apart, never buy production; a bot that only out-produces the default bots has not met a real opponent yet.
- **Short lines beat long walks.** The winner sent its soldiers 4.4 units on average and the loser 6.0. The winner moved every castle's production forward to a front castle every three turns (2,062 of its 3,373 soldiers sent went to its own castles), and its last strikes, of 74 to 98 soldiers, left from that castle. The loser attacked castles 6 to 8.2 units from where its soldiers stood, 12 to 17 turns of walking at speed level 1.
- **A defender that sees a long attack coming out-reinforces it.** On turn 53 the loser sent 84 soldiers at a castle that held 25, 8.2 units away. The defender added soldiers from two castles within 3.2 units and held 115 the turn before they arrived. Counting a target's defenders as its soldiers plus its growth is wrong for a walk of 17 turns: count what its owner can bring within the travel time.
- **Trickles lose to stacks.** After that, the loser kept sending groups of 9 to 21 from two castles at the same castle. They arrived one at a time and each lost alone.
- **The lightly held castle on the border decides a level game.** The castle at (0, -3) was captured four times, usually held under 20 soldiers while the stacks stood elsewhere, and its last capture, on turn 149, made production 19 against 12; the game ended 46 turns later.
- **Clashes trade one for one** (687 soldiers lost on each side), so nothing is won in the field: a group sent out of a castle at an arriving stack only delays it, when it is at least half the stack's size.
- **Neither bot bought speed**, in a game where attacks walked 12 to 17 turns. Whether speed level 2, for 10 soldiers, pays on a map this size is untested.

## Iteration 0: the founders

**Date:** 2026-11-21. **Maps:** 9 training (8 of the league's own and `claw`, the official map, rebuilt from the first board of a real game), 5 holdout. **Matches:** 504.

Four founders with different strategies, written to be simple and to disagree with each other, not to be good:

| Bot | Idea |
|---|---|
| `expander` | Each castle takes the cheapest castle it can afford alone, neutral or enemy; no upgrades. |
| `rusher` | Buy speed, then send every wave at the nearest enemy castle; neutrals only when close and cheap. |
| `economist` | Buy production levels first, expand with the surplus, attack enemies only with a margin. |
| `sentinel` | Burst and reinforce castles that would fall, and strike enemy castles that emptied. |

Training maps:

| Player | Rating | Score | W | D | L |
|---|---|---|---|---|---|
| economist | 1108 | 0.852 | 92 | 0 | 16 |
| level3 | 1000 | 0.667 | 44 | 8 | 20 |
| sentinel | 831 | 0.519 | 50 | 12 | 46 |
| expander | 797 | 0.472 | 47 | 8 | 53 |
| level2 | 751 | 0.361 | 23 | 6 | 43 |
| rusher | 742 | 0.398 | 34 | 18 | 56 |
| level1 | 481 | 0.111 | 0 | 16 | 56 |

Holdout maps:

| Player | Rating | Score | W | D | L |
|---|---|---|---|---|---|
| economist | 1462 | 0.983 | 59 | 0 | 1 |
| level3 | 1000 | 0.588 | 22 | 3 | 15 |
| sentinel | 836 | 0.508 | 27 | 7 | 26 |
| rusher | 802 | 0.467 | 25 | 6 | 29 |
| expander | 775 | 0.433 | 26 | 0 | 34 |
| level2 | 710 | 0.275 | 10 | 2 | 28 |
| level1 | 380 | 0.050 | 0 | 4 | 36 |

**Champion:** `economist`.

**Where it loses (training maps):** `islands` (4 of 12 points) and `strait` (8 of 12), and both `circle` games against `expander`. It wins all 12 of its games on `claw`. Nobody has read those games beyond `circle` yet; that is iteration 1's first job.

**Known from the review of the founders, for an iteration to fix** (a founder's code is not changed outside the loop):

- `sentinel` pays 10 soldiers for burst level 2 in a castle under attack to burst at most 4, and doesn't count the 10 in what the castle still lacks. On `archipelago` against `economist`, turn 52, castle 6 held 12 soldiers, paid 10 to burst 1, and fell.
- No founder defends against, or uses, anything the real game above showed: none moves soldiers forward between its own castles, none times several castles' groups to land together, and none counts a target's possible reinforcements.

**No bot failed a turn**, and no bot took more than 20 ms for a turn.
