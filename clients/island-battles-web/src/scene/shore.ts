import { type MapPoint, toMapDistance, toWorldDistance } from "./coordinates";
import { type IslandCastle, SHARED_LANDMASS_DISTANCE } from "./islands";
import { islandOutline, type Outline, waterline } from "./landforms";

// Where a boat leaves and reaches an island (issue #167). The server moves a group in a straight
// line from the centre of its origin castle to the centre of its destination, so a boat that has
// just left or is about to arrive is inside an island. The scene draws it at the island's
// shore instead. Pure: no scene, so the animation plan and the tests use it.
//
// Units. A castle's island is sized in world units (landforms.ts) around the castle's world
// position, and the animation plan works in map units, so every function here takes and returns
// MAP positions and does its geometry in world units around the castle, converting with
// coordinates.ts: one map unit is WORLD_SCALE world units, and the map's y is the world's -z.

/**
 * How far out to sea of the island's waterline a boat is drawn at the shore, in world units. A
 * boat's hull is 0.39 long and centred a little behind its middle, so its bow reaches 0.215 from
 * the point (0.37 for the biggest groups, which are drawn 1.7 times larger); the margin is
 * about one boat's length, so the bow does not touch the beach.
 */
export const SHORE_MARGIN = 0.35;

type Direction = readonly [number, number];

const waterlines = new Map<number, Outline>();
const outlines = new Map<number, Outline>();

function waterlineOf(castleId: number): Outline {
  let outline = waterlines.get(castleId);
  if (!outline) {
    outline = waterline(islandOutline(castleId));
    waterlines.set(castleId, outline);
  }
  return outline;
}

function outlineOf(castleId: number): Outline {
  let outline = outlines.get(castleId);
  if (!outline) {
    outline = islandOutline(castleId);
    outlines.set(castleId, outline);
  }
  return outline;
}

/**
 * How far from the origin a ray in `direction` leaves `outline`, which goes around the origin.
 * An island is a star-shaped outline around its castle, so there is one crossing; the farthest
 * is taken should an outline fold back.
 */
function leaves(outline: Outline, direction: Direction): number {
  let farthest = 0;
  for (let index = 0; index < outline.length; index++) {
    const [ax, az] = outline[index] as readonly [number, number];
    const [bx, bz] = outline[(index + 1) % outline.length] as readonly [
      number,
      number,
    ];
    const edgeX = bx - ax;
    const edgeZ = bz - az;
    // Solve origin + direction * t = a + edge * s for t and s.
    const determinant = direction[0] * -edgeZ - direction[1] * -edgeX;
    if (Math.abs(determinant) < 1e-12) continue;
    const t = (ax * -edgeZ - az * -edgeX) / determinant;
    const s = (direction[0] * az - direction[1] * ax) / determinant;
    if (s >= 0 && s <= 1 && t > farthest) farthest = t;
  }
  return farthest;
}

/** The world-space direction (local x, z) from one map point to another, or null if equal. */
function worldDirection(from: MapPoint, towards: MapPoint): Direction | null {
  const dx = towards.x - from.x;
  const dy = towards.y - from.y;
  const length = Math.hypot(dx, dy);
  if (length === 0) return null;
  return [dx / length, -dy / length];
}

/** True when a trip between the two castles crosses the land bridge of one landmass. */
export function sharesLandmass(first: MapPoint, second: MapPoint): boolean {
  return (
    Math.hypot(first.x - second.x, first.y - second.y) <
    SHARED_LANDMASS_DISTANCE
  );
}

/**
 * The shore of `castle`'s island on the straight line towards `towards`: where that line from the
 * castle's centre crosses the island's waterline (its real, uneven outline in that direction),
 * moved out to sea by SHORE_MARGIN. Both arguments and the result are MAP positions (map units).
 *
 * `bridged` is for a trip between two castles of one landmass (`sharesLandmass`): the land
 * bridge joins their islands, so the point is where the line leaves the island's own outline,
 * with no margin, and the boat crosses the bridge.
 *
 * With `towards` at the castle's own centre there is no direction: the centre is returned.
 */
export function shorePoint(
  castle: IslandCastle,
  towards: MapPoint,
  bridged = false,
): MapPoint {
  const direction = worldDirection(castle, towards);
  if (!direction) return { x: castle.x, y: castle.y };
  const reach = bridged
    ? leaves(outlineOf(castle.id), direction)
    : leaves(waterlineOf(castle.id), direction) + SHORE_MARGIN;
  const distance = toMapDistance(reach);
  return {
    x: castle.x + direction[0] * distance,
    y: castle.y - direction[1] * distance,
  };
}

/** How far the shore point of `castle` towards `towards` is from its centre, in map units. */
export function shoreReach(
  castle: IslandCastle,
  towards: MapPoint,
  bridged = false,
): number {
  const point = shorePoint(castle, towards, bridged);
  return Math.hypot(point.x - castle.x, point.y - castle.y);
}

/**
 * Where to DRAW a group whose true position (a map position, from the generation or the plan) is
 * `position`, on its way from `origin` to `destination` (map units in and out): at the origin's
 * shore point while the true position is nearer the origin than that point, at the destination's
 * shore point while it is nearer the destination than that, and at the true position otherwise.
 * Either castle may be missing (a group whose castle is not known), then its side is left alone.
 * A group with no destination is held at the origin's shore on the line to its position.
 */
export function drawnPosition(
  position: MapPoint,
  origin: IslandCastle | undefined,
  destination: IslandCastle | undefined,
): MapPoint {
  const bridged =
    origin !== undefined &&
    destination !== undefined &&
    sharesLandmass(origin, destination);
  if (origin) {
    const towards = destination ?? position;
    const distance = Math.hypot(position.x - origin.x, position.y - origin.y);
    if (distance < shoreReach(origin, towards, bridged)) {
      return shorePoint(origin, towards, bridged);
    }
  }
  if (destination) {
    const towards = origin ?? position;
    const distance = Math.hypot(
      position.x - destination.x,
      position.y - destination.y,
    );
    if (distance < shoreReach(destination, towards, bridged)) {
      return shorePoint(destination, towards, bridged);
    }
  }
  return { x: position.x, y: position.y };
}

/** Whether a map position is inside the waterline of `castle`'s island, for tests. */
export function insideWaterline(
  castle: IslandCastle,
  position: MapPoint,
): boolean {
  const outline = waterlineOf(castle.id);
  const px = toWorldDistance(position.x - castle.x);
  const pz = -toWorldDistance(position.y - castle.y);
  let inside = false;
  for (let index = 0, last = outline.length - 1; index < outline.length; ) {
    const [ax, az] = outline[index] as readonly [number, number];
    const [bx, bz] = outline[last] as readonly [number, number];
    if (az > pz !== bz > pz && px < ((bx - ax) * (pz - az)) / (bz - az) + ax) {
      inside = !inside;
    }
    last = index++;
  }
  return inside;
}
