// Label placement is pure, so it can be tested without a GPU. A label is drawn at its anchor,
// except that labels of one row group (the boats' soldier counts) that overlap each other are laid
// out side by side in a row, so every one of them stays readable (#212). Then, when two labels
// still overlap, the one with the lower priority is hidden. It comes back only once it would be
// clear by a small margin, so a boat passing at the edge of a label does not make it flicker.
// Rows have the same margin: a label leaves its row only once it is clear by it.

export interface LabelRequest {
  id: string;
  /** The anchor's screen position in pixels, which is the label's centre unless it is in a row. */
  x: number;
  y: number;
  width: number;
  height: number;
  /** When two labels overlap, the one with the lower priority is hidden. */
  priority: number;
  /** Whether the last call hid the label, so it needs the margin to come back. */
  wasHidden?: boolean;
  /**
   * Labels with the same row group that overlap one another are laid out side by side in a row
   * instead of hiding each other. Labels without one are always drawn at their anchors.
   */
  rowGroup?: string | undefined;
  /** The row the last call put the label in (its placement's `row`), if any. */
  previousRow?: string | undefined;
  /** Where the last call drew the label (its placement's `x`); keeps the order inside a row. */
  previousX?: number | undefined;
}

export interface LabelPlacement {
  id: string;
  visible: boolean;
  /** Where the label's centre is drawn, in screen pixels. */
  x: number;
  y: number;
  /** The row the label is laid out in, when it shares one with other labels. */
  row?: string;
}

/** A hidden label returns, and a label leaves its row, only when clear by this many pixels. */
export const RETURN_MARGIN = 4;
/**
 * The space between two labels in a row. It equals the margin, so two labels that leave their
 * row as their boats sail apart are already where their anchors are.
 */
export const ROW_GAP = RETURN_MARGIN;
/** How long a label takes to fade out or in, and to move to a new place in a row. */
export const FADE_MILLISECONDS = 150;

interface Box {
  x: number;
  y: number;
  width: number;
  height: number;
}

function overlaps(left: Box, right: Box, margin: number) {
  return (
    Math.abs(left.x - right.x) < (left.width + right.width) / 2 + margin &&
    Math.abs(left.y - right.y) < (left.height + right.height) / 2 + margin
  );
}

/** Two labels that shared a row in the last call stay in it until clear by the margin. */
function rowMargin(left: LabelRequest, right: LabelRequest) {
  return left.previousRow !== undefined &&
    left.previousRow === right.previousRow
    ? RETURN_MARGIN
    : 0;
}

/**
 * The order of a row, left to right: by the anchors' screen x, ties by ID. Two neighbours that
 * shared a row in the last call keep their order there until their anchors have crossed by more
 * than the margin, so boats sailing one behind the other do not make their labels swap places.
 */
function rowOrder(members: readonly LabelRequest[]): LabelRequest[] {
  const order = [...members].sort(
    (left, right) => left.x - right.x || left.id.localeCompare(right.id),
  );
  const keepsOrder = (left: LabelRequest, right: LabelRequest) =>
    left.previousRow !== undefined &&
    left.previousRow === right.previousRow &&
    left.previousX !== undefined &&
    right.previousX !== undefined &&
    right.previousX < left.previousX &&
    right.x - left.x <= RETURN_MARGIN;
  // Each swap puts one pair back in its earlier order, so this ends.
  let swapped = true;
  while (swapped) {
    swapped = false;
    for (let index = 0; index + 1 < order.length; index++) {
      const left = order[index] as LabelRequest;
      const right = order[index + 1] as LabelRequest;
      if (keepsOrder(left, right)) {
        order[index] = right;
        order[index + 1] = left;
        swapped = true;
      }
    }
  }
  return order;
}

/** Lays a row out, centred on the mean of its anchors. */
function layOutRow(
  members: readonly LabelRequest[],
  positions: Map<string, Box>,
): void {
  const centreX =
    members.reduce((sum, member) => sum + member.x, 0) / members.length;
  const centreY =
    members.reduce((sum, member) => sum + member.y, 0) / members.length;
  const total =
    members.reduce((sum, member) => sum + member.width, 0) +
    ROW_GAP * (members.length - 1);
  let left = centreX - total / 2;
  for (const member of rowOrder(members)) {
    positions.set(member.id, {
      x: left + member.width / 2,
      y: centreY,
      width: member.width,
      height: member.height,
    });
    left += member.width + ROW_GAP;
  }
}

/**
 * Where every label is drawn, and the row of those laid out together. Labels of one row group
 * that overlap (transitively, and again after a row has grown) are laid out in one row.
 */
function layOutRows(requests: readonly LabelRequest[]) {
  const positions = new Map<string, Box>();
  const rows = new Map<string, string>();
  const groups = new Map<string, LabelRequest[]>();
  for (const request of requests) {
    positions.set(request.id, request);
    if (request.rowGroup === undefined) continue;
    const members = groups.get(request.rowGroup) ?? [];
    members.push(request);
    groups.set(request.rowGroup, members);
  }
  for (const members of groups.values()) {
    // Sorted first, so the rows do not depend on the order of the input.
    const clusters = [...members]
      .sort(
        (left, right) => left.x - right.x || left.id.localeCompare(right.id),
      )
      .map((member) => [member]);
    const touch = (first: LabelRequest[], second: LabelRequest[]) =>
      first.some((left) =>
        second.some((right) =>
          overlaps(
            positions.get(left.id) as Box,
            positions.get(right.id) as Box,
            rowMargin(left, right),
          ),
        ),
      );
    let merged = true;
    while (merged) {
      merged = false;
      search: for (let first = 0; first < clusters.length; first++) {
        for (let second = first + 1; second < clusters.length; second++) {
          const left = clusters[first] as LabelRequest[];
          const right = clusters[second] as LabelRequest[];
          if (!touch(left, right)) continue;
          left.push(...right);
          clusters.splice(second, 1);
          layOutRow(left, positions);
          merged = true;
          break search;
        }
      }
    }
    for (const cluster of clusters) {
      if (cluster.length < 2) continue;
      const row = cluster
        .map((member) => member.id)
        .sort()
        .join(",");
      for (const member of cluster) rows.set(member.id, row);
    }
  }
  return { positions, rows };
}

/** Where each label is drawn and whether it is shown. The result is in the order of `requests`. */
export function placeLabels(
  requests: readonly LabelRequest[],
): LabelPlacement[] {
  const { positions, rows } = layOutRows(requests);
  const ordered = [...requests].sort(
    (left, right) =>
      right.priority - left.priority || left.id.localeCompare(right.id),
  );
  const shown: Box[] = [];
  const visibleIds = new Set<string>();
  for (const request of ordered) {
    const box = positions.get(request.id) as Box;
    const margin = request.wasHidden ? RETURN_MARGIN : 0;
    if (!shown.some((other) => overlaps(box, other, margin))) {
      shown.push(box);
      visibleIds.add(request.id);
    }
  }
  return requests.map((request) => {
    const box = positions.get(request.id) as Box;
    const row = rows.get(request.id);
    return {
      id: request.id,
      visible: visibleIds.has(request.id),
      x: box.x,
      y: box.y,
      ...(row === undefined ? {} : { row }),
    };
  });
}

/** The label's opacity after `elapsed` milliseconds of fading towards shown or hidden. */
export function fadeAlpha(
  alpha: number,
  visible: boolean,
  elapsed: number,
): number {
  const step = Math.max(0, elapsed) / FADE_MILLISECONDS;
  return visible ? Math.min(1, alpha + step) : Math.max(0, alpha - step);
}

/**
 * A label's offset from its anchor after `elapsed` milliseconds of easing towards `target`: most
 * of the way within the fade time, and exactly there once within half a pixel.
 */
export function easeOffset(
  offset: number,
  target: number,
  elapsed: number,
): number {
  const remaining =
    (target - offset) *
    Math.exp((-3 * Math.max(0, elapsed)) / FADE_MILLISECONDS);
  return Math.abs(remaining) < 0.5 ? target : target - remaining;
}
