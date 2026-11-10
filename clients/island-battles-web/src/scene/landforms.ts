import { heightAlong } from "./atlasUv";
import { toWorld } from "./coordinates";
import {
  hashSeed,
  ISLAND_RADIUS,
  type IslandCastle,
  islandRadii,
  seededRandom,
} from "./islands";
import {
  FlatGeometry,
  type Point3,
  type Shade,
  type SwatchPoint,
} from "./lowPoly";

// The islands of the board as low-poly landforms on the castle atlas (issue #102): a faceted
// cliff ring that runs from cream at the top to dark grey at the waterline, a flat grass top and
// a thin sand beach. Everything here is plain geometry, seeded by castle ID, so it is tested
// without a scene and an island never changes between turns.

/** The top of an island, where castles stand. */
export const ISLAND_TOP = 0.2;
/** The top of a land bridge between two castles of one landmass: a little lower than islands. */
export const BRIDGE_TOP = 0.13;
/**
 * The flat plateau every castle stands on reaches at least this far from its centre in every
 * direction. #140 measured the level 4 castle's base reaching 0.745 from its centre.
 */
export const PLATEAU_RADIUS = 0.78;
/** How far inside the cliff's top edge the flat grass ends. */
export const LIP = 0.05;
/** The number of corners around an island: the seeded outline of islands.ts, sampled finer. */
export const ISLAND_CORNERS = 24;

/** How far along the green swatch the grass top samples (0 dark green, 1 lime). */
export const GRASS_ALONG = 0.3;
/**
 * The grass top's tint, multiplied into the atlas colour per channel (vertex colours). [1, 1, 1]
 * keeps the atlas swatch's own green, as the owner decided on #102.
 */
export const GRASS_TINT: readonly [number, number, number] = [1, 1, 1];
/** The beach: the cream end of the cliff swatch. */
export const SAND_ALONG = 0.98;
/** The cliff runs from this point of the cliff swatch at the waterline (dark grey)... */
const CLIFF_LOW = 0.05;
/** ...to this one at its top (cream). */
const CLIFF_HIGH = 0.86;

export type Outline = readonly (readonly [number, number])[];

/** One ring of a landform: how far outside the outline, and at what height. */
interface Ring {
  offset: number;
  y: number;
  /** Seeded wobble of the ring's offset and height, for faceted cliffs. */
  jitter: number;
}

/** The outline at the top of a castle's cliff, in local x and z around the castle. */
export function islandOutline(
  castleId: number,
  corners = ISLAND_CORNERS,
): Outline {
  return islandRadii(castleId, corners).map((radius, index) => {
    const angle = (index / corners) * Math.PI * 2;
    const length = radius * ISLAND_RADIUS;
    return [Math.cos(angle) * length, Math.sin(angle) * length] as const;
  });
}

/**
 * The outline of a land bridge between two castles `length` apart, along local x and centred
 * on the middle: a capsule with a seeded wobble, `halfWidth` wide on each side.
 */
export function bridgeOutline(
  length: number,
  seed: number,
  halfWidth = 0.42,
): Outline {
  const random = seededRandom(seed);
  const points: [number, number][] = [];
  const half = length / 2;
  const capCorners = 7;
  const sideCorners = Math.max(2, Math.round(length / 0.35));
  // The two ends are half circles around the castles; the sides run straight between them.
  for (const [side, centre] of [
    [1, half],
    [-1, -half],
  ] as const) {
    for (let index = 0; index <= capCorners; index++) {
      const angle = -Math.PI / 2 + (index / capCorners) * Math.PI;
      const wobble = halfWidth * (0.92 + random() * 0.16);
      points.push([
        centre + side * Math.cos(angle) * wobble,
        side * Math.sin(angle) * wobble,
      ]);
    }
    for (let index = 1; index < sideCorners; index++) {
      const s = index / sideCorners;
      const wobble = halfWidth * (0.85 + random() * 0.2);
      points.push([side * (half - s * length), side * wobble]);
    }
  }
  return points;
}

function centroid(outline: Outline): [number, number] {
  let x = 0;
  let z = 0;
  for (const [px, pz] of outline) {
    x += px;
    z += pz;
  }
  return [x / outline.length, z / outline.length];
}

/** Each corner's outward direction: the average of its two edges' outward normals. */
function outwardNormals(outline: Outline): [number, number][] {
  const [cx, cz] = centroid(outline);
  const count = outline.length;
  const edgeNormal = (index: number): [number, number] => {
    const [ax, az] = outline[index % count] as [number, number];
    const [bx, bz] = outline[(index + 1) % count] as [number, number];
    let nx = bz - az;
    let nz = -(bx - ax);
    const midX = (ax + bx) / 2 - cx;
    const midZ = (az + bz) / 2 - cz;
    if (nx * midX + nz * midZ < 0) {
      nx = -nx;
      nz = -nz;
    }
    const length = Math.hypot(nx, nz) || 1;
    return [nx / length, nz / length];
  };
  return outline.map((_, index) => {
    const [ax, az] = edgeNormal((index + count - 1) % count);
    const [bx, bz] = edgeNormal(index);
    const length = Math.hypot(ax + bx, az + bz) || 1;
    return [(ax + bx) / length, (az + bz) / length];
  });
}

/** The outline pushed `offset` outward (negative is inward) along each corner's normal. */
export function offsetOutline(outline: Outline, offset: number): Outline {
  const normals = outwardNormals(outline);
  return outline.map(([x, z], index) => {
    const [nx, nz] = normals[index] as [number, number];
    return [x + nx * offset, z + nz * offset] as const;
  });
}

function ringsFor(top: number): Ring[] {
  return [
    { offset: -LIP, y: top, jitter: 0 },
    { offset: 0, y: top - 0.02, jitter: 0 },
    { offset: 0.04, y: top * 0.55, jitter: 1 },
    { offset: 0.07, y: 0.035, jitter: 0.4 },
    { offset: 0.16, y: 0.022, jitter: 0 },
    { offset: 0.28, y: -0.09, jitter: 0 },
  ];
}

/** How far outside the cliff's top edge the beach meets the sea (y = 0). */
export const WATERLINE_OFFSET = 0.16 + (0.022 / (0.022 + 0.09)) * 0.12;

/**
 * A landform: a flat grass top inside the outline (less a lip), a cliff ring that is light at
 * the top and dark at the waterline, and a sand beach sloping under the sea. `seed` wobbles the
 * cliff so it reads as faceted rock.
 */
export function buildLandform(
  outline: Outline,
  top: number,
  seed: number,
): FlatGeometry {
  const geometry = new FlatGeometry();
  const random = seededRandom(seed);
  const rings = ringsFor(top);
  const count = outline.length;
  const normals = outwardNormals(outline);
  const points: Point3[][] = rings.map((ring) => {
    const shifted = offsetOutline(outline, ring.offset);
    return shifted.map(([x, z], index) => {
      const [nx, nz] = normals[index] as [number, number];
      const wobble = ring.jitter * (random() - 0.4) * 0.05;
      const lift = ring.jitter * (random() - 0.5) * 0.04;
      return [x + nx * wobble, ring.y + lift, z + nz * wobble] as const;
    });
  });
  const grass: SwatchPoint = { swatch: "green", along: GRASS_ALONG };
  const sand: SwatchPoint = { swatch: "cliff", along: SAND_ALONG };
  const cliff = (y: number): SwatchPoint => ({
    swatch: "cliff",
    along: heightAlong(y, 0, top, CLIFF_LOW, CLIFF_HIGH),
  });
  const up: Point3 = [0, 1, 0];
  // Each grass face a little darker or not, so the flat top shows its facets.
  const grassShade = () => {
    const factor = 0.84 + 0.16 * random();
    return GRASS_TINT.map((channel) => channel * factor) as unknown as Shade;
  };

  // The flat top, a fan from the middle.
  const [cx, cz] = centroid(outline);
  const middle: Point3 = [cx, top, cz];
  const lip = points[0] as Point3[];
  for (let index = 0; index < count; index++) {
    const a = lip[index] as Point3;
    const b = lip[(index + 1) % count] as Point3;
    geometry.triangle(middle, a, b, [grass, grass, grass], up, grassShade());
  }
  // The bands between rings: grass edge, cliff, cliff foot, beach.
  for (let band = 0; band < rings.length - 1; band++) {
    const inner = points[band] as Point3[];
    const outer = points[band + 1] as Point3[];
    for (let index = 0; index < count; index++) {
      const next = (index + 1) % count;
      const a = inner[index] as Point3;
      const b = inner[next] as Point3;
      const c = outer[next] as Point3;
      const d = outer[index] as Point3;
      const [nx, nz] = normals[index] as [number, number];
      const toward: Point3 = [nx, 0.35, nz];
      if (band === 0) {
        geometry.quad(
          a,
          b,
          c,
          d,
          [grass, grass, grass, grass],
          up,
          grassShade(),
        );
      } else if (band >= 3) {
        geometry.quad(a, b, c, d, [sand, sand, sand, sand], toward);
      } else {
        // Alternate the diagonal so the facets zigzag around the cliff.
        const colours = [cliff(a[1]), cliff(b[1]), cliff(c[1]), cliff(d[1])];
        if (index % 2 === 0) {
          geometry.quad(a, b, c, d, colours as never, toward);
        } else {
          geometry.quad(
            b,
            c,
            d,
            a,
            [colours[1], colours[2], colours[3], colours[0]] as never,
            toward,
          );
        }
      }
    }
  }
  return geometry;
}

/** Where the sea meets an island's beach, for the sea's shallows and foam. */
export function waterline(outline: Outline): Outline {
  return offsetOutline(outline, WATERLINE_OFFSET);
}

// ---- props -----------------------------------------------------------------------------------

export interface PropPlacement {
  kind: "tree" | "rock";
  castleId: number;
  /** World position. */
  x: number;
  y: number;
  z: number;
  scale: number;
  yaw: number;
}

/**
 * Directions around a castle where props never go, as [centre, half width] in radians of the
 * local angle (x = cos, z = sin). Behind the castle (+z, up the screen) is under its soldier
 * count.
 */
export const PROP_FREE_SECTORS: readonly (readonly [number, number])[] = [
  [Math.PI / 2, (55 * Math.PI) / 180],
];
/** Props keep this far from any castle's centre: outside the plateau and its footprint. */
export const PROP_CLEARANCE = PLATEAU_RADIUS + 0.1;

function angleGap(left: number, right: number): number {
  const difference = Math.abs(left - right) % (Math.PI * 2);
  return Math.min(difference, Math.PI * 2 - difference);
}

export function inPropFreeSector(angle: number): boolean {
  return PROP_FREE_SECTORS.some(
    ([centre, half]) => angleGap(angle, centre) < half,
  );
}

/** The cliff's top edge at `angle` around a castle, from the seeded outline. */
function rimAt(castleId: number, angle: number): number {
  const radii = islandRadii(castleId, ISLAND_CORNERS);
  const step = (Math.PI * 2) / ISLAND_CORNERS;
  const wrapped = ((angle % (Math.PI * 2)) + Math.PI * 2) % (Math.PI * 2);
  const index = Math.floor(wrapped / step);
  const s = wrapped / step - index;
  const a = radii[index % ISLAND_CORNERS] ?? 1;
  const b = radii[(index + 1) % ISLAND_CORNERS] ?? 1;
  return (a + (b - a) * s) * ISLAND_RADIUS;
}

/**
 * The seeded trees on each island's grass and rocks on its beach. A prop stays outside every
 * castle's plateau, out of the label sector and clear of other props, so an island with no
 * room gets none. Positions are in world units.
 */
export function islandProps(castles: readonly IslandCastle[]): PropPlacement[] {
  const props: PropPlacement[] = [];
  const centres = castles.map((castle) => {
    const world = toWorld(castle);
    return { id: castle.id, x: world.x, z: world.z };
  });
  const clear = (x: number, z: number, room: number) =>
    centres.every(
      (centre) =>
        Math.hypot(x - centre.x, z - centre.z) >= PROP_CLEARANCE + room,
    ) &&
    props.every((prop) => Math.hypot(x - prop.x, z - prop.z) >= 0.16 + room);
  for (const castle of [...centres].sort((left, right) => left.id - right.id)) {
    const random = seededRandom(hashSeed(castle.id, 102));
    const wanted = {
      tree: 1 + Math.floor(random() * 3),
      rock: 1 + Math.floor(random() * 2),
    };
    const placed = { tree: 0, rock: 0 };
    for (let attempt = 0; attempt < 24; attempt++) {
      const kind = attempt % 2 === 0 ? "tree" : "rock";
      const angle = random() * Math.PI * 2;
      const pick = random();
      const scale = 0.8 + random() * 0.45;
      const yaw = random() * Math.PI * 2;
      if (placed[kind] >= wanted[kind] || inPropFreeSector(angle)) continue;
      const rim = rimAt(castle.id, angle);
      let distance: number;
      let y: number;
      if (kind === "tree") {
        const inner = PROP_CLEARANCE + 0.06 * scale;
        const outer = rim - LIP - 0.07 * scale;
        if (outer < inner) continue;
        distance = inner + (outer - inner) * pick;
        y = ISLAND_TOP;
      } else {
        distance = rim + 0.1 + 0.03 * pick;
        y = 0.02;
      }
      const x = castle.x + Math.cos(angle) * distance;
      const z = castle.z + Math.sin(angle) * distance;
      if (!clear(x, z, 0.06 * scale)) continue;
      placed[kind]++;
      props.push({ kind, castleId: castle.id, x, y, z, scale, yaw });
    }
  }
  return props;
}
