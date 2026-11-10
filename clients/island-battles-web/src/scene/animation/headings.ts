// The heading arrows during a turn (#168 case 13). Pure: no Babylon, so `bun test` runs it.
//
// Which groups have an arrow, their lanes and their shared heads are decided once per turn, from
// the layout of the turn's start (the previous generation, as it was shown) and of its end (the
// next generation, as it will be shown). Each frame then only moves the arrows with the boats:
// - a group with an arrow at both ends slides from its start lane and tip to its end ones;
// - a group with one only at the end fades in over the turn, and a group launched this turn gets
//   its arrow once it has left the shore;
// - a group with one only at the start fades out over the turn, or until it sinks; a group that
//   lands this turn keeps its arrow, getting shorter, until it lands;
// - an arrow too short for its head and a little shaft fades out over `fadeLength`.
// So no arrow pops in or out or changes lane in the middle of a turn.

import type { components } from "../../api/generated/game";
import type { MapPoint } from "../coordinates";
import { toMapDistance } from "../coordinates";
import {
  candidateOf,
  computeHeadings,
  type HeadingSegment,
  type HeadingStyle,
  headingStyle,
  type LaidLine,
  launchFade,
  launchPoint,
  layoutLines,
  mapSizes,
  segmentOf,
  startClearance,
} from "../headingLines";
import {
  isHeld,
  stoppedByClashAt,
  type TurnPlan,
  type TurnSample,
} from "./plan";

type Generation = components["schemas"]["Generation"];
type SoldierGroup = components["schemas"]["SoldierGroup"];

/** How long, as a fraction of the turn, an arrow takes to dim or brighten. */
const DIM_SPAN = 0.08;

// Which groups are not moving is #169's rule (plan.ts), shared with the rattle; these wrappers
// give it as sets of group IDs.

/** Groups of `generation` that one of its clashes stopped (`stoppedByClashAt`). */
export function clashHeld(generation: Generation): Set<number> {
  return new Set(
    generation.soldier_groups
      .map((group) => group.id)
      .filter((id) => stoppedByClashAt(generation, id) !== undefined),
  );
}

/**
 * Groups that are not moving at the end of the turn from `previous` to `next`: stopped by a
 * clash this turn, or still held where they were (`isHeld`).
 */
export function heldGroups(
  previous: Generation,
  next: Generation,
): Set<number> {
  const held = clashHeld(next);
  for (const group of next.soldier_groups) {
    if (isHeld(previous, next, group.id)) held.add(group.id);
  }
  return held;
}

/** The decided arrow of one group for one turn. */
export interface TurnHeading {
  groupId: number;
  team: number;
  castleId: number;
  start: LaidLine | null;
  end: LaidLine | null;
  /** Where the group left its island: its arrow waits until it has left there. */
  launch: MapPoint | null;
  /** The group was launched this turn. */
  launched: boolean;
  /** The group lands this turn. */
  landing: boolean;
  /** When a group gone by the end has gone (sinks, from its sink's start). */
  until: number;
  /** Dimmed at the start; dimmed from `dimFrom` on (or not at all when null). */
  dimStart: boolean;
  dimFrom: number | null;
  startClear: (scale: number) => number;
}

export interface TurnHeadings {
  lines: TurnHeading[];
  castles: { id: number; x: number; y: number }[];
  style: HeadingStyle;
}

function layoutOf(
  generation: Generation,
  castles: Generation["castles"],
  held: ReadonlySet<number>,
  boatScale: (soldiers: number) => number,
  style: HeadingStyle,
): Map<number, LaidLine> {
  const castleById = new Map(castles.map((castle) => [castle.id, castle]));
  const candidates = generation.soldier_groups
    .map((group) =>
      candidateOf(group, castleById, {
        style,
        held,
        boatScale: (g) => boatScale(g.soldiers),
      }),
    )
    .filter((candidate) => candidate !== null);
  return new Map(
    layoutLines(candidates, mapSizes(style)).map((line) => [line.id, line]),
  );
}

/**
 * Decides the arrows of a turn. `heldAtStart` is the set the previous generation was drawn
 * with; `boatScale` is the size a boat is drawn at for its soldiers.
 */
export function planTurnHeadings(
  plan: TurnPlan,
  heldAtStart: ReadonlySet<number>,
  boatScale: (soldiers: number) => number,
  style: HeadingStyle = headingStyle,
): TurnHeadings {
  const castles = [
    ...new Map(
      [...plan.previous.castles, ...plan.next.castles].map((c) => [c.id, c]),
    ).values(),
  ];
  const heldAtEnd = heldGroups(plan.previous, plan.next);
  const start = layoutOf(plan.previous, castles, heldAtStart, boatScale, style);
  const end = layoutOf(plan.next, castles, heldAtEnd, boatScale, style);
  const previousIds = new Set(plan.previous.soldier_groups.map((g) => g.id));
  const nextIds = new Set(plan.next.soldier_groups.map((g) => g.id));
  const sinkAt = new Map<number, number>();
  const mergeOf = new Set<number>();
  for (const track of plan.tracks) {
    if (track.kind === "sink") sinkAt.set(track.groupId, track.t0);
    if (track.kind === "merge") mergeOf.add(track.groupId);
  }
  // Where a clash stopped a group this turn, the arrow dims from the clash on.
  const stoppedAt = new Map<number, number>();
  for (const group of plan.next.soldier_groups) {
    const time = stoppedByClashAt(plan.next, group.id);
    if (time !== undefined) stoppedAt.set(group.id, time);
  }
  const castleById = new Map(castles.map((c) => [c.id, c]));
  const groups = new Map<number, SoldierGroup>(
    [...plan.previous.soldier_groups, ...plan.next.soldier_groups].map((g) => [
      g.id,
      g,
    ]),
  );
  const ids = [...new Set([...start.keys(), ...end.keys()])].sort(
    (left, right) => left - right,
  );
  const lines: TurnHeading[] = [];
  for (const id of ids) {
    const group = groups.get(id);
    const line = end.get(id) ?? start.get(id);
    if (!group || !line) continue;
    const dimEnd = heldAtEnd.has(id);
    lines.push({
      groupId: id,
      team: line.team,
      castleId: line.castleId,
      start: start.get(id) ?? null,
      end: end.get(id) ?? null,
      launch: launchPoint(
        castleById.get(group.origin_castle_id),
        castleById.get(group.destination_castle_id),
      ),
      launched: !previousIds.has(id),
      landing: !nextIds.has(id) && mergeOf.has(id) && !sinkAt.has(id),
      until: sinkAt.get(id) ?? 1,
      dimStart: heldAtStart.has(id),
      // Fully dimmed by the end, as the resting view of the next generation is.
      dimFrom: dimEnd ? Math.min(stoppedAt.get(id) ?? 0, 1 - DIM_SPAN) : null,
      startClear: (scale) => startClearance(scale, style),
    });
  }
  return { lines, castles, style };
}

const lerp = (from: number, to: number, s: number) => from + (to - from) * s;
const clamp01 = (value: number) => Math.min(1, Math.max(0, value));

function unit(dx: number, dy: number): MapPoint {
  const length = Math.hypot(dx, dy);
  return length === 0 ? { x: 0, y: 0 } : { x: dx / length, y: dy / length };
}

/** How dimmed (0 to 1) an arrow is at `t`. */
function dimming(line: TurnHeading, t: number): number {
  if (line.dimFrom === null) {
    return line.dimStart ? 1 - clamp01(t / DIM_SPAN) : 0;
  }
  if (line.dimStart) return 1;
  return clamp01((t - line.dimFrom) / DIM_SPAN);
}

/**
 * The arrows to draw at a sample of the turn. Per frame this only moves points: the layout was
 * decided by `planTurnHeadings`.
 */
export function sampleTurnHeadings(
  turn: TurnHeadings,
  sample: TurnSample,
  boatScale: (soldiers: number) => number,
): HeadingSegment[] {
  const { style } = turn;
  const sizes = mapSizes(style);
  const t = sample.t;
  const segments: HeadingSegment[] = [];
  turn.lines.forEach((line, rank) => {
    const pose = sample.groups.get(line.groupId);
    if (!pose?.visible) return;
    const from = line.start ?? line.end;
    const to = line.end ?? line.start;
    if (!from || !to) return;
    const s = line.start && line.end ? t : 0;
    const lane = {
      x: lerp(from.lane.x, to.lane.x, s),
      y: lerp(from.lane.y, to.lane.y, s),
    };
    const tip = {
      x: lerp(from.tip.x, to.tip.x, s),
      y: lerp(from.tip.y, to.tip.y, s),
    };
    const castle = turn.castles.find((c) => c.id === line.castleId);
    if (!castle) return;
    const position = { x: pose.x + lane.x, y: pose.y + lane.y };
    // The head turns gradually between the two ends' ways of pointing (at the castle, or
    // along the line).
    const pointing = (radial: boolean) =>
      radial
        ? unit(castle.x - tip.x, castle.y - tip.y)
        : unit(tip.x - position.x, tip.y - position.y);
    const fromDirection = pointing(from.radialHead);
    const toDirection = pointing(to.radialHead);
    const direction = unit(
      lerp(fromDirection.x, toDirection.x, s),
      lerp(fromDirection.y, toDirection.y, s),
    );
    // A head one end draws is drawn all turn: heads are shared only within a team, so where
    // one end has no head of its own it shares an identical one at the same tip, and the
    // two heads coincide there.
    const arrowhead = s < 1 ? from.arrowhead || to.arrowhead : to.arrowhead;
    const base = {
      x: tip.x - direction.x * sizes.headLength,
      y: tip.y - direction.y * sizes.headLength,
    };
    const clear = line.startClear(boatScale(pose.count) * pose.scale);
    const toBase = unit(base.x - position.x, base.y - position.y);
    const shaft =
      (base.x - position.x) * toBase.x +
      (base.y - position.y) * toBase.y -
      clear;
    // Measured towards the castle, so a base behind the boat is no shaft at all.
    const ahead =
      (base.x - position.x) * (castle.x - position.x) +
        (base.y - position.y) * (castle.y - position.y) >
      0;
    if (!ahead || shaft < sizes.minShaft) return;
    let opacity = clamp01(
      (shaft - sizes.minShaft) / toMapDistance(style.fadeLength),
    );
    if (line.start && !line.end) {
      // Gone by the end: a landing group keeps its arrow until it lands; others fade out.
      if (!line.landing) opacity *= 1 - clamp01(t / Math.max(line.until, 1e-9));
    } else if (!line.start && line.end && !line.launched) {
      opacity *= t;
    }
    // A boat at its island's shore has no arrow yet; it fades in as the boat sails off.
    opacity *= launchFade(pose, line.launch, style);
    opacity *= 1 - (1 - style.heldOpacity) * dimming(line, t);
    if (opacity <= 0) return;
    const start = {
      x: position.x + toBase.x * clear,
      y: position.y + toBase.y * clear,
    };
    segments.push(
      segmentOf(
        {
          id: line.groupId,
          team: line.team,
          castleId: line.castleId,
          start,
          tip,
          base,
          arrowhead,
          held: false,
        },
        rank,
        turn.castles,
        style,
        opacity,
      ),
    );
  });
  return segments;
}

/** The arrows of a generation drawn at rest, with `held` dimmed: the turn's end looks the same. */
export function restingHeadings(
  generation: Generation,
  held: ReadonlySet<number>,
  boatScale: (soldiers: number) => number,
  style: HeadingStyle = headingStyle,
): HeadingSegment[] {
  return computeHeadings(generation.castles, generation.soldier_groups, {
    style,
    held,
    boatScale: (group) => boatScale(group.soldiers),
  });
}
