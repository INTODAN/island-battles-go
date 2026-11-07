// Label placement is pure, so it can be tested without a GPU: given where each label's anchor
// is on the screen, it shifts labels so that none covers another.

export interface LabelRequest {
  id: string;
  /** The anchor's screen position in pixels. */
  x: number;
  y: number;
  width: number;
  height: number;
  /** Higher priorities are placed first and keep their spot. */
  priority: number;
  /** A label that is not required is hidden when it has no free spot. */
  required: boolean;
  /** The farthest, in pixels, the label may move from its anchor. */
  maxShift?: number;
}

export interface LabelPlacement {
  id: string;
  /** The offset from the anchor, in pixels, to the label's centre. */
  offsetX: number;
  offsetY: number;
  visible: boolean;
}

export interface Viewport {
  width: number;
  height: number;
}

interface Box {
  x: number;
  y: number;
  width: number;
  height: number;
}

const GAP = 3;

function overlaps(left: Box, right: Box): boolean {
  return (
    Math.abs(left.x - right.x) < (left.width + right.width) / 2 + GAP &&
    Math.abs(left.y - right.y) < (left.height + right.height) / 2 + GAP
  );
}

/** Offsets to try, in order: in place, then above and below, then to the sides, then further out. */
function candidates(width: number, height: number): [number, number][] {
  const stepY = height + GAP;
  const stepX = width + GAP;
  const list: [number, number][] = [[0, 0]];
  for (let ring = 1; ring <= 10; ring++) {
    list.push([0, -ring * stepY], [0, ring * stepY]);
    list.push(
      [stepX * ring * 0.6, -ring * stepY * 0.5],
      [-stepX * ring * 0.6, -ring * stepY * 0.5],
    );
    list.push(
      [stepX * ring * 0.6, ring * stepY * 0.5],
      [-stepX * ring * 0.6, ring * stepY * 0.5],
    );
    list.push([stepX * ring, 0], [-stepX * ring, 0]);
  }
  return list;
}

export function placeLabels(
  requests: readonly LabelRequest[],
  viewport: Viewport,
): LabelPlacement[] {
  const ordered = [...requests].sort(
    (left, right) =>
      right.priority - left.priority || left.id.localeCompare(right.id),
  );
  const placed: Box[] = [];
  const placements = new Map<string, LabelPlacement>();
  for (const request of ordered) {
    let chosen: LabelPlacement | null = null;
    for (const [offsetX, offsetY] of candidates(
      request.width,
      request.height,
    )) {
      if (
        Math.hypot(offsetX, offsetY) >
        (request.maxShift ?? Number.POSITIVE_INFINITY)
      ) {
        continue;
      }
      const box = {
        x: request.x + offsetX,
        y: request.y + offsetY,
        width: request.width,
        height: request.height,
      };
      const inside =
        box.x - box.width / 2 >= 0 &&
        box.x + box.width / 2 <= viewport.width &&
        box.y - box.height / 2 >= 0 &&
        box.y + box.height / 2 <= viewport.height;
      if (!inside || placed.some((other) => overlaps(box, other))) continue;
      placed.push(box);
      chosen = { id: request.id, offsetX, offsetY, visible: true };
      break;
    }
    if (!chosen) {
      // Nowhere free: a required label stays where it is, over the others; the rest hide.
      chosen = {
        id: request.id,
        offsetX: 0,
        offsetY: 0,
        visible: request.required,
      };
      if (request.required) {
        placed.push({
          x: request.x,
          y: request.y,
          width: request.width,
          height: request.height,
        });
      }
    }
    placements.set(request.id, chosen);
  }
  return requests.map(
    (request) => placements.get(request.id) as LabelPlacement,
  );
}
