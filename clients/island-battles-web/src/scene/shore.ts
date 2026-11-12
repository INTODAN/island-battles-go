import { type MapPoint, toMapDistance, toWorldDistance } from "./coordinates";
import { type IslandCastle, SHARED_LANDMASS_DISTANCE } from "./islands";
import {
  BRIDGE_TOP,
  ISLAND_TOP,
  islandOutline,
  type Outline,
  waterline,
} from "./landforms";

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
 * A group with no destination is held at the origin's shore on the line to `heading`, when
 * given, else to its position.
 */
export function drawnPosition(
  position: MapPoint,
  origin: IslandCastle | undefined,
  destination: IslandCastle | undefined,
  heading?: MapPoint,
): MapPoint {
  const bridged =
    origin !== undefined &&
    destination !== undefined &&
    sharesLandmass(origin, destination);
  if (origin) {
    const towards = destination ?? heading ?? position;
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
  return insidePolygon(waterlineOf(castle.id), castle, position);
}

// ---- the land part of a trip (#211) ----------------------------------------------------------
//
// While a group's true position is inside its origin's or destination's shore reach, the boat
// waits at the shore and the group is drawn as a squad of soldiers walking on the island at
// that position: at the origin they walk from the castle's gate to the cliff edge while their
// boat is built at the shore, then step down into it; at the destination they step off the
// docked boat, which shrinks away, and walk on to the gate. A trip between two castles of one
// landmass is walked the whole way, over the bridge, with no boat. Everything follows from the
// true position, so a generation at rest and the end of a turn into it look the same.

/** World units from a castle's centre where its squads come out of and go into the gate. */
export const GATE_RADIUS = 0.42;
/** World units inside the grass edge where a squad steps down to its boat. */
export const EDGE_INSET = 0.04;
/** The share of the land part that a squad spends stepping between the edge and the boat. */
export const BOARDING_SHARE = 0.2;
/** The share of the walk over which a squad fades in at the gate, or out into it. */
export const GATE_FADE_SHARE = 0.12;
/** World height of a squad standing in a boat. */
export const DECK_HEIGHT = 0.12;
/** How high a squad hops between the cliff edge and the deck, in world units. */
const HOP_HEIGHT = 0.08;
/** At the destination, the docked boat shrinks away over this part of the land part. */
const DOCK_SHRINK: readonly [number, number] = [0.1, 0.45];

export interface SquadLook {
  /** Where the squad stands, in map units. */
  x: number;
  y: number;
  /** World height of its feet. */
  height: number;
  /** 0 to 1: 1 at full size; smaller while it comes out of a gate or steps into a boat. */
  scale: number;
  /** World units walked from the gate, so the walk's bob follows the distance moved. */
  walked: number;
  /** The direction it faces (it walks from origin towards destination), in map units. */
  facingX: number;
  facingY: number;
}

export interface GroupLook {
  /** Where the boat is drawn, in map units: `drawnPosition`. */
  x: number;
  y: number;
  /** How much of the boat is built: 0 none (not drawn), 1 complete. */
  build: number;
  /** The docked boat's size, 1 until it shrinks away at the destination's shore, then 0. */
  boatScale: number;
  /** The squad on land, or null while the group is afloat. */
  squad: SquadLook | null;
}

function clamp01(value: number): number {
  return Math.min(1, Math.max(0, value));
}

function smooth(value: number): number {
  const s = clamp01(value);
  return s * s * (3 - 2 * s);
}

interface Ray {
  /** Unit direction from the castle along the trip, in map units. */
  dx: number;
  dy: number;
  /** World units from the castle's centre: the gate, the step-down point and the boat. */
  gate: number;
  edge: number;
  boat: number;
}

/** The land part of a trip from `castle` in the direction of `towards`, in world units. */
function rayFrom(castle: IslandCastle, towards: MapPoint): Ray | null {
  const direction = worldDirection(castle, towards);
  if (!direction) return null;
  const edge = Math.max(
    0,
    leaves(outlineOf(castle.id), direction) - EDGE_INSET,
  );
  return {
    dx: direction[0],
    dy: -direction[1],
    gate: Math.min(GATE_RADIUS, edge * 0.6),
    edge,
    boat: leaves(waterlineOf(castle.id), direction) + SHORE_MARGIN,
  };
}

function along(castle: MapPoint, ray: Ray, world: number): MapPoint {
  const distance = toMapDistance(world);
  return { x: castle.x + ray.dx * distance, y: castle.y + ray.dy * distance };
}

/**
 * The squad at `share` (0 at the gate, 1 in the boat) of the land part along `ray` from
 * `castle`: walking from the gate to the edge, then stepping down into the boat.
 */
function squadOnRay(
  castle: MapPoint,
  ray: Ray,
  share: number,
  facing: readonly [number, number],
): SquadLook {
  const walking = 1 - BOARDING_SHARE;
  let distance: number;
  let height = ISLAND_TOP;
  let scale: number;
  if (share <= walking) {
    const s = share / walking;
    distance = ray.gate + (ray.edge - ray.gate) * s;
    scale = smooth(s / GATE_FADE_SHARE);
  } else {
    const s = clamp01((share - walking) / BOARDING_SHARE);
    distance = ray.edge + (ray.boat - ray.edge) * s;
    height =
      ISLAND_TOP +
      (DECK_HEIGHT - ISLAND_TOP) * s +
      HOP_HEIGHT * Math.sin(Math.PI * s);
    scale = 1 - smooth(s);
  }
  const point = along(castle, ray, distance);
  return {
    ...point,
    height,
    scale,
    walked: distance - ray.gate,
    facingX: facing[0],
    facingY: facing[1],
  };
}

/**
 * How to draw a group whose true position is `position` on its way from `origin` to
 * `destination` (map units): where its boat is, how much of it is built, how far it has shrunk
 * at the destination, and its squad while it is on land. Either castle may be missing (a group
 * whose castle is not known); then that end has no land part. A group with no destination
 * heads for `heading` instead, when given (a group sent and defeated within one turn heads
 * for its first contact), else for its position.
 */
export function groupLook(
  position: MapPoint,
  origin: IslandCastle | undefined,
  destination: IslandCastle | undefined,
  heading?: MapPoint,
): GroupLook {
  const boat = drawnPosition(position, origin, destination, heading);
  const afloat: GroupLook = { ...boat, build: 1, boatScale: 1, squad: null };

  if (origin && destination && sharesLandmass(origin, destination)) {
    // Walked over the bridge, the whole way: no boat.
    const total = toWorldDistance(
      Math.hypot(destination.x - origin.x, destination.y - origin.y),
    );
    const walked = toWorldDistance(
      Math.hypot(position.x - origin.x, position.y - origin.y),
    );
    const direction = worldDirection(origin, destination);
    if (!direction || total === 0) return afloat;
    const fromGate = Math.min(walked, total - walked);
    const onIsland =
      insideOutline(origin, position) || insideOutline(destination, position);
    return {
      ...boat,
      build: 0,
      boatScale: 0,
      squad: {
        x: position.x,
        y: position.y,
        height: onIsland ? ISLAND_TOP : BRIDGE_TOP,
        scale: smooth((fromGate - GATE_RADIUS * 0.5) / (GATE_RADIUS * 0.5)),
        walked,
        facingX: direction[0],
        facingY: -direction[1],
      },
    };
  }

  if (origin) {
    const towards = destination ?? heading ?? position;
    if (samePoint(towards, origin)) {
      // Sent and lost at its own castle's centre: it never got out of the gate (#213).
      return {
        x: origin.x,
        y: origin.y,
        build: 0,
        boatScale: 0,
        squad: {
          x: origin.x,
          y: origin.y,
          height: ISLAND_TOP,
          scale: 0,
          walked: 0,
          facingX: 1,
          facingY: 0,
        },
      };
    }
    const ray = rayFrom(origin, towards);
    const reach = shoreReach(origin, towards);
    const distance = Math.hypot(position.x - origin.x, position.y - origin.y);
    if (ray && distance < reach) {
      const share = distance / reach;
      return {
        ...boat,
        build: clamp01(share / (1 - BOARDING_SHARE)),
        boatScale: 1,
        squad: squadOnRay(origin, ray, share, [ray.dx, ray.dy]),
      };
    }
  }

  if (destination) {
    const towards = origin ?? position;
    const ray = rayFrom(destination, towards);
    const reach = shoreReach(destination, towards);
    const distance = Math.hypot(
      position.x - destination.x,
      position.y - destination.y,
    );
    if (ray && distance < reach) {
      // The land part backwards: the squad steps off the boat (growing as it does), walks to
      // the gate and fades into it. `progress` is 0 at the shore and 1 at the castle's centre,
      // where the group lands.
      const progress = 1 - distance / reach;
      const squad = squadOnRay(destination, ray, distance / reach, [
        -ray.dx,
        -ray.dy,
      ]);
      return {
        ...boat,
        build: 1,
        boatScale:
          1 -
          smooth(
            (progress - (DOCK_SHRINK[0] as number)) /
              ((DOCK_SHRINK[1] as number) - (DOCK_SHRINK[0] as number)),
          ),
        squad,
      };
    }
  }
  return afloat;
}

function samePoint(left: MapPoint, right: MapPoint): boolean {
  return left.x === right.x && left.y === right.y;
}

/** Whether a map position is on the grass top of `castle`'s island (inside its outline). */
export function insideOutline(
  castle: IslandCastle,
  position: MapPoint,
): boolean {
  return insidePolygon(outlineOf(castle.id), castle, position);
}

function insidePolygon(
  outline: Outline,
  castle: MapPoint,
  position: MapPoint,
): boolean {
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
