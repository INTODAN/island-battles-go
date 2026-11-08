import { StandardMaterial } from "@babylonjs/core/Materials/standardMaterial";
import { RawTexture } from "@babylonjs/core/Materials/Textures/rawTexture";
import { Texture } from "@babylonjs/core/Materials/Textures/texture";
import { Color3 } from "@babylonjs/core/Maths/math.color";
import type { Scene } from "@babylonjs/core/scene";
import { mix, NEUTRAL_COLOR, teamColorHex } from "./palette";

// The castle atlas and its team copies (issue #140). Every castle mesh samples one 512 by 512
// atlas of vertical-gradient swatches (assets/castles/atlas.png, Unity's basic_mat_color.png).
// Unity shipped a copy of it per team that differs in three swatches only; here those copies are
// made at runtime from the team palette, so every team of palette.ts gets one. #102 reuses the
// same two materials for boats and props, so this module knows nothing about castles.

/** The atlas is this many pixels wide and high. */
export const ATLAS_SIZE = 512;

/** A pixel rectangle of the atlas, inclusive at both ends; y 0 is the top row. */
export interface SwatchRect {
  x0: number;
  x1: number;
  y0: number;
  y1: number;
}

/** The three swatches the team copies change, at the pixels Unity's copies differ in. */
export const SWATCH_RECTS = {
  /** Light grey stone on base faces; roofs, pennants and bands on team faces. */
  stone: { x0: 122, x1: 264, y0: 0, y1: 255 },
  /** The big flags' cloth and the blades' sails. */
  flags: { x0: 122, x1: 264, y0: 256, y1: 511 },
  accent: { x0: 265, x1: 366, y0: 256, y1: 511 },
} as const satisfies Record<string, SwatchRect>;

export type SwatchName = keyof typeof SWATCH_RECTS;

/** A vertical gradient: `top` on the swatch's first row, `bottom` on its last. */
export interface Gradient {
  top: string;
  bottom: string;
}

/** What a team copy paints into each swatch; `null` keeps the base atlas's pixels. */
export type TeamSwatches = Record<SwatchName, Gradient | null>;

/** The stone swatch's dark end on team faces, as in Unity's red and blue copies. */
export const TEAM_STONE_BOTTOM = "#46424a";
/** The warm tip of a team's flags. */
export const FLAG_GOLD = "#e6d23c";
/**
 * How far a flag's lower end is mixed toward `FLAG_GOLD`. Unity's flags ran from the team colour
 * all the way to gold, so half the flag read as gold; the owner chose team-coloured flags with a
 * warm tip instead. Tune it here from screenshots.
 */
export const FLAG_GOLD_MIX = 0.35;
/** The accent swatch runs from the team colour times this... */
export const ACCENT_TOP_SCALE = 0.6;
/** ...to the team colour times this, as in Unity's copies. */
export const ACCENT_BOTTOM_SCALE = 0.22;

/**
 * Neutral castles use Unity's grey copy (basic_mat_color_gray_castle.png) exactly: the stone
 * swatch is the base atlas's own, and the flags and accent are its grey gradients.
 */
export const NEUTRAL_SWATCHES: TeamSwatches = {
  stone: null,
  flags: { top: "#212021", bottom: "#9c9c9c" },
  accent: { top: "#8b8e90", bottom: "#3b3a32" },
};

function channels(hex: string): [number, number, number] {
  const value = Number.parseInt(hex.slice(1), 16);
  return [(value >> 16) & 255, (value >> 8) & 255, value & 255];
}

function scaled(hex: string, factor: number): string {
  return mix(hex, "#000000", 1 - factor);
}

/** The three gradients of a team colour's atlas copy. The neutral colour gets Unity's grey copy. */
export function teamSwatches(hex: string): TeamSwatches {
  if (hex.toLowerCase() === NEUTRAL_COLOR) return NEUTRAL_SWATCHES;
  return {
    stone: { top: hex, bottom: TEAM_STONE_BOTTOM },
    flags: { top: hex, bottom: mix(hex, FLAG_GOLD, FLAG_GOLD_MIX) },
    accent: {
      top: scaled(hex, ACCENT_TOP_SCALE),
      bottom: scaled(hex, ACCENT_BOTTOM_SCALE),
    },
  };
}

/** The colour of row `y` of a gradient that fills `rect`, as RGB bytes. */
export function gradientRow(
  gradient: Gradient,
  rect: SwatchRect,
  y: number,
): [number, number, number] {
  const top = channels(gradient.top);
  const bottom = channels(gradient.bottom);
  const t = (y - rect.y0) / (rect.y1 - rect.y0);
  return [0, 1, 2].map((index) =>
    Math.round(
      (top[index] ?? 0) + ((bottom[index] ?? 0) - (top[index] ?? 0)) * t,
    ),
  ) as [number, number, number];
}

/**
 * A team copy of the atlas: the base atlas's RGBA pixels (rows from the top) with each swatch of
 * `swatches` refilled with its vertical gradient. The input is left as it is.
 */
export function paintTeamAtlas(
  base: Uint8Array | Uint8ClampedArray,
  swatches: TeamSwatches,
  width = ATLAS_SIZE,
): Uint8Array {
  const pixels = new Uint8Array(base);
  for (const name of Object.keys(SWATCH_RECTS) as SwatchName[]) {
    const gradient = swatches[name];
    if (!gradient) continue;
    const rect = SWATCH_RECTS[name];
    for (let y = rect.y0; y <= rect.y1; y++) {
      const [red, green, blue] = gradientRow(gradient, rect, y);
      for (let x = rect.x0; x <= rect.x1; x++) {
        const offset = (y * width + x) * 4;
        pixels[offset] = red;
        pixels[offset + 1] = green;
        pixels[offset + 2] = blue;
        pixels[offset + 3] = 255;
      }
    }
  }
  return pixels;
}

/**
 * Decodes the atlas PNG at `url` to RGBA bytes, rows from the top, with no colour management:
 * Unity used the raw values, and the copied atlas carries no colour profile.
 */
export async function loadAtlasPixels(url: string): Promise<Uint8ClampedArray> {
  const response = await fetch(url);
  if (!response.ok) throw new Error(`castle atlas: HTTP ${response.status}`);
  const bitmap = await createImageBitmap(await response.blob(), {
    colorSpaceConversion: "none",
    premultiplyAlpha: "none",
  });
  const canvas = new OffscreenCanvas(bitmap.width, bitmap.height);
  const context = canvas.getContext("2d");
  if (!context) throw new Error("castle atlas: no 2D canvas");
  context.drawImage(bitmap, 0, 0);
  bitmap.close();
  return context.getImageData(0, 0, canvas.width, canvas.height).data;
}

interface AtlasCache {
  base: StandardMaterial;
  teams: Map<string, StandardMaterial>;
}

const caches = new WeakMap<Scene, AtlasCache>();
const pixelsByScene = new WeakMap<Scene, Uint8Array | Uint8ClampedArray>();

/** Gives a scene the base atlas's pixels; `teamAtlas` and `baseAtlasMaterial` need them first. */
export function setAtlasPixels(
  scene: Scene,
  pixels: Uint8Array | Uint8ClampedArray,
): void {
  if (pixels.length !== ATLAS_SIZE * ATLAS_SIZE * 4) {
    throw new Error(`castle atlas: expected ${ATLAS_SIZE}² RGBA pixels`);
  }
  pixelsByScene.set(scene, pixels);
}

export function hasAtlasPixels(scene: Scene): boolean {
  return pixelsByScene.has(scene);
}

function atlasTexture(
  scene: Scene,
  name: string,
  pixels: Uint8Array,
): RawTexture {
  // Rows from the top with invertY off: v = 0 is the image's top row, as glTF's UVs expect.
  const texture = RawTexture.CreateRGBATexture(
    pixels,
    ATLAS_SIZE,
    ATLAS_SIZE,
    scene,
    true,
    false,
    Texture.TRILINEAR_SAMPLINGMODE,
  );
  texture.name = name;
  texture.wrapU = Texture.CLAMP_ADDRESSMODE;
  texture.wrapV = Texture.CLAMP_ADDRESSMODE;
  return texture;
}

function atlasMaterial(
  scene: Scene,
  name: string,
  pixels: Uint8Array,
): StandardMaterial {
  const material = new StandardMaterial(name, scene);
  const texture = atlasTexture(scene, name, pixels);
  material.diffuseTexture = texture;
  // A little of the atlas glows on its own, so shaded walls keep their stone colour.
  material.emissiveTexture = texture;
  material.emissiveColor = new Color3(0.28, 0.28, 0.28);
  material.specularColor = Color3.Black();
  return material;
}

function cacheOf(scene: Scene): AtlasCache {
  let cache = caches.get(scene);
  if (cache) return cache;
  const pixels = pixelsByScene.get(scene);
  if (!pixels) throw new Error("castle atlas: no pixels for this scene yet");
  cache = {
    base: atlasMaterial(scene, "castle-base", new Uint8Array(pixels)),
    teams: new Map(),
  };
  caches.set(scene, cache);
  return cache;
}

/** The `castle-base` material: the base atlas, one material shared by every castle. */
export function baseAtlasMaterial(scene: Scene): StandardMaterial {
  return cacheOf(scene).base;
}

/**
 * The `castle-team` material of a team (team_color; 0 is neutral): the base atlas with the team's
 * swatches. One texture and one material per team colour, cached. Teams past the sixth wrap, as
 * `teamColorHex` does, so they share a material with the team of the same colour.
 */
export function teamAtlas(scene: Scene, team: number): StandardMaterial {
  const cache = cacheOf(scene);
  const hex = teamColorHex(team);
  let material = cache.teams.get(hex);
  if (!material) {
    const pixels = pixelsByScene.get(scene) as Uint8Array;
    material = atlasMaterial(
      scene,
      `castle-team-${hex.slice(1)}`,
      paintTeamAtlas(pixels, teamSwatches(hex)),
    );
    cache.teams.set(hex, material);
  }
  return material;
}
