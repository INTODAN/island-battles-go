import { ArcRotateCamera } from "@babylonjs/core/Cameras/arcRotateCamera";
import { Engine } from "@babylonjs/core/Engines/engine";
import { HemisphericLight } from "@babylonjs/core/Lights/hemisphericLight";
import { StandardMaterial } from "@babylonjs/core/Materials/standardMaterial";
import { Color3, Color4 } from "@babylonjs/core/Maths/math.color";
import { Vector3 } from "@babylonjs/core/Maths/math.vector";
import { CreateGround } from "@babylonjs/core/Meshes/Builders/groundBuilder";
import { Scene } from "@babylonjs/core/scene";

const canvas = document.getElementById("scene");
if (!(canvas instanceof HTMLCanvasElement)) {
  throw new Error("index.html has no #scene canvas");
}

// The dev page for the 3D board: /?fixture=<file> draws a saved game from test/fixtures, no server.
const fixture = new URLSearchParams(location.search).get("fixture");

function startScaffold(canvas: HTMLCanvasElement) {
  const engine = new Engine(canvas, true);
  const scene = new Scene(engine);
  scene.clearColor = new Color4(0.06, 0.12, 0.17, 1);

  const camera = new ArcRotateCamera(
    "camera",
    -Math.PI / 2,
    Math.PI / 3,
    24,
    Vector3.Zero(),
    scene,
  );
  camera.attachControl(canvas, true);

  new HemisphericLight("light", new Vector3(0.3, 1, 0.2), scene);
  const ground = CreateGround("ground", { width: 16, height: 16 }, scene);
  const groundMaterial = new StandardMaterial("ground-material", scene);
  groundMaterial.diffuseColor = new Color3(0.3, 0.6, 0.4);
  groundMaterial.specularColor = Color3.Black();
  ground.material = groundMaterial;

  engine.runRenderLoop(() => scene.render());
  window.addEventListener("resize", () => engine.resize());
}

// Dev entry points for the game view (src/ui/game/devpage.ts) until #98 wires it into the app.
const gameLobby = new URLSearchParams(location.search).get("game");
const demoFile = new URLSearchParams(location.search).get("demo");

if (gameLobby !== null) {
  const { startGamePage } = await import("./ui/game/devpage");
  startGamePage(gameLobby);
} else if (demoFile !== null) {
  const { startDemoPage } = await import("./ui/game/devpage");
  await startDemoPage(demoFile);
} else if (fixture !== null) {
  const { startFixturePage } = await import("./scene/devpage");
  await startFixturePage(canvas, fixture);
} else {
  startScaffold(canvas);
}
