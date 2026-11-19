// Drag to send (#343): the board's side of `src/play/drag.ts`. It feeds the model the pointer,
// key and amount events, holds the camera off the dragged pointer (`holdCameraPointer`), draws
// the line, the target ring and the chip, and gives the order through hand play.
//
// Which pointer events count (read in Babylon 9.33.1):
// - Babylon makes up a POINTERUP for a cancelled pointer, the canvas losing focus and a pen
//   leaving; it reaches the observers before any listener of ours. Only a real `PointerEvent` is a
//   release; the made-up ones cancel.
// - A second mouse button pressed while one is held arrives as a POINTERDOWN, and its release as a
//   POINTERUP, of the same pointer: a release must be of the main button (`button === 0`).
// - The camera sees every event of the held pointer and ignores only its moves, so a second finger
//   pinches and pans from where the fingers really are.

import type { IPointerEvent } from "@babylonjs/core/Events/deviceInputEvents";
import { PointerEventTypes } from "@babylonjs/core/Events/pointerEvents";
import type { Material } from "@babylonjs/core/Materials/material";
import { Matrix } from "@babylonjs/core/Maths/math.vector";
import { Mesh } from "@babylonjs/core/Meshes/mesh";
import type { Scene } from "@babylonjs/core/scene";
import type { HandPlay } from "../play";
import {
  type AmountControl,
  chipText,
  type DragContext,
  type DragEffect,
  type DragEvent,
  type DragState,
  initialDragState,
  sliderView,
  step,
} from "../play/drag";
import { createFlatRing, SEA_COLOR } from "./art";
import {
  anchorFor,
  battlefieldOf,
  HEADING_GROUP,
  setLabel,
  setRibbon,
} from "./battlefield";
import { CASTLE_RADIUS } from "./board";
import { holdCameraPointer } from "./camera";
import { castleLabelHeight } from "./castles";
import { type MapPoint, toMap, toWorld } from "./coordinates";
import { dragLineSegment } from "./dragLine";
import { headingDrawOrder, headingStyle, ribbonGeometry } from "./headingLines";
import { ISLAND_TOP } from "./landforms";
import { teamColorHex, tint } from "./palette";

export interface DragControlsOptions {
  handPlay: HandPlay;
  /** The player's status is `playing` and the view shows the newest turn. Read each frame. */
  canOrder: () => boolean;
  amountControl?: AmountControl | undefined;
  /** Shows a refused order's sentence (`HandSurface.showOrderFeedback`). */
  showOrderFeedback?:
    | ((sentence: string, orderId?: number) => void)
    | undefined;
  /** Hides the hover tooltip and ring: called when a drag starts. */
  hideHover: () => void;
  /** Whether the gesture going on became a drag or was cancelled, so it is no click or tap. */
  setTaken: (taken: boolean) => void;
}

/** A line that stays after its order was sent, dimmed, until its boat's arrow takes over. */
interface Pending {
  meshes: LineMeshes;
  /** The generation the order was given in; the drawn board past it shows the boat. */
  generationId: number;
  /** Turns opened since; a fallback for a generation the board never draws. */
  opened: number;
}

interface LineMeshes {
  line: Mesh;
  casing: Mesh;
}

/** The colour a refused order's line flashes before it goes. */
const REJECTED_COLOR = "#e5402f";
const FLASH_MS = 600;
/** How far beyond the castle's model a fingertip still counts as on it, in world units. */
const CASTLE_TOLERANCE = CASTLE_RADIUS + 0.35;
/** How far past the board's edge the line's end may go, in world units. */
const END_MARGIN = 2;
/** Above every heading arrow, whose ranks are small. */
const LINE_RANK = 100_000;
const CHIP = "drag:amount";
const LINE_HEIGHT = ISLAND_TOP + headingStyle.height;

/** The bit `buttons` has for `button` (pointer events number them differently). */
function buttonBit(button: number): number {
  if (button === 1) return 4;
  if (button === 2) return 2;
  return 1 << button;
}

/** Installs drag to send for a hand player; returns the function that removes it. */
export function installDragControls(
  scene: Scene,
  options: DragControlsOptions,
): () => void {
  const canvas = scene.getEngine().getRenderingCanvas();
  if (!canvas) throw new Error("the scene's engine has no canvas");
  const { handPlay } = options;
  let state: DragState = initialDragState();
  let disposed = false;
  /** Fingers down on the board (never the slider's). */
  const touches = new Set<number>();
  /** Where the held pointer is, in canvas pixels and in the page. */
  let pointer: {
    x: number;
    y: number;
    clientX: number;
    clientY: number;
  } | null = null;
  /** The last point on the board the pointer was over, for a pointer above the horizon. */
  let lastEnd: MapPoint | null = null;
  /** What the line and the target were last worked out for; the same again needs no new work. */
  let drawnKey = "";
  let pickedKey = "";
  let live: LineMeshes | null = null;
  let ring: Mesh | null = null;
  let chip = "";
  let viewKey = "";
  let canOrderNow = options.canOrder();
  let openGeneration = handPlay.state.get().turn?.generationId ?? null;
  const pending = new Map<number, Pending>();
  const flashes: { meshes: LineMeshes; started: number }[] = [];

  const context = (): DragContext => ({
    canOrder: canOrderNow,
    ledger: handPlay.state.get().castles,
  });

  const local = (event: IPointerEvent) => {
    const rect = canvas.getBoundingClientRect();
    return {
      x: event.clientX - rect.left,
      y: event.clientY - rect.top,
      clientX: event.clientX,
      clientY: event.clientY,
    };
  };

  /** The pointer's point on the plane the line is drawn in, in map units; null above the horizon. */
  const boardPoint = (x: number, y: number): MapPoint | null => {
    const field = battlefieldOf(scene);
    if (!field) return null;
    const ray = scene.createPickingRay(x, y, Matrix.Identity(), field.camera);
    if (ray.direction.y > -1e-4) return null;
    const t = (LINE_HEIGHT - ray.origin.y) / ray.direction.y;
    if (t < 0) return null;
    const halfWidth = field.bounds.halfWidth + END_MARGIN;
    const halfDepth = field.bounds.halfDepth + END_MARGIN;
    return toMap({
      x: Math.max(
        -halfWidth,
        Math.min(halfWidth, ray.origin.x + ray.direction.x * t),
      ),
      z: Math.max(
        -halfDepth,
        Math.min(halfDepth, ray.origin.z + ray.direction.z * t),
      ),
    });
  };

  /** The castle under a point of the canvas: its model, or a fingertip just beside it. */
  const castleAt = (x: number, y: number): number | null => {
    const field = battlefieldOf(scene);
    const generation = field?.generation;
    if (!field || !generation) return null;
    const picked = scene.pick(
      x,
      y,
      (mesh) => mesh.isPickable && mesh.metadata?.kind === "castle",
    );
    const id = picked?.hit ? picked.pickedMesh?.metadata?.id : undefined;
    if (typeof id === "number") return id;
    const point = boardPoint(x, y);
    if (!point) return null;
    let best: number | null = null;
    let bestDistance = Infinity;
    for (const castle of generation.castles) {
      const world = toWorld(castle);
      const at = toWorld(point);
      const distance = Math.hypot(world.x - at.x, world.z - at.z);
      if (distance < CASTLE_TOLERANCE && distance < bestDistance) {
        best = castle.id;
        bestDistance = distance;
      }
    }
    return best;
  };

  /** The castle a drag would send to: none under the page's overlays (the timeline, the bar). */
  const targetAt = (point: {
    x: number;
    y: number;
    clientX: number;
    clientY: number;
  }) => {
    if (document.elementFromPoint(point.clientX, point.clientY) !== canvas) {
      return null;
    }
    return castleAt(point.x, point.y);
  };

  const createLine = (): LineMeshes => {
    const line = new Mesh("drag-line", scene);
    const casing = new Mesh("drag-line-casing", scene);
    const order = headingDrawOrder(LINE_RANK);
    for (const mesh of [line, casing]) {
      mesh.isPickable = false;
      mesh.renderingGroupId = HEADING_GROUP;
      mesh.position.y = LINE_HEIGHT;
    }
    line.alphaIndex = order.line;
    casing.alphaIndex = order.casing;
    return { line, casing };
  };

  const disposeLine = (meshes: LineMeshes) => {
    meshes.line.dispose();
    meshes.casing.dispose();
  };

  const lineColor = (): string => {
    const team = handPlay.state.get().teamColor;
    return tint(teamColorHex(team ?? 0), headingStyle.tint);
  };

  const paint = (meshes: LineMeshes, color: string, opacity: number) => {
    const field = battlefieldOf(scene);
    if (!field) return;
    const material: Material = field.materials.overlay(color);
    meshes.line.material = material;
    meshes.casing.material = field.materials.overlay(SEA_COLOR);
    meshes.line.visibility = opacity;
    meshes.casing.visibility = opacity;
  };

  const flash = (meshes: LineMeshes) => {
    paint(meshes, REJECTED_COLOR, 1);
    flashes.push({ meshes, started: performance.now() });
  };

  /** Draws the drag's line for the state now; hides it when there is nothing to draw. */
  const drawLive = () => {
    const field = battlefieldOf(scene);
    const generation = field?.generation;
    const drag = state;
    if (drag.phase !== "dragging" || !field || !generation || !pointer) {
      drawnKey = "";
      live?.line.setEnabled(false);
      live?.casing.setEnabled(false);
      ring?.setEnabled(false);
      return;
    }
    const origin = generation.castles.find(
      (castle) => castle.id === drag.origin,
    );
    const targetId = drag.target;
    const target =
      targetId === null
        ? null
        : (generation.castles.find((castle) => castle.id === targetId) ?? null);
    const end = boardPoint(pointer.x, pointer.y) ?? lastEnd;
    if (end) lastEnd = end;
    const key = JSON.stringify([
      drag.origin,
      targetId,
      end,
      generation.id,
      handPlay.state.get().teamColor,
    ]);
    if (key === drawnKey && live) return;
    drawnKey = key;
    const segment =
      origin && end
        ? dragLineSegment(origin, end, target, generation.castles)
        : null;
    if (!live) live = createLine();
    live.line.setEnabled(segment !== null);
    live.casing.setEnabled(segment !== null);
    if (segment) {
      const shaft = ribbonGeometry(segment, headingStyle);
      setRibbon(live.line, shaft.points, shaft.indices);
      const casing = ribbonGeometry(
        segment,
        headingStyle,
        headingStyle.casingWidth,
      );
      setRibbon(live.casing, casing.points, casing.indices);
      paint(live, lineColor(), 1);
    }
    if (target) {
      if (!ring) {
        // Over the island's top, drawn with the lines so the island never hides it.
        ring = createFlatRing(
          scene,
          "drag-target-ring",
          CASTLE_RADIUS + 0.3,
          0.07,
        );
        ring.material = field.materials.flat("#ffffff");
        ring.renderingGroupId = HEADING_GROUP;
      }
      const world = toWorld(target);
      ring.position.set(world.x, LINE_HEIGHT, world.z);
      ring.setEnabled(true);
    } else {
      ring?.setEnabled(false);
    }
  };

  /** The chip at the origin and the amount bar, when what they show changed. */
  const render = () => {
    const castles = handPlay.state.get().castles;
    const view = canOrderNow ? sliderView(state, castles) : null;
    const key = JSON.stringify(view);
    if (key !== viewKey) {
      viewKey = key;
      options.amountControl?.render(view);
    }
    const field = battlefieldOf(scene);
    const text = chipText(state, castles) ?? "";
    if (text === chip || !field) return;
    chip = text;
    const entity =
      state.phase === "dragging" ? field.castles.get(state.origin) : undefined;
    if (!text || !entity) {
      field.labels.remove(CHIP);
      return;
    }
    const color = teamColorHex(handPlay.state.get().teamColor ?? entity.team);
    setLabel(field, CHIP, {
      lines: [text],
      anchor: anchorFor(
        scene,
        CHIP,
        entity.view.root,
        castleLabelHeight(scene, entity.view.level) + 0.45,
      ),
      accent: color,
      textColor: color,
      fontSize: 15,
      bold: true,
      priority: 200,
    });
  };

  const run = (effect: DragEffect) => {
    if (effect.type === "dropped") {
      if (live) disposeLine(live);
      live = null;
      return;
    }
    // Taken before the order is given: hand play tells its subscribers at once, and the
    // drawing of an idle drag would hide the line.
    const meshes = live;
    live = null;
    const result = handPlay.send(effect.origin, effect.target, effect.amount);
    if (!result.ok) {
      if (meshes) flash(meshes);
      options.showOrderFeedback?.(result.sentence);
      return;
    }
    const { order } = result;
    if (meshes) {
      paint(meshes, lineColor(), headingStyle.heldOpacity);
      pending.set(order.id, {
        meshes,
        generationId: order.generationId,
        opened: 0,
      });
    }
    void result.settled.then((settled) => {
      if (disposed) return;
      const entry = pending.get(settled.id);
      if (settled.state === "rejected") {
        if (entry) {
          pending.delete(settled.id);
          flash(entry.meshes);
        }
        options.showOrderFeedback?.(
          settled.sentence ?? "The order was refused.",
          settled.id,
        );
      } else if (entry) {
        // Hand play gives an order to the next turn when the turn ended before it was sent.
        entry.generationId = settled.generationId;
      }
    });
  };

  const apply = (event: DragEvent) => {
    const before = state;
    const result = step(state, event, context());
    state = result.state;
    options.setTaken(state.taken);
    const field = battlefieldOf(scene);
    if (field && before.held !== state.held) {
      holdCameraPointer(field.camera, state.held);
    }
    if (before.phase !== "dragging" && state.phase === "dragging") {
      // Nothing of an earlier drag carries over.
      lastEnd = null;
      drawnKey = "";
      pickedKey = "";
      options.hideHover();
    }
    for (const effect of result.effects) run(effect);
    if (state.phase !== "dragging") drawLive();
    render();
    return result;
  };

  const pointerObserver = scene.onPointerObservable.add((info) => {
    const event = info.event as IPointerEvent;
    const touch = event.pointerType === "touch";
    if (info.type === PointerEventTypes.POINTERDOWN) {
      if (touch && touches.size > 0) {
        touches.add(event.pointerId);
        apply({ type: "second-pointer" });
        return;
      }
      if (touch) touches.add(event.pointerId);
      const newGesture = touch || event.buttons === buttonBit(event.button);
      const point = local(event);
      if (newGesture && event.button === 0) pointer = point;
      apply({
        type: "press",
        pointerId: event.pointerId,
        point,
        castle: newGesture ? castleAt(point.x, point.y) : null,
        primary: event.button === 0 && newGesture,
        newGesture,
      });
    } else if (info.type === PointerEventTypes.POINTERMOVE) {
      if (event.pointerId !== state.held || state.phase === "idle") return;
      // Safari sends no button change for a second mouse button: a right button held is a cancel.
      if (!touch && (event.buttons & 2) !== 0) {
        apply({ type: "cancel" });
        return;
      }
      pointer = local(event);
      apply({
        type: "move",
        pointerId: event.pointerId,
        point: pointer,
        target: targetAt(pointer),
      });
      drawLive();
    } else if (info.type === PointerEventTypes.POINTERUP) {
      if (touch) touches.delete(event.pointerId);
      const real =
        typeof PointerEvent !== "undefined" && event instanceof PointerEvent;
      const point = real ? local(event) : null;
      apply({
        type: "up",
        pointerId: event.pointerId,
        target: point ? targetAt(point) : null,
        real,
        button: event.button,
      });
    }
  });

  // On the window in the capture phase, so they run before Babylon's own listeners on the canvas
  // make up a release for the same cancel.
  const onCancel = (event: Event) => {
    // A blur is heard from every element on the page; only the window's own means the page lost
    // the focus, and fingers may still be down for any other.
    if (event.type === "blur") {
      if (event.target !== window) return;
      touches.clear();
    }
    if (event instanceof PointerEvent) {
      if (event.pointerType === "touch") touches.delete(event.pointerId);
      // Another pointer's cancel (a second finger on the amount bar) leaves the drag alone.
      if (event.type === "pointercancel" && event.pointerId !== state.held) {
        return;
      }
    }
    if (event.type === "visibilitychange" && !document.hidden) return;
    if (
      event.type === "lostpointercapture" &&
      (event as PointerEvent).pointerId !== state.held
    ) {
      return;
    }
    if (state.phase !== "idle") apply({ type: "cancel" });
  };
  const cancelEvents = [
    "pointercancel",
    "lostpointercapture",
    "blur",
    "visibilitychange",
  ] as const;
  for (const name of cancelEvents) {
    window.addEventListener(name, onCancel, { capture: true });
  }

  const onKey = (event: KeyboardEvent) => {
    if (state.phase === "idle") return;
    if (event.key !== "Escape") {
      if (event.ctrlKey || event.metaKey || event.altKey) return;
      const focus = event.target;
      if (
        focus instanceof HTMLInputElement ||
        focus instanceof HTMLTextAreaElement ||
        focus instanceof HTMLSelectElement ||
        (focus instanceof HTMLElement && focus.isContentEditable)
      ) {
        return;
      }
      // w and s repeat on their own while held (`tick`), not at the keyboard's pace.
      if (event.repeat && state.phase === "dragging") {
        if (/^[wsWS]$/.test(event.key)) event.preventDefault();
        return;
      }
    }
    const result = apply({
      type: "key",
      key: event.key,
      now: performance.now(),
    });
    if (result.handled) event.preventDefault();
  };
  const onKeyUp = (event: KeyboardEvent) => {
    if (state.phase === "dragging") apply({ type: "key-up", key: event.key });
  };
  window.addEventListener("keydown", onKey, { capture: true });
  window.addEventListener("keyup", onKeyUp, { capture: true });

  const stopAmount = options.amountControl?.onChange((amount) => {
    apply({ type: "amount", amount });
  });

  const stopState = handPlay.state.subscribe((play) => {
    const opened = play.turn?.generationId ?? null;
    if (opened !== openGeneration) {
      openGeneration = opened;
      for (const [id, entry] of pending) {
        entry.opened += 1;
        // The board never drew the boat (scrubbed, or the game ended): the line goes.
        if (entry.opened >= 3) {
          disposeLine(entry.meshes);
          pending.delete(id);
        }
      }
    }
    apply({ type: "context" });
  });

  const frameObserver = scene.onBeforeRenderObservable.add(() => {
    const now = performance.now();
    const can = options.canOrder();
    if (can !== canOrderNow) {
      canOrderNow = can;
      apply({ type: "context" });
      if (!can) {
        for (const entry of pending.values()) disposeLine(entry.meshes);
        pending.clear();
      }
    }
    if (state.phase === "dragging") {
      if (state.hold) apply({ type: "tick", now });
      const field = battlefieldOf(scene);
      if (pointer && field && state.phase === "dragging") {
        // The camera may have zoomed under a still pointer, or the canvas been resized under it
        // (a phone turned around, the logs opened): look again only when the pointer, the camera,
        // the canvas size or the drawn board changed.
        const { alpha, beta, radius, target } = field.camera;
        const engine = scene.getEngine();
        const key = JSON.stringify([
          engine.getRenderWidth(),
          engine.getRenderHeight(),
          pointer.clientX,
          pointer.clientY,
          alpha,
          beta,
          radius,
          target.x,
          target.z,
          field.generation?.id,
        ]);
        if (key !== pickedKey) {
          pickedKey = key;
          apply({ type: "target", target: targetAt(pointer) });
        }
      }
      drawLive();
    }
    const drawn = battlefieldOf(scene)?.generation?.id;
    for (const [id, entry] of pending) {
      if (drawn !== undefined && drawn > entry.generationId) {
        disposeLine(entry.meshes);
        pending.delete(id);
      }
    }
    for (let index = flashes.length - 1; index >= 0; index--) {
      const entry = flashes[index];
      if (!entry) continue;
      const progress = (now - entry.started) / FLASH_MS;
      if (progress >= 1) {
        disposeLine(entry.meshes);
        flashes.splice(index, 1);
      } else {
        entry.meshes.line.visibility = 1 - progress;
        entry.meshes.casing.visibility = 1 - progress;
      }
    }
  });

  render();

  return () => {
    if (disposed) return;
    if (state.phase !== "idle") apply({ type: "cancel" });
    disposed = true;
    scene.onPointerObservable.remove(pointerObserver);
    scene.onBeforeRenderObservable.remove(frameObserver);
    for (const name of cancelEvents) {
      window.removeEventListener(name, onCancel, { capture: true });
    }
    window.removeEventListener("keydown", onKey, { capture: true });
    window.removeEventListener("keyup", onKeyUp, { capture: true });
    stopAmount?.();
    stopState();
    options.amountControl?.render(null);
    const field = battlefieldOf(scene);
    if (field) {
      holdCameraPointer(field.camera, null);
      field.labels.remove(CHIP);
    }
    if (live) disposeLine(live);
    for (const entry of pending.values()) disposeLine(entry.meshes);
    for (const entry of flashes) disposeLine(entry.meshes);
    pending.clear();
    flashes.length = 0;
    ring?.dispose();
  };
}
