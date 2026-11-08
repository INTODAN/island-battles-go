import { ATLAS_SIZE, SWATCH_RECTS, type SwatchRect } from "./castleAtlas";

// Where the board's own pieces (islands, boats, props) sample the castle atlas (issue #102). The
// atlas is seven vertical-gradient swatches; a face picks a swatch and a height within its
// gradient, so it is coloured, and lit, the way the castles are.

/** Every swatch of the base atlas, as pixel rectangles (inclusive; y 0 is the top row). */
export const ATLAS_SWATCHES = {
  /** Dark grey at the top to cream at the bottom: cliffs, rocks and sand. */
  cliff: { x0: 0, x1: 121, y0: 0, y1: 255 },
  /** Light blue-grey stone to dark slate; team faces repaint it with the team colour. */
  stone: SWATCH_RECTS.stone,
  /** Pale ice-blue to grey teal. */
  ice: { x0: 265, x1: 366, y0: 0, y1: 255 },
  /** Light to dark brown wood, the whole height of the atlas. */
  wood: { x0: 367, x1: 511, y0: 0, y1: 511 },
  /** Dark green at the top to lime at the bottom: grass and leaves. */
  green: { x0: 0, x1: 121, y0: 256, y1: 511 },
  /** Red to gold; team faces repaint it from the team colour toward gold. */
  flags: SWATCH_RECTS.flags,
  /** Red to dark brown; team faces repaint it with a darker team colour. */
  accent: SWATCH_RECTS.accent,
} as const satisfies Record<string, SwatchRect>;

export type AtlasSwatch = keyof typeof ATLAS_SWATCHES;

/**
 * The UV of a point `along` a swatch's gradient: 0 is the swatch's top row, 1 its bottom row. It
 * samples the middle of the swatch's width and stays a pixel inside its edges, so mipmaps and
 * filtering never bleed in a neighbour. v = 0 is the image's top row, as the castle GLBs expect.
 */
export function swatchUv(swatch: AtlasSwatch, along: number): [number, number] {
  const rect = ATLAS_SWATCHES[swatch];
  const t = Math.min(1, Math.max(0, along));
  const x = (rect.x0 + rect.x1 + 1) / 2;
  const y = rect.y0 + 1.5 + t * (rect.y1 - rect.y0 - 2);
  return [x / ATLAS_SIZE, y / ATLAS_SIZE];
}

/**
 * Where a face's height falls in a gradient: `from` at the bottom of the range (`low`) and `to`
 * at its top (`high`), linearly in between. A cliff passes the cliff swatch's dark end (0) at
 * the waterline and its cream end (1) at the top, for example.
 */
export function heightAlong(
  height: number,
  low: number,
  high: number,
  from: number,
  to: number,
): number {
  const s = high === low ? 0 : (height - low) / (high - low);
  return from + (to - from) * Math.min(1, Math.max(0, s));
}
