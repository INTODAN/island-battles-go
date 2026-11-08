import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { NullEngine } from "@babylonjs/core/Engines/nullEngine";
import { Scene } from "@babylonjs/core/scene";
import {
  ATLAS_SIZE,
  baseAtlasMaterial,
  FLAG_GOLD,
  FLAG_GOLD_MIX,
  gradientRow,
  NEUTRAL_SWATCHES,
  paintTeamAtlas,
  SWATCH_RECTS,
  type SwatchName,
  setAtlasPixels,
  TEAM_STONE_BOTTOM,
  teamAtlas,
  teamSwatches,
} from "./castleAtlas";
import { mix, NEUTRAL_COLOR, TEAM_COLORS } from "./palette";
import { decodePng } from "./testing/png";

const atlas = decodePng(
  readFileSync(new URL("../../assets/castles/atlas.png", import.meta.url)),
);
const names = Object.keys(SWATCH_RECTS) as SwatchName[];

function pixel(pixels: Uint8Array, x: number, y: number): string {
  const offset = (y * ATLAS_SIZE + x) * 4;
  return `#${[0, 1, 2]
    .map((channel) =>
      (pixels[offset + channel] ?? 0).toString(16).padStart(2, "0"),
    )
    .join("")}`;
}

function inside(name: SwatchName, x: number, y: number): boolean {
  const rect = SWATCH_RECTS[name];
  return x >= rect.x0 && x <= rect.x1 && y >= rect.y0 && y <= rect.y1;
}

describe("teamSwatches", () => {
  test("a team colour runs to Unity's dark stone, a gold-tipped flag and a dark accent", () => {
    const blue = TEAM_COLORS[0];
    expect(teamSwatches(blue)).toEqual({
      stone: { top: blue, bottom: TEAM_STONE_BOTTOM },
      flags: { top: blue, bottom: mix(blue, FLAG_GOLD, FLAG_GOLD_MIX) },
      accent: {
        top: mix(blue, "#000000", 0.4),
        bottom: mix(blue, "#000000", 0.78),
      },
    });
    expect(TEAM_STONE_BOTTOM).toBe("#46424a");
    expect(FLAG_GOLD).toBe("#e6d23c");
    expect(FLAG_GOLD_MIX).toBe(0.35);
    // #3464e0 mixed 35% toward #e6d23c, and times 0.6 and 0.22.
    expect(teamSwatches(blue).flags?.bottom).toBe("#728ba7");
    expect(teamSwatches(blue).accent).toEqual({
      top: "#1f3c86",
      bottom: "#0b1631",
    });
  });

  test("neutral is Unity's grey copy: stone unchanged, grey flags and accent", () => {
    expect(teamSwatches(NEUTRAL_COLOR)).toBe(NEUTRAL_SWATCHES);
    expect(NEUTRAL_SWATCHES).toEqual({
      stone: null,
      flags: { top: "#212021", bottom: "#9c9c9c" },
      accent: { top: "#8b8e90", bottom: "#3b3a32" },
    });
  });
});

describe("the team atlas pixels", () => {
  test("the committed atlas is Unity's 512 by 512 base atlas", () => {
    expect(atlas.width).toBe(ATLAS_SIZE);
    expect(atlas.height).toBe(ATLAS_SIZE);
    // Spot checks against basic_mat_color.png at c8b43b3: stone, flag and accent swatches.
    expect(pixel(atlas.pixels, 127, 0)).toBe("#bcc9d6");
    expect(pixel(atlas.pixels, 265, 300)).toBe("#b24e42");
  });

  for (const hex of [...TEAM_COLORS, NEUTRAL_COLOR]) {
    test(`${hex}: base pixels outside the swatches, gradients inside`, () => {
      const swatches = teamSwatches(hex);
      const painted = paintTeamAtlas(atlas.pixels, swatches);
      let outsideDifferences = 0;
      for (let y = 0; y < ATLAS_SIZE; y++) {
        for (let x = 0; x < ATLAS_SIZE; x++) {
          const swatch = names.find((name) => inside(name, x, y));
          if (swatch && swatches[swatch]) continue;
          for (let channel = 0; channel < 4; channel++) {
            const offset = (y * ATLAS_SIZE + x) * 4 + channel;
            if (painted[offset] !== atlas.pixels[offset]) outsideDifferences++;
          }
        }
      }
      expect(outsideDifferences).toBe(0);
      for (const name of names) {
        const gradient = swatches[name];
        if (!gradient) continue;
        const rect = SWATCH_RECTS[name];
        for (let x = rect.x0; x <= rect.x1; x++) {
          expect(pixel(painted, x, rect.y0)).toBe(gradient.top);
          expect(pixel(painted, x, rect.y1)).toBe(gradient.bottom);
        }
        // Rows in between run evenly from top to bottom.
        const middle = Math.round((rect.y0 + rect.y1) / 2);
        const [red, green, blue] = gradientRow(gradient, rect, middle);
        const offset = (middle * ATLAS_SIZE + rect.x0) * 4;
        expect([...painted.subarray(offset, offset + 3)]).toEqual([
          red,
          green,
          blue,
        ]);
      }
    });
  }

  test("painting leaves the base pixels as they were", () => {
    const before = new Uint8Array(atlas.pixels);
    paintTeamAtlas(atlas.pixels, teamSwatches(TEAM_COLORS[1]));
    expect(atlas.pixels).toEqual(before);
  });
});

describe("teamAtlas", () => {
  test("one cached material per team colour, wrapping past the sixth team", () => {
    const scene = new Scene(new NullEngine());
    setAtlasPixels(scene, atlas.pixels);
    const base = baseAtlasMaterial(scene);
    expect(base.name).toBe("castle-base");
    expect(baseAtlasMaterial(scene)).toBe(base);
    const blue = teamAtlas(scene, 1);
    expect(blue.name).toBe("castle-team-3464e0");
    expect(teamAtlas(scene, 1)).toBe(blue);
    expect(teamAtlas(scene, 7)).toBe(blue);
    expect(teamAtlas(scene, 0).name).toBe(
      `castle-team-${NEUTRAL_COLOR.slice(1)}`,
    );
    const materials = new Set(
      [0, 1, 2, 3, 4, 5, 6, 7, 8].map((team) => teamAtlas(scene, team)),
    );
    expect(materials.size).toBe(7);
    expect(materials.has(base)).toBe(false);
    scene.getEngine().dispose();
  });
});
