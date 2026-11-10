import type { components } from "../api/generated/game";
import { type MapPoint, toMapDistance } from "./coordinates";
import { drawnPosition, sharesLandmass, shorePoint, shoreReach } from "./shore";

type Castle = components["schemas"]["Castle"];
type SoldierGroup = components["schemas"]["SoldierGroup"];

// The heading arrows of #89, with the edge cases of #168 fixed. The rules (the 2D replay's
// heading-lines block in backend/services/replay.html follows the same ones, in pixels):
//
// - A group's line runs from just ahead of its boat's bow to the shore of its destination castle,
//   where the boat will land (#167). A line too short for its head and a short piece of shaft is
//   not drawn (case 9).
// - Groups of one team on one route are one line: the rearmost group's.
// - Visible lines that are nearly parallel and closer than a lane over a shared stretch are put
//   in parallel lanes, whatever their teams, origins and destinations (cases 3, 4 and 5).
// - At each castle, lines of one team arriving within 15 degrees in total share one arrowhead
//   (cases 2, 7 and 8); the other lines end at the base of that head (case 6). Then every head at
//   the castle, of every team, is spread along the shore so that no two touch (case 1).
// - A line leaves a gap where it passes another castle's model (case 12), and a group that is
//   not moving is drawn dimmed (case 14).
//
// Sizes in `headingStyle` are world units, so lines keep their look whatever WORLD_SCALE is.
// The computing below is in map coordinates, so each size goes through toMapDistance first.
export const headingStyle = {
  /** Width of a line, in world units. */
  lineWidth: 0.1,
  /** Extra width of the sea-coloured casing around a line, 2px (0.02) on each side as the owner picked 1B on #89; 0 draws none. */
  casingWidth: 0.04,
  arrowLength: 0.34,
  arrowWidth: 0.3,
  /** How far a line's colour is mixed toward white: a light tint of the team colour. */
  tint: 0.45,
  /** Height above the island tops, so lines show over land. Every line is at this one height (case 15). */
  height: 0.14,
  /** Gap between a boat's bow and the start of its line. */
  startGap: 0.06,
  /** The empty space between two lanes' casings, head included (case 3). */
  laneGap: 0.04,
  /** The least space between two arrowheads at one castle, casings included (case 1). */
  headGap: 0.06,
  /** The least shaft a line must show besides its head, or it is not drawn (case 9). */
  minShaft: 0.1,
  /** Over this much shaft a line that is getting too short fades out. */
  fadeLength: 0.2,
  /**
   * A boat that has just launched has no line while it is at its island's shore, and its line
   * fades in over this much sailing from there (#168 case 13).
   */
  launchFade: 0.3,
  /** Same-team lines arriving at a castle within this angle in total share one arrowhead. */
  sharedArrowDegrees: 15,
  /** Same-team lines to one castle within this angle are one route, drawn once (#89 case 3). */
  samePathDegrees: 1.5,
  /** Lines this close to parallel (or to opposite) can share a corridor and get lanes (case 4). */
  parallelDegrees: 4,
  /**
   * A line has a gap where it passes within this distance of another castle's centre: the
   * level 4 castle's base reaches 0.745 (landforms.ts), plus a margin (case 12).
   */
  castleGap: 0.85,
  /** The opacity of the line of a group that is not moving (case 14). */
  heldOpacity: 0.4,
} as const;

export type HeadingStyle = typeof headingStyle;

/**
 * How far a boat's bow reaches ahead of its position at scale 1, in world units: the tip of its
 * hull (HULL_SECTIONS in art.ts, which uses this value).
 */
export const BOAT_BOW = 0.215;

export interface HeadingSegment {
  groupId: number;
  teamColor: number;
  /** Where the line starts, just ahead of the boat's bow. */
  start: MapPoint;
  /** Where the arrowhead's tip sits, at the destination's shore. */
  tip: MapPoint;
  /** Where the shaft ends: the base of the arrowhead, its own or the one it shares. */
  base: MapPoint;
  /** Whether this line draws the arrowhead (lines sharing one let only one draw it). */
  arrowhead: boolean;
  /** Position in the stacking order; a higher rank is drawn over lower ones. */
  rank: number;
  /** Castles, other than the destination, that this line passes, leaving a gap. */
  passesUnder: number[];
  /** The stretches of the shaft left out, as distances from `start` along it. */
  gaps: [number, number][];
  /** 1, less while fading or for a group that is not moving. */
  opacity: number;
}

const radians = (degrees: number) => (degrees * Math.PI) / 180;

export function angleBetween(left: number, right: number): number {
  const difference = Math.abs(left - right) % (Math.PI * 2);
  return Math.min(difference, Math.PI * 2 - difference);
}

function unit(dx: number, dy: number): MapPoint {
  const length = Math.hypot(dx, dy);
  return length === 0 ? { x: 0, y: 0 } : { x: dx / length, y: dy / length };
}

/** The distance from `point` to the segment from `start` to `end`. */
export function distanceToSegment(
  point: MapPoint,
  start: MapPoint,
  end: MapPoint,
): number {
  const dx = end.x - start.x;
  const dy = end.y - start.y;
  const lengthSquared = dx * dx + dy * dy;
  const along =
    lengthSquared === 0
      ? 0
      : Math.max(
          0,
          Math.min(
            1,
            ((point.x - start.x) * dx + (point.y - start.y) * dy) /
              lengthSquared,
          ),
        );
  return Math.hypot(
    point.x - (start.x + dx * along),
    point.y - (start.y + dy * along),
  );
}

// ---------------------------------------------------------------------------------------------
// The layout, in any one unit. The 2D replay's planHeadings has the same steps.

/** A group that may get a line. */
export interface LineCandidate {
  id: number;
  team: number;
  /** Where the boat is drawn. */
  position: MapPoint;
  /** The line starts this far ahead of `position`. */
  startClear: number;
  castle: { id: number; x: number; y: number };
  /** How far from the castle's centre a tip arriving from `angle` sits. */
  reach: (angle: number) => number;
  held: boolean;
}

export interface LayoutSizes {
  /** A line's whole width, casing included. */
  lineWidth: number;
  headLength: number;
  /** A head's whole width, casing included. */
  headWidth: number;
  laneGap: number;
  headGap: number;
  minShaft: number;
  sharedRadians: number;
  samePathRadians: number;
  parallelRadians: number;
}

export interface LaidLine {
  id: number;
  team: number;
  castleId: number;
  /** The boat's position moved into its lane. */
  position: MapPoint;
  /** The lane's offset from the boat's position. */
  lane: MapPoint;
  start: MapPoint;
  tip: MapPoint;
  base: MapPoint;
  arrowhead: boolean;
  /**
   * Whether the head points at the castle's centre: a head shared with other lines, or one moved
   * aside from its line to keep clear of other heads. Otherwise it points along its line.
   */
  radialHead: boolean;
  held: boolean;
}

interface Route {
  candidate: LineCandidate;
  direction: MapPoint;
  /** The angle of the boat seen from the castle. */
  angle: number;
  distance: number;
  start: MapPoint;
  tip: MapPoint;
}

function routeOf(candidate: LineCandidate): Route | null {
  const { position, castle } = candidate;
  const distance = Math.hypot(castle.x - position.x, castle.y - position.y);
  if (!(distance > 0)) return null;
  const direction = unit(castle.x - position.x, castle.y - position.y);
  const angle = Math.atan2(position.y - castle.y, position.x - castle.x);
  const reach = candidate.reach(angle);
  return {
    candidate,
    direction,
    angle,
    distance,
    start: {
      x: position.x + direction.x * candidate.startClear,
      y: position.y + direction.y * candidate.startClear,
    },
    tip: {
      x: castle.x + Math.cos(angle) * reach,
      y: castle.y + Math.sin(angle) * reach,
    },
  };
}

/** The visible length of a line from `start` to `end` heading along `direction`. */
function along(start: MapPoint, end: MapPoint, direction: MapPoint): number {
  return (end.x - start.x) * direction.x + (end.y - start.y) * direction.y;
}

/**
 * Whether two lines share a corridor: nearly parallel or nearly opposite, overlapping over more
 * than a head's length, and closer than `spacing` somewhere on that stretch (case 4).
 */
export function conflicting(
  first: { start: MapPoint; tip: MapPoint },
  second: { start: MapPoint; tip: MapPoint },
  spacing: number,
  parallelRadians: number,
  minimumOverlap: number,
): boolean {
  const a = unit(first.tip.x - first.start.x, first.tip.y - first.start.y);
  const b = unit(second.tip.x - second.start.x, second.tip.y - second.start.y);
  const cosine = a.x * b.x + a.y * b.y;
  if (Math.abs(cosine) < Math.cos(parallelRadians)) return false;
  const lengthA = along(first.start, first.tip, a);
  const p = along(first.start, second.start, a);
  const q = along(first.start, second.tip, a);
  const low = Math.max(0, Math.min(p, q));
  const high = Math.min(lengthA, Math.max(p, q));
  if (high - low < minimumOverlap) return false;
  const gapAt = (distance: number) => {
    const point = {
      x: first.start.x + a.x * distance,
      y: first.start.y + a.y * distance,
    };
    return Math.abs(
      (point.x - second.start.x) * b.y - (point.y - second.start.y) * b.x,
    );
  };
  return Math.min(gapAt(low), gapAt(high)) < spacing;
}

/**
 * The lane offset of each line, as a vector: lines that share a corridor (`conflicting`) are
 * spread across it `spacing` apart. Along one corridor, lines going one way keep to the right of
 * their travel and lines going the other way to theirs (#89's 4B); lines all going one way are
 * centred on it. Within a side, lower group IDs are nearer the middle.
 */
export function laneOffsets(
  lines: readonly { id: number; start: MapPoint; tip: MapPoint }[],
  spacing: number,
  parallelRadians: number,
  minimumOverlap: number,
): Map<number, MapPoint> {
  const parent = lines.map((_, index) => index);
  const root = (index: number): number => {
    let found = index;
    while (parent[found] !== found) found = parent[found] as number;
    return found;
  };
  for (let left = 0; left < lines.length; left++) {
    for (let right = left + 1; right < lines.length; right++) {
      const first = lines[left];
      const second = lines[right];
      if (
        first &&
        second &&
        conflicting(first, second, spacing, parallelRadians, minimumOverlap)
      ) {
        parent[root(right)] = root(left);
      }
    }
  }
  const components = new Map<number, (typeof lines)[number][]>();
  lines.forEach((line, index) => {
    const key = root(index);
    components.set(key, [...(components.get(key) ?? []), line]);
  });
  const offsets = new Map<number, MapPoint>();
  for (const members of components.values()) {
    const byId = [...members].sort((left, right) => left.id - right.id);
    const reference = byId[0];
    if (!reference) continue;
    const direction = unit(
      reference.tip.x - reference.start.x,
      reference.tip.y - reference.start.y,
    );
    // To the right of travel on a screen whose y points down.
    const side = { x: -direction.y, y: direction.x };
    const forward = byId.filter(
      (line) =>
        along(line.start, line.tip, direction) >= 0 || line.id === reference.id,
    );
    const backward = byId.filter((line) => !forward.includes(line));
    const place = (line: (typeof lines)[number], slot: number): void => {
      offsets.set(line.id, {
        x: side.x * slot * spacing,
        y: side.y * slot * spacing,
      });
    };
    if (byId.length === 1) {
      place(reference, 0);
    } else if (backward.length === 0) {
      for (const [index, line] of forward.entries()) {
        place(line, index - (forward.length - 1) / 2);
      }
    } else {
      for (const [index, line] of forward.entries()) place(line, index + 0.5);
      for (const [index, line] of backward.entries()) {
        place(line, -(index + 0.5));
      }
    }
  }
  return offsets;
}

/**
 * Groups angles (radians) into clusters whose span is at most `span`, going round the circle:
 * the angles either side of ±180 degrees are neighbours too (case 8), and no cluster grows past
 * `span` in total (case 7). Returns clusters of indices into `angles`.
 */
export function clusterAngles(
  angles: readonly number[],
  span: number,
): number[][] {
  if (angles.length === 0) return [];
  const order = angles
    .map((angle, index) => ({
      angle: ((angle % (2 * Math.PI)) + 2 * Math.PI) % (2 * Math.PI),
      index,
    }))
    .sort(
      (left, right) => left.angle - right.angle || left.index - right.index,
    );
  // Start after the widest gap, so no cluster is cut by where the circle starts.
  let widest = 0;
  let widestAfter = 0;
  order.forEach((entry, position) => {
    const next = order[(position + 1) % order.length] as typeof entry;
    const gap =
      position === order.length - 1
        ? next.angle + 2 * Math.PI - entry.angle
        : next.angle - entry.angle;
    if (gap > widest) {
      widest = gap;
      widestAfter = (position + 1) % order.length;
    }
  });
  const rotated = [...order.slice(widestAfter), ...order.slice(0, widestAfter)];
  const unwrapped = rotated.map((entry, position) => ({
    index: entry.index,
    angle:
      entry.angle +
      (position > 0 && entry.angle < (rotated[0]?.angle ?? 0)
        ? 2 * Math.PI
        : 0),
  }));
  const clusters: number[][] = [];
  let first = Number.NEGATIVE_INFINITY;
  for (const entry of unwrapped) {
    const current = clusters[clusters.length - 1];
    if (current && entry.angle - first <= span + 1e-12) {
      current.push(entry.index);
    } else {
      clusters.push([entry.index]);
      first = entry.angle;
    }
  }
  return clusters;
}

/** The circular mean of angles. */
export function meanAngle(angles: readonly number[]): number {
  return Math.atan2(
    angles.reduce((total, angle) => total + Math.sin(angle), 0),
    angles.reduce((total, angle) => total + Math.cos(angle), 0),
  );
}

/**
 * Moves angles (radians) round the circle as little as needed so that neighbours are at least
 * `step` apart (case 1). Angles already far enough apart do not move. Returns the new angles,
 * in the order given.
 */
export function spreadAngles(
  angles: readonly number[],
  step: number,
): number[] {
  const count = angles.length;
  if (count < 2) return [...angles];
  const order = angles
    .map((angle, index) => ({
      angle: Math.atan2(Math.sin(angle), Math.cos(angle)),
      index,
    }))
    .sort(
      (left, right) => left.angle - right.angle || left.index - right.index,
    );
  const result = new Array<number>(count);
  if (count * step >= 2 * Math.PI) {
    // No room to keep them apart: evenly round the circle, in order.
    const first = order[0]?.angle ?? 0;
    order.forEach((entry, position) => {
      result[entry.index] = first + (position * 2 * Math.PI) / count;
    });
    return result;
  }
  // Cut the circle in its widest gap and spread on a line: blocks of touching angles are
  // centred on the mean of where their members want to be, and merged until none overlap.
  let widest = -1;
  let cut = 0;
  order.forEach((entry, position) => {
    const next = order[(position + 1) % count] as typeof entry;
    const gap =
      position === count - 1
        ? next.angle + 2 * Math.PI - entry.angle
        : next.angle - entry.angle;
    if (gap > widest) {
      widest = gap;
      cut = (position + 1) % count;
    }
  });
  const rotated = [...order.slice(cut), ...order.slice(0, cut)];
  const base = rotated[0]?.angle ?? 0;
  const wanted = rotated.map((entry) =>
    entry.angle < base ? entry.angle + 2 * Math.PI : entry.angle,
  );
  interface Block {
    first: number;
    count: number;
    /** Where its first member sits. */
    start: number;
  }
  const blocks: Block[] = [];
  wanted.forEach((angle, position) => {
    blocks.push({ first: position, count: 1, start: angle });
    for (;;) {
      const last = blocks[blocks.length - 1] as Block;
      const before = blocks[blocks.length - 2];
      if (!before || before.start + before.count * step <= last.start + 1e-12) {
        break;
      }
      const merged: Block = {
        first: before.first,
        count: before.count + last.count,
        start: 0,
      };
      let total = 0;
      for (let offset = 0; offset < merged.count; offset++) {
        total += (wanted[merged.first + offset] ?? 0) - offset * step;
      }
      merged.start = total / merged.count;
      blocks.splice(blocks.length - 2, 2, merged);
    }
  });
  for (const block of blocks) {
    for (let offset = 0; offset < block.count; offset++) {
      const entry = rotated[block.first + offset];
      if (!entry) continue;
      const moved = block.start + offset * step;
      const wantedAngle = wanted[block.first + offset] ?? moved;
      // Each angle moves by its displacement; a lone one keeps exactly its own value.
      const own = angles[entry.index] ?? moved;
      result[entry.index] =
        block.count === 1 ? own : own + (moved - wantedAngle);
    }
  }
  return result;
}

/** Where the line through `position` along `direction` first reaches the castle's tip curve. */
function tipOnCurve(
  position: MapPoint,
  direction: MapPoint,
  castle: { x: number; y: number },
  reach: (angle: number) => number,
  angle: number,
): MapPoint {
  let guess = angle;
  let point = {
    x: castle.x + Math.cos(guess) * reach(guess),
    y: castle.y + Math.sin(guess) * reach(guess),
  };
  for (let round = 0; round < 3; round++) {
    const radius = reach(guess);
    const toCentre = { x: castle.x - position.x, y: castle.y - position.y };
    const ahead = toCentre.x * direction.x + toCentre.y * direction.y;
    const across = toCentre.x * direction.y - toCentre.y * direction.x;
    const distance =
      ahead - Math.sqrt(Math.max(0, radius * radius - across * across));
    point = {
      x: position.x + direction.x * distance,
      y: position.y + direction.y * distance,
    };
    guess = Math.atan2(point.y - castle.y, point.x - castle.x);
  }
  return point;
}

/**
 * The lines to draw for `candidates`, in group ID order: every rule above except the gaps and
 * the dimming, which the caller adds. Positions and sizes are in one unit, whatever it is.
 */
export function layoutLines(
  candidates: readonly LineCandidate[],
  sizes: LayoutSizes,
): LaidLine[] {
  // Too short already where the boat is: no line, no stub, no backward head.
  const routes: Route[] = [];
  for (const candidate of [...candidates].sort(
    (left, right) => left.id - right.id,
  )) {
    const route = routeOf(candidate);
    if (!route) continue;
    if (
      along(route.start, route.tip, route.direction) <
      sizes.headLength + sizes.minShaft
    ) {
      continue;
    }
    routes.push(route);
  }
  // Groups of one team on one route lie on one line: keep the rearmost, hide the rest.
  const rearmost: Route[] = [];
  for (const route of [...routes].sort(
    (left, right) =>
      right.distance - left.distance || left.candidate.id - right.candidate.id,
  )) {
    const duplicate = rearmost.some(
      (kept) =>
        kept.candidate.castle.id === route.candidate.castle.id &&
        kept.candidate.team === route.candidate.team &&
        angleBetween(kept.angle, route.angle) < sizes.samePathRadians,
    );
    if (!duplicate) rearmost.push(route);
  }
  let active = rearmost.sort(
    (left, right) => left.candidate.id - right.candidate.id,
  );
  // A line found too short once lanes and shared heads moved it is dropped, and the rest are
  // laid out again without it, so it neither takes a lane nor holds a shared head.
  for (let round = 0; round <= candidates.length; round++) {
    const lines = arrange(active, sizes);
    const short = new Set(
      lines
        .filter((line) => {
          const route = active.find((entry) => entry.candidate.id === line.id);
          if (!route) return true;
          // Measured towards the castle, so a base behind the boat counts as negative.
          const toBase = along(line.position, line.base, route.direction);
          return !(toBase - route.candidate.startClear >= sizes.minShaft);
        })
        .map((line) => line.id),
    );
    if (short.size === 0) return lines;
    active = active.filter((route) => !short.has(route.candidate.id));
  }
  return [];
}

function arrange(routes: readonly Route[], sizes: LayoutSizes): LaidLine[] {
  const spacing = sizes.headWidth / 2 + sizes.lineWidth / 2 + sizes.laneGap;
  const offsets = laneOffsets(
    routes.map((route) => ({
      id: route.candidate.id,
      start: route.start,
      tip: route.tip,
    })),
    spacing,
    sizes.parallelRadians,
    sizes.headLength,
  );
  interface Placed {
    route: Route;
    lane: MapPoint;
    position: MapPoint;
    tip: MapPoint;
    tipAngle: number;
  }
  const placed: Placed[] = routes.map((route) => {
    const { candidate } = route;
    const lane = offsets.get(candidate.id) ?? { x: 0, y: 0 };
    const position = {
      x: candidate.position.x + lane.x,
      y: candidate.position.y + lane.y,
    };
    const tip =
      lane.x === 0 && lane.y === 0
        ? route.tip
        : tipOnCurve(
            position,
            route.direction,
            candidate.castle,
            candidate.reach,
            route.angle,
          );
    return {
      route,
      lane,
      position,
      tip,
      tipAngle: Math.atan2(
        tip.y - candidate.castle.y,
        tip.x - candidate.castle.x,
      ),
    };
  });

  const result = new Map<number, LaidLine>();
  const byCastle = new Map<number, Placed[]>();
  for (const entry of placed) {
    const id = entry.route.candidate.castle.id;
    byCastle.set(id, [...(byCastle.get(id) ?? []), entry]);
  }
  for (const arriving of byCastle.values()) {
    const castle = arriving[0]?.route.candidate.castle;
    if (!castle) continue;
    // Shared heads: one team's lines within sharedRadians in total.
    interface Head {
      members: Placed[];
      angle: number;
    }
    const heads: Head[] = [];
    const teams = new Map<number, Placed[]>();
    for (const entry of arriving) {
      const team = entry.route.candidate.team;
      teams.set(team, [...(teams.get(team) ?? []), entry]);
    }
    for (const team of [...teams.keys()].sort((left, right) => left - right)) {
      const members = teams.get(team) ?? [];
      for (const cluster of clusterAngles(
        members.map((entry) => entry.tipAngle),
        sizes.sharedRadians,
      )) {
        const clustered = cluster
          .map((index) => members[index])
          .filter((entry): entry is Placed => entry !== undefined);
        heads.push({
          members: clustered,
          angle:
            clustered.length === 1
              ? (clustered[0] as Placed).tipAngle
              : meanAngle(clustered.map((entry) => entry.tipAngle)),
        });
      }
    }
    // Spread every head at this castle so that none touches another.
    const radius = Math.min(
      ...heads.map((head) =>
        (head.members[0] as Placed).route.candidate.reach(head.angle),
      ),
    );
    // Measured at the tips, so heads that stay pointing along their (parallel) lines are as far
    // apart as heads turned to the castle are at their bases.
    const step = (sizes.headWidth + sizes.headGap) / Math.max(radius, 1e-9);
    const spread = spreadAngles(
      heads.map((head) => head.angle),
      step,
    );
    heads.forEach((head, index) => {
      const angle = spread[index] ?? head.angle;
      const leader = head.members.reduce((lowest, entry) =>
        entry.route.candidate.id < lowest.route.candidate.id ? entry : lowest,
      );
      const shared = head.members.length > 1;
      const moved = angleBetween(angle, head.angle) > 1e-12;
      const reach = leader.route.candidate.reach(angle);
      const tip =
        !shared && !moved
          ? leader.tip
          : {
              x: castle.x + Math.cos(angle) * reach,
              y: castle.y + Math.sin(angle) * reach,
            };
      // A head of its own points along its line; a shared or moved one points at the castle,
      // so that heads spread round the castle stay apart along their whole length.
      const radial = shared || moved;
      const direction = radial
        ? unit(castle.x - tip.x, castle.y - tip.y)
        : unit(tip.x - leader.position.x, tip.y - leader.position.y);
      const base = {
        x: tip.x - direction.x * sizes.headLength,
        y: tip.y - direction.y * sizes.headLength,
      };
      for (const entry of head.members) {
        const { candidate } = entry.route;
        const toBase = unit(
          base.x - entry.position.x,
          base.y - entry.position.y,
        );
        result.set(candidate.id, {
          id: candidate.id,
          team: candidate.team,
          castleId: castle.id,
          position: entry.position,
          lane: entry.lane,
          start: {
            x: entry.position.x + toBase.x * candidate.startClear,
            y: entry.position.y + toBase.y * candidate.startClear,
          },
          tip,
          base,
          arrowhead: entry === leader,
          radialHead: radial,
          held: candidate.held,
        });
      }
    });
  }
  return [...result.values()].sort((left, right) => left.id - right.id);
}

// ---------------------------------------------------------------------------------------------
// The 3D board's lines.

/** The layout sizes of `style`, in map units. */
export function mapSizes(style: HeadingStyle): LayoutSizes {
  return {
    lineWidth: toMapDistance(style.lineWidth + style.casingWidth),
    headLength: toMapDistance(style.arrowLength),
    headWidth: toMapDistance(style.arrowWidth + style.casingWidth),
    laneGap: toMapDistance(style.laneGap),
    headGap: toMapDistance(style.headGap),
    minShaft: toMapDistance(style.minShaft),
    sharedRadians: radians(style.sharedArrowDegrees),
    samePathRadians: radians(style.samePathDegrees),
    parallelRadians: radians(style.parallelDegrees),
  };
}

/** How far ahead of a boat drawn at `scale` its line starts, in map units (case 11). */
export function startClearance(
  scale: number,
  style: HeadingStyle = headingStyle,
): number {
  return toMapDistance(BOAT_BOW * scale + style.startGap);
}

export interface HeadingOptions {
  style?: HeadingStyle;
  /** Groups that are not moving: their lines are dimmed. */
  held?: ReadonlySet<number>;
  /** The scale a group's boat is drawn at (1 by default). */
  boatScale?: (group: SoldierGroup) => number;
}

/**
 * Where a group sailing from `origin` to `destination` leaves its island: the origin's shore
 * point on the line between them, or null when either castle is not known.
 */
export function launchPoint(
  origin: Castle | undefined,
  destination: Castle | undefined,
): MapPoint | null {
  if (!origin || !destination) return null;
  return shorePoint(origin, destination, sharesLandmass(origin, destination));
}

/**
 * How strongly to draw the line of a boat drawn at `position` that left its island at
 * `launch`: 0 at the shore, growing to 1 over `launchFade` of sailing.
 */
export function launchFade(
  position: MapPoint,
  launch: MapPoint | null,
  style: HeadingStyle = headingStyle,
): number {
  if (!launch) return 1;
  const sailed = Math.hypot(position.x - launch.x, position.y - launch.y);
  return Math.min(1, sailed / toMapDistance(style.launchFade));
}

/**
 * The candidate line of `group`, drawn where its boat is drawn (shore.ts), with how strongly it
 * is drawn; null when it has none (no destination, or still at its island's shore).
 */
export function candidateOf(
  group: SoldierGroup,
  castleById: ReadonlyMap<number, Castle>,
  options: HeadingOptions = {},
): (LineCandidate & { strength: number }) | null {
  const style = options.style ?? headingStyle;
  const destination = castleById.get(group.destination_castle_id);
  if (!destination) return null;
  const origin = castleById.get(group.origin_castle_id);
  const bridged = origin !== undefined && sharesLandmass(origin, destination);
  const position = drawnPosition(group, origin, destination);
  const strength = launchFade(
    position,
    launchPoint(origin, destination),
    style,
  );
  if (strength <= 0) return null;
  return {
    id: group.id,
    team: group.team_color,
    strength,
    position,
    startClear: startClearance(options.boatScale?.(group) ?? 1, style),
    castle: destination,
    // The tip is on the destination's shore, where the boat will land (case 10).
    reach: (angle) =>
      shoreReach(
        destination,
        {
          x: destination.x + Math.cos(angle),
          y: destination.y + Math.sin(angle),
        },
        bridged,
      ),
    held: options.held?.has(group.id) ?? false,
  };
}

/**
 * The stretches of the shaft from `start` to `end` that pass within `radius` of a castle other
 * than `destinationId`, as distances from `start`, merged and in order; and those castles.
 */
export function castleGaps(
  start: MapPoint,
  end: MapPoint,
  castles: readonly { id: number; x: number; y: number }[],
  destinationId: number,
  radius: number,
): { gaps: [number, number][]; castles: number[] } {
  const length = Math.hypot(end.x - start.x, end.y - start.y);
  if (length === 0) return { gaps: [], castles: [] };
  const direction = {
    x: (end.x - start.x) / length,
    y: (end.y - start.y) / length,
  };
  const found: [number, number][] = [];
  const passed: number[] = [];
  for (const castle of castles) {
    if (castle.id === destinationId) continue;
    const ahead = along(start, castle, direction);
    const across = Math.abs(
      (castle.x - start.x) * direction.y - (castle.y - start.y) * direction.x,
    );
    if (across >= radius) continue;
    const half = Math.sqrt(radius * radius - across * across);
    const from = Math.max(0, ahead - half);
    const to = Math.min(length, ahead + half);
    if (to <= from) continue;
    passed.push(castle.id);
    found.push([from, to]);
  }
  found.sort((left, right) => left[0] - right[0]);
  const gaps: [number, number][] = [];
  for (const gap of found) {
    const last = gaps[gaps.length - 1];
    if (last && gap[0] <= last[1]) last[1] = Math.max(last[1], gap[1]);
    else gaps.push([gap[0], gap[1]]);
  }
  return { gaps, castles: passed };
}

/** A laid-out line as a segment to draw: its gaps, rank and opacity. */
export function segmentOf(
  line: Pick<
    LaidLine,
    "id" | "team" | "castleId" | "start" | "tip" | "base" | "arrowhead" | "held"
  >,
  rank: number,
  castles: readonly { id: number; x: number; y: number }[],
  style: HeadingStyle = headingStyle,
  opacity = 1,
): HeadingSegment {
  const { gaps, castles: passed } = castleGaps(
    line.start,
    line.base,
    castles,
    line.castleId,
    toMapDistance(style.castleGap),
  );
  return {
    groupId: line.id,
    teamColor: line.team,
    start: line.start,
    tip: line.tip,
    base: line.base,
    arrowhead: line.arrowhead,
    rank,
    passesUnder: passed,
    gaps,
    opacity: opacity * (line.held ? style.heldOpacity : 1),
  };
}

/**
 * A line with little more shaft than `minShaft` is drawn fainter, fading out over `fadeLength`:
 * the turn animation fades a line that gets short the same way, so the view at rest matches it.
 */
export function shaftFade(
  line: Pick<LaidLine, "start" | "base">,
  sizes: LayoutSizes,
  style: HeadingStyle,
): number {
  const shaft = Math.hypot(
    line.base.x - line.start.x,
    line.base.y - line.start.y,
  );
  return Math.min(
    1,
    Math.max(0, (shaft - sizes.minShaft) / toMapDistance(style.fadeLength)),
  );
}

/**
 * One heading line per moving group, following the rules at the top of this file. Positions are
 * map coordinates; groups are where the generation has them, and are drawn at `drawnPosition`.
 */
export function computeHeadings(
  castles: readonly Castle[],
  groups: readonly SoldierGroup[],
  options: HeadingOptions = {},
): HeadingSegment[] {
  const style = options.style ?? headingStyle;
  const castleById = new Map(castles.map((castle) => [castle.id, castle]));
  const candidates = groups
    .map((group) => candidateOf(group, castleById, options))
    .filter((candidate) => candidate !== null);
  const strength = new Map(candidates.map((c) => [c.id, c.strength]));
  const sizes = mapSizes(style);
  return layoutLines(candidates, sizes).map((line, rank) =>
    segmentOf(
      line,
      rank,
      castles,
      style,
      shaftFade(line, sizes, style) * (strength.get(line.id) ?? 1),
    ),
  );
}

// ---------------------------------------------------------------------------------------------
// Drawing.

export interface RibbonGeometry {
  /** Flat triangles on the XZ plane, as map-space points [x, y, ...] per vertex. */
  points: number[];
  indices: number[];
}

/**
 * The shaft (in pieces, around its gaps) and arrowhead of a line as flat triangles in map
 * space; the style's sizes are world units and are converted. `grow` (world units) widens it on
 * both sides and lengthens the head, which is how the casing is made.
 */
export function ribbonGeometry(
  segment: Pick<
    HeadingSegment,
    "start" | "tip" | "base" | "arrowhead" | "gaps"
  >,
  style: HeadingStyle,
  grow = 0,
): RibbonGeometry {
  const points: number[] = [];
  const indices: number[] = [];
  const corner = (origin: MapPoint, normal: MapPoint, offset: number) => {
    points.push(origin.x + normal.x * offset, origin.y + normal.y * offset);
    return points.length / 2 - 1;
  };
  const shaftLength = Math.hypot(
    segment.base.x - segment.start.x,
    segment.base.y - segment.start.y,
  );
  const half = toMapDistance(style.lineWidth + grow) / 2;
  const growth = toMapDistance(grow) / 2;
  if (shaftLength > 0) {
    const direction = {
      x: (segment.base.x - segment.start.x) / shaftLength,
      y: (segment.base.y - segment.start.y) / shaftLength,
    };
    const normal = { x: -direction.y, y: direction.x };
    const at = (distance: number) => ({
      x: segment.start.x + direction.x * distance,
      y: segment.start.y + direction.y * distance,
    });
    // The pieces between the gaps; a casing's piece reaches `growth` into each gap, as its sides do.
    let from = 0;
    const pieces: [number, number][] = [];
    for (const [gapStart, gapEnd] of segment.gaps) {
      if (gapStart > from) pieces.push([from, gapStart]);
      from = Math.max(from, gapEnd);
    }
    if (from < shaftLength) pieces.push([from, shaftLength]);
    for (const [pieceStart, pieceEnd] of pieces) {
      const first = at(pieceStart > 0 ? pieceStart - growth : pieceStart);
      const last = at(pieceEnd < shaftLength ? pieceEnd + growth : pieceEnd);
      const a = corner(first, normal, -half);
      const b = corner(first, normal, half);
      const c = corner(last, normal, half);
      const d = corner(last, normal, -half);
      indices.push(a, b, c, a, c, d);
    }
  }
  if (segment.arrowhead) {
    const headLength = Math.hypot(
      segment.tip.x - segment.base.x,
      segment.tip.y - segment.base.y,
    );
    if (headLength > 0) {
      const direction = {
        x: (segment.tip.x - segment.base.x) / headLength,
        y: (segment.tip.y - segment.base.y) / headLength,
      };
      const normal = { x: -direction.y, y: direction.x };
      const wing = toMapDistance(style.arrowWidth + grow) / 2;
      // The casing's head starts a little behind the base, as its sides stand out of the head's.
      const back = {
        x: segment.base.x - direction.x * growth,
        y: segment.base.y - direction.y * growth,
      };
      const left = corner(back, normal, -wing);
      const right = corner(back, normal, wing);
      points.push(
        segment.tip.x + direction.x * growth,
        segment.tip.y + direction.y * growth,
      );
      indices.push(left, right, points.length / 2 - 1);
    }
  }
  return { points, indices };
}

/**
 * The drawing order of a line's two meshes (case 15): every line is at one height, with no depth
 * test between lines, and the order decides which is on top: a higher rank over a lower one, and
 * each line over its own casing. These are alpha indices: lower draws first.
 */
export function headingDrawOrder(rank: number): {
  casing: number;
  line: number;
} {
  return { casing: rank * 2, line: rank * 2 + 1 };
}
