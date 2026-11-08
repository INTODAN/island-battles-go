import { describe, expect, test } from "bun:test";
import { distanceField, sampleField } from "./seaDistance";

const square = [
  [-1, -1],
  [1, -1],
  [1, 1],
  [-1, 1],
] as const;

describe("the sea's distance to the nearest island (#102)", () => {
  const field = distanceField([square], 1, 20);

  test("zero on the island, growing with the distance from its shore", () => {
    expect(sampleField(field, 0, 0)).toBe(0);
    expect(sampleField(field, 1.3, 0)).toBeCloseTo(0.3, 1);
    expect(sampleField(field, 0, -1.55)).toBeCloseTo(0.55, 1);
    // Past a corner, the distance is to the corner.
    expect(sampleField(field, 1.4, 1.4)).toBeCloseTo(Math.hypot(0.4, 0.4), 1);
  });

  test("open sea past the maximum distance, and at every edge texel", () => {
    expect(sampleField(field, 2.5, 0)).toBe(1);
    for (let column = 0; column < field.columns; column++) {
      expect(field.values[column]).toBe(1);
      expect(field.values[(field.rows - 1) * field.columns + column]).toBe(1);
    }
  });

  test("the nearest of several islands counts", () => {
    const shifted = square.map(([x, z]) => [x + 3, z] as const);
    const two = distanceField([square, shifted], 1, 20);
    expect(sampleField(two, 1.5, 0)).toBeCloseTo(0.5, 1);
    expect(sampleField(two, 1.9, 0)).toBeCloseTo(0.1, 1);
  });

  test("no islands give an open sea", () => {
    const empty = distanceField([], 1, 4);
    expect([...empty.values].every((value) => value === 1)).toBe(true);
  });
});
