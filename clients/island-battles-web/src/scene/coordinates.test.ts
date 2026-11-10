import { describe, expect, test } from "bun:test";
import {
  toMap,
  toMapDistance,
  toWorld,
  toWorldDistance,
  WORLD_SCALE,
} from "./coordinates";

describe("coordinate mapping", () => {
  test("one map unit is WORLD_SCALE world units, and the map's y is world -z", () => {
    expect(toWorld({ x: 3, y: 2 })).toEqual({
      x: 3 * WORLD_SCALE,
      y: 0,
      z: -2 * WORLD_SCALE,
    });
    expect(toWorld({ x: -1.5, y: -4 }, 0.5)).toEqual({
      x: -1.5 * WORLD_SCALE,
      y: 0.5,
      z: 4 * WORLD_SCALE,
    });
  });

  test("distances are multiplied by WORLD_SCALE", () => {
    const first = toWorld({ x: 0, y: 0 });
    const second = toWorld({ x: 3, y: 4 });
    expect(Math.hypot(second.x - first.x, second.z - first.z)).toBeCloseTo(
      5 * WORLD_SCALE,
      9,
    );
  });

  test("two castles 2.0 map units apart are 2.0 x WORLD_SCALE world units apart", () => {
    const first = toWorld({ x: 2, y: 0 });
    const second = toWorld({ x: 1, y: Math.sqrt(3) });
    expect(Math.hypot(second.x - first.x, second.z - first.z)).toBeCloseTo(
      2 * WORLD_SCALE,
      9,
    );
    expect(toWorldDistance(2)).toBeCloseTo(2 * WORLD_SCALE, 9);
  });

  test("the distance helpers undo each other", () => {
    expect(toMapDistance(toWorldDistance(0.37))).toBeCloseTo(0.37, 12);
    expect(toWorldDistance(toMapDistance(0.5))).toBeCloseTo(0.5, 12);
  });

  test("a larger y is nearer a camera that looks along +z, as in the 2D replay", () => {
    expect(toWorld({ x: 0, y: 1 }).z).toBeLessThan(toWorld({ x: 0, y: -1 }).z);
  });

  test("toMap undoes toWorld", () => {
    const point = { x: 2.25, y: -7.5 };
    const back = toMap(toWorld(point));
    expect(back.x).toBeCloseTo(point.x, 12);
    expect(back.y).toBeCloseTo(point.y, 12);
  });
});
