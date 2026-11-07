import { describe, expect, test } from "bun:test";
import { boardBounds } from "./board";
import { CAMERA, fitRadius } from "./camera";

describe("board fitting", () => {
  test("keeps at least the generated map's 8 by 6 units", () => {
    expect(boardBounds([])).toEqual({ halfWidth: 4, halfDepth: 3 });
    expect(
      boardBounds([
        { x: 2, y: 0 },
        { x: -2, y: 0 },
      ]),
    ).toEqual({
      halfWidth: 4,
      halfDepth: 3,
    });
  });

  test("grows to give each castle a unit of room, as the replay does", () => {
    expect(
      boardBounds([
        { x: -6.5, y: 1 },
        { x: 3, y: -5 },
      ]),
    ).toEqual({
      halfWidth: 7.5,
      halfDepth: 6,
    });
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
