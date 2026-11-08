import { ArcRotateCamera } from "@babylonjs/core/Cameras/arcRotateCamera";
import { Engine } from "@babylonjs/core/Engines/engine";
import { Vector3 } from "@babylonjs/core/Maths/math.vector";
import { Scene } from "@babylonjs/core/scene";
import {
  createBoat,
  createIsland,
  createPropMeshes,
  MaterialCache,
  placeProps,
} from "../../scene/art";
import { loadBoardAssets } from "../../scene/boardAssets";
import { createCastleView } from "../../scene/castles";
import { applyGraphics, savedGraphics } from "../../scene/graphics";
import { islandOutline, islandProps, waterline } from "../../scene/landforms";
import { createBoardLook } from "../../scene/look";

export interface Ocean {
  dispose(): void;
}

// A handful of islands, each with a castle in a team colour: [x, z, team, castle level].
const islands: [number, number, number, number][] = [
  [-3.2, 0.6, 1, 4],
  [3.4, -0.8, 2, 3],
  [0.2, 3.6, 3, 2],
  [-1.4, -3.6, 4, 1],
  [5.6, 3.2, 5, 2],
  [-6, -2.6, 6, 3],
];

/**
 * The backdrop of the lobby screens: a few castles on islands and some boats on the board's
 * sea, with the camera slowly circling. It makes no calls but for the board's art, which loads
 * after the screens are up; the canvas fades in once it has. It reads no state.
 */
export function startOcean(canvas: HTMLCanvasElement): Ocean {
  const engine = new Engine(canvas, true);
  const scene = new Scene(engine);
  const look = createBoardLook(scene);
  const materials = new MaterialCache(scene);
  canvas.classList.add("ocean-loading");

  const camera = new ArcRotateCamera(
    "ocean-camera",
    0.4,
    1.0,
    13,
    new Vector3(0, 0, 0.4),
    scene,
  );
  camera.fov = 0.7;

  const castles = islands.map(([x, z], index) => ({
    id: 100 + index,
    x,
    y: -z,
  }));
  islands.forEach(([x, z, team, level], index) => {
    const island = createIsland(scene, 100 + index, materials);
    island.position.set(x, 0, z);
    const castle = createCastleView(
      scene,
      100 + index,
      level,
      team,
      materials,
      null,
    );
    castle.root.position.set(x, 0, z);
  });
  look.setIslands(
    islands.map(([x, z], index) =>
      waterline(islandOutline(100 + index)).map(
        ([lx, lz]) => [x + lx, z + lz] as const,
      ),
    ),
  );
  placeProps(createPropMeshes(scene, materials), islandProps(castles));

  const boats = islands.slice(0, 4).map(([x, z, team], index) => {
    const boat = createBoat(scene, 100 + index, materials, team);
    const radius = 2.2 + index * 0.35;
    const phase = index * 1.7;
    return { boat, x, z, radius, phase };
  });

  void applyGraphics(scene, camera, savedGraphics());
  loadBoardAssets(scene).then(
    () => canvas.classList.remove("ocean-loading"),
    () => canvas.classList.remove("ocean-loading"),
  );

  const started = performance.now();
  scene.onBeforeRenderObservable.add(() => {
    const seconds = (performance.now() - started) / 1000;
    camera.alpha = 0.4 + seconds * 0.045;
    for (const { boat, x, z, radius, phase } of boats) {
      const angle = seconds * 0.25 + phase;
      boat.root.position.set(
        x + Math.cos(angle) * radius,
        Math.sin(seconds * 1.6 + phase) * 0.02,
        z + Math.sin(angle) * radius,
      );
      // The bow points along +z, so turn it to the direction of travel.
      boat.root.rotation.y = Math.atan2(-Math.sin(angle), Math.cos(angle));
    }
  });

  engine.runRenderLoop(() => scene.render());
  const onResize = () => engine.resize();
  window.addEventListener("resize", onResize);

  return {
    dispose() {
      window.removeEventListener("resize", onResize);
      canvas.classList.remove("ocean-loading");
      engine.stopRenderLoop();
      scene.dispose();
      engine.dispose();
    },
  };
}
