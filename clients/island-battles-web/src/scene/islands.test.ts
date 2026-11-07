import { describe, expect, test } from "bun:test";
import {
  hashSeed,
  ISLAND_SIDES,
  islandRadii,
  landmasses,
  landmassLinks,
  seededRandom,
} from "./islands";

describe("island seeding", () => {
  test("a castle ID always gives the same shape", () => {
    expect(islandRadii(7)).toEqual(islandRadii(7));
    expect(islandRadii(7)).toHaveLength(ISLAND_SIDES);
  });

  test("different castles get different shapes", () => {
    const shapes = new Set(
      Array.from({ length: 12 }, (_, id) => islandRadii(id).join(",")),
    );
    expect(shapes.size).toBe(12);
  });

  test("radii stay within a gentle range, so islands never collapse or spike", () => {
    for (let id = 0; id < 50; id++) {
      for (const radius of islandRadii(id)) {
        expect(radius).toBeGreaterThanOrEqual(0.75);
        expect(radius).toBeLessThanOrEqual(1.25);
      }
    }
  });

  test("the seeded generator repeats and stays in [0, 1)", () => {
    const first = seededRandom(42);
    const second = seededRandom(42);
    for (let step = 0; step < 100; step++) {
      const value = first();
      expect(value).toBe(second());
      expect(value).toBeGreaterThanOrEqual(0);
      expect(value).toBeLessThan(1);
    }
  });

  test("a pair seeds differently in each order", () => {
    expect(hashSeed(1, 2)).not.toBe(hashSeed(2, 1));
  });
});

describe("shared landmasses", () => {
  const castles = [
    { id: 0, x: 0, y: 0 },
    { id: 1, x: 1.2, y: 0 },
    { id: 2, x: 1.2, y: 1.4 },
    { id: 3, x: 6, y: 0 },
    { id: 4, x: 7.5, y: 0 },
  ];

  test("castles closer than 1.5 units are linked", () => {
    expect(landmassLinks(castles)).toEqual([
      [0, 1],
      [1, 2],
    ]);
  });

  test("exactly 1.5 units apart is not close enough", () => {
    expect(landmassLinks([castles[3] as never, castles[4] as never])).toEqual(
      [],
    );
  });

  test("links chain into one landmass", () => {
    expect(landmasses(castles)).toEqual([[0, 1, 2], [3], [4]]);
  });

  test("the grouping does not depend on the castles' order", () => {
    expect(landmasses([...castles].reverse())).toEqual(landmasses(castles));
  });
});
