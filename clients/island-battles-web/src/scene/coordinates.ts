// The one place that maps map coordinates to world coordinates. One map unit is one world
// unit. The map's x is world x. The map's y is world -z: the 2D replay draws y downward on
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

export function toWorld(point: MapPoint, height = 0): WorldPoint {
  return { x: point.x, y: height, z: -point.y };
}

export function toMap(point: { x: number; z: number }): MapPoint {
  return { x: point.x, y: -point.z };
}
