import { describe, expect, test } from "bun:test";
import { ArcRotateCamera } from "@babylonjs/core/Cameras/arcRotateCamera";
import { NullEngine } from "@babylonjs/core/Engines/nullEngine";
import {
  PointerEventTypes,
  PointerInfo,
} from "@babylonjs/core/Events/pointerEvents";
import { Vector3 } from "@babylonjs/core/Maths/math.vector";
import { Scene } from "@babylonjs/core/scene";
import { BoardPointersInput, holdCameraPointer } from "./camera";

/** A camera with the board's pointer input, fed pointer events as Babylon's input manager does. */
function setup() {
  const engine = new NullEngine();
  const scene = new Scene(engine);
  const camera = new ArcRotateCamera(
    "camera",
    -Math.PI / 2,
    0.96,
    15,
    Vector3.Zero(),
    scene,
  );
  camera.inputs.removeByType("ArcRotateCameraPointersInput");
  camera.inputs.add(new BoardPointersInput());
  camera.pinchDeltaPercentage = 0.005;
  camera.panningSensibility = 1500;
  camera.attachControl(true);
  const send = (
    type: number,
    pointerId: number,
    clientX: number,
    clientY: number,
  ) => {
    const event = {
      pointerType: "touch",
      button: type === PointerEventTypes.POINTERMOVE ? -1 : 0,
      buttons: 1,
      pointerId,
      clientX,
      clientY,
      target: null,
      preventDefault() {},
    };
    scene.onPointerObservable.notifyObservers(
      new PointerInfo(type, event as never, null),
      type,
    );
  };
  const settle = () => {
    for (let frame = 0; frame < 60; frame++) scene.render();
    return {
      alpha: camera.alpha,
      radius: camera.radius,
      target: camera.target.clone(),
    };
  };
  return { camera, send, settle, dispose: () => engine.dispose() };
}
const {
  POINTERDOWN: DOWN,
  POINTERMOVE: MOVE,
  POINTERUP: UP,
} = PointerEventTypes;

describe("the board camera's pointer input", () => {
  test("a finger turns the board as before", () => {
    const { send, settle, dispose } = setup();
    const before = settle();
    send(DOWN, 5, 100, 100);
    send(MOVE, 5, 300, 100);
    send(UP, 5, 300, 100);
    expect(Math.abs(settle().alpha - before.alpha)).toBeGreaterThan(0.1);
    dispose();
  });

  test("a held pointer never turns it, and the next gesture does", () => {
    const { camera, send, settle, dispose } = setup();
    const before = settle();
    send(DOWN, 5, 100, 100);
    holdCameraPointer(camera, 5);
    send(MOVE, 5, 300, 100);
    send(MOVE, 5, 400, 120);
    expect(settle()).toEqual(before);
    send(UP, 5, 400, 120);
    holdCameraPointer(camera, null);
    send(DOWN, 6, 100, 100);
    send(MOVE, 6, 300, 100);
    expect(Math.abs(settle().alpha - before.alpha)).toBeGreaterThan(0.1);
    dispose();
  });

  test("a second finger after a held drag pans from where the fingers are, with no jump", () => {
    const { camera, send, settle, dispose } = setup();
    send(DOWN, 5, 100, 100);
    holdCameraPointer(camera, 5);
    send(MOVE, 5, 400, 100);
    send(DOWN, 6, 400, 300);
    holdCameraPointer(camera, null);
    const before = settle();
    // The second finger moves first, then both pan together by 30 px.
    send(MOVE, 6, 401, 300);
    send(MOVE, 5, 401, 100);
    for (let step = 1; step <= 30; step++) {
      send(MOVE, 5, 401, 100 + step);
      send(MOVE, 6, 401, 300 + step);
    }
    const after = settle();
    expect(Math.abs(after.radius - before.radius)).toBeLessThan(0.5);
    expect(Vector3.Distance(after.target, before.target)).toBeLessThan(1);
    dispose();
  });
});
