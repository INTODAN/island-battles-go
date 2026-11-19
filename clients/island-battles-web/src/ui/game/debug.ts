// A read-only hook for browser tests of the game view (#343): with `debug=1` in the page's URL,
// `window.gameViewDebug` tells where each castle is on screen, how the camera stands and what the
// board's labels show. Like `window.battlefieldDebug` on the dev page; it changes nothing.

import { Matrix, Vector3 } from "@babylonjs/core/Maths/math.vector";
import type { Scene } from "@babylonjs/core/scene";
import { battlefieldOf } from "../../scene/battlefield";

export interface GameViewDebug {
  /** Each castle of the drawn turn, at its centre in page (client) pixels. */
  castles(): {
    id: number;
    team: number;
    soldiers: number;
    x: number;
    y: number;
  }[];
  camera(): {
    alpha: number;
    beta: number;
    radius: number;
    target: { x: number; y: number; z: number };
  } | null;
  /** Drag to send's meshes (its line, casing and target ring): enabled ones and their opacity. */
  dragMeshes(): { name: string; visibility: number }[];
  /** The board's labels (`LabelLayer.describe`), such as the drag's chip `drag:amount`. */
  labels(): ReturnType<
    NonNullable<ReturnType<typeof battlefieldOf>>["labels"]["describe"]
  >;
}

declare global {
  interface Window {
    gameViewDebug?: GameViewDebug;
  }
}

/** Installs the hook when the URL asks for it; returns the function that removes it. */
export function installGameViewDebug(scene: Scene): () => void {
  if (new URLSearchParams(window.location.search).get("debug") !== "1") {
    return () => {};
  }
  const hook: GameViewDebug = {
    castles() {
      const field = battlefieldOf(scene);
      const canvas = scene.getEngine().getRenderingCanvas();
      const camera = scene.activeCamera;
      if (!field?.generation || !canvas || !camera) return [];
      const engine = scene.getEngine();
      const width = engine.getRenderWidth();
      const height = engine.getRenderHeight();
      const viewport = camera.viewport.toGlobal(width, height);
      const rect = canvas.getBoundingClientRect();
      const scale = rect.width / width;
      return field.generation.castles.flatMap((castle) => {
        const entity = field.castles.get(castle.id);
        if (!entity) return [];
        const projected = Vector3.Project(
          entity.view.root.getAbsolutePosition(),
          Matrix.IdentityReadOnly,
          scene.getTransformMatrix(),
          viewport,
        );
        return [
          {
            id: castle.id,
            team: castle.team_color,
            soldiers: castle.soldiers,
            x: rect.left + projected.x * scale,
            y: rect.top + projected.y * scale,
          },
        ];
      });
    },
    camera() {
      const camera = battlefieldOf(scene)?.camera;
      if (!camera) return null;
      const { x, y, z } = camera.target;
      return {
        alpha: camera.alpha,
        beta: camera.beta,
        radius: camera.radius,
        target: { x, y, z },
      };
    },
    dragMeshes() {
      return scene.meshes
        .filter((mesh) => mesh.name.startsWith("drag-") && mesh.isEnabled())
        .map((mesh) => ({ name: mesh.name, visibility: mesh.visibility }));
    },
    labels() {
      return battlefieldOf(scene)?.labels.describe() ?? [];
    },
  };
  window.gameViewDebug = hook;
  return () => {
    if (window.gameViewDebug === hook) delete window.gameViewDebug;
  };
}
