import { describe, expect, test } from "bun:test";
import { type LabelRequest, placeLabels } from "./labels";

const viewport = { width: 800, height: 600 };
const label = (
  id: string,
  x: number,
  y: number,
  extra: Partial<LabelRequest> = {},
): LabelRequest => ({
  id,
  x,
  y,
  width: 40,
  height: 20,
  priority: 10,
  required: true,
  ...extra,
});

function boxes(requests: LabelRequest[]) {
  const placements = placeLabels(requests, viewport);
  return placements
    .map((placement, index) => {
      const request = requests[index] as LabelRequest;
      return {
        id: request.id,
        visible: placement.visible,
        x: request.x + placement.offsetX,
        y: request.y + placement.offsetY,
        width: request.width,
        height: request.height,
      };
    })
    .filter((box) => box.visible);
}

function countOverlaps(placed: ReturnType<typeof boxes>): number {
  let overlaps = 0;
  for (const [index, first] of placed.entries()) {
    for (const second of placed.slice(index + 1)) {
      if (
        Math.abs(first.x - second.x) < (first.width + second.width) / 2 &&
        Math.abs(first.y - second.y) < (first.height + second.height) / 2
      ) {
        overlaps++;
      }
    }
  }
  return overlaps;
}

describe("label placement", () => {
  test("a lone label stays on its anchor", () => {
    expect(placeLabels([label("a", 100, 100)], viewport)).toEqual([
      { id: "a", offsetX: 0, offsetY: 0, visible: true },
    ]);
  });

  test("labels on the same spot are spread so none covers another", () => {
    const requests = Array.from({ length: 8 }, (_, index) =>
      label(`group:${index}`, 300, 300),
    );
    const placed = boxes(requests);
    expect(placed).toHaveLength(8);
    expect(countOverlaps(placed)).toBe(0);
  });

  test("a busy turn of clustered anchors has no overlaps", () => {
    const requests: LabelRequest[] = [];
    for (let index = 0; index < 30; index++) {
      requests.push(
        label(
          `l${index}`,
          300 + (index % 6) * 14,
          250 + Math.floor(index / 6) * 9,
          {
            priority: index % 3 === 0 ? 100 : 50,
          },
        ),
      );
    }
    expect(countOverlaps(boxes(requests))).toBe(0);
  });

  test("the higher priority keeps its spot", () => {
    const placements = placeLabels(
      [
        label("low", 100, 100, { priority: 1 }),
        label("high", 100, 100, { priority: 9 }),
      ],
      viewport,
    );
    expect(placements[1]).toMatchObject({ id: "high", offsetX: 0, offsetY: 0 });
    expect(placements[0]?.offsetY).not.toBe(0);
  });

  test("placement does not depend on the request order", () => {
    const requests = [
      label("a", 100, 100),
      label("b", 105, 100),
      label("c", 110, 100),
    ];
    const forward = placeLabels(requests, viewport);
    const backward = placeLabels([...requests].reverse(), viewport).reverse();
    expect(backward).toEqual(forward);
  });

  test("a label that is not required hides when it has nowhere to go", () => {
    const placements = placeLabels(
      [
        label("main", 100, 100, { priority: 9 }),
        label("extra", 100, 100, { required: false, maxShift: 5, priority: 1 }),
      ],
      viewport,
    );
    expect(placements[1]?.visible).toBe(false);
    expect(placements[0]?.visible).toBe(true);
  });

  test("a required label always shows", () => {
    const placements = placeLabels(
      [
        label("a", 100, 100, { maxShift: 0 }),
        label("b", 100, 100, { maxShift: 0 }),
      ],
      viewport,
    );
    expect(placements.every((placement) => placement.visible)).toBe(true);
  });

  test("labels stay inside the viewport", () => {
    const placed = boxes([
      label("edge", 5, 5),
      label("edge2", 5, 5),
      label("edge3", 795, 595),
    ]);
    for (const box of placed) {
      expect(box.x - box.width / 2).toBeGreaterThanOrEqual(0);
      expect(box.y - box.height / 2).toBeGreaterThanOrEqual(0);
      expect(box.x + box.width / 2).toBeLessThanOrEqual(viewport.width);
      expect(box.y + box.height / 2).toBeLessThanOrEqual(viewport.height);
    }
  });
});
