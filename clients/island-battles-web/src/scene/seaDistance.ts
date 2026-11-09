import type { Outline } from "./landforms";

// The distance from every point of the sea to the nearest island's waterline, sampled on a grid
// for the sea shader's shallows and foam (issue #102). Plain arrays, so it is tested on its own.

export interface DistanceField {
  /** World x and z of the grid's first texel's centre... */
  minX: number;
  minZ: number;
  /** ...and the grid's size in world units. */
  width: number;
  depth: number;
  columns: number;
  rows: number;
  /** Distance divided by `maxDistance`, clamped to [0, 1], row by row from minZ. */
  values: Float32Array;
  maxDistance: number;
}

/** A polygon in world x and z. */
export type WorldPolygon = Outline;

function distanceToSegment(
  px: number,
  pz: number,
  ax: number,
  az: number,
  bx: number,
  bz: number,
): number {
  const dx = bx - ax;
  const dz = bz - az;
  const lengthSquared = dx * dx + dz * dz;
  const t =
    lengthSquared === 0
      ? 0
      : Math.max(
          0,
          Math.min(1, ((px - ax) * dx + (pz - az) * dz) / lengthSquared),
        );
  return Math.hypot(px - (ax + dx * t), pz - (az + dz * t));
}

function inside(px: number, pz: number, polygon: WorldPolygon): boolean {
  let result = false;
  for (let i = 0, j = polygon.length - 1; i < polygon.length; j = i++) {
    const [xi, zi] = polygon[i] as [number, number];
    const [xj, zj] = polygon[j] as [number, number];
    if (zi > pz !== zj > pz && px < ((xj - xi) * (pz - zi)) / (zj - zi) + xi) {
      result = !result;
    }
  }
  return result;
}

/**
 * The distance field around `polygons` (island waterlines in world x and z), `texelsPerUnit`
 * fine, reaching `maxDistance` past the farthest shore so the edge texels are open sea.
 */
export function distanceField(
  polygons: readonly WorldPolygon[],
  maxDistance = 1,
  texelsPerUnit = 16,
): DistanceField {
  let minX = Number.POSITIVE_INFINITY;
  let minZ = Number.POSITIVE_INFINITY;
  let maxX = Number.NEGATIVE_INFINITY;
  let maxZ = Number.NEGATIVE_INFINITY;
  for (const polygon of polygons) {
    for (const [x, z] of polygon) {
      minX = Math.min(minX, x);
      minZ = Math.min(minZ, z);
      maxX = Math.max(maxX, x);
      maxZ = Math.max(maxZ, z);
    }
  }
  if (!Number.isFinite(minX)) {
    minX = -1;
    minZ = -1;
    maxX = 1;
    maxZ = 1;
  }
  const margin = maxDistance + 0.5;
  minX -= margin;
  minZ -= margin;
  maxX += margin;
  maxZ += margin;
  const columns = Math.ceil((maxX - minX) * texelsPerUnit);
  const rows = Math.ceil((maxZ - minZ) * texelsPerUnit);
  const step = 1 / texelsPerUnit;
  const values = new Float32Array(columns * rows).fill(1);
  for (const polygon of polygons) {
    let px0 = Number.POSITIVE_INFINITY;
    let pz0 = Number.POSITIVE_INFINITY;
    let px1 = Number.NEGATIVE_INFINITY;
    let pz1 = Number.NEGATIVE_INFINITY;
    for (const [x, z] of polygon) {
      px0 = Math.min(px0, x);
      pz0 = Math.min(pz0, z);
      px1 = Math.max(px1, x);
      pz1 = Math.max(pz1, z);
    }
    const c0 = Math.max(0, Math.floor((px0 - maxDistance - minX) / step));
    const c1 = Math.min(
      columns - 1,
      Math.ceil((px1 + maxDistance - minX) / step),
    );
    const r0 = Math.max(0, Math.floor((pz0 - maxDistance - minZ) / step));
    const r1 = Math.min(rows - 1, Math.ceil((pz1 + maxDistance - minZ) / step));
    for (let row = r0; row <= r1; row++) {
      const z = minZ + (row + 0.5) * step;
      for (let column = c0; column <= c1; column++) {
        const x = minX + (column + 0.5) * step;
        const index = row * columns + column;
        if ((values[index] ?? 1) === 0) continue;
        let distance = Number.POSITIVE_INFINITY;
        if (inside(x, z, polygon)) {
          distance = 0;
        } else {
          for (let i = 0, j = polygon.length - 1; i < polygon.length; j = i++) {
            const [ax, az] = polygon[j] as [number, number];
            const [bx, bz] = polygon[i] as [number, number];
            distance = Math.min(
              distance,
              distanceToSegment(x, z, ax, az, bx, bz),
            );
          }
        }
        values[index] = Math.min(values[index] ?? 1, distance / maxDistance);
      }
    }
  }
  return {
    minX,
    minZ,
    width: columns * step,
    depth: rows * step,
    columns,
    rows,
    values,
    maxDistance,
  };
}

/** The field's value at a world point, from its nearest texel. */
export function sampleField(
  field: DistanceField,
  x: number,
  z: number,
): number {
  const column = Math.min(
    field.columns - 1,
    Math.max(0, Math.floor(((x - field.minX) / field.width) * field.columns)),
  );
  const row = Math.min(
    field.rows - 1,
    Math.max(0, Math.floor(((z - field.minZ) / field.depth) * field.rows)),
  );
  return (field.values[row * field.columns + column] ?? 1) * field.maxDistance;
}
