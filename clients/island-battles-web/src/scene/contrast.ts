import { DEEP_SEA_COLOR, SAIL_ALONG, SHALLOWS_COLOR } from "./art";
import { swatchUv } from "./atlasUv";
import {
  ATLAS_SIZE,
  gradientRow,
  SWATCH_RECTS,
  teamSwatches,
} from "./castleAtlas";
import { GRASS_ALONG, GRASS_TINT } from "./landforms";
import { TEAM_COLORS } from "./palette";

// The contrast of team colours against the board (issue #102): every team's roof colour (the
// stone/team swatch of `teamSwatches`) and boat sail against the deep sea, the shallows and the
// grass top, by WCAG's contrast ratio. The owner dropped the 3:1 minimum on #102 (the issue's
// shades give 1.3 to 2.4), so this only measures and reports; nothing enforces a threshold.

export type Rgb = readonly [number, number, number];

export function hexToRgb(hex: string): Rgb {
  const value = Number.parseInt(hex.slice(1), 16);
  return [(value >> 16) & 255, (value >> 8) & 255, value & 255];
}

export function rgbToHex([red, green, blue]: Rgb): string {
  return `#${[red, green, blue]
    .map((channel) => Math.round(channel).toString(16).padStart(2, "0"))
    .join("")}`;
}

/** WCAG relative luminance of an sRGB colour. */
export function luminance([red, green, blue]: Rgb): number {
  const linear = (channel: number) => {
    const c = channel / 255;
    return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * linear(red) + 0.7152 * linear(green) + 0.0722 * linear(blue);
}

/** WCAG contrast ratio, from 1 to 21. */
export function contrastRatio(left: Rgb, right: Rgb): number {
  const a = luminance(left);
  const b = luminance(right);
  return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05);
}

/** The atlas pixel a UV samples, from RGBA pixels with rows from the top. */
export function atlasPixel(
  pixels: ArrayLike<number>,
  uv: readonly [number, number],
): Rgb {
  const x = Math.min(ATLAS_SIZE - 1, Math.floor(uv[0] * ATLAS_SIZE));
  const y = Math.min(ATLAS_SIZE - 1, Math.floor(uv[1] * ATLAS_SIZE));
  const offset = (y * ATLAS_SIZE + x) * 4;
  return [
    pixels[offset] ?? 0,
    pixels[offset + 1] ?? 0,
    pixels[offset + 2] ?? 0,
  ];
}

/** The grass top's colour at its lightest: the atlas's green at GRASS_ALONG, times GRASS_TINT. */
export function grassColour(pixels: ArrayLike<number>): Rgb {
  const [red, green, blue] = atlasPixel(pixels, swatchUv("green", GRASS_ALONG));
  return [red * GRASS_TINT[0], green * GRASS_TINT[1], blue * GRASS_TINT[2]];
}

/** A team's colours on the board: its roofs and the two ends of its sails' gradient. */
export function teamBoardColours(hex: string): { roof: Rgb; sail: Rgb[] } {
  const stone = teamSwatches(hex).stone;
  if (!stone) throw new Error(`${hex} has no team stone swatch`);
  const rect = SWATCH_RECTS.stone;
  const sail = SAIL_ALONG.map((along) => {
    const [, v] = swatchUv("stone", along);
    const row = Math.floor(v * ATLAS_SIZE);
    return gradientRow(stone, rect, row) as Rgb;
  });
  return { roof: hexToRgb(stone.top), sail };
}

export interface ContrastRow {
  team: string;
  thing: "roof" | "sail";
  colour: string;
  against: "deep sea" | "shallows" | "grass top";
  background: string;
  ratio: number;
}

/** Every pair the issue asks about, with the sail's worse end. */
export function contrastTable(pixels: ArrayLike<number>): ContrastRow[] {
  const backgrounds: [ContrastRow["against"], Rgb][] = [
    ["deep sea", hexToRgb(DEEP_SEA_COLOR)],
    ["shallows", hexToRgb(SHALLOWS_COLOR)],
    ["grass top", grassColour(pixels)],
  ];
  const rows: ContrastRow[] = [];
  for (const team of TEAM_COLORS) {
    const { roof, sail } = teamBoardColours(team);
    for (const [against, background] of backgrounds) {
      rows.push({
        team,
        thing: "roof",
        colour: rgbToHex(roof),
        against,
        background: rgbToHex(background),
        ratio: contrastRatio(roof, background),
      });
      const worst = sail.reduce((low, colour) =>
        contrastRatio(colour, background) < contrastRatio(low, background)
          ? colour
          : low,
      );
      rows.push({
        team,
        thing: "sail",
        colour: rgbToHex(worst),
        against,
        background: rgbToHex(background),
        ratio: contrastRatio(worst, background),
      });
    }
  }
  return rows;
}
