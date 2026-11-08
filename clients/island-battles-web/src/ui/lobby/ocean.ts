import { ArcRotateCamera } from "@babylonjs/core/Cameras/arcRotateCamera";
import { Engine } from "@babylonjs/core/Engines/engine";
import { DirectionalLight } from "@babylonjs/core/Lights/directionalLight";
import { HemisphericLight } from "@babylonjs/core/Lights/hemisphericLight";
import { Color3, Color4 } from "@babylonjs/core/Maths/math.color";
import { Vector3 } from "@babylonjs/core/Maths/math.vector";
import { CreateGround } from "@babylonjs/core/Meshes/Builders/groundBuilder";
import { Scene } from "@babylonjs/core/scene";
import {
  createBoat,
  createCastle,
  createIsland,
  MaterialCache,
  SEA_COLOR,
} from "../../scene/art";
import { teamColorHex } from "../../scene/palette";

export interface Ocean {
  dispose(): void;
}

// A handful of islands, each with a castle in a team colour: [x, z, team].
const islands: [number, number, number][] = [
  [-3.2, 0.6, 1],
  [3.4, -0.8, 2],
  [0.2, 3.6, 3],
  [-1.4, -3.6, 4],
  [5.6, 3.2, 5],
  [-6, -2.6, 6],
];

/**
 * The backdrop of the lobby screens: a few castles on islands and some boats
 * on a calm sea, with the camera slowly circling. It makes no network calls
 * and reads no state.
 */
export function startOcean(canvas: HTMLCanvasElement): Ocean {
  const engine = new Engine(canvas, true);
  const scene = new Scene(engine);
  scene.clearColor = Color4.FromColor3(Color3.FromHexString(SEA_COLOR), 1);
  const materials = new MaterialCache(scene);

  const camera = new ArcRotateCamera(
    "ocean-camera",
    0.4,
    1.0,
    13,
    new Vector3(0, 0, 0.4),
    scene,
  );
  camera.fov = 0.7;
  const sky = new HemisphericLight(
    "ocean-sky",
    new Vector3(0.2, 1, -0.3),
    scene,
  );
  sky.intensity = 0.65;
  sky.groundColor = new Color3(0.4, 0.55, 0.65);
  const sun = new DirectionalLight(
    "ocean-sun",
    new Vector3(-0.4, -1, 0.5),
    scene,
  );
  sun.intensity = 0.7;

  const sea = CreateGround("ocean-sea", { width: 200, height: 200 }, scene);
  sea.material = materials.flat(SEA_COLOR);

  islands.forEach(([x, z, team], index) => {
    const island = createIsland(scene, 100 + index, materials);
    island.position.set(x, 0, z);
    const castle = createCastle(scene, 100 + index, materials);
    castle.root.position.set(x, 0, z);
    const colour = materials.lit(teamColorHex(team));
    castle.roof.material = colour;
    castle.flag.material = colour;
  });

  const boats = islands.slice(0, 4).map(([x, z], index) => {
    const boat = createBoat(scene, 100 + index, materials);
    const radius = 2.2 + index * 0.35;
    const phase = index * 1.7;
    return { boat, x, z, radius, phase };
  });

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
      engine.stopRenderLoop();
      scene.dispose();
      engine.dispose();
    },
  };
}
