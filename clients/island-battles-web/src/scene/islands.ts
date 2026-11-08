import type { MapPoint } from "./coordinates";

/** Castles closer than this share one landmass. */
export const SHARED_LANDMASS_DISTANCE = 1.5;
/** The radius of the island under one castle, in map units. */
export const ISLAND_RADIUS = 1;
export const ISLAND_SIDES = 16;
/**
 * The smallest radius multiplier of an island. It was 0.75 until #102, which keeps a flat
 * plateau of PLATEAU_RADIUS (0.78, landforms.ts) for the level 4 castle's base (0.745) inside
 * every island's cliff, so the narrowest sides of the seeded outlines are a little wider now.
 */
export const MIN_ISLAND_RADIUS = 0.84;
/** The largest radius multiplier of an island. */
export const MAX_ISLAND_RADIUS = 1.25;
// mulberry32: a small seeded generator, so an island's shape is a function of the castle ID.
export function seededRandom(seed: number): () => number {
  let state = seed >>> 0;
  return () => {
    state = (state + 0x6d2b79f5) >>> 0;
    let value = state;
    value = Math.imul(value ^ (value >>> 15), value | 1);
    value ^= value + Math.imul(value ^ (value >>> 7), value | 61);
    return ((value ^ (value >>> 14)) >>> 0) / 4294967296;
  };
}

/** Mixes the numbers into one seed, so (a, b) and (b, a) differ and neighbouring IDs do not correlate. */
export function hashSeed(...values: number[]): number {
  let hash = 2166136261;
  for (const value of values) {
    hash = Math.imul(hash ^ (value + 0x9e3779b9), 16777619);
    hash ^= hash >>> 13;
  }
  return hash >>> 0;
}

/**
 * The radius multipliers around an island, from MIN_ISLAND_RADIUS to MAX_ISLAND_RADIUS of
 * ISLAND_RADIUS, a smooth blend of a few seeded waves. They depend only on the castle ID, so the
 * island never changes; more `sides` sample the same outline more finely.
 */
export function islandRadii(castleId: number, sides = ISLAND_SIDES): number[] {
  const random = seededRandom(hashSeed(castleId, 1));
  const waves = [2, 3, 5].map((frequency) => ({
    frequency,
    amplitude: 0.1 + random() * 0.08,
    phase: random() * Math.PI * 2,
  }));
  return Array.from({ length: sides }, (_, index) => {
    const angle = (index / sides) * Math.PI * 2;
    const offset = waves.reduce(
      (total, wave) =>
        total + wave.amplitude * Math.sin(wave.frequency * angle + wave.phase),
      0,
    );
    return Math.min(MAX_ISLAND_RADIUS, Math.max(MIN_ISLAND_RADIUS, 1 + offset));
  });
}

export interface IslandCastle extends MapPoint {
  id: number;
}

/** Pairs of castle IDs, lower ID first, that are closer than `distance`. */
export function landmassLinks(
  castles: readonly IslandCastle[],
  distance = SHARED_LANDMASS_DISTANCE,
): [number, number][] {
  const links: [number, number][] = [];
  const sorted = [...castles].sort((left, right) => left.id - right.id);
  for (const [index, first] of sorted.entries()) {
    for (const second of sorted.slice(index + 1)) {
      if (Math.hypot(first.x - second.x, first.y - second.y) < distance) {
        links.push([first.id, second.id]);
      }
    }
  }
  return links;
}

/** Castle IDs grouped by landmass, each group and the list sorted by ID. */
export function landmasses(
  castles: readonly IslandCastle[],
  distance = SHARED_LANDMASS_DISTANCE,
): number[][] {
  const parent = new Map(castles.map((castle) => [castle.id, castle.id]));
  const find = (id: number): number => {
    let root = id;
    while (parent.get(root) !== root) root = parent.get(root) ?? root;
    return root;
  };
  for (const [first, second] of landmassLinks(castles, distance)) {
    parent.set(find(second), find(first));
  }
  const groups = new Map<number, number[]>();
  for (const castle of [...castles].sort((left, right) => left.id - right.id)) {
    const root = find(castle.id);
    groups.set(root, [...(groups.get(root) ?? []), castle.id]);
  }
  return [...groups.values()].sort(
    (left, right) => (left[0] ?? 0) - (right[0] ?? 0),
  );
}
