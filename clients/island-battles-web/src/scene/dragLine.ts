// The line of a drag to send (#343), as map-space geometry: from the origin's shore to the
// pointer, or to the target's shore while a castle is under the pointer. It is drawn as the
// heading arrows are (`ribbonGeometry`), so pure here and drawn by `dragControls.ts`.

import type { MapPoint } from "./coordinates";
import { toMapDistance } from "./coordinates";
import {
  castleGaps,
  type HeadingSegment,
  type HeadingStyle,
  headingStyle,
} from "./headingLines";
import { sharesLandmass, shorePoint } from "./shore";

export interface LineCastle {
  id: number;
  x: number;
  y: number;
}

export type DragSegment = Pick<
  HeadingSegment,
  "start" | "tip" | "base" | "arrowhead" | "gaps"
>;

/**
 * The line from `origin` to `end` (the pointer, in map units), or to `target`'s shore when one is
 * under the pointer: it starts at the origin's shore and ends with an arrowhead, with gaps where
 * it passes other castles. Null while it would be too short to show a shaft and a head (the
 * pointer is still over the origin's island). Between castles too close for a head it is a stub
 * of shaft alone; with less than the least shaft between their shores there is none.
 */
export function dragLineSegment(
  origin: LineCastle,
  end: MapPoint,
  target: LineCastle | null,
  castles: readonly LineCastle[],
  style: HeadingStyle = headingStyle,
): DragSegment | null {
  const towards = target ?? end;
  const bridged = sharesLandmass(origin, towards);
  const start = shorePoint(origin, towards, bridged);
  const tip = target ? shorePoint(target, origin, bridged) : end;
  const length = Math.hypot(tip.x - start.x, tip.y - start.y);
  const head = toMapDistance(style.arrowLength);
  const minShaft = toMapDistance(style.minShaft);
  if (length < head + minShaft) {
    if (!target) return null;
    // Snapped to a castle so close that there is no room for a head: a headless stub, so the
    // line does not vanish as it snaps and the pending line stays to be seen.
    // The shores may have passed each other (the tip behind the start): no line then.
    const forward =
      (tip.x - start.x) * (target.x - origin.x) +
      (tip.y - start.y) * (target.y - origin.y);
    if (length < minShaft || forward <= 0) return null;
    return { start, tip, base: tip, arrowhead: false, gaps: [] };
  }
  // The pointer went back over the origin's island: no line.
  if (
    !target &&
    Math.hypot(end.x - origin.x, end.y - origin.y) <
      Math.hypot(start.x - origin.x, start.y - origin.y)
  ) {
    return null;
  }
  const direction = {
    x: (tip.x - start.x) / length,
    y: (tip.y - start.y) / length,
  };
  const base = {
    x: tip.x - direction.x * head,
    y: tip.y - direction.y * head,
  };
  const { gaps } = castleGaps(
    start,
    base,
    castles.filter((castle) => castle.id !== origin.id),
    target?.id ?? -1,
    toMapDistance(style.castleGap),
  );
  return { start, tip, base, arrowhead: true, gaps };
}
