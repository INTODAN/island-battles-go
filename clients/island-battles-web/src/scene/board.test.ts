import { describe, expect, test } from "bun:test";
import { boardBounds } from "./board";
import {
  CAMERA,
  farPlane,
  fitRadius,
  followRadius,
  MIN_FAR_PLANE,
} from "./camera";
import { WORLD_SCALE } from "./coordinates";

describe("board fitting", () => {
  test("keeps at least 8 by 6 map units, in world units", () => {
    const minimum = {
      halfWidth: 4 * WORLD_SCALE,
      halfDepth: 3 * WORLD_SCALE,
    };
    expect(boardBounds([])).toEqual(minimum);
    expect(
      boardBounds([
        { x: 2, y: 0 },
        { x: -2, y: 0 },
      ]),
    ).toEqual(minimum);
  });

  test("grows to give each castle a world unit of room", () => {
    const bounds = boardBounds([
      { x: -6.5, y: 1 },
      { x: 3, y: -5 },
    ]);
    expect(bounds.halfWidth).toBeCloseTo(6.5 * WORLD_SCALE + 1, 9);
    expect(bounds.halfDepth).toBeCloseTo(5 * WORLD_SCALE + 1, 9);
  });

  test("a bigger board needs a bigger camera distance", () => {
    const small = fitRadius({ halfWidth: 4, halfDepth: 3 }, 1.6);
    const wide = fitRadius({ halfWidth: 12, halfDepth: 3 }, 1.6);
    const deep = fitRadius({ halfWidth: 4, halfDepth: 12 }, 1.6);
    expect(wide).toBeGreaterThan(small);
    expect(deep).toBeGreaterThan(small);
  });

  test("a narrow window sits further back than a wide one", () => {
    const bounds = { halfWidth: 5, halfDepth: 4 };
    expect(fitRadius(bounds, 0.56)).toBeGreaterThan(fitRadius(bounds, 1.78));
  });

  test("the fitted distance puts the board's corners inside the view", () => {
    const bounds = { halfWidth: 5, halfDepth: 4 };
    const aspect = 1.6;
    const radius = fitRadius(bounds, aspect);
    // Project the near and far corners by hand: the camera looks at the origin from the south.
    const eyeY = radius * Math.cos(CAMERA.beta);
    const eyeZ = -radius * Math.sin(CAMERA.beta);
    const forward = [0, -eyeY / radius, -eyeZ / radius] as const;
    const up = [0, -forward[2], forward[1]] as const;
    for (const [x, z] of [
      [-5, -4],
      [5, -4],
      [-5, 4],
      [5, 4],
    ] as const) {
      const offset = [x - 0, 0 - eyeY, z - eyeZ] as const;
      const depth = offset[1] * forward[1] + offset[2] * forward[2];
      const vertical = (offset[1] * up[1] + offset[2] * up[2]) / depth;
      const horizontal = offset[0] / depth;
      expect(Math.abs(vertical)).toBeLessThanOrEqual(
        Math.tan(CAMERA.fov / 2) + 1e-6,
      );
      expect(Math.abs(horizontal)).toBeLessThanOrEqual(
        Math.tan(CAMERA.fov / 2) * aspect + 1e-6,
      );
    }
  });

  test("the tilt limits keep the camera above the sea and off straight down", () => {
    expect(CAMERA.minBeta).toBeGreaterThan(0);
    expect(CAMERA.maxBeta).toBeLessThan(Math.PI / 2);
    expect(CAMERA.beta).toBeGreaterThan(CAMERA.minBeta);
    expect(CAMERA.beta).toBeLessThan(CAMERA.maxBeta);
  });
});

describe("following a board that changes size (the map builder's preview)", () => {
  const angle = { alpha: CAMERA.alpha, beta: CAMERA.beta, fov: CAMERA.fov };
  const small = boardBounds([{ x: 3, y: 0 }]);
  const large = boardBounds([{ x: 30, y: 20 }]);

  test("keeps the zoom while the board still fits", () => {
    const radius = fitRadius(large, 1.5) * 1.2;
    expect(followRadius(radius, small, 1.5, angle)).toBe(radius);
  });

  test("draws far enough for the sea around the largest board", () => {
    // A game's board keeps the usual far plane.
    expect(farPlane(fitRadius(small, 1.5) * CAMERA.maxRadiusFactor)).toBe(
      MIN_FAR_PLANE,
    );
    // The preview's largest board: castles 100 map units out.
    const largest = boardBounds([{ x: 100, y: 100 }]);
    const upper = fitRadius(largest, 0.7) * CAMERA.maxRadiusFactor;
    expect(farPlane(upper)).toBeGreaterThan(upper + 200);
  });

  test("moves back as far as a board that grew past the view needs, at the camera's angle", () => {
    const radius = fitRadius(small, 1.5);
    const turned = { ...angle, alpha: angle.alpha + 1, beta: angle.beta - 0.3 };
    const needed = followRadius(radius, large, 1.5, turned);
    expect(needed).toBeGreaterThan(radius);
    expect(needed).toBe(
      fitRadius(large, 1.5, turned.fov, turned.beta, turned.alpha),
    );
  });
});
