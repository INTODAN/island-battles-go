# Visual indications for clashes

Design proposal for [issue #10](https://github.com/INTODAN/island-battles-go/issues/10).
Open [the interactive comparison](clash-indicators.html) in a browser; it is a
self-contained mockup with synthetic data. The selected effect is also implemented
in the real lobby battlefield and saved replays.

## Options

| Option | What the player sees | Tradeoff |
| --- | --- | --- |
| Impact ring and losses | A short amber pulse at the clash, with a loss number above each participant. | Small visual footprint, but the disappearance can still be easy to miss. |
| Defeated-group outline, ring, and losses — selected | The impact ring, loss numbers above surviving groups only, and a dashed outline with × where each eliminated group fought. | Explains the disappearance directly; outlines need to clear before the next turn to avoid clutter. |
| Clash marker with details | A persistent crossed-swords marker and a nearby summary: “Blue 40 → 15 · Red 25 → 0”. | Best for inspecting busy turns; less immediate than seeing each group's loss in place. |

The second option is selected, with loss labels only above surviving groups,
no crossed-swords icon, and no “Defeated” label on the battlefield. Keep the ×
inside each defeated group's dashed outline and a compact summary below the view.
For example, two groups with 40 and 25 soldiers meet: show an impact ring, “−25”
above blue's surviving 15 soldiers, and red's dashed outline with ×.
For equal groups, show two outlines with × and no loss labels. The summary reads
“Both groups defeated”. Shapes and text convey the outcome even when team colors
are hard to distinguish.

## Playback behavior

- Animate the impact and fade over roughly 300–400 ms at normal playback speed.
  This fits within the replay's current 500 ms frame interval. Tune timing in the
  real renderer; the comparison intentionally plays more slowly for inspection.
- Keep a subdued impact ring, defeated outlines, and the text summary visible
  for the resulting generation. A brief flash alone cannot explain a paused frame.
- Stepping or scrubbing to a generation shows that generation's settled markers
  immediately. Going backward clears later effects. Repeated rendering of the
  same generation should not restart the animation.
- Clear the previous generation's overlays when moving on. Preserve past events
  in replay data so revisiting a generation restores its explanation.
- Honor reduced-motion preferences with static markers and text. Keep the summary
  available without hover, for keyboard and touch users as well.
- For simultaneous clashes, draw an impact ring for each event and list their summaries.
  Multi-group clashes need losses for each surviving participant, not an assumed winning pair.
  Castle attacks/captures should eventually have distinct markers; friendly
  arrivals should never be labelled as clashes.

## Game integration

The [SVG replay](../backend/services/replay.html) renders recorded clash events in
both live games and saved history. A vanished group could have been defeated or
arrived at a castle, so comparing snapshots is not a reliable event source.
The separate [ECharts debug graph](../backend/services/game_graph.go) still shows
generation snapshots without clash overlays.

The simulation already calculates ordered clashes with a time within the turn,
participant positions, and remaining soldiers in
[`internal.Clash`](../backend/dal/commandHandlers/move_soldier_groups/internal/models.go).
The integration uses those results as the event source:

1. Capture immutable event values during resolution, before group updates or
   deletion. Include an event ID scoped to its generation, turn fraction,
   persistent group ID, team, contact position, and soldier counts before and after
   each clash. Explicitly capture the count before **each event**: a group can
   participate in multiple clashes in one turn, so its starting generation count
   is insufficient for later losses. Do not retain mutable group pointers.
2. Persist events as JSON atomically with the resulting generation. Command retries
   reuse the saved events. Store persistent participant IDs as values so deleting a
   defeated group cannot remove its event history.
3. Add optional clash events to saved replay frames and the authorized generation
   response used by live play. Older replays with no events remain renderable;
   show no inferred clashes for them. The generated Huma schema and Python client
   expose the event records as well.
4. Render a separate overlay in the lobby view. Put group outlines at their recorded
   contact positions and the impact ring at the mean participant position. Keep
   markers at those exact positions even when they overlap other objects, without
   offset lines. Combine losses above groups surviving the full turn.
   Keep overlays out of soldier totals, selection, and collision logic. The
   animation must not change simulation timing or clash-stop upgrades.

## Verification cases

- Unequal clash: only surviving participants get loss labels, matching their
  actual before/after counts. Defeated participants get a dashed outline with ×,
  without loss numbers or “Defeated” text. No crossed-swords icon is shown.
- Equal clash: both groups have outlines with × and neither has a loss label.
- Multiple events, including repeated participation: correct event ordering,
  positions, and per-event losses; no duplicated effects on retry.
- Friendly arrival and castle capture: no false group-clash indicators.
- Pause, forward/backward step, arbitrary seek, and replay: deterministic overlays
  with no effects left over from another generation.
- Old replay data, reduced motion, and a narrow viewport: usable rendering and
  readable summaries.
