import { describe, expect, test } from "bun:test";
import { toMap, toWorld } from "./coordinates";

describe("coordinate mapping", () => {
  test("one map unit is one world unit, and the map's y is world -z", () => {
    expect(toWorld({ x: 3, y: 2 })).toEqual({ x: 3, y: 0, z: -2 });
    expect(toWorld({ x: -1.5, y: -4 }, 0.5)).toEqual({ x: -1.5, y: 0.5, z: 4 });
  });

  test("distances are preserved", () => {
    const first = toWorld({ x: 0, y: 0 });
    const second = toWorld({ x: 3, y: 4 });
    expect(Math.hypot(second.x - first.x, second.z - first.z)).toBe(5);
  });

  test("a larger y is nearer a camera that looks along +z, as in the 2D replay", () => {
    expect(toWorld({ x: 0, y: 1 }).z).toBeLessThan(toWorld({ x: 0, y: -1 }).z);
  });

  test("toMap undoes toWorld", () => {
    const point = { x: 2.25, y: -7.5 };
    expect(toMap(toWorld(point))).toEqual(point);
  });
});
