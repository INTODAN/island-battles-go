// Plans what happens between two generations. Pure: no Babylon, no DOM, so `bun test` runs it.
//
// A generation holds end-of-turn positions only. A turn's clashes carry `time` (the fraction of
// the turn at which contact happened) and each participant's contact position and soldier counts
// (backend/models/clash.go). The simulation (backend/simulation) runs a turn as: the players'
// commands (sends and upgrades) at t = 0, then every group moves in a straight line at its speed
// and clashes on the way, then every castle gains one soldier, then groups that reached their
// castle land, in group ID order. Group IDs are never reused, so an ID names one group for the
// whole game.

import type { components } from "../../api/generated/game";
import { CASTLE_RADIUS } from "../board";
import { castleDisplayLevel } from "../castleLevel";

type Generation = components["schemas"]["Generation"];
type Castle = components["schemas"]["Castle"];
type SoldierGroup = components["schemas"]["SoldierGroup"];

export type Ease = "linear" | "in" | "out" | "inOut";

/** A point on a map, in map units (see coordinates.ts). */
export interface PathKey {
  t: number;
  x: number;
  y: number;
  /** How the segment that ends at this key is eased. Missing means linear. */
  ease?: Ease;
}

export interface CountKey {
  t: number;
  value: number;
}

/** A group's path. Between keys it is a straight line; at and after the last key it stays. */
export interface MoveTrack {
  kind: "move";
  groupId: number;
  keys: PathKey[];
}

/** A group's soldier count. Counts are rolled between keys and rounded. */
export interface GroupCountTrack {
  kind: "groupCount";
  groupId: number;
  keys: CountKey[];
}

/** A castle's soldier count. */
export interface CastleCountTrack {
  kind: "castleCount";
  castleId: number;
  /** False when the turn's arithmetic did not add up and the count just ticks to the result. */
  replayed: boolean;
  keys: CountKey[];
}

/** A group is hidden before `t0` and grows to full size by `t1`: it has just left a castle. */
export interface SpawnTrack {
  kind: "spawn";
  groupId: number;
  t0: number;
  t1: number;
}

/** A group arrives at a castle: it shrinks into it from `t0` and is gone at `t1`. */
export interface MergeTrack {
  kind: "merge";
  groupId: number;
  castleId: number;
  t0: number;
  t1: number;
}

/** A defeated group sinks where it was lost, from `t0` to `t1`, and is gone at `t1`. */
export interface SinkTrack {
  kind: "sink";
  groupId: number;
  t0: number;
  t1: number;
}

/** A castle's team over the turn, as `team` from each key's `t` on; the first key is at 0. */
export interface CastleTeamTrack {
  kind: "castleTeam";
  castleId: number;
  keys: { t: number; team: number }[];
}

/** A castle's display level changes at `t` (the start of the turn). A pop effect hooks on it. */
export interface CastleLevelTrack {
  kind: "castleLevel";
  castleId: number;
  t: number;
  from: number;
  to: number;
}

export interface ClashPoint {
  groupId: number;
  teamColor: number;
  x: number;
  y: number;
  radius: number;
  soldiersBefore: number;
  soldiersAfter: number;
}

/** A clash effect at `t`, at each participant's position. */
export interface ClashTrack {
  kind: "clash";
  clashId: number;
  t: number;
  points: ClashPoint[];
}

export type Track =
  | MoveTrack
  | GroupCountTrack
  | CastleCountTrack
  | SpawnTrack
  | MergeTrack
  | SinkTrack
  | CastleTeamTrack
  | CastleLevelTrack
  | ClashTrack;

export interface TurnPlan {
  previous: Generation;
  next: Generation;
  tracks: Track[];
  /**
   * Groups drawn for this turn that the next generation does not have: those that arrive or are
   * defeated, taken from the previous generation. A group that was sent and defeated within the
   * same turn is in neither generation; it is built from its clash, and appears at its contact.
   */
  transients: SoldierGroup[];
}

/** How long, as a fraction of the turn, a count takes to roll from one value to the next. */
export const TICK_SPAN = 0.06;
/** How long a clash flash lasts. */
export const FLASH_SPAN = 0.25;
/** How long a defeated group takes to sink, at most. */
export const SINK_SPAN = 0.4;
/** How long a launched group takes to grow to full size. */
export const SPAWN_SPAN = 0.15;

const SPEED_BY_LEVEL = [0.5, 1, 1.5, 2];
/** Soldier cost of reaching each level, from level 1; mirrors backend/models/upgrades.go. */
export const UPGRADE_COSTS: Readonly<Record<string, readonly number[]>> = {
  speed: [0, 10, 20, 30],
  clash_stop_generations: [0, 10, 20],
};

/** World units a group moves per turn, from its snapshotted speed upgrade. */
export function speedOf(upgrades: Record<string, number>): number {
  const level = Math.min(
    SPEED_BY_LEVEL.length,
    Math.max(1, Math.trunc(upgrades.speed ?? 1)),
  );
  return SPEED_BY_LEVEL[level - 1] ?? 0.5;
}

function upgradeCost(
  before: Record<string, number>,
  after: Record<string, number>,
): number {
  let cost = 0;
  for (const type of new Set([...Object.keys(before), ...Object.keys(after)])) {
    const costs = UPGRADE_COSTS[type];
    if (!costs) continue;
    const from = (before[type] ?? 1) + 1;
    for (let level = from; level <= (after[type] ?? 1); level++) {
      cost += costs[level - 1] ?? 0;
    }
  }
  return cost;
}

interface Participation {
  clashId: number;
  t: number;
  point: ClashPoint;
}

function clamp01(value: number): number {
  return Math.min(1, Math.max(0, value));
}

/** Adds a rolled change of a count to `to` at `t`, holding the old value until then. */
function pushStep(keys: CountKey[], t: number, to: number): void {
  const last = keys[keys.length - 1];
  if (!last || last.value === to) return;
  const start = Math.min(Math.max(t, last.t), 1 - TICK_SPAN);
  if (start > last.t) keys.push({ t: start, value: last.value });
  keys.push({ t: start + TICK_SPAN, value: to });
}

/** Ends a count track at `end` at t = 1. */
function finishCount(keys: CountKey[], end: number): CountKey[] {
  pushStep(keys, 1 - TICK_SPAN, end);
  const last = keys[keys.length - 1];
  if (last && last.t < 1) keys.push({ t: 1, value: last.value });
  return keys;
}

function groupCountKeys(
  start: number,
  end: number,
  participations: readonly Participation[],
): CountKey[] {
  const keys: CountKey[] = [{ t: 0, value: start }];
  for (const { t, point } of participations) {
    pushStep(keys, t, point.soldiersBefore);
    pushStep(keys, t, point.soldiersAfter);
  }
  return finishCount(keys, end);
}

interface Arrival {
  group: SoldierGroup;
  soldiers: number;
  /** The time the group reaches the castle's centre, and so lands. */
  landing: number;
}

/** Where a group launched at `origin` starts: the castle's edge, toward where it first is. */
function edgePoint(
  origin: { x: number; y: number },
  toward: { x: number; y: number },
): { x: number; y: number } {
  const distance = Math.hypot(toward.x - origin.x, toward.y - origin.y);
  if (distance === 0) return { x: origin.x, y: origin.y };
  const reach = Math.min(CASTLE_RADIUS, distance);
  return {
    x: origin.x + ((toward.x - origin.x) / distance) * reach,
    y: origin.y + ((toward.y - origin.y) / distance) * reach,
  };
}

/**
 * Plans the turn between two consecutive generations as tracks over t in [0, 1]. At t = 1 every
 * position, count and team equals `next` exactly.
 */
export function planTurn(previous: Generation, next: Generation): TurnPlan {
  const tracks: Track[] = [];
  const transients: SoldierGroup[] = [];
  const previousGroups = new Map(previous.soldier_groups.map((g) => [g.id, g]));
  const nextGroups = new Map(next.soldier_groups.map((g) => [g.id, g]));
  const previousCastles = new Map(previous.castles.map((c) => [c.id, c]));
  const nextCastles = new Map(next.castles.map((c) => [c.id, c]));
  const castleAt = (id: number) =>
    nextCastles.get(id) ?? previousCastles.get(id);

  const clashes = [...next.clashes].sort(
    (left, right) => left.time - right.time || left.id - right.id,
  );
  const participations = new Map<number, Participation[]>();
  const defeated = new Set<number>();
  for (const clash of clashes) {
    const t = clamp01(clash.time);
    const points: ClashPoint[] = clash.participants.map((p) => ({
      groupId: p.group_id,
      teamColor: p.team_color,
      x: p.x,
      y: p.y,
      radius: p.clash_radius,
      soldiersBefore: p.soldiers_before,
      soldiersAfter: p.soldiers_after,
    }));
    tracks.push({ kind: "clash", clashId: clash.id, t, points });
    for (const point of points) {
      const list = participations.get(point.groupId) ?? [];
      list.push({ clashId: clash.id, t, point });
      participations.set(point.groupId, list);
      if (point.soldiersAfter === 0) defeated.add(point.groupId);
    }
  }

  /** Keys that put a group at each of its contacts, at the contact's time. */
  const contactKeys = (list: readonly Participation[]): PathKey[] =>
    list.map(({ t, point }) => ({ t, x: point.x, y: point.y }));

  const sinkAt = (groupId: number, t0: number): SinkTrack => ({
    kind: "sink",
    groupId,
    t0,
    t1: Math.min(1, t0 + SINK_SPAN),
  });

  const launched = new Map<number, number>(); // castle ID -> soldiers that left it
  const arrivals = new Map<number, Arrival[]>(); // castle ID -> groups landing there

  // Groups in the next generation: sailing on, or just launched.
  for (const group of next.soldier_groups) {
    const before = previousGroups.get(group.id);
    const list = participations.get(group.id) ?? [];
    let start: { x: number; y: number };
    if (before) {
      start = { x: before.x, y: before.y };
    } else {
      const origin = castleAt(group.origin_castle_id);
      const first = list[0]?.point ?? group;
      start = origin ? edgePoint(origin, first) : first;
      tracks.push({ kind: "spawn", groupId: group.id, t0: 0, t1: SPAWN_SPAN });
    }
    const keys: PathKey[] = [
      { t: 0, ...start },
      ...contactKeys(list),
      { t: 1, x: group.x, y: group.y },
    ];
    if (!before && keys[1]) keys[1].ease = "in";
    tracks.push({ kind: "move", groupId: group.id, keys });
    const initial = before
      ? before.soldiers
      : (list[0]?.point.soldiersBefore ?? group.soldiers);
    tracks.push({
      kind: "groupCount",
      groupId: group.id,
      keys: groupCountKeys(initial, group.soldiers, list),
    });
    if (!before) {
      launched.set(
        group.origin_castle_id,
        (launched.get(group.origin_castle_id) ?? 0) + initial,
      );
    }
  }

  // Groups only in the previous generation: defeated, or arriving at their castle.
  for (const group of previous.soldier_groups) {
    if (nextGroups.has(group.id)) continue;
    transients.push(group);
    const list = participations.get(group.id) ?? [];
    const keys: PathKey[] = [
      { t: 0, x: group.x, y: group.y },
      ...contactKeys(list),
    ];
    const remaining = list.at(-1)?.point.soldiersAfter ?? group.soldiers;
    tracks.push({
      kind: "groupCount",
      groupId: group.id,
      keys: groupCountKeys(group.soldiers, remaining, list),
    });
    if (defeated.has(group.id)) {
      const lost = list.find((entry) => entry.point.soldiersAfter === 0);
      tracks.push({ kind: "move", groupId: group.id, keys });
      tracks.push(sinkAt(group.id, lost?.t ?? 1));
      continue;
    }
    const castle = castleAt(group.destination_castle_id);
    const last = keys[keys.length - 1] as PathKey;
    if (!castle) {
      tracks.push({ kind: "move", groupId: group.id, keys });
      tracks.push({
        kind: "merge",
        groupId: group.id,
        castleId: group.destination_castle_id,
        t0: 0.9,
        t1: 1,
      });
      continue;
    }
    // Sail on from the last contact to the castle's edge, then in to its centre where it lands.
    const speed = speedOf(group.upgrades);
    const distance = Math.hypot(castle.x - last.x, castle.y - last.y);
    const landing = Math.min(1, last.t + distance / speed);
    const reach = Math.min(distance, CASTLE_RADIUS);
    const edgeTime = Math.max(last.t, landing - reach / speed);
    const edge =
      distance <= CASTLE_RADIUS
        ? { x: last.x, y: last.y }
        : {
            x: castle.x + ((last.x - castle.x) / distance) * CASTLE_RADIUS,
            y: castle.y + ((last.y - castle.y) / distance) * CASTLE_RADIUS,
          };
    keys.push({ t: edgeTime, ...edge });
    keys.push({ t: landing, x: castle.x, y: castle.y, ease: "out" });
    tracks.push({ kind: "move", groupId: group.id, keys });
    tracks.push({
      kind: "merge",
      groupId: group.id,
      castleId: group.destination_castle_id,
      t0: edgeTime,
      t1: landing,
    });
    const landed = arrivals.get(group.destination_castle_id) ?? [];
    landed.push({ group, soldiers: remaining, landing });
    arrivals.set(group.destination_castle_id, landed);
  }

  // Groups in neither generation: sent and defeated within the turn. Their castle is found by
  // the soldiers missing from it; without one, the group appears at its first contact.
  const castlePairs = next.castles.flatMap((after) => {
    const before = previousCastles.get(after.id);
    return before ? [{ before, after }] : [];
  });
  for (const [groupId, list] of participations) {
    if (previousGroups.has(groupId) || nextGroups.has(groupId)) continue;
    const first = list[0];
    if (!first) continue;
    const lost = list.find((entry) => entry.point.soldiersAfter === 0);
    const initial = first.point.soldiersBefore;
    const origin = castlePairs
      .filter(
        ({ before, after }) =>
          before.team_color === first.point.teamColor &&
          replayCastle(
            before,
            after,
            launched.get(after.id) ?? 0,
            arrivals.get(after.id) ?? [],
          ).soldiers -
            after.soldiers ===
            initial,
      )
      .map(({ after }) => after)
      .sort(
        (left, right) =>
          mapDistance(left, first.point) - mapDistance(right, first.point),
      )[0];
    transients.push({
      id: groupId,
      team_color: first.point.teamColor,
      soldiers: initial,
      x: first.point.x,
      y: first.point.y,
      origin_castle_id: origin?.id ?? -1,
      destination_castle_id: -1,
      clash_radius: first.point.radius,
      upgrades: {},
    });
    const keys = contactKeys(list);
    if (origin) {
      launched.set(origin.id, (launched.get(origin.id) ?? 0) + initial);
      keys.unshift({ t: 0, ...edgePoint(origin, first.point) });
      if (keys[1]) keys[1].ease = "in";
      tracks.push({ kind: "spawn", groupId, t0: 0, t1: SPAWN_SPAN });
    } else {
      tracks.push({ kind: "spawn", groupId, t0: first.t, t1: first.t });
    }
    tracks.push({ kind: "move", groupId, keys });
    tracks.push({
      kind: "groupCount",
      groupId,
      keys: groupCountKeys(
        initial,
        list.at(-1)?.point.soldiersAfter ?? 0,
        list,
      ),
    });
    tracks.push(sinkAt(groupId, lost?.t ?? 1));
  }

  for (const { before, after } of castlePairs) {
    planCastle(
      tracks,
      before,
      after,
      launched.get(after.id) ?? 0,
      arrivals.get(after.id) ?? [],
    );
  }
  return { previous, next, tracks, transients };
}

function mapDistance(
  left: { x: number; y: number },
  right: { x: number; y: number },
): number {
  return Math.hypot(left.x - right.x, left.y - right.y);
}

interface Replay {
  soldiers: number;
  team: number;
  countKeys: CountKey[];
  teamKeys: { t: number; team: number }[];
}

/**
 * Replays the turn's arithmetic for a castle: its commands at t = 0 (soldiers sent, upgrades
 * paid), growth by one, then the landings in group ID order. The landing times are handed out
 * in time order.
 */
function replayCastle(
  before: Castle,
  after: Castle,
  launched: number,
  arrived: readonly Arrival[],
): Replay {
  const landings = [...arrived].sort(
    (left, right) => left.group.id - right.group.id,
  );
  const times = arrived
    .map((arrival) => arrival.landing)
    .sort((left, right) => left - right);
  const growth =
    times.length > 0 ? Math.max(0.02, (times[0] ?? 0) - 0.05) : 0.9;
  const countKeys: CountKey[] = [{ t: 0, value: before.soldiers }];
  const teamKeys = [{ t: 0, team: before.team_color }];
  let soldiers =
    before.soldiers - launched - upgradeCost(before.upgrades, after.upgrades);
  let team = before.team_color;
  pushStep(countKeys, 0, soldiers);
  soldiers += 1;
  pushStep(countKeys, growth, soldiers);
  landings.forEach((arrival, index) => {
    const time = times[index] ?? 1;
    if (arrival.group.team_color === team) {
      soldiers += arrival.soldiers;
    } else if (arrival.soldiers > soldiers) {
      team = arrival.group.team_color;
      soldiers = arrival.soldiers - soldiers;
      teamKeys.push({ t: time, team });
    } else {
      soldiers -= arrival.soldiers;
    }
    pushStep(countKeys, time, soldiers);
  });
  return { soldiers, team, countKeys, teamKeys };
}

function planCastle(
  tracks: Track[],
  before: Castle,
  after: Castle,
  launched: number,
  arrived: readonly Arrival[],
): void {
  const level = {
    from: castleDisplayLevel(before.upgrades),
    to: castleDisplayLevel(after.upgrades),
  };
  if (level.from !== level.to) {
    tracks.push({ kind: "castleLevel", castleId: after.id, t: 0, ...level });
  }

  const replay = replayCastle(before, after, launched, arrived);
  const replayed =
    replay.soldiers === after.soldiers && replay.team === after.team_color;
  let counts: CountKey[];
  let teamKeys = replay.teamKeys;
  if (replayed) {
    counts = finishCount(replay.countKeys, after.soldiers);
  } else {
    // Something happened that the generations do not show; tick straight to the result.
    counts = finishCount([{ t: 0, value: before.soldiers }], after.soldiers);
    teamKeys = [{ t: 0, team: before.team_color }];
    if (after.team_color !== before.team_color) {
      const landing = Math.max(
        1 - TICK_SPAN,
        ...arrived.map((arrival) => arrival.landing),
      );
      teamKeys.push({ t: landing, team: after.team_color });
    }
  }
  tracks.push({
    kind: "castleCount",
    castleId: after.id,
    replayed,
    keys: counts,
  });
  tracks.push({ kind: "castleTeam", castleId: after.id, keys: teamKeys });
}

// ---------------------------------------------------------------------------------------------
// Sampling

export interface GroupPose {
  x: number;
  y: number;
  /** 1 at full size; less while launching or merging into a castle. */
  scale: number;
  /** 0 afloat to 1 gone under. */
  sink: number;
  visible: boolean;
  count: number;
}

export interface CastlePose {
  count: number;
  team: number;
  level: number;
}

export interface FlashPose {
  clashId: number;
  groupId: number;
  teamColor: number;
  x: number;
  y: number;
  radius: number;
  /** 0 at contact to 1 when the flash is over. */
  age: number;
}

export interface TurnSample {
  t: number;
  groups: Map<number, GroupPose>;
  castles: Map<number, CastlePose>;
  flashes: FlashPose[];
}

export function ease(kind: Ease | undefined, s: number): number {
  switch (kind) {
    case "in":
      return s * s;
    case "out":
      return 1 - (1 - s) * (1 - s);
    case "inOut":
      return s < 0.5 ? 2 * s * s : 1 - 2 * (1 - s) * (1 - s);
    default:
      return s;
  }
}

function pathAt(keys: readonly PathKey[], t: number): { x: number; y: number } {
  const first = keys[0] as PathKey;
  const last = keys[keys.length - 1] as PathKey;
  if (t <= first.t) return { x: first.x, y: first.y };
  if (t >= last.t) return { x: last.x, y: last.y };
  let index = 0;
  while (index < keys.length - 1 && (keys[index + 1] as PathKey).t <= t)
    index++;
  const from = keys[index] as PathKey;
  const to = keys[index + 1] as PathKey;
  const s = ease(to.ease, (t - from.t) / (to.t - from.t));
  return { x: from.x + (to.x - from.x) * s, y: from.y + (to.y - from.y) * s };
}

function countAt(keys: readonly CountKey[], t: number): number {
  const first = keys[0] as CountKey;
  const last = keys[keys.length - 1] as CountKey;
  if (t <= first.t) return first.value;
  if (t >= last.t) return last.value;
  let index = 0;
  while (index < keys.length - 1 && (keys[index + 1] as CountKey).t <= t)
    index++;
  const from = keys[index] as CountKey;
  const to = keys[index + 1] as CountKey;
  return Math.round(
    from.value + ((to.value - from.value) * (t - from.t)) / (to.t - from.t),
  );
}

function progress(t: number, t0: number, t1: number): number {
  if (t1 <= t0) return t >= t0 ? 1 : 0;
  return clamp01((t - t0) / (t1 - t0));
}

/** The state of every animated thing at `time` in [0, 1] of the turn. */
export function sampleTurn(plan: TurnPlan, time: number): TurnSample {
  const t = clamp01(time);
  const groups = new Map<number, GroupPose>();
  const castles = new Map<number, CastlePose>();
  for (const castle of plan.next.castles) {
    castles.set(castle.id, {
      count: castle.soldiers,
      team: castle.team_color,
      level: castleDisplayLevel(castle.upgrades),
    });
  }
  const flashes: FlashPose[] = [];
  const group = (id: number): GroupPose => {
    let pose = groups.get(id);
    if (!pose) {
      pose = { x: 0, y: 0, scale: 1, sink: 0, visible: true, count: 0 };
      groups.set(id, pose);
    }
    return pose;
  };
  const castle = (id: number): CastlePose => {
    let pose = castles.get(id);
    if (!pose) {
      pose = { count: 0, team: 0, level: 1 };
      castles.set(id, pose);
    }
    return pose;
  };
  for (const track of plan.tracks) {
    switch (track.kind) {
      case "move":
        Object.assign(group(track.groupId), pathAt(track.keys, t));
        break;
      case "groupCount":
        group(track.groupId).count = countAt(track.keys, t);
        break;
      case "spawn": {
        const pose = group(track.groupId);
        if (t < track.t0) pose.visible = false;
        else
          pose.scale *=
            0.3 + 0.7 * ease("out", progress(t, track.t0, track.t1));
        break;
      }
      case "merge": {
        const pose = group(track.groupId);
        pose.scale *= 1 - ease("in", progress(t, track.t0, track.t1));
        if (t >= track.t1) pose.visible = false;
        break;
      }
      case "sink": {
        const pose = group(track.groupId);
        pose.sink = progress(t, track.t0, track.t1);
        if (t >= track.t1) pose.visible = false;
        break;
      }
      case "castleCount":
        castle(track.castleId).count = countAt(track.keys, t);
        break;
      case "castleTeam": {
        let team = track.keys[0]?.team ?? 0;
        for (const key of track.keys) if (key.t <= t) team = key.team;
        castle(track.castleId).team = team;
        break;
      }
      case "castleLevel":
        castle(track.castleId).level = t > track.t ? track.to : track.from;
        break;
      case "clash": {
        const age = (t - track.t) / FLASH_SPAN;
        if (age < 0 || age > 1) break;
        for (const point of track.points) {
          flashes.push({
            clashId: track.clashId,
            groupId: point.groupId,
            teamColor: point.teamColor,
            x: point.x,
            y: point.y,
            radius: point.radius,
            age,
          });
        }
        break;
      }
    }
  }
  return { t, groups, castles, flashes };
}
