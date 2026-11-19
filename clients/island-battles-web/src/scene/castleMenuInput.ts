// The castle menu's gestures on the board (#344): a right-click that didn't drag, a double-click
// and a double-tap on a castle. They only report the castle: whether the menu opens (your castle,
// while you play, on the newest turn) is the menu's rule (`menuAllowed` in `src/play/`). Their state
// is their own, apart from the drag to send (#343) and the tooltip.

import { PointerEventTypes } from "@babylonjs/core/Events/pointerEvents";
import { Matrix, Vector3 } from "@babylonjs/core/Maths/math.vector";
import type { Scene } from "@babylonjs/core/scene";
import { isRightClick, type MenuOpener } from "../play/castleMenu";
import type { PickTarget } from "./art";
import { battlefieldOf } from "./battlefield";

export interface CastleMenuHooks {
  /** Opens the menu on `castleId` when it may open there; returns whether it opened. */
  open(castleId: number, opener: MenuOpener): boolean;
}

/** The castle under the pointer, or null. */
function castleAt(scene: Scene): number | null {
  const result = scene.pick(
    scene.pointerX,
    scene.pointerY,
    (mesh) => mesh.isPickable && mesh.metadata?.kind !== undefined,
  );
  const target = result?.pickedMesh?.metadata as PickTarget | undefined;
  return result?.hit && target?.kind === "castle" ? target.id : null;
}

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

/**
 * Listens for the menu's gestures on the board's canvas and keeps the browser's context menu off
 * it. `onOpened` runs after the menu opened (the board hides its tooltip). Returns the remover.
 */
export function installCastleMenuOpeners(
  scene: Scene,
  hooks: CastleMenuHooks,
  onOpened: () => void = () => {},
): () => void {
  const canvas = scene.getEngine().getRenderingCanvas();
  if (!canvas) throw new Error("the scene's engine has no canvas");
  /** Where the right button went down, while it is down. */
  let rightDown: { x: number; y: number } | null = null;

  const open = (opener: MenuOpener) => {
    const castleId = castleAt(scene);
    if (castleId !== null && hooks.open(castleId, opener)) onOpened();
  };

  const observer = scene.onPointerObservable.add((info) => {
    const event = info.event as PointerEvent;
    if (info.type === PointerEventTypes.POINTERDOWN) {
      rightDown =
        event.button === 2 ? { x: event.clientX, y: event.clientY } : null;
    } else if (info.type === PointerEventTypes.POINTERUP) {
      if (event.button !== 2 || !rightDown) return;
      const down = rightDown;
      rightDown = null;
      // A right-drag pans the camera; only a right-click opens the menu.
      if (isRightClick(down, { x: event.clientX, y: event.clientY })) {
        open("right-click");
      }
    } else if (info.type === PointerEventTypes.POINTERDOUBLETAP) {
      open(event.pointerType === "touch" ? "double-tap" : "double-click");
    }
  });
  const noContextMenu = (event: Event) => event.preventDefault();
  canvas.addEventListener("contextmenu", noContextMenu);
  return () => {
    scene.onPointerObservable.remove(observer);
    canvas.removeEventListener("contextmenu", noContextMenu);
  };
}
