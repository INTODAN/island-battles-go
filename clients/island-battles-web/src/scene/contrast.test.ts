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

describe("contrast of team colours on the board (#102)", () => {
  test("WCAG's ratio: black on white is 21, a colour on itself is 1", () => {
    expect(contrastRatio([0, 0, 0], [255, 255, 255])).toBeCloseTo(21, 5);
    expect(contrastRatio([52, 100, 224], [52, 100, 224])).toBe(1);
  });

  test("every team's roof and sail has at least 3:1 against the deep sea, the shallows and the grass top", () => {
    const rows = contrastTable(pixels);
    // Six teams, roof and sail, three backgrounds.
    expect(rows).toHaveLength(TEAM_COLORS.length * 2 * 3);
    for (const row of rows) {
      if (row.ratio < 3) {
        throw new Error(
          `${row.team} ${row.thing} ${row.colour} on ${row.against} ${row.background}: ${row.ratio.toFixed(2)}:1`,
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
