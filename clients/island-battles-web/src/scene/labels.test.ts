import { describe, expect, test } from "bun:test";
import {
  easeOffset,
  FADE_MILLISECONDS,
  fadeAlpha,
  type LabelPlacement,
  type LabelRequest,
  placeLabels,
  RETURN_MARGIN,
  ROW_GAP,
} from "./labels";

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
  ...extra,
});

/** A boat's soldier count, as the battlefield asks for it. */
const boat = (
  id: string,
  x: number,
  y: number,
  extra: Partial<LabelRequest> = {},
): LabelRequest =>
  label(id, x, y, { priority: 80, rowGroup: "boats", ...extra });

const byId = (placements: LabelPlacement[]) =>
  new Map(placements.map((placement) => [placement.id, placement]));

/** The IDs of the placed labels, left to right. */
const leftToRight = (placements: LabelPlacement[]) =>
  [...placements]
    .sort((left, right) => left.x - right.x)
    .map((placement) => placement.id);

/** Whether two placed labels (40 by 20) overlap. */
const placedOverlap = (left: LabelPlacement, right: LabelPlacement) =>
  Math.abs(left.x - right.x) < 40 && Math.abs(left.y - right.y) < 20;

/** The next frame's request for a label, carrying over its placement as the label layer does. */
const carried = (
  request: LabelRequest,
  placement: LabelPlacement,
  x: number,
  y: number,
): LabelRequest => ({
  ...request,
  x,
  y,
  wasHidden: !placement.visible,
  previousRow: placement.row,
  previousX: placement.x,
});

const visibleIds = (requests: LabelRequest[]) =>
  placeLabels(requests)
    .filter((placement) => placement.visible)
    .map((placement) => placement.id)
    .sort();

describe("label placement", () => {
  test("a lone label is shown", () => {
    expect(placeLabels([label("a", 100, 100)])).toEqual([
      { id: "a", visible: true, x: 100, y: 100 },
    ]);
  });

  test("a label without a row group is always drawn at its anchor", () => {
    expect(placeLabels([label("a", 100, 100), label("b", 105, 100)])).toEqual([
      { id: "a", visible: true, x: 100, y: 100 },
      { id: "b", visible: false, x: 105, y: 100 },
    ]);
  });

  test("of two overlapping labels the lower priority hides", () => {
    const placements = placeLabels([
      label("low", 100, 100, { priority: 30 }),
      label("high", 110, 105, { priority: 100 }),
    ]);
    expect(placements).toEqual([
      { id: "low", visible: false, x: 100, y: 100 },
      { id: "high", visible: true, x: 110, y: 105 },
    ]);
  });

  test("labels that do not overlap are all shown", () => {
    expect(visibleIds([label("a", 100, 100), label("b", 141, 100)])).toEqual([
      "a",
      "b",
    ]);
  });

  test("a hidden label does not cover the ones below it", () => {
    // low overlaps high; lowest overlaps only low, so it is clear of every visible label.
    expect(
      visibleIds([
        label("high", 100, 100, { priority: 100 }),
        label("low", 130, 100, { priority: 50 }),
        label("lowest", 165, 100, { priority: 10 }),
      ]),
    ).toEqual(["high", "lowest"]);
  });

  test("a hidden label returns only when clear by the margin", () => {
    const blocker = label("blocker", 100, 100, { priority: 100 });
    // Centres 40 apart: just touching (width 40), no overlap.
    const gap = (extra: number) =>
      label("back", 140 + extra, 100, { priority: 10, wasHidden: true });
    expect(visibleIds([blocker, gap(RETURN_MARGIN - 1)])).toEqual(["blocker"]);
    expect(visibleIds([blocker, gap(RETURN_MARGIN)])).toEqual([
      "back",
      "blocker",
    ]);
    // A label that is already shown does not need the margin.
    expect(
      visibleIds([blocker, label("back", 140, 100, { wasHidden: false })]),
    ).toEqual(["back", "blocker"]);
  });

  test("the result does not depend on the order of the input", () => {
    const requests = [
      label("a", 100, 100, { priority: 100 }),
      label("b", 105, 100),
      label("c", 110, 100, { priority: 100 }),
      label("d", 300, 300),
    ];
    const forward = placeLabels(requests);
    const backward = placeLabels([...requests].reverse()).reverse();
    expect(backward).toEqual(forward);
  });

  test("the rows do not depend on the order of the input", () => {
    const requests = [
      boat("a", 100, 100),
      boat("b", 110, 104),
      boat("c", 150, 100),
      boat("d", 300, 300),
      label("castle", 90, 130, { priority: 100 }),
    ];
    const forward = placeLabels(requests);
    const backward = placeLabels([...requests].reverse()).reverse();
    expect(backward).toEqual(forward);
  });

  test("a busy cluster has no two shown labels overlapping", () => {
    const requests: LabelRequest[] = [];
    for (let index = 0; index < 30; index++) {
      requests.push(
        label(
          `l${index}`,
          300 + (index % 6) * 14,
          250 + Math.floor(index / 6) * 9,
          { priority: index % 3 === 0 ? 100 : 50 },
        ),
      );
    }
    const shown = requests.filter((request) =>
      visibleIds(requests).includes(request.id),
    );
    for (const [index, first] of shown.entries()) {
      for (const second of shown.slice(index + 1)) {
        const apart =
          Math.abs(first.x - second.x) >= 40 ||
          Math.abs(first.y - second.y) >= 20;
        expect(apart).toBe(true);
      }
    }
  });

  test("a busy cluster of boats and castles has no two shown labels overlapping", () => {
    const requests: LabelRequest[] = [];
    for (let index = 0; index < 30; index++) {
      const x = 300 + (index % 6) * 14;
      const y = 250 + Math.floor(index / 6) * 9;
      requests.push(
        index % 5 === 0
          ? label(`castle${index}`, x, y, { priority: 100 })
          : boat(`boat${index}`, x, y),
      );
    }
    const shown = placeLabels(requests).filter(
      (placement) => placement.visible,
    );
    expect(shown.some((placement) => placement.row !== undefined)).toBe(true);
    for (const [index, first] of shown.entries()) {
      for (const second of shown.slice(index + 1)) {
        expect(placedOverlap(first, second)).toBe(false);
      }
    }
  });
});

describe("boat labels side by side (#212)", () => {
  test("two overlapping boat labels are both shown, side by side", () => {
    const [a, b] = placeLabels([boat("a", 100, 100), boat("b", 110, 104)]) as [
      LabelPlacement,
      LabelPlacement,
    ];
    expect(a.visible).toBe(true);
    expect(b.visible).toBe(true);
    expect(placedOverlap(a, b)).toBe(false);
    // Next to each other with the gap between them, on one line, centred on the anchors' mean.
    expect(b.x - a.x).toBe(40 + ROW_GAP);
    expect((a.x + b.x) / 2).toBe(105);
    expect(a.y).toBe(102);
    expect(b.y).toBe(102);
    expect(a.row).toBeDefined();
    expect(a.row).toBe(b.row);
  });

  test("three in a cluster are laid out in anchor-x order, centred on their mean", () => {
    // c overlaps b and b overlaps a, so all three are one row.
    const placements = placeLabels([
      boat("c", 130, 96),
      boat("a", 100, 100),
      boat("b", 115, 110),
    ]);
    expect(leftToRight(placements)).toEqual(["a", "b", "c"]);
    const at = byId(placements);
    expect(at.get("a")?.x).toBeCloseTo(115 - 40 - ROW_GAP);
    expect(at.get("b")?.x).toBeCloseTo(115);
    expect(at.get("c")?.x).toBeCloseTo(115 + 40 + ROW_GAP);
    for (const placement of placements) {
      expect(placement.visible).toBe(true);
      expect(placement.y).toBeCloseTo(102);
    }
  });

  test("anchors at the same x are ordered by ID", () => {
    expect(
      leftToRight(placeLabels([boat("b", 100, 100), boat("a", 100, 108)])),
    ).toEqual(["a", "b"]);
  });

  test("a row that grows takes in the labels it then overlaps", () => {
    // c is clear of b's anchor, but not of b's place in the row of a and b.
    const placements = placeLabels([
      boat("a", 100, 100),
      boat("b", 100, 100),
      boat("c", 155, 100),
    ]);
    expect(placements.every((placement) => placement.visible)).toBe(true);
    for (const [index, first] of placements.entries()) {
      for (const second of placements.slice(index + 1)) {
        expect(placedOverlap(first, second)).toBe(false);
      }
    }
    expect(new Set(placements.map((placement) => placement.row)).size).toBe(1);
    expect(leftToRight(placements)).toEqual(["a", "b", "c"]);
  });

  test("a castle label is never moved and still hides a boat label that overlaps it", () => {
    const placements = byId(
      placeLabels([
        label("castle", 100, 100, { priority: 100 }),
        boat("a", 105, 102),
        boat("b", 112, 100),
      ]),
    );
    expect(placements.get("castle")).toEqual({
      id: "castle",
      visible: true,
      x: 100,
      y: 100,
    });
    // The row of a and b is centred on 108.5: a at 86.5 and b at 130.5. Both still overlap the
    // castle's label, so both are hidden.
    expect(placements.get("a")?.visible).toBe(false);
    expect(placements.get("b")?.visible).toBe(false);
    // A boat label that the row moves clear of the castle's label is shown.
    const clear = byId(
      placeLabels([
        label("castle", 100, 100, { priority: 100 }),
        boat("a", 130, 100),
        boat("b", 150, 100),
      ]),
    );
    expect(clear.get("castle")?.x).toBe(100);
    expect(clear.get("castle")?.visible).toBe(true);
    expect(clear.get("a")?.visible).toBe(false);
    expect(clear.get("b")?.visible).toBe(true);
  });

  test("boat labels that do not overlap stay at their anchors", () => {
    expect(placeLabels([boat("a", 100, 100), boat("b", 140, 100)])).toEqual([
      { id: "a", visible: true, x: 100, y: 100 },
      { id: "b", visible: true, x: 140, y: 100 },
    ]);
  });

  test("a loss label is not laid out with the boats", () => {
    expect(
      placeLabels([
        boat("boat", 100, 100),
        label("loss", 105, 100, { priority: 60 }),
      ]),
    ).toEqual([
      { id: "boat", visible: true, x: 100, y: 100 },
      { id: "loss", visible: false, x: 105, y: 100 },
    ]);
  });

  test("the order is stable when the anchors move by a pixel", () => {
    let requests = [boat("a", 100, 100), boat("b", 102, 106)];
    let placements = placeLabels(requests);
    expect(leftToRight(placements)).toEqual(["a", "b"]);
    // Frame by frame, the boats move a pixel each and cross each other's x, by up to the margin.
    const step = (ax: number, bx: number) => {
      requests = requests.map((request, index) =>
        carried(
          request,
          placements[index] as LabelPlacement,
          request.id === "a" ? ax : bx,
          request.y,
        ),
      );
      placements = placeLabels(requests);
    };
    for (const [ax, bx] of [
      [101, 101],
      [102, 100],
      [103, 99],
      [102, 100],
      [101, 101],
      [102, 101],
    ] as const) {
      step(ax, bx);
      expect(leftToRight(placements)).toEqual(["a", "b"]);
    }
    // Once they have crossed by more than the margin, the order follows them.
    step(102 + RETURN_MARGIN + 1, 102);
    expect(leftToRight(placements)).toEqual(["b", "a"]);
  });

  test("a label leaves its row only once its anchor is clear by the margin", () => {
    const first = placeLabels([boat("a", 100, 100), boat("b", 120, 100)]);
    expect(first[0]?.row).toBeDefined();
    const next = (bx: number) =>
      placeLabels([
        carried(boat("a", 100, 100), first[0] as LabelPlacement, 100, 100),
        carried(boat("b", 120, 100), first[1] as LabelPlacement, bx, 100),
      ]);
    // Clear, but not by the margin: still in the row.
    expect(next(140 + RETURN_MARGIN - 1)[1]?.row).toBe(first[0]?.row);
    // Clear by the margin: back at its anchor.
    expect(next(140 + RETURN_MARGIN)[1]).toEqual({
      id: "b",
      visible: true,
      x: 140 + RETURN_MARGIN,
      y: 100,
    });
    // Labels that were not in a row join one only when they overlap.
    expect(
      placeLabels([boat("a", 100, 100), boat("b", 139, 100)])[0]?.row,
    ).toBeDefined();
    expect(
      placeLabels([boat("a", 100, 100), boat("b", 140, 100)])[0]?.row,
    ).toBeUndefined();
  });

  test("a label leaving its row is already where its anchor is", () => {
    // Just inside the margin, the row puts both labels at their anchors.
    const placements = placeLabels([
      boat("a", 100, 100, { previousRow: "row" }),
      boat("b", 140 + ROW_GAP - 0.001, 100, { previousRow: "row" }),
    ]);
    expect(placements[0]?.row).toBeDefined();
    expect(placements[0]?.x).toBeCloseTo(100, 2);
    expect(placements[1]?.x).toBeCloseTo(140 + ROW_GAP, 2);
  });
});

describe("label offset easing", () => {
  test("moves most of the way within the fade time, and never past", () => {
    const eased = easeOffset(0, 40, FADE_MILLISECONDS);
    expect(eased).toBeGreaterThan(36);
    expect(eased).toBeLessThan(40);
    expect(easeOffset(0, 40, FADE_MILLISECONDS / 10)).toBeGreaterThan(0);
    expect(easeOffset(0, -40, FADE_MILLISECONDS)).toBeLessThan(-36);
  });

  test("settles exactly on the target", () => {
    expect(easeOffset(39.7, 40, 16)).toBe(40);
    expect(easeOffset(0, 40, 10_000)).toBe(40);
    expect(easeOffset(12, 12, 16)).toBe(12);
    expect(easeOffset(10, 40, -5)).toBe(10);
  });
});

describe("label fade", () => {
  test("fades in and out over the fade time", () => {
    expect(fadeAlpha(0, true, FADE_MILLISECONDS / 2)).toBeCloseTo(0.5);
    expect(fadeAlpha(1, false, FADE_MILLISECONDS / 2)).toBeCloseTo(0.5);
  });

  test("never leaves 0 to 1", () => {
    expect(fadeAlpha(0.9, true, 1000)).toBe(1);
    expect(fadeAlpha(0.1, false, 1000)).toBe(0);
    expect(fadeAlpha(0.5, true, -5)).toBe(0.5);
  });
});
