// The castle menu's place on screen (#344): the board's controls open the menu (`picking.ts`, with
// the gestures of `MenuGestures` in `src/play/`), and the menu follows its castle with this.

import { Matrix, Vector3 } from "@babylonjs/core/Maths/math.vector";
import type { Scene } from "@babylonjs/core/scene";
import { battlefieldOf } from "./battlefield";

/**
 * A castle's place on screen, in CSS pixels from the canvas's top left, as `LabelLayer.place`
 * projects its anchors; null when the castle isn't drawn or is behind the camera.
 */
export function castleScreenPoint(
  scene: Scene,
  castleId: number,
): { x: number; y: number } | null {
  const field = battlefieldOf(scene);
  const view = field?.castles.get(castleId)?.view;
  const camera = scene.activeCamera;
  const canvas = scene.getEngine().getRenderingCanvas();
  if (!view || !camera || !canvas) return null;
  const engine = scene.getEngine();
  const width = engine.getRenderWidth();
  const height = engine.getRenderHeight();
  const projected = Vector3.Project(
    view.root.getAbsolutePosition(),
    Matrix.IdentityReadOnly,
    scene.getTransformMatrix(),
    camera.viewport.toGlobal(width, height),
  );
  if (projected.z < 0 || projected.z > 1) return null;
  // Render pixels to CSS pixels: the engine may render at another resolution than the canvas shows.
  const scaleX = canvas.clientWidth / Math.max(1, width);
  const scaleY = canvas.clientHeight / Math.max(1, height);
  return { x: projected.x * scaleX, y: projected.y * scaleY };
}
