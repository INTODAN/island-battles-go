import { afterAll, beforeAll, describe, expect, test } from "bun:test";
import { FreeCamera } from "@babylonjs/core/Cameras/freeCamera";
import { NullEngine } from "@babylonjs/core/Engines/nullEngine";
import { Color4 } from "@babylonjs/core/Maths/math.color";
import { Vector3 } from "@babylonjs/core/Maths/math.vector";
import { Scene } from "@babylonjs/core/scene";
import { createTurnEffects } from "./animation/effects";
import { createBoat, disposeBoat, MaterialCache } from "./art";
import type { Battlefield } from "./battlefield";
import {
  BURST_KINDS,
  type BurstOptions,
  Bursts,
  burstsOf,
  kindKey,
} from "./particles";

// The particle pool of #197: every system is built by `warmUp` and reused, and nothing is built
// while a turn plays. These run on CPU particles and the null engine, so no GPU is needed.

let engine: NullEngine;
let scene: Scene;
let frameMs = 16;

beforeAll(() => {
  engine = new NullEngine();
  (engine as unknown as { getDeltaTime: () => number }).getDeltaTime = () =>
    frameMs;
  scene = new Scene(engine);
  new FreeCamera("camera", new Vector3(0, 5, -5), scene);
});

afterAll(() => {
  scene.dispose();
  engine.dispose();
});

function frames(count: number): void {
  for (let frame = 0; frame < count; frame++) scene.render();
}

function burst(overrides: Partial<BurstOptions> = {}): BurstOptions {
  return {
    name: "test",
    position: new Vector3(1, 0, 2),
    count: 10,
    duration: 0.1,
    life: [0.2, 0.4],
    size: [0.1, 0.2],
    grow: 2.1,
    speed: [0.2, 0.4],
    direction1: new Vector3(-1, 1, -1),
    direction2: new Vector3(1, 1, 1),
    spread: 0.1,
    color1: new Color4(1, 0, 0, 1),
    color2: new Color4(0, 1, 0, 1),
    colorDead: new Color4(0, 0, 1, 0),
    shape: "soft",
    ...overrides,
  };
}

function where(system: { emitter: unknown } | undefined): Vector3 {
  if (!system) throw new Error("no system is playing");
  return system.emitter as Vector3;
}

function counts() {
  return {
    systems: scene.particleSystems.length,
    textures: scene.textures.length,
    materials: scene.materials.length,
  };
}

function running(): number {
  return scene.particleSystems.filter((system) => system.emitter !== null)
    .length;
}

describe("pooled bursts (#197)", () => {
  test("warmUp builds every system, and a burst builds nothing", () => {
    const bursts = new Bursts(scene, false);
    bursts.warmUp();
    const total = BURST_KINDS.reduce((sum, kind) => sum + kind.systems, 0);
    expect(scene.particleSystems.length).toBe(total);
    expect(bursts.stats().systems).toBe(total);
    bursts.clear();
    const before = counts();
    for (let round = 0; round < 30; round++) {
      bursts.emit(burst());
      frames(3);
    }
    expect(counts()).toEqual(before);
    bursts.dispose();
  });

  test("a second warmUp does nothing", () => {
    const bursts = new Bursts(scene, false);
    bursts.warmUp();
    const before = counts();
    bursts.warmUp();
    expect(counts()).toEqual(before);
    bursts.dispose();
  });

  test("a burst plays on a pooled system and gives it back after its last particle", () => {
    const bursts = new Bursts(scene, false);
    bursts.warmUp();
    bursts.clear();
    expect(running()).toBe(0);
    bursts.emit(burst({ position: new Vector3(3, 0, 4) }));
    expect(running()).toBe(1);
    const playing = scene.particleSystems.find(
      (system) => system.emitter !== null,
    );
    expect(where(playing).x).toBe(3);
    expect(where(playing).z).toBe(4);
    expect(playing?.color1.r).toBe(1);
    expect(playing?.targetStopDuration).toBe(0.1);
    // 0.1 s of emitting, up to 0.4 s of life and 0.2 s to spare: 0.7 s.
    frameMs = 100;
    frames(6);
    expect(running()).toBe(1);
    frames(2);
    expect(running()).toBe(0);
    frameMs = 16;
    // The next burst takes the same system, set up afresh.
    bursts.emit(
      burst({
        position: new Vector3(-5, 0, 6),
        color1: new Color4(0, 0, 1, 1),
      }),
    );
    const again = scene.particleSystems.find(
      (system) => system.emitter !== null,
    );
    expect(again).toBe(playing);
    expect(where(again).x).toBe(-5);
    expect(again?.color1.b).toBe(1);
    bursts.dispose();
  });

  test("more bursts than the pool holds are dropped, and the ones playing carry on", () => {
    const bursts = new Bursts(scene, false);
    bursts.warmUp();
    bursts.clear();
    const options = burst();
    const size = BURST_KINDS.find((kind) => kind.key === kindKey(options))
      ?.systems as number;
    expect(size).toBeGreaterThan(0);
    const before = counts();
    for (let index = 0; index < size + 5; index++) {
      bursts.emit(
        burst({ position: new Vector3(index, 0, 0), name: `burst-${index}` }),
      );
    }
    expect(running()).toBe(size);
    expect(bursts.stats().dropped).toBe(5);
    expect(counts()).toEqual(before);
    // The first ones are still where they were put.
    const xs = scene.particleSystems
      .filter((system) => system.emitter !== null)
      .map((system) => (system.emitter as Vector3).x)
      .sort((a, b) => a - b);
    expect(xs).toEqual(Array.from({ length: size }, (_, index) => index));
    bursts.dispose();
  });

  test("a stream holds its system until stopped, follows the boat and then gives it back", () => {
    const bursts = new Bursts(scene, false);
    bursts.warmUp();
    bursts.clear();
    const stream = bursts.stream(
      burst({ grow: 1, count: 0, life: [0.45, 0.8] }),
      36,
    );
    frameMs = 100;
    frames(30);
    expect(running()).toBe(1);
    stream.move(new Vector3(7, 0, 8));
    const playing = scene.particleSystems.find(
      (system) => system.emitter !== null,
    );
    expect(where(playing).x).toBe(7);
    stream.stop();
    frames(14);
    expect(running()).toBe(0);
    // A stale handle does not touch a system that someone else may be using.
    bursts.emit(burst({ grow: 1 }));
    stream.move(new Vector3(99, 0, 99));
    const reused = scene.particleSystems.find(
      (system) => system.emitter !== null,
    );
    expect(where(reused).x).toBe(1);
    frameMs = 16;
    bursts.dispose();
  });

  test("a kind that is not in the table is dropped rather than built", () => {
    const bursts = new Bursts(scene, false);
    bursts.warmUp();
    bursts.clear();
    const before = counts();
    bursts.emit(burst({ shape: "spark", additive: true, grow: 7 }));
    expect(bursts.stats().dropped).toBe(1);
    expect(counts()).toEqual(before);
    bursts.dispose();
  });

  test("dispose takes every system out of the scene", () => {
    const before = scene.particleSystems.length;
    const bursts = new Bursts(scene, false);
    bursts.warmUp();
    expect(scene.particleSystems.length).toBeGreaterThan(before);
    bursts.dispose();
    expect(scene.particleSystems.length).toBe(before);
    expect(burstsOf(scene)).toBe(bursts);
  });
});

describe("the turn's effects use the pool (#197)", () => {
  test("clashes, captures, level-ups and sinking boats build nothing and drop nothing", async () => {
    const materials = new MaterialCache(scene);
    const boat = createBoat(scene, 3, materials, 1);
    await import("./particles");
    const effects = createTurnEffects(scene);
    await new Promise((resolve) => setTimeout(resolve, 0));
    const bursts = burstsOf(scene) as Bursts;
    bursts.clear();
    const field = {
      scene,
      castles: new Map(),
      generation: { castles: [{ id: 1, x: 2, y: 3 }] },
    } as unknown as Battlefield;
    const before = counts();
    const flashes = [
      { clashId: 1, groupId: 1, x: 4, y: 5, radius: 0.3 },
      { clashId: 1, groupId: 2, x: 4.5, y: 5, radius: 0.6 },
    ];
    effects.clash(field, flashes as never);
    effects.castleCaptured(field, 1, 2);
    effects.castleLevelUp(field, 1, 1, 2);
    effects.sink(boat, 0.2);
    effects.sink(boat, 0.6);
    expect(running()).toBeGreaterThan(0);
    expect(bursts.stats().dropped).toBe(0);
    expect(counts()).toEqual(before);
    // The sinking boat's bubbles stop with the turn.
    effects.clear();
    expect(counts()).toEqual(before);
    disposeBoat(boat);
    bursts.dispose();
  });
});
