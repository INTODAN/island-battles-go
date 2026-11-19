import { describe, expect, test } from "bun:test";
import { staleLabel } from "./battlefield";
import { dragLineSegment } from "./dragLine";
import { shorePoint } from "./shore";

const origin = { id: 1, x: 0, y: 0 };
const target = { id: 2, x: 10, y: 0 };
const between = { id: 3, x: 5, y: 0.1 };
const aside = { id: 4, x: 5, y: 6 };

const near = (a: { x: number; y: number }, b: { x: number; y: number }) =>
  Math.hypot(a.x - b.x, a.y - b.y) < 1e-9;

describe("the drag's line", () => {
  test("it starts at the origin's shore and ends at the pointer", () => {
    const end = { x: 6, y: 3 };
    const segment = dragLineSegment(origin, end, null, [origin, aside]);
    expect(segment).not.toBeNull();
    expect(near(segment?.start ?? end, shorePoint(origin, end))).toBe(true);
    expect(segment?.tip).toEqual(end);
    expect(segment?.arrowhead).toBe(true);
  });

  test("over a castle it snaps to that castle's shore, where a boat lands", () => {
    const segment = dragLineSegment(origin, { x: 9.8, y: 0.3 }, target, [
      origin,
      target,
    ]);
    expect(near(segment?.tip ?? origin, shorePoint(target, origin))).toBe(true);
    // The arrowhead's base is behind its tip, towards the origin.
    expect(segment?.base.x).toBeLessThan(segment?.tip.x ?? 0);
  });

  test("it leaves a gap where it passes another castle", () => {
    const segment = dragLineSegment(origin, { x: 10, y: 0 }, target, [
      origin,
      target,
      between,
    ]);
    expect(segment?.gaps.length).toBe(1);
    const far = dragLineSegment(origin, { x: 10, y: 0 }, target, [
      origin,
      target,
      aside,
    ]);
    expect(far?.gaps).toEqual([]);
  });

  test("with the pointer still over the origin's island there is no line", () => {
    expect(
      dragLineSegment(origin, { x: 0.05, y: 0 }, null, [origin]),
    ).toBeNull();
  });

  test("the chip's label is never taken for a gone castle's", () => {
    expect(staleLabel("drag:amount", new Set(), new Set())).toBe(false);
  });
});
