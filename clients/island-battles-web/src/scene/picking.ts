import "@babylonjs/core/Culling/ray";
import { PointerEventTypes } from "@babylonjs/core/Events/pointerEvents";
import type { Mesh } from "@babylonjs/core/Meshes/mesh";
import type { Scene } from "@babylonjs/core/scene";
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
 * Hover highlight and tooltip, double-click to focus a castle, and F to refit the board. Returns
 * a function that removes the listeners.
 */
export function installBoardControls(scene: Scene): () => void {
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

  const pointerObserver = scene.onPointerObservable.add((info) => {
    const field = battlefieldOf(scene);
    if (!field) return;
    if (info.type === PointerEventTypes.POINTERMOVE) {
      if (info.event.buttons !== 0) return hide();
      const target = pickTargetAt(scene);
      if (!target) return hide();
      hovered = target;
      show(field, target);
    } else if (info.type === PointerEventTypes.POINTERDOUBLETAP) {
      const target = pickTargetAt(scene);
      const castle =
        target?.kind === "castle"
          ? field.generation?.castles.find((entry) => entry.id === target.id)
          : undefined;
      if (castle) focusCamera(scene, field.camera, castle);
    }
  });
  const onLeave = () => hide();
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
    const field = battlefieldOf(scene);
    if (field) fitCamera(scene, field.camera, field.bounds, 450);
  };
  window.addEventListener("keydown", onKey);
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
    tooltip.remove();
    highlight?.dispose();
  };
}
