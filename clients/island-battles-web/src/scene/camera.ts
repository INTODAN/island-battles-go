import { ArcRotateCamera } from "@babylonjs/core/Cameras/arcRotateCamera";
import { Vector3 } from "@babylonjs/core/Maths/math.vector";
import type { Scene } from "@babylonjs/core/scene";
import type { BoardBounds } from "./board";
import { toWorld } from "./coordinates";

// A 3/4 strategy view: looking along +z from the south, tilted about 55 degrees from straight
// down. Larger beta is lower and flatter.
export const CAMERA = {
  alpha: -Math.PI / 2,
  beta: 0.96,
  fov: 0.8,
  /** Never straight down, and never lower than this above the sea. */
  minBeta: 0.45,
  maxBeta: 1.3,
  minRadius: 3.5,
  /** The farthest zoom is this many times the radius that fits the board. */
  maxRadiusFactor: 1.5,
  /** How much of the view, from the centre to the edge, the board may fill. */
  fitFill: 0.9,
  /** The height of the tallest thing on the board, which the fit keeps in view. */
  featureHeight: 1.3,
  /** How far past the board's edge the camera may pan. */
  panMargin: 1,
  focusRadius: 5,
} as const;

/**
 * The smallest distance at which the whole board, and the tallest things standing on it, are in
 * view at tilt `beta`, with a vertical field of view `fov` and the given aspect ratio (width over
 * height). It projects the board's corners through the camera, so perspective is included.
 */
export function fitRadius(
  bounds: BoardBounds,
  aspect: number,
  fov: number = CAMERA.fov,
  beta: number = CAMERA.beta,
  alpha: number = CAMERA.alpha,
): number {
  const tanVertical = Math.tan(fov / 2);
  const tanHorizontal = tanVertical * aspect;
  const corners: [number, number, number][] = [];
  for (const x of [-bounds.halfWidth, bounds.halfWidth]) {
    for (const z of [-bounds.halfDepth, bounds.halfDepth]) {
      for (const y of [0, CAMERA.featureHeight]) corners.push([x, y, z]);
    }
  }
  const unit = (vector: number[]) => {
    const length = Math.hypot(...vector);
    return vector.map((value) => value / length) as [number, number, number];
  };
  const dot = (left: number[], right: number[]) =>
    left.reduce(
      (total, value, index) => total + value * (right[index] ?? 0),
      0,
    );
  const cross = (left: number[], right: number[]): [number, number, number] => [
    (left[1] ?? 0) * (right[2] ?? 0) - (left[2] ?? 0) * (right[1] ?? 0),
    (left[2] ?? 0) * (right[0] ?? 0) - (left[0] ?? 0) * (right[2] ?? 0),
    (left[0] ?? 0) * (right[1] ?? 0) - (left[1] ?? 0) * (right[0] ?? 0),
  ];
  const fits = (radius: number) => {
    const eye = [
      radius * Math.sin(beta) * Math.cos(alpha),
      radius * Math.cos(beta),
      radius * Math.sin(beta) * Math.sin(alpha),
    ];
    const forward = unit(eye.map((value) => -value));
    const right = unit(cross(forward, [0, 1, 0]));
    const up = cross(right, forward);
    return corners.every((corner) => {
      const offset = corner.map((value, index) => value - (eye[index] ?? 0));
      const depth = dot(offset, forward);
      return (
        depth > 0 &&
        Math.abs(dot(offset, right)) / (depth * tanHorizontal) <=
          CAMERA.fitFill &&
        Math.abs(dot(offset, up)) / (depth * tanVertical) <= CAMERA.fitFill
      );
    });
  };
  let near = 1;
  let far = 1000;
  for (let step = 0; step < 40; step++) {
    const middle = (near + far) / 2;
    if (fits(middle)) far = middle;
    else near = middle;
  }
  return far;
}

interface CameraState {
  bounds: BoardBounds;
  stopTween: () => void;
}
const states = new WeakMap<ArcRotateCamera, CameraState>();

export function createBoardCamera(
  scene: Scene,
  canvas: HTMLCanvasElement,
  bounds: BoardBounds,
): ArcRotateCamera {
  const camera = new ArcRotateCamera(
    "camera",
    CAMERA.alpha,
    CAMERA.beta,
    10,
    Vector3.Zero(),
    scene,
  );
  camera.fov = CAMERA.fov;
  camera.minZ = 0.1;
  camera.maxZ = 400;
  camera.lowerBetaLimit = CAMERA.minBeta;
  camera.upperBetaLimit = CAMERA.maxBeta;
  camera.lowerRadiusLimit = CAMERA.minRadius;
  camera.wheelDeltaPercentage = 0.01;
  camera.pinchDeltaPercentage = 0.005;
  camera.panningSensibility = 1500;
  camera.panningInertia = 0.8;
  camera.inertia = 0.85;
  // Left-drag turns the board; right-drag, or a two-finger drag, pans it.
  camera.attachControl(canvas, true);
  states.set(camera, { bounds, stopTween: () => {} });

  // Panning stays near the board, so it can never be lost off screen.
  scene.onBeforeRenderObservable.add(() => {
    const state = states.get(camera);
    if (!state) return;
    const { halfWidth, halfDepth } = state.bounds;
    camera.target.x = Math.max(
      -halfWidth - CAMERA.panMargin,
      Math.min(halfWidth + CAMERA.panMargin, camera.target.x),
    );
    camera.target.z = Math.max(
      -halfDepth - CAMERA.panMargin,
      Math.min(halfDepth + CAMERA.panMargin, camera.target.z),
    );
    camera.target.y = 0;
  });
  return camera;
}

function tween(
  scene: Scene,
  camera: ArcRotateCamera,
  goal: { alpha: number; beta: number; radius: number; target: Vector3 },
  milliseconds: number,
): void {
  const state = states.get(camera);
  state?.stopTween();
  const from = {
    alpha: camera.alpha,
    beta: camera.beta,
    radius: camera.radius,
    target: camera.target.clone(),
  };
  if (milliseconds <= 0) {
    camera.alpha = goal.alpha;
    camera.beta = goal.beta;
    camera.radius = goal.radius;
    camera.target.copyFrom(goal.target);
    return;
  }
  const started = performance.now();
  const observer = scene.onBeforeRenderObservable.add(() => {
    const progress = Math.min(1, (performance.now() - started) / milliseconds);
    const eased = 1 - (1 - progress) ** 3;
    camera.alpha = from.alpha + (goal.alpha - from.alpha) * eased;
    camera.beta = from.beta + (goal.beta - from.beta) * eased;
    camera.radius = from.radius + (goal.radius - from.radius) * eased;
    camera.target.copyFrom(Vector3.Lerp(from.target, goal.target, eased));
    if (progress >= 1) scene.onBeforeRenderObservable.remove(observer);
  });
  if (state) {
    state.stopTween = () => scene.onBeforeRenderObservable.remove(observer);
  }
}

/** Frames the whole board at the default 3/4 view. */
export function fitCamera(
  scene: Scene,
  camera: ArcRotateCamera,
  bounds: BoardBounds,
  milliseconds = 0,
): void {
  const state = states.get(camera);
  if (state) state.bounds = bounds;
  const aspect = scene.getEngine().getAspectRatio(camera);
  const radius = fitRadius(bounds, aspect);
  camera.upperRadiusLimit = radius * CAMERA.maxRadiusFactor;
  tween(
    scene,
    camera,
    {
      alpha: CAMERA.alpha,
      beta: CAMERA.beta,
      radius,
      target: Vector3.Zero(),
    },
    milliseconds,
  );
}

/** Moves in on a point of the map, keeping the current angle. */
export function focusCamera(
  scene: Scene,
  camera: ArcRotateCamera,
  point: { x: number; y: number },
  milliseconds = 450,
): void {
  const world = toWorld(point);
  tween(
    scene,
    camera,
    {
      alpha: camera.alpha,
      beta: camera.beta,
      radius: Math.min(camera.radius, CAMERA.focusRadius),
      target: new Vector3(world.x, 0, world.z),
    },
    milliseconds,
  );
}
