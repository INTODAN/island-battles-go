import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { DEEP_SEA_COLOR, FOAM_COLOR, SHALLOWS_COLOR } from "./art";
import { CASTLE_ATLAS_URL } from "./castleAssets";
import {
  contrastRatio,
  contrastTable,
  grassColour,
  hexToRgb,
  luminance,
} from "./contrast";
import { TEAM_COLORS } from "./palette";
import { decodePng } from "./testing/png";

const pixels = decodePng(readFileSync(CASTLE_ATLAS_URL)).pixels;

describe("contrast of team colours on the board (#102, measured, not enforced)", () => {
  test("WCAG's ratio: black on white is 21, a colour on itself is 1", () => {
    expect(contrastRatio([0, 0, 0], [255, 255, 255])).toBeCloseTo(21, 5);
    expect(contrastRatio([52, 100, 224], [52, 100, 224])).toBe(1);
  });

  test("the contrast table is computed for every team, roof and sail, on three backgrounds", () => {
    const rows = contrastTable(pixels);
    expect(rows).toHaveLength(TEAM_COLORS.length * 2 * 3);
    for (const row of rows) {
      expect(Number.isFinite(row.ratio)).toBe(true);
      expect(row.ratio).toBeGreaterThanOrEqual(1);
      expect(row.ratio).toBeLessThanOrEqual(21);
    }
    // Information only (#102): print the table, no minimum is enforced.
    if (process.env.CONTRAST_TABLE) {
      for (const row of rows) {
        console.log(
          `${row.team} ${row.thing} ${row.colour} on ${row.against} ${row.background}: ${row.ratio.toFixed(2)}`,
        );
      }
    }
  });

  test("the sea still reads as deep water with light shallows and white foam", () => {
    const deep = luminance(hexToRgb(DEEP_SEA_COLOR));
    const shallows = luminance(hexToRgb(SHALLOWS_COLOR));
    const foam = luminance(hexToRgb(FOAM_COLOR));
    expect(shallows).toBeGreaterThan(deep);
    expect(foam).toBeGreaterThan(shallows);
    // Blue-green, not grey: green and blue above red.
    const [red, green, blue] = hexToRgb(DEEP_SEA_COLOR);
    expect(green).toBeGreaterThan(red);
    expect(blue).toBeGreaterThan(red);
  });

  test("the grass top is green", () => {
    const [red, green, blue] = grassColour(pixels);
    expect(green).toBeGreaterThan(red);
    expect(green).toBeGreaterThan(blue);
  });
});
