import type { MapPoint } from "./coordinates";

// The replay keeps at least the generated map's 8 by 6 units, with a unit of room around each
// castle for its circle and label. Castles never move, so any frame's castles bound the board.
export const MIN_BOARD_WIDTH = 8;
export const MIN_BOARD_DEPTH = 6;
export const BOARD_MARGIN = 1;
/** The visual radius of a castle, as in the replay. */
export const CASTLE_RADIUS = 0.5;

export interface BoardBounds {
  /** Half the board's width in map units (along x), centred on the origin. */
  halfWidth: number;
  /** Half the board's depth in map units (along y), centred on the origin. */
  halfDepth: number;
}

export function boardBounds(castles: readonly MapPoint[]): BoardBounds {
  return {
    halfWidth: castles.reduce(
      (largest, castle) => Math.max(largest, Math.abs(castle.x) + BOARD_MARGIN),
      MIN_BOARD_WIDTH / 2,
    ),
    halfDepth: castles.reduce(
      (largest, castle) => Math.max(largest, Math.abs(castle.y) + BOARD_MARGIN),
      MIN_BOARD_DEPTH / 2,
    ),
  };
}
