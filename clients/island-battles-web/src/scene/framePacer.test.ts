// The "draw now or wait" decision of #403, with no GPU: which states ask for full rate, how long
// it lingers, and that a wake brings it back before the next frame.

import { describe, expect, test } from "bun:test";
import { ArcRotateCamera } from "@babylonjs/core/Cameras/arcRotateCamera";
import { NullEngine } from "@babylonjs/core/Engines/nullEngine";
import { Vector3 } from "@babylonjs/core/Maths/math.vector";
import { Scene } from "@babylonjs/core/scene";
import {
  type Activity,
  cameraWatcher,
  FramePacer,
  frameCap,
  IDLE_FPS,
  isActive,
  LINGER_MS,
} from "./framePacer";

describe("isActive (#403)", () => {
  test("nothing moving asks for no full rate", () => {
    expect(isActive({})).toBe(false);
    expect(
      isActive({
        turn: false,
        camera: false,
        pointer: false,
        particles: false,
        steering: false,
      }),
    ).toBe(false);
  });

  const reasons: (keyof Activity)[] = [
    "turn",
    "camera",
    "pointer",
    "particles",
    "steering",
  ];
  for (const reason of reasons) {
    test(`${reason} alone asks for full rate`, () => {
      expect(isActive({ [reason]: true })).toBe(true);
    });
  }
});

describe("frameCap (#403)", () => {
  test("full rate while active, whenever the last activity was", () => {
    expect(frameCap(10_000, 0, true)).toBeUndefined();
  });

  test("full rate lingers after the last activity, then the idle rate", () => {
    expect(frameCap(LINGER_MS - 1, 0, false)).toBeUndefined();
    expect(frameCap(LINGER_MS, 0, false)).toBe(IDLE_FPS);
    expect(frameCap(60_000, 0, false)).toBe(IDLE_FPS);
  });

  test("the idle rate keeps the sea and the castle loops alive", () => {
    expect(IDLE_FPS).toBeGreaterThanOrEqual(20);
    expect(IDLE_FPS).toBeLessThanOrEqual(30);
  });
});

/** A pacer on a fake engine and clock, polling `activity`. */
function pacerWith(activity: Activity) {
  let now = 0;
  const engine: { maxFPS: number | undefined } = { maxFPS: 12 };
  const pacer = new FramePacer(engine, () => activity, { now: () => now });
  return {
    pacer,
    engine,
    advance(ms: number) {
      now += ms;
    },
  };
}

describe("FramePacer (#403)", () => {
  test("starts at full rate while the board builds", () => {
    const { engine } = pacerWith({});
    expect(engine.maxFPS).toBeUndefined();
  });

  test("drops to the idle rate once nothing moved for the linger time", () => {
    const { pacer, engine, advance } = pacerWith({});
    advance(LINGER_MS / 2);
    pacer.update();
    expect(engine.maxFPS).toBeUndefined();
    advance(LINGER_MS / 2);
    pacer.update();
    expect(engine.maxFPS).toBe(IDLE_FPS);
  });

  test("stays at full rate for as long as a turn plays, and lingers after it", () => {
    const activity: Activity = { turn: true };
    const { pacer, engine, advance } = pacerWith(activity);
    for (let frame = 0; frame < 600; frame++) {
      advance(16);
      pacer.update();
      expect(engine.maxFPS).toBeUndefined();
    }
    activity.turn = false;
    advance(16);
    pacer.update();
    expect(engine.maxFPS).toBeUndefined();
    advance(LINGER_MS);
    pacer.update();
    expect(engine.maxFPS).toBe(IDLE_FPS);
  });

  test("a wake brings full rate back at once, before any frame is drawn", () => {
    const { pacer, engine, advance } = pacerWith({});
    advance(LINGER_MS * 3);
    pacer.update();
    expect(engine.maxFPS).toBe(IDLE_FPS);
    pacer.wake();
    expect(engine.maxFPS).toBeUndefined();
    // And it lasts the linger time, though nothing else moves.
    advance(LINGER_MS - 1);
    pacer.update();
    expect(engine.maxFPS).toBeUndefined();
    advance(1);
    pacer.update();
    expect(engine.maxFPS).toBe(IDLE_FPS);
  });

  test("each reason on its own brings full rate back from idle", () => {
    for (const reason of [
      "turn",
      "camera",
      "pointer",
      "particles",
      "steering",
    ] as const) {
      const activity: Activity = {};
      const { pacer, engine, advance } = pacerWith(activity);
      advance(LINGER_MS * 2);
      pacer.update();
      expect(engine.maxFPS).toBe(IDLE_FPS);
      activity[reason] = true;
      advance(33);
      pacer.update();
      expect(engine.maxFPS).toBeUndefined();
    }
  });
});

describe("cameraWatcher (#403)", () => {
  const engine = new NullEngine();
  const scene = new Scene(engine);
  const camera = new ArcRotateCamera("c", 0, 1, 10, Vector3.Zero(), scene);

  test("tells a moved camera from a still one, and inertia left from none", () => {
    const moved = cameraWatcher(camera);
    expect(moved()).toBe(true);
    expect(moved()).toBe(false);
    camera.alpha += 0.01;
    expect(moved()).toBe(true);
    expect(moved()).toBe(false);
    camera.target.x += 0.5;
    expect(moved()).toBe(true);
    expect(moved()).toBe(false);
    camera.radius *= 1.1;
    expect(moved()).toBe(true);
    camera.inertialAlphaOffset = 0.001;
    expect(moved()).toBe(true);
    expect(moved()).toBe(true);
    camera.inertialAlphaOffset = 0;
    expect(moved()).toBe(false);
  });
});
