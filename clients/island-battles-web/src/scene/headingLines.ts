import type { components } from "../api/generated/game";
import { CASTLE_RADIUS } from "./board";
import { type MapPoint, toMapDistance } from "./coordinates";

type Castle = components["schemas"]["Castle"];
type SoldierGroup = components["schemas"]["SoldierGroup"];

// Sizes in `headingStyle` are world units, so lines keep their look whatever WORLD_SCALE is.
// The computing below is in map coordinates, so each size goes through toMapDistance first.
// Everything that decides how the heading lines look is here, so the owner's picks on #89
// (casing or none, arrowhead style, lane offset, ...) are one-value changes.
export const headingStyle = {
  /** Width of a line, in world units. */
  lineWidth: 0.1,
  /** Extra width of the sea-coloured casing around a line, 2px (0.02) on each side as the owner picked 1B on #89; 0 draws none. */
  casingWidth: 0.04,
  arrowLength: 0.34,
  arrowWidth: 0.3,
  /** How far a line's colour is mixed toward white: a light tint of the team colour. */
  tint: 0.45,
  /** Height above the island tops, so lines show over land. */
  height: 0.14,
  /** Height between two lines, so the later one by group ID is on top where they cross. */
  stackStep: 0.006,
  /** Gap between a group's circle and the start of its line. */
  startGap: 0.06,
  /** Opposite directions on one route are shifted this far (7px, pick 4B on #89) to the right of travel. */
  laneOffset: 0.07,
  /** Same-team lines that arrive within this angle share one arrowhead (#89 case 2). */
  sharedArrowDegrees: 15,
  /** Same-team lines to one castle within this angle are one route, drawn once (#89 case 3). */
  samePathDegrees: 1.5,
  /**
   * A line passing within this distance of a castle's edge is reported in `passesUnder`. The 2D board draws a gap ring
   * there and haloes castle text and groups (picks 5B and 6B on #89); the 3D scene has no text or ring to cut, so it
   * draws lines under castles and groups and makes no ring or halo.
   */
  passUnderMargin: 0.08,
} as const;

export type HeadingStyle = typeof headingStyle;

export interface HeadingSegment {
  groupId: number;
  teamColor: number;
  /** Where the line starts, just ahead of the group. */
  start: MapPoint;
  /** Where the arrowhead's tip sits, on the destination castle's edge. */
  tip: MapPoint;
  /** Whether this line draws an arrowhead (lines sharing one let only one draw it). */
  arrowhead: boolean;
  /** Position in the stacking order; a higher rank is drawn over lower ones. */
  rank: number;
  /** Castles, other than the destination, that this line passes under. */
  passesUnder: number[];
}

function angleBetween(left: number, right: number): number {
  const difference = Math.abs(left - right) % (Math.PI * 2);
  return Math.min(difference, Math.PI * 2 - difference);
}

const radians = (degrees: number) => (degrees * Math.PI) / 180;

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

/**
 * One heading line per moving group, following #89's rules: it runs from just ahead of the
 * group to the destination castle's edge; a group about to arrive has none; groups of one team
 * on one route share the rearmost group's line; opposite directions are offset into lanes;
 * same-team lines arriving from nearly one direction share an arrowhead; and the stacking
 * order is by group ID. Positions are map coordinates.
 */
export function computeHeadings(
  castles: readonly Castle[],
  groups: readonly SoldierGroup[],
  style: HeadingStyle = headingStyle,
): HeadingSegment[] {
  const castleById = new Map(castles.map((castle) => [castle.id, castle]));
  const castleReach = toMapDistance(CASTLE_RADIUS);
  const startGap = toMapDistance(style.startGap);
  const arrowLength = toMapDistance(style.arrowLength);
  const laneOffset = toMapDistance(style.laneOffset);
  const passUnderMargin = toMapDistance(style.passUnderMargin);
  interface Candidate {
    group: SoldierGroup;
    castle: Castle;
    distance: number;
    /** The angle of the group seen from the castle. */
    arrivalAngle: number;
    lane: number;
  }
  const candidates: Candidate[] = [];
  for (const group of [...groups].sort((left, right) => left.id - right.id)) {
    const castle = castleById.get(group.destination_castle_id);
    if (!castle) continue;
    const dx = group.x - castle.x;
    const dy = group.y - castle.y;
    const distance = Math.hypot(dx, dy);
    // A group about to arrive, or inside the castle's circle: no line, no stub, no backward head.
    const visibleLength =
      distance - castleReach - group.clash_radius - startGap;
    if (visibleLength < arrowLength) continue;
    candidates.push({
      group,
      castle,
      distance,
      arrivalAngle: Math.atan2(dy, dx),
      lane: 0,
    });
  }

  // Groups of one team on one route lie on one line: keep the rearmost, hide the rest.
  const rearmost: Candidate[] = [];
  for (const candidate of [...candidates].sort(
    (left, right) => right.distance - left.distance,
  )) {
    const duplicate = rearmost.some(
      (kept) =>
        kept.castle.id === candidate.castle.id &&
        kept.group.team_color === candidate.group.team_color &&
        angleBetween(kept.arrivalAngle, candidate.arrivalAngle) <
          radians(style.samePathDegrees),
    );
    if (!duplicate) rearmost.push(candidate);
  }
  const kept = rearmost.sort((left, right) => left.group.id - right.group.id);

  // Opposite directions on one route: A -> B and B -> A each shift to the right of travel.
  const routes = new Set(
    groups.map(
      (group) => `${group.origin_castle_id}>${group.destination_castle_id}`,
    ),
  );
  for (const candidate of kept) {
    const reverse = `${candidate.group.destination_castle_id}>${candidate.group.origin_castle_id}`;
    if (routes.has(reverse)) candidate.lane = laneOffset;
  }

  // Same-team lines arriving within sharedArrowDegrees of each other end at one point with one head.
  const sharedTip = new Map<number, { angle: number; leader: boolean }>();
  const groupsByCastleAndTeam = new Map<string, Candidate[]>();
  for (const candidate of kept) {
    if (candidate.lane !== 0) continue;
    const key = `${candidate.castle.id}/${candidate.group.team_color}`;
    groupsByCastleAndTeam.set(key, [
      ...(groupsByCastleAndTeam.get(key) ?? []),
      candidate,
    ]);
  }
  for (const sameTeam of groupsByCastleAndTeam.values()) {
    const byAngle = [...sameTeam].sort(
      (left, right) => left.arrivalAngle - right.arrivalAngle,
    );
    let cluster: Candidate[] = [];
    const flush = () => {
      if (cluster.length === 0) return;
      // The circular mean, so a cluster across the +-180 degree seam is averaged correctly.
      const angle = Math.atan2(
        cluster.reduce(
          (total, member) => total + Math.sin(member.arrivalAngle),
          0,
        ),
        cluster.reduce(
          (total, member) => total + Math.cos(member.arrivalAngle),
          0,
        ),
      );
      const leaderId = Math.min(...cluster.map((member) => member.group.id));
      for (const member of cluster) {
        sharedTip.set(member.group.id, {
          angle,
          leader: member.group.id === leaderId,
        });
      }
      cluster = [];
    };
    for (const candidate of byAngle) {
      const previous = cluster[cluster.length - 1];
      if (
        previous &&
        angleBetween(previous.arrivalAngle, candidate.arrivalAngle) >=
          radians(style.sharedArrowDegrees)
      ) {
        flush();
      }
      cluster.push(candidate);
    }
    flush();
    // The first and last of the sorted angles may be neighbours across the seam; merging them
    // would need another pass, and two heads that close are still readable, so they are left.
  }

  return kept.map((candidate, rank) => {
    const { group, castle } = candidate;
    const toCastle = { x: castle.x - group.x, y: castle.y - group.y };
    const length = Math.hypot(toCastle.x, toCastle.y);
    const direction = { x: toCastle.x / length, y: toCastle.y / length };
    // To the right of travel on a screen whose y points down.
    const side = { x: -direction.y, y: direction.x };
    const shifted = {
      x: group.x + side.x * candidate.lane,
      y: group.y + side.y * candidate.lane,
    };
    const start = {
      x: shifted.x + direction.x * (group.clash_radius + startGap),
      y: shifted.y + direction.y * (group.clash_radius + startGap),
    };
    const shared = sharedTip.get(group.id);
    let tip: MapPoint;
    if (shared) {
      tip = {
        x: castle.x + Math.cos(shared.angle) * castleReach,
        y: castle.y + Math.sin(shared.angle) * castleReach,
      };
    } else {
      // Where the (shifted) line first meets the castle's circle.
      const offsetFromCentre = {
        x: shifted.x - castle.x,
        y: shifted.y - castle.y,
      };
      const along =
        offsetFromCentre.x * direction.x + offsetFromCentre.y * direction.y;
      const across =
        offsetFromCentre.x * -direction.y + offsetFromCentre.y * direction.x;
      const reach = Math.sqrt(
        Math.max(0, castleReach * castleReach - across * across),
      );
      const distanceAlong = -along - reach;
      tip = {
        x: shifted.x + direction.x * distanceAlong,
        y: shifted.y + direction.y * distanceAlong,
      };
    }
    const passesUnder = castles
      .filter(
        (other) =>
          other.id !== castle.id &&
          distanceToSegment(other, start, tip) < castleReach + passUnderMargin,
      )
      .map((other) => other.id);
    return {
      groupId: group.id,
      teamColor: group.team_color,
      start,
      tip,
      arrowhead: shared ? shared.leader : true,
      rank,
      passesUnder,
    };
  });
}

export interface RibbonGeometry {
  /** Flat triangles on the XZ plane, as map-space points [x, y, ...] per vertex. */
  points: number[];
  indices: number[];
}

/**
 * The shaft and arrowhead of a line as flat triangles in map space; the style's sizes are world
 * units and are converted. `grow` (world units) widens it on both
 * sides and lengthens the head, which is how the casing is made.
 */
export function ribbonGeometry(
  segment: HeadingSegment,
  style: HeadingStyle,
  grow = 0,
): RibbonGeometry {
  const dx = segment.tip.x - segment.start.x;
  const dy = segment.tip.y - segment.start.y;
  const length = Math.hypot(dx, dy);
  const direction = { x: dx / length, y: dy / length };
  const normal = { x: -direction.y, y: direction.x };
  const half = toMapDistance(style.lineWidth + grow) / 2;
  const headLength = segment.arrowhead ? toMapDistance(style.arrowLength) : 0;
  const shaftEnd = {
    x: segment.tip.x - direction.x * headLength,
    y: segment.tip.y - direction.y * headLength,
  };
  const points: number[] = [];
  const indices: number[] = [];
  const corner = (origin: MapPoint, offset: number) => {
    points.push(origin.x + normal.x * offset, origin.y + normal.y * offset);
    return points.length / 2 - 1;
  };
  const a = corner(segment.start, -half);
  const b = corner(segment.start, half);
  const c = corner(shaftEnd, half);
  const d = corner(shaftEnd, -half);
  indices.push(a, b, c, a, c, d);
  if (segment.arrowhead) {
    const wing = toMapDistance(style.arrowWidth + grow) / 2;
    const left = corner(shaftEnd, -wing);
    const right = corner(shaftEnd, wing);
    points.push(
      segment.tip.x + direction.x * toMapDistance(grow) * 0.5,
      segment.tip.y + direction.y * toMapDistance(grow) * 0.5,
    );
    indices.push(left, right, points.length / 2 - 1);
  }
  return { points, indices };
}
