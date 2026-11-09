import { describe, expect, test } from "bun:test";
import {
  ATLAS_SWATCHES,
  type AtlasSwatch,
  heightAlong,
  swatchUv,
} from "./atlasUv";
import { ATLAS_SIZE, SWATCH_RECTS } from "./castleAtlas";

describe("atlas swatches for the board (#102)", () => {
  test("the team swatches are the ones castleAtlas.ts repaints", () => {
    expect(ATLAS_SWATCHES.stone).toEqual(SWATCH_RECTS.stone);
    expect(ATLAS_SWATCHES.flags).toEqual(SWATCH_RECTS.flags);
    expect(ATLAS_SWATCHES.accent).toEqual(SWATCH_RECTS.accent);
  });

  test("the swatches tile the atlas without overlapping", () => {
    const owner = new Map<string, string>();
    for (const [name, rect] of Object.entries(ATLAS_SWATCHES)) {
      for (let x = rect.x0; x <= rect.x1; x += 7) {
        for (let y = rect.y0; y <= rect.y1; y += 7) {
          const key = `${x},${y}`;
          expect(owner.get(key)).toBeUndefined();
          owner.set(key, name);
        }
      }
    }
  });

  test("a UV stays a pixel inside its swatch, at every point of the gradient", () => {
    for (const name of Object.keys(ATLAS_SWATCHES) as AtlasSwatch[]) {
      const rect = ATLAS_SWATCHES[name];
      for (const along of [-1, 0, 0.25, 0.5, 1, 2]) {
        const [u, v] = swatchUv(name, along);
        const x = u * ATLAS_SIZE;
        const y = v * ATLAS_SIZE;
        expect(x).toBeGreaterThan(rect.x0 + 1);
        expect(x).toBeLessThan(rect.x1);
        expect(y).toBeGreaterThanOrEqual(rect.y0 + 1);
        expect(y).toBeLessThanOrEqual(rect.y1);
      }
    }
  });

  test("0 is the swatch's top row and 1 its bottom row", () => {
    const [, top] = swatchUv("cliff", 0);
    const [, bottom] = swatchUv("cliff", 1);
    expect(top * ATLAS_SIZE).toBeCloseTo(ATLAS_SWATCHES.cliff.y0 + 1.5, 5);
    expect(bottom * ATLAS_SIZE).toBeCloseTo(ATLAS_SWATCHES.cliff.y1 - 0.5, 5);
  });

  test("a height maps linearly into a part of the gradient, clamped at its ends", () => {
    expect(heightAlong(0, 0, 0.2, 0.05, 0.85)).toBeCloseTo(0.05, 10);
    expect(heightAlong(0.2, 0, 0.2, 0.05, 0.85)).toBeCloseTo(0.85, 10);
    expect(heightAlong(0.1, 0, 0.2, 0.05, 0.85)).toBeCloseTo(0.45, 10);
    expect(heightAlong(-1, 0, 0.2, 0.05, 0.85)).toBeCloseTo(0.05, 10);
    expect(heightAlong(1, 0, 0.2, 0.05, 0.85)).toBeCloseTo(0.85, 10);
  });
});
