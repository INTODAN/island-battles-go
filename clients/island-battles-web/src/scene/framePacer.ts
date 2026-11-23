import type { ArcRotateCamera } from "@babylonjs/core/Cameras/arcRotateCamera";
import { PointerEventTypes } from "@babylonjs/core/Events/pointerEvents";
import type { Scene } from "@babylonjs/core/scene";

// Draw only when something changes (#403). A board drew every frame at the display's full rate,
// also between turns, on a paused replay or with the camera still. Now it draws at full rate
// only while something moves that the player follows (a turn, the camera, a drag, particles),
// and for a short while after, so the tail of a pop, a fade or a boat's turn finishes smoothly;
// otherwise at IDLE_FPS, which keeps the sea and the castles' loops alive. Any input or change
// of state asks for full rate again before the next frame (`wake`).
//
// The rate is set with the engine's own `maxFPS`, which skips a frame before it begins: the
// engine's delta time is then the time since the last frame drawn, so everything that runs on
// it (the sea, the castle loops, the turn player) keeps its real-time speed at either rate.

/** Frames a second with nothing moving but the sea and the castles' loops. */
export const IDLE_FPS = 30;
/** How long full rate lasts after the last activity, for the tails of short animations. */
export const LINGER_MS = 1000;

/** What asks for full rate now; any one of them does. */
export interface Activity {
  /** A turn plays, or waits to play. */
  turn?: boolean;
  /** The camera moved since the last frame drawn: a drag, its inertia, a zoom or a tween. */
  camera?: boolean;
  /** A pointer button is held on the board: a drag to send or a camera drag. */
  pointer?: boolean;
  /** Particles are alive. */
  particles?: boolean;
  /** Boats are still turning to their heading. */
  steering?: boolean;
}

/** Whether `activity` asks for full rate. */
export function isActive(activity: Activity): boolean {
  return (
    activity.turn === true ||
    activity.camera === true ||
    activity.pointer === true ||
    activity.particles === true ||
    activity.steering === true
  );
}

/**
 * The cap for the next frames: `undefined` (full rate) while active or within `lingerMs` of the
 * last activity, else `idleFps`.
 */
export function frameCap(
  now: number,
  lastActive: number,
  active: boolean,
  idleFps = IDLE_FPS,
  lingerMs = LINGER_MS,
): number | undefined {
  return active || now - lastActive < lingerMs ? undefined : idleFps;
}

/** The part of an engine the pacer sets. */
export interface FrameCapTarget {
  maxFPS: number | undefined;
}

export interface FramePacerOptions {
  idleFps?: number;
  lingerMs?: number;
  now?: () => number;
}

/**
 * Sets `target.maxFPS` from `poll` after every frame drawn (`update`), and to full rate at once
 * on `wake`. Knows nothing of Babylon beyond `maxFPS`, so it is tested with a fake engine.
 */
export class FramePacer {
  private lastActive: number;
  private readonly idleFps: number;
  private readonly lingerMs: number;
  private readonly now: () => number;

  constructor(
    private readonly target: FrameCapTarget,
    private readonly poll: () => Activity,
    options: FramePacerOptions = {},
  ) {
    this.idleFps = options.idleFps ?? IDLE_FPS;
    this.lingerMs = options.lingerMs ?? LINGER_MS;
    this.now = options.now ?? (() => performance.now());
    // A board starts at full rate, while it builds.
    this.lastActive = this.now();
    target.maxFPS = undefined;
  }

  /** Something changed: draw the next frames at full rate. */
  wake(): void {
    this.lastActive = this.now();
    this.target.maxFPS = undefined;
  }

  /** After a frame is drawn: decides the rate of the next ones. */
  update(): void {
    const now = this.now();
    const active = isActive(this.poll());
    if (active) this.lastActive = now;
    this.target.maxFPS = frameCap(
      now,
      this.lastActive,
      active,
      this.idleFps,
      this.lingerMs,
    );
  }

  /** The cap now: `undefined` for full rate. */
  get cap(): number | undefined {
    return this.target.maxFPS;
  }
}

/**
 * Whether `camera` moved since the last call, by its angles, distance and target, or has
 * inertia left. Allocates nothing.
 */
export function cameraWatcher(camera: ArcRotateCamera): () => boolean {
  let alpha = Number.NaN;
  let beta = Number.NaN;
  let radius = Number.NaN;
  let x = Number.NaN;
  let y = Number.NaN;
  let z = Number.NaN;
  return () => {
    const target = camera.target;
    const moved =
      camera.alpha !== alpha ||
      camera.beta !== beta ||
      camera.radius !== radius ||
      target.x !== x ||
      target.y !== y ||
      target.z !== z ||
      camera.inertialAlphaOffset !== 0 ||
      camera.inertialBetaOffset !== 0 ||
      camera.inertialRadiusOffset !== 0 ||
      camera.inertialPanningX !== 0 ||
      camera.inertialPanningY !== 0;
    alpha = camera.alpha;
    beta = camera.beta;
    radius = camera.radius;
    x = target.x;
    y = target.y;
    z = target.z;
    return moved;
  };
}

/**
 * Paces `scene`'s engine with `poll`: decides after each frame drawn, wakes on any pointer or
 * wheel event on the board and any key, and keeps full rate while a pointer button is held on the
 * board. Returns the pacer and a function that removes what it added (call it before the scene
 * is disposed).
 */
export function paceScene(
  scene: Scene,
  poll: () => Activity,
  options: FramePacerOptions = {},
): { pacer: FramePacer; dispose: () => void } {
  const engine = scene.getEngine();
  let held = false;
  const pacer = new FramePacer(
    engine,
    () => (held ? { pointer: true } : poll()),
    options,
  );
  const afterRender = scene.onAfterRenderObservable.add(() => pacer.update());
  const pointer = scene.onPrePointerObservable.add((info) => {
    if (info.type === PointerEventTypes.POINTERDOWN) held = true;
    else if (info.type === PointerEventTypes.POINTERUP) held = false;
    pacer.wake();
  });
  const wake = () => pacer.wake();
  window.addEventListener("keydown", wake);
  return {
    pacer,
    dispose: () => {
      scene.onAfterRenderObservable.remove(afterRender);
      scene.onPrePointerObservable.remove(pointer);
      window.removeEventListener("keydown", wake);
      engine.maxFPS = undefined;
    },
  };
}
