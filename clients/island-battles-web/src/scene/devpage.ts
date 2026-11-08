import { Engine } from "@babylonjs/core/Engines/engine";
import { Matrix, Vector3 } from "@babylonjs/core/Maths/math.vector";
import { Scene } from "@babylonjs/core/scene";
import type { components } from "../api/generated/game";
import { showBoardLoading } from "../ui/boardLoading";
import { createScenePlayer, DEFAULT_TURN_MS } from "./animation";
import { type Battlefield, battlefieldOf } from "./battlefield";
import { loadBoardAssets } from "./boardAssets";
import { setCastleAnimation } from "./castles";
import { toWorld } from "./coordinates";
import { applyGraphics, type Graphics, savedGraphics } from "./graphics";
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
      /** Shows a frame at once, with no animation. */
      showFrame: (index: number) => void;
      /** Shows the turn into frame `index`, frozen at `progress` (0 to 1) of the turn. */
      scrub: (index: number, progress: number) => void;
      /** Plays the turn into frame `index` from the frame before it. */
      playTurn: (index: number) => void;
      /** Starts or stops playing the fixture on from the frame shown. */
      setPlaying: (playing: boolean) => void;
      /** Fake time for tests: advances the player by `ms` without rendering. */
      advance: (ms: number) => void;
      player: ReturnType<typeof createScenePlayer>["player"];
      /** Where a map point is on the page, in CSS pixels, for tests that point at the board. */
      screenPosition: (
        x: number,
        y: number,
        height?: number,
      ) => { x: number; y: number };
      /** Settles once the board's art has loaded. */
      assets: Promise<void>;
      /** Switches post-processing, shadows and castle loops. */
      setGraphics: (value: Graphics) => Promise<void>;
      /** Counts for one rendered frame, from Babylon's own counters. */
      stats: () => {
        drawCalls: number | null;
        activeMeshes: number;
        triangles: number;
        meshes: number;
        particleSystems: number;
      };
      /**
       * Stops the render loop and makes every later `renderFrame` advance time by `ms`, so a
       * recording is steady however slowly frames render.
       */
      fixedTime: (ms: number) => void;
      renderFrame: () => void;
    };
  }
}

/**
 * The dev page, `/?fixture=<file>`: one saved game on the 3D board with a frame slider, and no
 * server. `&frame=<n>` starts on the n-th frame, `&play=1` plays on from it, `&speed=<x>` sets the
 * pace and `&turn=<ms>` the length of a turn (1500 by default). `&still=1` freezes the level 3
 * and 4 castles' flags and blades in their bind pose. The slider and the arrows jump with no
 * animation; the play button plays the game on, a turn at a time.
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
  const play = Object.assign(document.createElement("button"), {
    type: "button",
    textContent: "Play",
    ariaLabel: "Play the game on",
  });
  const speedBox = Object.assign(document.createElement("select"), {
    ariaLabel: "Speed",
  });
  for (const speed of [0.5, 1, 2, 4]) {
    speedBox.append(
      Object.assign(document.createElement("option"), {
        value: String(speed),
        textContent: `${speed}×`,
        selected: speed === 1,
      }),
    );
  }
  const effects = Object.assign(document.createElement("input"), {
    type: "checkbox",
    ariaLabel: "Effects",
  });
  const effectsLabel = document.createElement("label");
  effectsLabel.append(effects, " Effects");
  bar.append(previous, play, slider, next, speedBox, effectsLabel, status);
  canvas.parentElement?.append(bar);
  const loading = canvas.parentElement
    ? showBoardLoading(canvas.parentElement)
    : null;

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
  const assets = loadBoardAssets(scene, (progress) =>
    loading?.update(progress),
  );
  assets.then(
    () => loading?.done(),
    (error) => {
      console.error("the board's art failed to load", error);
      loading?.failed();
    },
  );

  const query = new URLSearchParams(location.search);
  const fx = query.get("fx");
  let graphics: Graphics =
    fx === "0" ? "low" : fx === "1" ? "high" : savedGraphics();
  effects.checked = graphics === "high";
  const turnMs = Number(query.get("turn") ?? DEFAULT_TURN_MS);
  slider.max = String(demo.frames.length - 1);
  let current = 0;
  /** The last frame queued to play, which is ahead of `current` while a turn plays. */
  let queued = 0;
  let playing = false;
  const last = demo.frames.length - 1;

  const show = (index: number) => {
    current = index;
    const frame = demo.frames[current];
    if (!frame) return;
    slider.value = String(current);
    previous.disabled = current === 0;
    next.disabled = current === last;
    status.textContent = `${fixtureName} · generation ${frame.id} · frame ${current + 1} / ${demo.frames.length}`;
  };
  const { player } = createScenePlayer(scene, demo.teams, {
    turnMs: Number.isFinite(turnMs) && turnMs > 0 ? turnMs : DEFAULT_TURN_MS,
    onShown: (generation) => {
      const index = demo.frames.findIndex(
        (frame) => frame.id === generation.id,
      );
      if (index >= 0) show(index);
    },
  });
  const speed = Number(query.get("speed") ?? "1");
  player.setSpeed(Number.isFinite(speed) ? speed : 1);
  speedBox.value = String(player.speed);

  const clampIndex = (index: number) => Math.min(last, Math.max(0, index));
  const showFrame = (index: number) => {
    const target = clampIndex(index);
    const frame = demo.frames[target];
    if (!frame) return;
    queued = target;
    player.seek(frame);
  };
  const playTurn = (index: number) => {
    const target = clampIndex(index);
    const from = demo.frames[target - 1];
    const to = demo.frames[target];
    if (!from || !to) return;
    queued = target;
    player.play(from, to);
  };
  const scrub = (index: number, progress: number) => {
    const target = clampIndex(index);
    const from = demo.frames[target - 1];
    const to = demo.frames[target];
    if (!from || !to) return;
    queued = target;
    player.scrub(from, to, progress);
  };
  const setPlaying = (value: boolean) => {
    playing = value;
    play.textContent = playing ? "Pause" : "Play";
  };
  const advance = (ms: number) => {
    player.tick(ms);
    if (playing && !player.busy) feed();
  };
  // Keep one turn queued ahead while playing, so a fixture plays on without a break.
  const feed = () => {
    if (!playing) return;
    if (queued >= last) {
      setPlaying(false);
      return;
    }
    playTurn(queued + 1);
  };

  window.battlefieldDebug = {
    get field() {
      const found = battlefieldOf(scene);
      if (!found) throw new Error("nothing is drawn yet");
      return found;
    },
    frameCount: demo.frames.length,
    showFrame,
    scrub,
    playTurn,
    setPlaying,
    advance,
    player,
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
    assets,
    setGraphics: (value) => setGraphics(value),
    stats: () => {
      const counter = (
        engine as unknown as {
          _drawCalls?: { fetchNewFrame(): void; current: number };
        }
      )._drawCalls;
      counter?.fetchNewFrame();
      scene.render();
      return {
        drawCalls: counter?.current ?? null,
        activeMeshes: scene.getActiveMeshes().length,
        triangles: Math.round(scene.getActiveIndices() / 3),
        meshes: scene.meshes.length,
        particleSystems: scene.particleSystems.length,
      };
    },
    fixedTime: (ms) => {
      engine.stopRenderLoop();
      (engine as unknown as { getDeltaTime: () => number }).getDeltaTime = () =>
        ms;
    },
    renderFrame: () => scene.render(),
  };

  slider.addEventListener("input", () => showFrame(Number(slider.value)));
  previous.addEventListener("click", () => showFrame(current - 1));
  next.addEventListener("click", () => showFrame(current + 1));
  play.addEventListener("click", () => {
    if (!playing && queued >= last) showFrame(0);
    setPlaying(!playing);
  });
  speedBox.addEventListener("change", () =>
    player.setSpeed(Number(speedBox.value)),
  );
  const start = Number(query.get("frame") ?? "1");
  showFrame(Number.isFinite(start) ? start - 1 : 0);
  const still = query.get("still") === "1";
  const setGraphics = async (value: Graphics) => {
    graphics = value;
    effects.checked = value === "high";
    const camera = scene.activeCamera;
    const applied = camera ? applyGraphics(scene, camera, value) : undefined;
    // `&still=1` keeps the castles still whatever the setting.
    if (still) setCastleAnimation(false);
    await applied;
  };
  void setGraphics(graphics);
  effects.addEventListener("change", () => {
    void setGraphics(effects.checked ? "high" : "low");
  });
  if (query.get("play") === "1") setPlaying(true);
  scene.onBeforeRenderObservable.add(() => {
    if (playing && !player.busy) feed();
  });
  engine.runRenderLoop(() => scene.render());
  engine.resize();
}
