import { describe, expect, test } from "bun:test";
import {
  FADE_MILLISECONDS,
  fadeAlpha,
  type LabelRequest,
  placeLabels,
  RETURN_MARGIN,
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

const visibleIds = (requests: LabelRequest[]) =>
  placeLabels(requests)
    .filter((placement) => placement.visible)
    .map((placement) => placement.id)
    .sort();

describe("label placement", () => {
  test("a lone label is shown", () => {
    expect(placeLabels([label("a", 100, 100)])).toEqual([
      { id: "a", visible: true },
    ]);
  });

  test("a placement carries no offset: a label is always drawn at its anchor", () => {
    for (const placement of placeLabels([
      label("a", 100, 100),
      label("b", 105, 100),
    ])) {
      expect(Object.keys(placement).sort()).toEqual(["id", "visible"]);
    }
  });

  test("of two overlapping labels the lower priority hides", () => {
    const placements = placeLabels([
      label("low", 100, 100, { priority: 30 }),
      label("high", 110, 105, { priority: 100 }),
    ]);
    expect(placements).toEqual([
      { id: "low", visible: false },
      { id: "high", visible: true },
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
