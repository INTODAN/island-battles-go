import { afterAll, beforeAll, describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { NullEngine } from "@babylonjs/core/Engines/nullEngine";
import { Color4 } from "@babylonjs/core/Maths/math.color";
import { Vector3 } from "@babylonjs/core/Maths/math.vector";
import { Scene } from "@babylonjs/core/scene";
import { createTurnEffects } from "./animation/effects";
import {
  BOAT_FLOAT,
  boatGeometry,
  createBoat,
  createIsland,
  createPropMeshes,
  disposeBoat,
  MaterialCache,
  placeProps,
  SAIL_ALONG,
  setBoatTeam,
} from "./art";
import { ATLAS_SWATCHES } from "./atlasUv";
import { CASTLE_ATLAS_URL } from "./castleAssets";
import {
  ATLAS_SIZE,
  baseAtlasMaterial,
  setAtlasPixels,
  teamAtlas,
} from "./castleAtlas";
import { islandProps } from "./landforms";
import { Bursts } from "./particles";
import { decodePng } from "./testing/png";

let engine: NullEngine;
let scene: Scene;
let materials: MaterialCache;

beforeAll(() => {
  engine = new NullEngine();
  scene = new Scene(engine);
  materials = new MaterialCache(scene);
});

afterAll(() => {
  scene.dispose();
  engine.dispose();
});

function swatchesOf(uvs: readonly number[]): Set<string> {
  const found = new Set<string>();
  for (let index = 0; index < uvs.length; index += 2) {
    const x = (uvs[index] ?? 0) * ATLAS_SIZE;
    const y = (uvs[index + 1] ?? 0) * ATLAS_SIZE;
    for (const [name, rect] of Object.entries(ATLAS_SWATCHES)) {
      if (x >= rect.x0 && x <= rect.x1 && y >= rect.y0 && y <= rect.y1)
        found.add(name);
    }
  }
  return found;
}

describe("boats and props on the castle atlas (#102)", () => {
  test("a hull of wood, and a sail and pennant on the team's swatches", () => {
    const { hull, sail } = boatGeometry();
    expect(swatchesOf(hull.uvs)).toEqual(new Set(["wood"]));
    expect(swatchesOf(sail.uvs)).toEqual(new Set(["stone", "flags"]));
    // The sail samples the top of the stone swatch: the team colour itself.
    const stone = ATLAS_SWATCHES.stone;
    for (let index = 0; index < sail.uvs.length; index += 2) {
      const x = (sail.uvs[index] ?? 0) * ATLAS_SIZE;
      const y = (sail.uvs[index + 1] ?? 0) * ATLAS_SIZE;
      // The flags swatch sits under the stone swatch, in the same columns.
      if (x < stone.x0 || x > stone.x1 || y > stone.y1) continue;
      expect((y - stone.y0) / (stone.y1 - stone.y0)).toBeLessThanOrEqual(
        SAIL_ALONG[1] + 0.01,
      );
    }
  });

  test("before the atlas arrives a boat has stand-in colours; then castle-base and castle-team", () => {
    const boat = createBoat(scene, 1, materials, 2);
    const island = createIsland(scene, 1, materials);
    expect(boat.hull[0]?.material?.name).toStartWith("lit:");
    setAtlasPixels(scene, decodePng(readFileSync(CASTLE_ATLAS_URL)).pixels);
    expect(boat.hull[0]?.material).toBe(baseAtlasMaterial(scene));
    expect(boat.sail[0]?.material).toBe(teamAtlas(scene, 2));
    expect(island.material).toBe(baseAtlasMaterial(scene));
    // A capture recolours only the sail, as it does a castle's team faces.
    setBoatTeam(boat, 5, materials);
    expect(boat.sail[0]?.material).toBe(teamAtlas(scene, 5));
    expect(boat.hull[0]?.material).toBe(baseAtlasMaterial(scene));
    disposeBoat(boat);
    expect(boat.root.isDisposed()).toBe(true);
    expect(boat.wake.isDisposed()).toBe(true);
  });

  test("props are thin instances: one mesh per kind however many there are", () => {
    const meshes = createPropMeshes(scene, materials);
    const castles = [
      { id: 1, x: 0, y: 0 },
      { id: 2, x: 4, y: 0 },
      { id: 3, x: 0, y: 4 },
    ];
    const props = islandProps(castles);
    placeProps(meshes, props);
    const trees = props.filter((prop) => prop.kind === "tree").length;
    const rocks = props.filter((prop) => prop.kind === "rock").length;
    expect(meshes.tree.thinInstanceCount).toBe(trees);
    expect(meshes.rock.thinInstanceCount).toBe(rocks);
  });
});

describe("turn effects (#102)", () => {
  test("a sinking boat tilts, slides under and is put back when the turn is cleared", async () => {
    const boat = createBoat(scene, 7, materials, 1);
    const effects = createTurnEffects(scene);
    // The particle code loads on its own.
    await import("./particles");
    await new Promise((resolve) => setTimeout(resolve, 0));
    effects.sink(boat, 0.5);
    expect(boat.body.position.y).toBeLessThan(BOAT_FLOAT);
    expect(boat.body.position.z).toBeGreaterThan(0);
    expect(boat.body.rotation.x).toBeLessThan(0);
    expect(boat.root.rotation.z).toBeGreaterThan(0);
    // Bubbles rise while it goes under.
    expect(scene.particleSystems.length).toBeGreaterThan(0);
    effects.sink(boat, 1);
    expect(boat.body.position.y).toBeLessThan(BOAT_FLOAT - 0.3);
    effects.clear();
    expect(boat.body.position.y).toBe(BOAT_FLOAT);
    expect(boat.body.position.z).toBe(0);
    expect(boat.body.rotation.x).toBe(0);
    expect(boat.root.rotation.z).toBe(0);
    disposeBoat(boat);
  });

  test("a rattled boat rolls and shakes its body only, fades out, and is put back; with Effects off it stays still", () => {
    const stored = new Map<string, string>([
      ["island-battles-web.graphics", "high"],
    ]);
    const original = Object.getOwnPropertyDescriptor(
      globalThis,
      "localStorage",
    );
    Object.defineProperty(globalThis, "localStorage", {
      configurable: true,
      value: {
        getItem: (key: string) => stored.get(key) ?? null,
        setItem: (key: string, value: string) => stored.set(key, value),
      },
    });
    try {
      const boat = createBoat(scene, 8, materials, 1);
      boat.root.position.set(1, 0, 2);
      const effects = createTurnEffects(scene);
      effects.rattle(boat, 0.1);
      expect(Math.abs(boat.body.rotation.z)).toBeGreaterThan(0.05);
      expect(Math.abs(boat.body.position.x)).toBeGreaterThan(0);
      // The boat's real position does not move.
      expect(boat.root.position.x).toBe(1);
      expect(boat.root.position.z).toBe(2);
      effects.rattle(boat, 1);
      expect(boat.body.rotation.z).toBeCloseTo(0, 9);
      expect(boat.body.position.x).toBeCloseTo(0, 9);
      effects.rattle(boat, 0.1);
      effects.clear();
      expect(boat.body.rotation.z).toBe(0);
      expect(boat.body.position.x).toBe(0);
      stored.set("island-battles-web.graphics", "low");
      effects.rattle(boat, 0.1);
      expect(boat.body.rotation.z).toBe(0);
      expect(boat.body.position.x).toBe(0);
      disposeBoat(boat);
    } finally {
      if (original) Object.defineProperty(globalThis, "localStorage", original);
      else Reflect.deleteProperty(globalThis, "localStorage");
    }
  });
});

describe("particle bursts (#102)", () => {
  test("a finished burst leaves the shared texture for the next one", () => {
    const bursts = new Bursts(scene, false);
    bursts.warmUp();
    bursts.clear();
    bursts.emit({
      name: "test",
      position: Vector3.Zero(),
      count: 4,
      life: [0.1, 0.2],
      size: [0.1, 0.1],
      speed: [0, 1],
      direction1: Vector3.Up(),
      direction2: Vector3.Up(),
      color1: new Color4(1, 1, 1, 1),
      color2: new Color4(1, 1, 1, 1),
      colorDead: new Color4(1, 1, 1, 0),
      shape: "soft",
    });
    const system = scene.particleSystems.at(-1);
    expect(system?.particleTexture?.getInternalTexture()).not.toBeNull();
    bursts.dispose();
  });
});
