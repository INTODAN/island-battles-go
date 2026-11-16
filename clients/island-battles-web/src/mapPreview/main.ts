import { Engine } from "@babylonjs/core/Engines/engine";
import { Scene } from "@babylonjs/core/scene";
import { type Battlefield, battlefieldOf, renderGeneration } from "../scene";
import { loadBoardAssets } from "../scene/boardAssets";
import { fitCamera } from "../scene/camera";
import { applyGraphics, savedGraphics } from "../scene/graphics";
import { installBoardControls } from "../scene/picking";
import { showBoardLoading } from "../ui/boardLoading";
import {
  mapFromMessage,
  type Preview,
  previewGeneration,
  previewKey,
  READY_MESSAGE_TYPE,
} from "./preview";

// The map builder's live 3D preview (#278), map-preview.html: the 3D board of the map that the
// builder in the parent window posts, redrawn on every edit with the game's own scene code.

declare global {
  interface Window {
    /** For browser tests of the preview. */
    mapPreviewDebug?: {
      readonly field: Battlefield | undefined;
      /** How many maps have been drawn, the empty board first. */
      readonly draws: number;
      /** Settles once the board's art has loaded. */
      assets: Promise<void>;
    };
  }
}

const canvas = document.getElementById("scene");
if (!(canvas instanceof HTMLCanvasElement)) {
  throw new Error("map-preview.html has no #scene canvas");
}
const note = document.getElementById("note");

function say(text: string): void {
  if (!note) return;
  note.textContent = text;
  note.hidden = text === "";
}

const inFrame = window.parent !== window;

let engine: Engine;
try {
  engine = new Engine(canvas, true, { preserveDrawingBuffer: true });
} catch (error) {
  say("This browser can't draw the 3D preview: it needs WebGL.");
  throw error;
}
const scene = new Scene(engine);
installBoardControls(scene);
const loading = showBoardLoading(document.body);
const assets = loadBoardAssets(scene, (progress) => loading.update(progress));
assets.then(
  () => loading.done(),
  (error) => {
    console.error("the board's art failed to load", error);
    loading.failed();
  },
);

let draws = 0;
let drawnKey = "";
const draw = (preview: Preview) => {
  const key = previewKey(preview);
  if (key === drawnKey) return;
  drawnKey = key;
  draws++;
  renderGeneration(scene, preview.generation, preview.teams);
};

// The empty board makes the camera, so the scene can render before the first map arrives.
draw(previewGeneration({}));
const camera = scene.activeCamera;
if (camera) void applyGraphics(scene, camera, savedGraphics());

window.mapPreviewDebug = {
  get field() {
    return battlefieldOf(scene);
  },
  get draws() {
    return draws;
  },
  assets,
};

window.addEventListener("message", (event) => {
  const map = mapFromMessage(
    event,
    inFrame ? window.parent : null,
    location.origin,
  );
  if (map !== undefined) draw(previewGeneration(map));
});
if (inFrame) {
  window.parent.postMessage({ type: READY_MESSAGE_TYPE }, location.origin);
} else {
  say(
    "This page is the map builder's 3D preview. Open the map builder at /map/builder/ and turn on its 3D preview.",
  );
}

// The builder hides the frame while the preview is off: draw nothing until it shows again.
engine.runRenderLoop(() => {
  if (canvas.clientWidth > 0 && canvas.clientHeight > 0) scene.render();
});
let aspect = 0;
new ResizeObserver(() => {
  if (canvas.clientWidth === 0 || canvas.clientHeight === 0) return;
  engine.resize();
  // A new shape, such as the builder going from side by side to one above the other, needs the
  // board fitted again.
  const field = battlefieldOf(scene);
  const now = engine.getRenderWidth() / Math.max(1, engine.getRenderHeight());
  if (field && Math.abs(now / Math.max(aspect, 1e-6) - 1) > 0.15) {
    aspect = now;
    fitCamera(scene, field.camera, field.bounds);
  }
}).observe(canvas);
