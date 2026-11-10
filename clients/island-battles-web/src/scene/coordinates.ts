// The one place that maps map coordinates to world coordinates. One map unit is WORLD_SCALE
// world units: the islands, castles and boats keep their size in world units, and the sea
// between them grows. The map's x is world x. The map's y is world -z: the 2D replay draws y downward on
// the screen, and the default camera looks along +z, so a larger y is nearer the camera and
// the 3D board is laid out as the replay is.

export interface MapPoint {
  x: number;
  y: number;
}

export interface WorldPoint {
  x: number;
  y: number;
  z: number;
}

/**
 * World units per map unit. Positions and distances that come from the map (castle and boat
 * positions, clash radii, the distance between castles) are multiplied by it; sizes of things
 * in the scene (islands, castle models, boats, labels, line widths) are world units and are not.
 */
export const WORLD_SCALE = 1.75;

/** A distance measured on the map, as a distance in the world. */
export function toWorldDistance(mapDistance: number): number {
  return mapDistance * WORLD_SCALE;
}

/** A distance measured in the world, as a distance on the map. */
export function toMapDistance(worldDistance: number): number {
  return worldDistance / WORLD_SCALE;
}

export function toWorld(point: MapPoint, height = 0): WorldPoint {
  return {
    x: toWorldDistance(point.x),
    y: height,
    z: -toWorldDistance(point.y),
  };
}

export function toMap(point: { x: number; z: number }): MapPoint {
  return { x: toMapDistance(point.x), y: -toMapDistance(point.z) };
}
