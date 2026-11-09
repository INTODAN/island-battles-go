import { DirectionalLight } from "@babylonjs/core/Lights/directionalLight";
import { HemisphericLight } from "@babylonjs/core/Lights/hemisphericLight";
import type { ShadowGenerator } from "@babylonjs/core/Lights/Shadows/shadowGenerator";
import { Texture } from "@babylonjs/core/Materials/Textures/texture";
import { Color3, Color4 } from "@babylonjs/core/Maths/math.color";
import { Vector3 } from "@babylonjs/core/Maths/math.vector";
import type { AbstractMesh } from "@babylonjs/core/Meshes/abstractMesh";
import type { Scene } from "@babylonjs/core/scene";
import skyUrl from "../../assets/environment/sky.png";
import { SEA_COLOR } from "./art";
import { fetchAsset } from "./assetLoader";
import {
  FILL_INTENSITY,
  GROUND_COLOR,
  SKY_COLOR,
  SUN_COLOR,
  SUN_DIRECTION,
  SUN_INTENSITY,
} from "./lookConstants";
import { createSea, type Sea } from "./sea";
import { distanceField, type WorldPolygon } from "./seaDistance";

// The board's light and sea (issue #102): a warm sun with soft shadows, a cool hemispheric fill,
// the toon sea, and the environment texture (assets/environment/sky.png, made by
// tools/environment/sky.ts) that the sea reflects toward the horizon. The lobby's ocean and the
// game board share it.

/** The environment texture's URL: the build copies it to /play/assets/ with a hashed name. */
export const SKY_URL: string = skyUrl;

/** Meshes whose names start like this cast shadows: castles, boats and props. */
const CASTER = /^(castle-|boat-\d+-(hull|sail)|props-)/;

export interface BoardLook {
  sea: Sea;
  sun: DirectionalLight;
  fill: HemisphericLight;
  /** Shadows from the sun, while they are on. */
  shadows: ShadowGenerator | null;
  setShadows(on: boolean): void;
  /** Draws the shallows and foam around these waterlines (world x and z). */
  setIslands(polygons: readonly WorldPolygon[]): void;
  /** Settles once the environment texture has loaded (or failed to). */
  skyReady: Promise<void>;
}

const looks = new WeakMap<Scene, BoardLook>();

export function lookOf(scene: Scene): BoardLook | undefined {
  return looks.get(scene);
}

/** Sets up the scene's sun, fill, sea and environment texture, once. */
export function createBoardLook(scene: Scene): BoardLook {
  const existing = looks.get(scene);
  if (existing) return existing;
  scene.clearColor = Color4.FromColor3(Color3.FromHexString(SEA_COLOR), 1);
  scene.ambientColor = Color3.Black();
  const fill = new HemisphericLight("fill", new Vector3(0.2, 1, -0.3), scene);
  fill.diffuse = Color3.FromHexString(SKY_COLOR);
  fill.groundColor = Color3.FromHexString(GROUND_COLOR);
  fill.specular = Color3.Black();
  fill.intensity = FILL_INTENSITY;
  const sun = new DirectionalLight(
    "sun",
    new Vector3(...SUN_DIRECTION).normalize(),
    scene,
  );
  sun.diffuse = Color3.FromHexString(SUN_COLOR);
  sun.specular = Color3.Black();
  sun.intensity = SUN_INTENSITY;
  sun.position = sun.direction.scale(-30);
  sun.shadowMinZ = 1;
  sun.shadowMaxZ = 80;
  const sea = createSea(scene);
  let wantShadows = false;

  const look: BoardLook = {
    sea,
    sun,
    fill,
    shadows: null,
    setShadows(on) {
      wantShadows = on;
      if (!on) {
        look.shadows?.dispose();
        look.shadows = null;
        return;
      }
      if (look.shadows) return;
      // The shadow code loads on first use: the low setting never needs it.
      import("./shadows").then(({ createShadows }) => {
        if (!wantShadows || look.shadows || scene.isDisposed) return;
        look.shadows = createShadows(sun, scene, CASTER);
      });
    },
    setIslands(polygons) {
      sea.setField(distanceField(polygons));
    },
    skyReady: Promise.resolve(),
  };
  scene.onNewMeshAddedObservable.add((mesh: AbstractMesh) => {
    if (!CASTER.test(mesh.name)) return;
    look.shadows?.addShadowCaster(mesh, false);
  });
  looks.set(scene, look);
  look.skyReady = loadSky(scene, sea);
  return look;
}

/** Loads the environment texture from the page's own origin; the sea reflects it once loaded. */
function loadSky(scene: Scene, sea: Sea): Promise<void> {
  // A test scene (NullEngine) has nothing to draw it with.
  if (!scene.getEngine().getRenderingCanvas()) return Promise.resolve();
  // The download goes through fetchAsset so the loading screen counts it; the texture then
  // reads the same file.
  return fetchAsset(SKY_URL).then(
    () =>
      new Promise<void>((resolve) => {
        const texture = new Texture(
          SKY_URL,
          scene,
          false,
          false,
          Texture.BILINEAR_SAMPLINGMODE,
          () => {
            sea.setSky(texture);
            resolve();
          },
          () => resolve(),
        );
        texture.name = "environment-sky";
        texture.wrapU = Texture.WRAP_ADDRESSMODE;
        texture.wrapV = Texture.CLAMP_ADDRESSMODE;
      }),
    (error) => {
      console.error("environment texture failed to load", error);
    },
  );
}
