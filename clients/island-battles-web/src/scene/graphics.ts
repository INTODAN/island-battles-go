import type { Camera } from "@babylonjs/core/Cameras/camera";
import type { Scene } from "@babylonjs/core/scene";
import { setCastleAnimation } from "./castles";
import { lookOf } from "./look";
import type { PostFx } from "./postFx";

// The graphics setting (issue #102). "high" turns on post-processing (tone mapping, bloom and
// FXAA), the sun's shadows and the castles' flag and windmill loops; "low" turns all three off
// for slow devices. The choice is kept in localStorage for the next visit.

export type Graphics = "high" | "low";

const STORAGE_KEY = "island-battles-web.graphics";

function storage(): Storage | null {
  try {
    return globalThis.localStorage ?? null;
  } catch {
    return null;
  }
}

/** A device that is likely to struggle: two cores or less, or 2 GB of memory or less. */
export function looksSlow(
  nav: {
    hardwareConcurrency?: number;
    deviceMemory?: number;
  } = globalThis.navigator ?? {},
): boolean {
  const cores = nav.hardwareConcurrency ?? 8;
  const memory = nav.deviceMemory ?? 8;
  return cores <= 2 || memory <= 2;
}

/** The saved choice, or "low" on a device that looks slow and "high" otherwise. */
export function savedGraphics(): Graphics {
  const saved = storage()?.getItem(STORAGE_KEY);
  if (saved === "high" || saved === "low") return saved;
  return looksSlow() ? "low" : "high";
}

export function saveGraphics(value: Graphics): void {
  try {
    storage()?.setItem(STORAGE_KEY, value);
  } catch {
    // A private window may refuse; the choice then lasts for this page only.
  }
}

const effects = new WeakMap<
  Scene,
  { value: Graphics; postFx: PostFx | null }
>();

/** The setting `scene` is drawn with: what `applyGraphics` last set, else the saved choice. */
export function graphicsOf(scene: Scene): Graphics {
  return effects.get(scene)?.value ?? savedGraphics();
}

/**
 * Applies `value` to a scene drawn by `camera`: post-processing (loaded on first use), shadows
 * and the castle loops. Resolves once the pipeline is in place.
 */
export async function applyGraphics(
  scene: Scene,
  camera: Camera,
  value: Graphics,
): Promise<void> {
  let state = effects.get(scene);
  if (!state) {
    state = { value, postFx: null };
    effects.set(scene, state);
  }
  state.value = value;
  const high = value === "high";
  setCastleAnimation(high);
  const look = lookOf(scene);
  look?.setShadows(high);
  look?.sea.setDetail(high);
  if (!high) {
    state.postFx?.dispose();
    state.postFx = null;
    return;
  }
  if (state.postFx) return;
  const { createPostFx } = await import("./postFx");
  // The setting may have changed, or the scene gone, while the module loaded.
  if (scene.isDisposed || state.value !== "high" || state.postFx) return;
  state.postFx = createPostFx(scene, camera);
}
