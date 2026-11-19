import "@babylonjs/core/Culling/ray";
import { PointerEventTypes } from "@babylonjs/core/Events/pointerEvents";
import type { Mesh } from "@babylonjs/core/Meshes/mesh";
import type { Scene } from "@babylonjs/core/scene";
import type { HandPlay } from "../play";
import {
  doubleClickFocuses,
  MenuGestures,
  type MenuOpener,
  type MenuPointer,
} from "../play/castleMenu";
import { createFlatRing, type PickTarget } from "./art";
import { type Battlefield, battlefieldOf } from "./battlefield";
import { CASTLE_RADIUS } from "./board";
import { fitCamera, focusCamera } from "./camera";
import { toWorld, toWorldDistance } from "./coordinates";
import { castleTooltip, groupTooltip } from "./format";
import { CLASH_COLOR } from "./palette";

function pickTargetAt(scene: Scene): PickTarget | null {
  const result = scene.pick(
    scene.pointerX,
    scene.pointerY,
    (mesh) => mesh.isPickable && mesh.metadata?.kind !== undefined,
  );
  const target = result?.pickedMesh?.metadata as PickTarget | undefined;
  return result?.hit && target ? target : null;
}

/** The tooltip lines for what the pointer is over, or null. */
export function describeTarget(
  field: Battlefield,
  target: PickTarget,
): string[] | null {
  const generation = field.generation;
  if (!generation) return null;
  if (target.kind === "castle") {
    const castle = generation.castles.find((entry) => entry.id === target.id);
    return castle ? castleTooltip(castle, field.teams) : null;
  }
  const group = generation.soldier_groups.find(
    (entry) => entry.id === target.id,
  );
  return group ? groupTooltip(group, field.teams) : null;
}

/**
 * What a touch tap does to the tooltip: a tap on a castle or boat pins its tooltip, a tap
 * elsewhere clears it, and anything that is not a touch (a mouse hovers instead) changes
 * nothing, which is `undefined`.
 */
export function touchTapTarget(
  pointerType: string,
  picked: PickTarget | null,
): PickTarget | null | undefined {
  return pointerType === "touch" ? picked : undefined;
}

/** Moves the camera back to show the whole board, in 450 ms: what the F key and the board's Fit button do. */
export function fitBoard(scene: Scene): void {
  const field = battlefieldOf(scene);
  if (field) fitCamera(scene, field.camera, field.bounds, 450);
}

export interface BoardControlsOptions {
  /**
   * The hand player's orders (`src/play/`), or null for a watcher, a demo, or a game view given
   * none. Orders are given only while its status is `playing`; with null, or any other status,
   * the board's controls are a watcher's.
   */
  handPlay?: HandPlay | null;
  /**
   * The castle menu (#344): a right-click that didn't drag, a double-click or a double-tap on a
   * castle (`MenuGestures`) asks it to open there; it answers whether it opened. Given, the
   * browser's context menu never shows over the board. Absent, those gestures do what they do for
   * a watcher.
   */
  onCastleMenu?: ((castleId: number, opener: MenuOpener) => boolean) | null;
}

/**
 * Hover highlight and tooltip (a tap on a touch screen), double-click to focus a castle (not for a
 * hand player who plays: `doubleClickFocuses`), the castle menu's gestures, and F to refit the board. Returns
 * a function that removes the listeners, the tooltip and the highlight: call it before the scene is disposed.
 */
export function installBoardControls(
  scene: Scene,
  options: BoardControlsOptions = {},
): () => void {
  const canvas = scene.getEngine().getRenderingCanvas();
  if (!canvas) throw new Error("the scene's engine has no canvas");
  const tooltip = document.createElement("div");
  tooltip.className = "board-tooltip";
  tooltip.hidden = true;
  canvas.parentElement?.append(tooltip);
  let highlight: Mesh | null = null;
  let hovered: PickTarget | null = null;

  const hide = () => {
    tooltip.hidden = true;
    highlight?.setEnabled(false);
    hovered = null;
  };

  const show = (field: Battlefield, target: PickTarget) => {
    const lines = describeTarget(field, target);
    const generation = field.generation;
    if (!lines || !generation) return hide();
    tooltip.replaceChildren(
      ...lines.map((line, index) => {
        const element = document.createElement(index === 0 ? "strong" : "span");
        element.textContent = line;
        return element;
      }),
    );
    tooltip.hidden = false;
    const rect = canvas.getBoundingClientRect();
    const x = Math.min(
      scene.pointerX + 14,
      rect.width - tooltip.offsetWidth - 6,
    );
    const y = Math.min(
      scene.pointerY + 14,
      rect.height - tooltip.offsetHeight - 6,
    );
    tooltip.style.left = `${Math.max(4, x)}px`;
    tooltip.style.top = `${Math.max(4, y)}px`;

    const castle =
      target.kind === "castle"
        ? generation.castles.find((entry) => entry.id === target.id)
        : undefined;
    const group =
      target.kind === "group"
        ? generation.soldier_groups.find((entry) => entry.id === target.id)
        : undefined;
    const subject = castle ?? group;
    if (!subject) return;
    const radius = castle
      ? CASTLE_RADIUS + 0.12
      : Math.max(0.3, toWorldDistance(group?.clash_radius ?? 0.25) + 0.08);
    highlight?.dispose();
    highlight = createFlatRing(scene, "hover-ring", radius, 0.05);
    highlight.material = field.materials.flat(
      target.kind === "castle" ? "#ffffff" : CLASH_COLOR,
    );
    const world = toWorld(subject);
    highlight.position.set(world.x, 0.2, world.z);
  };

  const menuGestures = new MenuGestures();
  /** The castle menu's view of a pointer event, with the castle under the pointer. */
  const menuPointer = (
    event: PointerEvent,
    target: PickTarget | null,
  ): MenuPointer => ({
    button: event.button,
    pointerType: event.pointerType,
    castleId: target?.kind === "castle" ? target.id : null,
    x: event.clientX,
    y: event.clientY,
    at: performance.now(),
  });
  /** Opens the castle menu when the gesture is one of its openers; returns whether it opened. */
  const openCastleMenu = (
    opener: MenuOpener | null,
    castleId: number | null,
  ): boolean => {
    if (!opener || castleId === null || !options.onCastleMenu) return false;
    if (!options.onCastleMenu(castleId, opener)) return false;
    hide();
    return true;
  };

  const pointerObserver = scene.onPointerObservable.add((info) => {
    const field = battlefieldOf(scene);
    if (!field) return;
    if (info.type === PointerEventTypes.POINTERDOWN) {
      // Only the right button's press, for the castle menu's right-click (no pick needed).
      const event = info.event as PointerEvent;
      if (event.button === 2 && options.onCastleMenu) {
        menuGestures.down(menuPointer(event, null));
      }
    } else if (info.type === PointerEventTypes.POINTERTAP) {
      const event = info.event as PointerEvent;
      const picked = pickTargetAt(scene);
      if (options.onCastleMenu) {
        const pointer = menuPointer(event, picked);
        if (openCastleMenu(menuGestures.tap(pointer), pointer.castleId)) return;
      }
      const tapped = touchTapTarget(event.pointerType, picked);
      if (tapped === undefined) return;
      hovered = tapped;
      if (tapped) show(field, tapped);
      else hide();
    } else if (info.type === PointerEventTypes.POINTERMOVE) {
      // A finger has no hover: its tooltip stays until the next tap.
      if ((info.event as PointerEvent).pointerType === "touch") return;
      if (info.event.buttons !== 0) return hide();
      const target = pickTargetAt(scene);
      if (!target) return hide();
      hovered = target;
      show(field, target);
    } else if (info.type === PointerEventTypes.POINTERDOUBLETAP) {
      const target = pickTargetAt(scene);
      if (options.onCastleMenu) {
        const pointer = menuPointer(info.event as PointerEvent, target);
        if (openCastleMenu(menuGestures.doubleTap(pointer), pointer.castleId))
          return;
      }
      // For a hand player who plays, a double-click never moves the camera (it opens the castle
      // menu on their own castle, above, and does nothing on any other).
      const status = options.handPlay?.state.get().status ?? null;
      if (!doubleClickFocuses(status)) return;
      const castle =
        target?.kind === "castle"
          ? field.generation?.castles.find((entry) => entry.id === target.id)
          : undefined;
      if (castle) focusCamera(scene, field.camera, castle);
    }
  });
  const onLeave = (event: PointerEvent) => {
    if (event.pointerType !== "touch") hide();
  };
  canvas.addEventListener("pointerleave", onLeave);
  const onKey = (event: KeyboardEvent) => {
    if (
      event.key.toLowerCase() !== "f" ||
      event.ctrlKey ||
      event.metaKey ||
      event.altKey
    )
      return;
    if (
      event.target instanceof HTMLInputElement ||
      event.target instanceof HTMLTextAreaElement
    )
      return;
    fitBoard(scene);
  };
  window.addEventListener("keydown", onKey);
  // The right button belongs to the castle menu (and to panning): no context menu over the board.
  // Only while the hand player plays: a player whose bot plays keeps the browser's menu.
  const noContextMenu = (event: Event) => {
    if (options.handPlay?.state.get().status === "playing") {
      event.preventDefault();
    }
  };
  if (options.onCastleMenu) {
    canvas.addEventListener("contextmenu", noContextMenu);
  }
  // The hovered entity may have moved or gone when a new generation is drawn.
  const frameObserver = scene.onBeforeRenderObservable.add(() => {
    const field = battlefieldOf(scene);
    if (hovered && field && !describeTarget(field, hovered)) hide();
  });
  return () => {
    scene.onPointerObservable.remove(pointerObserver);
    scene.onBeforeRenderObservable.remove(frameObserver);
    canvas.removeEventListener("pointerleave", onLeave);
    window.removeEventListener("keydown", onKey);
    canvas.removeEventListener("contextmenu", noContextMenu);
    tooltip.remove();
    highlight?.dispose();
  };
}
