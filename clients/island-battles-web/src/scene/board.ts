import { type MapPoint, toWorld, toWorldDistance } from "./coordinates";

// The board keeps at least the generated map's 8 by 6 map units, with a margin around each
// castle for its island and label. Castles never move, so any frame's castles bound the board.
/** The smallest board, in map units. */
export const MIN_BOARD_WIDTH = 8;
export const MIN_BOARD_DEPTH = 6;
/** Room around the outermost castles, in world units: an island, its label and some sea. */
export const BOARD_MARGIN = 1;
/** The visual radius of a castle, as in the replay, in world units. */
export const CASTLE_RADIUS = 0.5;

export interface BoardBounds {
  /** Half the board's width in world units (along x), centred on the origin. */
  halfWidth: number;
  /** Half the board's depth in world units (along z), centred on the origin. */
  halfDepth: number;
}

export function boardBounds(castles: readonly MapPoint[]): BoardBounds {
  return {
    halfWidth: castles.reduce(
      (largest, castle) =>
        Math.max(largest, Math.abs(toWorld(castle).x) + BOARD_MARGIN),
      toWorldDistance(MIN_BOARD_WIDTH / 2),
    ),
    halfDepth: castles.reduce(
      (largest, castle) =>
        Math.max(largest, Math.abs(toWorld(castle).z) + BOARD_MARGIN),
      toWorldDistance(MIN_BOARD_DEPTH / 2),
    ),
  };
}
