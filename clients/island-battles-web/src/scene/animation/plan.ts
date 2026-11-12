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
import { castleDisplayLevel } from "../castleLevel";
import { groupLook, insideWaterline, type SquadLook } from "../shore";

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

/**
 * A group is hidden before `t0` and grows to full size by `t1`: a group whose origin castle is
 * not known, which appears at its first contact. A group launched from a known castle needs
 * none: its squad walks out of the gate as its boat is built (shore.ts `groupLook`).
 */
export interface SpawnTrack {
  kind: "spawn";
  groupId: number;
  t0: number;
  t1: number;
}

/**
 * A group lands at its castle: it shrinks from `t0` and is gone at `t1`. With a known castle
 * both are the landing time, when its squad reaches the castle's centre (the squad has already
 * faded into the gate, shore.ts `groupLook`).
 */
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

/**
 * A boat that a clash has stopped is rattled, as `rattle` goes from 0 at `t0` to 1 at `t1`: from
 * the clash to the end of the turn, or a short burst at the start of a turn it is still held.
 */
export interface RattleTrack {
  kind: "rattle";
  groupId: number;
  t0: number;
  t1: number;
  /**
   * The direction of the opponents of the clash that holds it (map units, unit length), so a
   * squad held on land faces them (#213); missing when not known.
   */
  facing?: { x: number; y: number };
  /** Until when in the turn the squad faces its opponents: 1 when stopped, `t1` when held. */
  facingUntil?: number;
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
  | RattleTrack
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
/** How long a group that appears at its first contact takes to grow to full size. */
export const SPAWN_SPAN = 0.15;

/** How much of the turn the rattle of a boat that is still held lasts, from the start. */
export const HELD_RATTLE_SPAN = 0.3;

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

/** Map units: positions closer than this are the same place. */
const SAME_PLACE = 1e-6;

function samePlace(
  left: { x: number; y: number },
  right: { x: number; y: number },
): boolean {
  return Math.hypot(left.x - right.x, left.y - right.y) <= SAME_PLACE;
}

type Clash = Generation["clashes"][number];

/**
 * Whether a clash stops its participants; mirrors `shouldStop` in
 * backend/simulation/calculate_clash_positions.go. Some two opposing participants must be
 * balanced: the smaller group, by its count before the clash, at least half of the larger. In an
 * unbalanced clash the bigger group sails on.
 */
export function clashStops(clash: Clash): boolean {
  const { participants } = clash;
  return participants.some((participant, index) =>
    participants.slice(index + 1).some((opponent) => {
      if (participant.team_color === opponent.team_color) return false;
      const smaller = Math.min(
        participant.soldiers_before,
        opponent.soldiers_before,
      );
      const larger = Math.max(
        participant.soldiers_before,
        opponent.soldiers_before,
      );
      return smaller >= larger - smaller;
    }),
  );
}

/**
 * The time, as a fraction of the turn that ends in `next`, at which a clash stopped the group
 * `groupId`, or `undefined` when none did. The group must have survived its last clash of the
 * turn, that clash must stop its participants, and the group must be in `next` at the clash's
 * midpoint (the mean of the participants' contact positions, where the server stops them), so a
 * group that sailed on does not count. Compares true positions, not drawn ones.
 */
export function stoppedByClashAt(
  next: Generation,
  groupId: number,
): number | undefined {
  const group = next.soldier_groups.find((entry) => entry.id === groupId);
  if (!group) return undefined;
  let last: Clash | undefined;
  for (const clash of next.clashes) {
    if (!clash.participants.some((p) => p.group_id === groupId)) continue;
    if (!last || clash.time >= last.time) last = clash;
  }
  if (!last || !clashStops(last)) return undefined;
  const mine = last.participants.find((p) => p.group_id === groupId);
  if (!mine || mine.soldiers_after <= 0) return undefined;
  const count = last.participants.length;
  const midpoint = {
    x: last.participants.reduce((sum, p) => sum + p.x, 0) / count,
    y: last.participants.reduce((sum, p) => sum + p.y, 0) / count,
  };
  return samePlace(group, midpoint) ? clamp01(last.time) : undefined;
}

/**
 * Whether the group `groupId` is still held in the turn from `previous` to `next`: in both at
 * the same position, and not at its destination castle. Compares true positions, not drawn ones.
 * A group that a clash stopped this turn also stays in place; see `stoppedByClashAt`, which
 * takes precedence for the rattle.
 */
export function isHeld(
  previous: Generation,
  next: Generation,
  groupId: number,
): boolean {
  const before = previous.soldier_groups.find((entry) => entry.id === groupId);
  const after = next.soldier_groups.find((entry) => entry.id === groupId);
  if (!before || !after || !samePlace(before, after)) return false;
  const destination = [...next.castles, ...previous.castles].find(
    (castle) => castle.id === after.destination_castle_id,
  );
  return !destination || !samePlace(after, destination);
}

/**
 * Where the group `groupId` faces in the last clash of `generation` it took part in: towards
 * the mean contact position of its opponents (map units, unit length). Empty when it had none.
 */
export function facingOf(
  generation: Generation,
  groupId: number,
): { facing?: { x: number; y: number } } {
  let last: Clash | undefined;
  for (const clash of generation.clashes) {
    if (!clash.participants.some((p) => p.group_id === groupId)) continue;
    if (!last || clash.time >= last.time) last = clash;
  }
  const mine = last?.participants.find((p) => p.group_id === groupId);
  const opponents =
    last?.participants.filter((p) => p.team_color !== mine?.team_color) ?? [];
  if (!mine || opponents.length === 0) return {};
  const x =
    opponents.reduce((sum, p) => sum + p.x, 0) / opponents.length - mine.x;
  const y =
    opponents.reduce((sum, p) => sum + p.y, 0) / opponents.length - mine.y;
  const length = Math.hypot(x, y);
  return length > SAME_PLACE
    ? { facing: { x: x / length, y: y / length } }
    : {};
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
      // The server sends a group from its castle's centre: its squad walks out from there.
      const origin = castleAt(group.origin_castle_id);
      if (origin) {
        start = { x: origin.x, y: origin.y };
      } else {
        start = list[0]?.point ?? group;
        tracks.push({
          kind: "spawn",
          groupId: group.id,
          t0: 0,
          t1: SPAWN_SPAN,
        });
      }
    }
    const keys: PathKey[] = [
      { t: 0, ...start },
      ...contactKeys(list),
      { t: 1, x: group.x, y: group.y },
    ];
    tracks.push({ kind: "move", groupId: group.id, keys });
    const stoppedAt = stoppedByClashAt(next, group.id);
    if (stoppedAt !== undefined) {
      tracks.push({
        kind: "rattle",
        groupId: group.id,
        t0: stoppedAt,
        t1: 1,
        ...facingOf(next, group.id),
        facingUntil: 1,
      });
    } else if (isHeld(previous, next, group.id)) {
      tracks.push({
        kind: "rattle",
        groupId: group.id,
        t0: 0,
        t1: HELD_RATTLE_SPAN,
        // It faces its opponents at the start only if it did at rest: stopped last turn.
        ...(stoppedByClashAt(previous, group.id) !== undefined
          ? facingOf(previous, group.id)
          : {}),
        facingUntil: HELD_RATTLE_SPAN,
      });
    }
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
    // Move on from the last contact to the castle's centre at the group's speed, as the
    // server does, and land there: the moment the castle's count changes. The boat docks at
    // the shore on the way and the squad walks on (shore.ts `groupLook`).
    const speed = speedOf(group.upgrades);
    const distance = Math.hypot(castle.x - last.x, castle.y - last.y);
    const landing = Math.min(1, last.t + distance / speed);
    if (landing > last.t) keys.push({ t: landing, x: castle.x, y: castle.y });
    else keys[keys.length - 1] = { ...last, x: castle.x, y: castle.y };
    tracks.push({ kind: "move", groupId: group.id, keys });
    tracks.push({
      kind: "merge",
      groupId: group.id,
      castleId: group.destination_castle_id,
      t0: landing,
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
    // Its castle is one of its team whose turn adds up with these soldiers sent: replayed
    // exactly with them (which also holds when the castle is captured after they left), or
    // short of exactly them. Failing both, it is the castle of its team on whose island it
    // was lost (#213: a group sent out against attackers at its own gate).
    const sameTeam = castlePairs.filter(
      ({ before }) => before.team_color === first.point.teamColor,
    );
    const nearest = (candidates: typeof castlePairs) =>
      candidates
        .map(({ after }) => after)
        .sort(
          (left, right) =>
            mapDistance(left, first.point) - mapDistance(right, first.point),
        )[0];
    const origin =
      nearest(
        sameTeam.filter(({ before, after }) => {
          const replay = replayCastle(
            before,
            after,
            (launched.get(after.id) ?? 0) + initial,
            arrivals.get(after.id) ?? [],
          );
          return (
            replay.soldiers === after.soldiers &&
            replay.team === after.team_color
          );
        }),
      ) ??
      nearest(
        sameTeam.filter(
          ({ before, after }) =>
            replayCastle(
              before,
              after,
              launched.get(after.id) ?? 0,
              arrivals.get(after.id) ?? [],
            ).soldiers -
              after.soldiers ===
            initial,
        ),
      ) ??
      nearest(
        sameTeam.filter(({ before }) => insideWaterline(before, first.point)),
      );
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
      keys.unshift({ t: 0, x: origin.x, y: origin.y });
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

/** The team colour of castles that belong to no team; they don't grow. */
const NEUTRAL_TEAM = 0;

/**
 * Replays the turn's arithmetic for a castle: its commands at t = 0 (soldiers sent, upgrades
 * paid), growth by one, then the landings in group ID order. The landing times are handed out
 * in time order. Only a castle that a team owns grows, and growth comes before the landings, as
 * on the server: a neutral castle keeps its soldiers, and one captured this turn was still
 * neutral at the growth step.
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
  if (before.team_color !== NEUTRAL_TEAM) {
    soldiers += 1;
    pushStep(countKeys, growth, soldiers);
  }
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
  /** Where the boat is drawn (map units): at a shore while the group is on land. */
  x: number;
  y: number;
  /** The boat's size factor: 1 at full size; less while it appears or docks and shrinks away. */
  scale: number;
  /** How much of the boat is built at its origin's shore: 0 none, 1 complete. */
  build: number;
  /** The squad walking on land, or null while the group is afloat (shore.ts `groupLook`). */
  squad: SquadLook | null;
  /** 0 afloat to 1 gone under. */
  sink: number;
  /** 0 before a boat that a clash holds is rattled, to 1 when its rattle is over. */
  rattle: number;
  /** Where a group held by a clash faces its opponents (map units), while it is rattled. */
  fighting: { x: number; y: number } | null;
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
  /** Where the participant is drawn: its squad on land, else its boat (#213). */
  x: number;
  y: number;
  /** Whether it fights on land: no splash, dust instead (#213). */
  onLand: boolean;
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

interface Route {
  origin: Castle | undefined;
  destination: Castle | undefined;
  /** Where a group with no known destination heads: its first contact. */
  heading?: { x: number; y: number };
}

const routes = new WeakMap<TurnPlan, Map<number, Route>>();

/**
 * The castles each group of a plan sails between, found once per plan. A group in neither
 * generation has the origin its transient was guessed to have; a castle that is not known (ID
 * -1) is `undefined`.
 */
function routesOf(plan: TurnPlan): Map<number, Route> {
  const known = routes.get(plan);
  if (known) return known;
  const castles = new Map(
    [...plan.previous.castles, ...plan.next.castles].map((c) => [c.id, c]),
  );
  const found = new Map<number, Route>(
    [
      ...plan.previous.soldier_groups,
      ...plan.transients,
      ...plan.next.soldier_groups,
    ].map((group): [number, Route] => {
      const destination = castles.get(group.destination_castle_id);
      return [
        group.id,
        destination
          ? { origin: castles.get(group.origin_castle_id), destination }
          : {
              origin: castles.get(group.origin_castle_id),
              destination,
              // A transient sent and defeated within the turn is built at its first contact.
              heading: { x: group.x, y: group.y },
            },
      ];
    }),
  );
  routes.set(plan, found);
  return found;
}

/** The state of every animated thing at `time` in [0, 1] of the turn. */
export function sampleTurn(plan: TurnPlan, time: number): TurnSample {
  const t = clamp01(time);
  const look = (groupId: number, position: { x: number; y: number }) => {
    const route = routesOf(plan).get(groupId);
    return groupLook(
      position,
      route?.origin,
      route?.destination,
      route?.heading,
    );
  };
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
      pose = {
        x: 0,
        y: 0,
        scale: 1,
        build: 1,
        squad: null,
        sink: 0,
        rattle: 0,
        fighting: null,
        visible: true,
        count: 0,
      };
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
      case "move": {
        const pose = group(track.groupId);
        const drawn = look(track.groupId, pathAt(track.keys, t));
        pose.x = drawn.x;
        pose.y = drawn.y;
        pose.scale *= drawn.boatScale;
        pose.build = drawn.build;
        pose.squad = drawn.squad;
        break;
      }
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
      case "rattle": {
        const pose = group(track.groupId);
        pose.rattle = progress(t, track.t0, track.t1);
        pose.fighting =
          t >= track.t0 && t <= (track.facingUntil ?? track.t1)
            ? (track.facing ?? null)
            : null;
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
          // A clash is drawn where its participants are drawn, so they stay together: between
          // the squads when they meet on land (#213), at the boats at sea or at a shore.
          const drawn = look(point.groupId, point);
          const at = drawn.squad ?? drawn;
          flashes.push({
            clashId: track.clashId,
            groupId: point.groupId,
            teamColor: point.teamColor,
            x: at.x,
            y: at.y,
            onLand: drawn.squad !== null,
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
