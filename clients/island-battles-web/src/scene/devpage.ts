import { Engine } from "@babylonjs/core/Engines/engine";
import { Matrix, Vector3 } from "@babylonjs/core/Maths/math.vector";
import { Scene } from "@babylonjs/core/scene";
import type { components } from "../api/generated/game";
import { type Battlefield, renderGeneration } from "./battlefield";
import { toWorld } from "./coordinates";
import { installBoardControls } from "./picking";

type Demo = components["schemas"]["Demo"];

/** A fixture is a saved demo file from test/fixtures, served by the dev server at /fixtures/. */
export async function loadFixture(name: string): Promise<Demo> {
  if (!/^[\w.-]+\.json$/.test(name)) {
    throw new Error(`fixture "${name}" is not a plain .json file name`);
  }
  const response = await fetch(`/fixtures/${name}`);
  if (!response.ok) {
    throw new Error(`fixture ${name}: HTTP ${response.status}`);
  }
  const demo = (await response.json()) as Demo;
  if (demo.format !== "island-battles-demo" || demo.frames.length === 0) {
    throw new Error(`fixture ${name} is not a demo with frames`);
  }
  return demo;
}

declare global {
  interface Window {
    /** For browser tests of the dev page. */
    battlefieldDebug?: {
      field: Battlefield;
      frameCount: number;
      showFrame: (index: number) => void;
      /** Where a map point is on the page, in CSS pixels, for tests that point at the board. */
      screenPosition: (
        x: number,
        y: number,
        height?: number,
      ) => { x: number; y: number };
    };
  }
}

/**
 * The dev page, `/?fixture=<file>`: one saved game on the 3D board with a frame slider, and no
 * server. `&frame=<n>` starts on the n-th frame.
 */
export async function startFixturePage(
  canvas: HTMLCanvasElement,
  fixtureName: string,
): Promise<void> {
  const bar = document.createElement("div");
  bar.className = "fixture-bar";
  const previous = Object.assign(document.createElement("button"), {
    type: "button",
    textContent: "◀",
    ariaLabel: "Previous frame",
  });
  const next = Object.assign(document.createElement("button"), {
    type: "button",
    textContent: "▶",
    ariaLabel: "Next frame",
  });
  const slider = Object.assign(document.createElement("input"), {
    type: "range",
    min: "0",
    value: "0",
    ariaLabel: "Frame",
  });
  const status = document.createElement("span");
  status.setAttribute("role", "status");
  bar.append(previous, slider, next, status);
  canvas.parentElement?.append(bar);

  let demo: Demo;
  try {
    demo = await loadFixture(fixtureName);
  } catch (error) {
    status.textContent = error instanceof Error ? error.message : String(error);
    throw error;
  }

  const engine = new Engine(canvas, true, { preserveDrawingBuffer: true });
  const scene = new Scene(engine);
  installBoardControls(scene);
  window.addEventListener("resize", () => engine.resize());

  slider.max = String(demo.frames.length - 1);
  let current = 0;
  let field: Battlefield | null = null;
  const showFrame = (index: number) => {
    current = Math.min(demo.frames.length - 1, Math.max(0, index));
    const frame = demo.frames[current];
    if (!frame) return;
    field = renderGeneration(scene, frame, demo.teams);
    slider.value = String(current);
    previous.disabled = current === 0;
    next.disabled = current === demo.frames.length - 1;
    status.textContent = `${fixtureName} · generation ${frame.id} · frame ${current + 1} / ${demo.frames.length}`;
    if (field) {
      window.battlefieldDebug = {
        field,
        frameCount: demo.frames.length,
        showFrame,
        screenPosition: (x, y, height = 0.3) => {
          const world = toWorld({ x, y }, height);
          const projected = Vector3.Project(
            new Vector3(world.x, world.y, world.z),
            Matrix.IdentityReadOnly,
            scene.getTransformMatrix(),
            scene.activeCamera?.viewport.toGlobal(
              engine.getRenderWidth(),
              engine.getRenderHeight(),
            ) as never,
          );
          return { x: projected.x, y: projected.y };
        },
      };
    }
  };
  slider.addEventListener("input", () => showFrame(Number(slider.value)));
  previous.addEventListener("click", () => showFrame(current - 1));
  next.addEventListener("click", () => showFrame(current + 1));
  const start = Number(
    new URLSearchParams(location.search).get("frame") ?? "1",
  );
  showFrame(Number.isFinite(start) ? start - 1 : 0);
  engine.runRenderLoop(() => scene.render());
  engine.resize();
}
