// Label placement is pure, so it can be tested without a GPU. Every label is pinned to its own
// anchor and never moves; when two would overlap on the screen, the one with the lower priority
// is hidden. It comes back only once it would be clear by a small margin, so a boat passing at
// the edge of a label does not make it flicker.

export interface LabelRequest {
  id: string;
  /** The anchor's screen position in pixels, which is the label's centre. */
  x: number;
  y: number;
  width: number;
  height: number;
  /** When two labels overlap, the one with the lower priority is hidden. */
  priority: number;
  /** Whether the last call hid the label, so it needs the margin to come back. */
  wasHidden?: boolean;
}

export interface LabelPlacement {
  id: string;
  visible: boolean;
}

/** A hidden label returns only when it is clear of the visible ones by this many pixels. */
export const RETURN_MARGIN = 4;
/** How long a label takes to fade out or in. */
export const FADE_MILLISECONDS = 150;

function overlaps(left: LabelRequest, right: LabelRequest, margin: number) {
  return (
    Math.abs(left.x - right.x) < (left.width + right.width) / 2 + margin &&
    Math.abs(left.y - right.y) < (left.height + right.height) / 2 + margin
  );
}

/** Which labels are shown. The result is in the order of `requests`. */
export function placeLabels(
  requests: readonly LabelRequest[],
): LabelPlacement[] {
  const ordered = [...requests].sort(
    (left, right) =>
      right.priority - left.priority || left.id.localeCompare(right.id),
  );
  const shown: LabelRequest[] = [];
  const visibleIds = new Set<string>();
  for (const request of ordered) {
    const margin = request.wasHidden ? RETURN_MARGIN : 0;
    if (!shown.some((other) => overlaps(request, other, margin))) {
      shown.push(request);
      visibleIds.add(request.id);
    }
  }
  return requests.map((request) => ({
    id: request.id,
    visible: visibleIds.has(request.id),
  }));
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
