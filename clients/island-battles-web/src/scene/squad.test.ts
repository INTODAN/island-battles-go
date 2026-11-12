import { describe, expect, test } from "bun:test";
import {
  MAX_SQUAD_SIZE,
  squadFormation,
  squadGeometry,
  squadSize,
} from "./art";

describe("squads (#211)", () => {
  test("a squad shows 1 soldier for 1 to 4, one more per doubling, and 5 at most", () => {
    const sizes = [1, 4, 5, 9, 10, 19, 20, 39, 40, 500].map(squadSize);
    expect(sizes).toEqual([1, 1, 2, 2, 3, 3, 4, 4, 5, 5]);
    expect(squadSize(0)).toBe(1);
  });

  test("each size has that many soldiers, a few dozen triangles each, standing apart", () => {
    for (let size = 1; size <= MAX_SQUAD_SIZE; size++) {
      const formation = squadFormation(size);
      expect(formation).toHaveLength(size);
      for (const [index, [x, z]] of formation.entries()) {
        for (const [otherX, otherZ] of formation.slice(index + 1)) {
          expect(Math.hypot(x - otherX, z - otherZ)).toBeGreaterThan(0.04);
        }
      }
      const { base, team } = squadGeometry(size);
      // Five boxes on castle-base and two on castle-team per soldier, 12 triangles a box.
      expect(base.triangleCount).toBe(size * 5 * 12);
      expect(team.triangleCount).toBe(size * 2 * 12);
    }
  });
});
