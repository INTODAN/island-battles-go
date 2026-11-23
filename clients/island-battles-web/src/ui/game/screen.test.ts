// Disposing the game screen removes every listener it added to `window` and `document` (#403):
// a tab that plays many games must not keep one game's handlers, and the closures they hold,
// after the next one starts.
//
// What is real: the screen, the board (a Babylon scene on a `NullEngine` standing in for the
// WebGL engine, whose canvas is the screen's own), the controller, hand play over a store, and
// the page, which is happy-dom's. What is made up: the server behind hand play, which never
// answers, and the board's art, which fails to download (the board then keeps its stand-ins).

import { afterAll, beforeAll, describe, expect, mock, test } from "bun:test";
import { NullEngine } from "@babylonjs/core/Engines/nullEngine";
import { Window } from "happy-dom";

/** Listeners on one target, by type, listener and capture, counted as added minus removed. */
class ListenerLedger {
  private readonly live = new Map<string, number>();
  private readonly ids = new WeakMap<object, number>();
  private next = 1;

  watch(target: EventTarget): void {
    const add = target.addEventListener.bind(target);
    const remove = target.removeEventListener.bind(target);
    const key = (
      type: string,
      listener: unknown,
      options?: boolean | EventListenerOptions,
    ) => {
      const capture =
        typeof options === "boolean" ? options : (options?.capture ?? false);
      let id = this.ids.get(listener as object);
      if (id === undefined) {
        id = this.next++;
        this.ids.set(listener as object, id);
      }
      return `${type}#${id}${capture ? " (capture)" : ""}`;
    };
    target.addEventListener = (type, listener, options) => {
      if (listener) {
        const name = key(type, listener, options);
        // The DOM adds one listener once, however often it is given.
        if (!this.live.has(name)) this.live.set(name, 1);
      }
      add(type, listener, options);
    };
    target.removeEventListener = (type, listener, options) => {
      if (listener) this.live.delete(key(type, listener, options));
      remove(type, listener, options);
    };
  }

  /** The listeners added and not removed. */
  get names(): string[] {
    return [...this.live.keys()].sort();
  }
}

const page = new Window({ url: "http://localhost:3000/" });
const pageGlobals = {
  window: page,
  document: page.document,
  navigator: page.navigator,
  location: page.location,
  localStorage: page.localStorage,
  HTMLElement: page.HTMLElement,
  HTMLInputElement: page.HTMLInputElement,
  HTMLTextAreaElement: page.HTMLTextAreaElement,
  HTMLButtonElement: page.HTMLButtonElement,
  HTMLCanvasElement: page.HTMLCanvasElement,
  Element: page.Element,
  Node: page.Node,
  Event: page.Event,
  KeyboardEvent: page.KeyboardEvent,
  PointerEvent: page.PointerEvent,
  MouseEvent: page.MouseEvent,
  ResizeObserver: page.ResizeObserver,
  requestAnimationFrame: (run: (time: number) => void) =>
    setTimeout(() => run(performance.now()), 16),
  cancelAnimationFrame: (handle: number) => clearTimeout(handle),
} as const;
const saved = new Map<string, PropertyDescriptor | undefined>();

// The WebGL engine, swapped for a `NullEngine` that keeps the screen's canvas, so the board's
// controls attach to it as they do in the browser. A copy of the real module, put back at the end:
// `mock.module` is process-wide.
const realEngine = { ...(await import("@babylonjs/core/Engines/engine")) };
class CanvasNullEngine extends NullEngine {
  constructor(private readonly canvas: HTMLCanvasElement) {
    super();
  }
  override getRenderingCanvas(): HTMLCanvasElement {
    return this.canvas;
  }
  override getInputElement(): HTMLCanvasElement {
    return this.canvas;
  }
}
mock.module("@babylonjs/core/Engines/engine", () => ({
  ...realEngine,
  Engine: CanvasNullEngine,
}));
afterAll(() => {
  mock.module("@babylonjs/core/Engines/engine", () => realEngine);
});

beforeAll(() => {
  for (const [name, value] of Object.entries(pageGlobals)) {
    saved.set(name, Object.getOwnPropertyDescriptor(globalThis, name));
    Object.defineProperty(globalThis, name, {
      value,
      configurable: true,
      writable: true,
    });
  }
  // The art downloads fail at once: the board keeps its stand-ins.
  saved.set("fetch", Object.getOwnPropertyDescriptor(globalThis, "fetch"));
  Object.defineProperty(globalThis, "fetch", {
    value: () => Promise.reject(new Error("no network in this test")),
    configurable: true,
    writable: true,
  });
});
afterAll(async () => {
  for (const [name, descriptor] of saved) {
    if (descriptor) Object.defineProperty(globalThis, name, descriptor);
    else delete (globalThis as Record<string, unknown>)[name];
  }
  await page.happyDOM.close();
});

const { GameScreen } = await import("./screen");
const { LogBook } = await import("./logs");
const { sampleDemo } = await import("./testing");
const { createHandPlay } = await import("../../play");
const { createStore } = await import("../../realtime/store");

/** A hand play whose server never answers: the screen only reads its state. */
function quietHandPlay() {
  return createHandPlay({
    game: createStore(null),
    postCommands: () => new Promise(() => {}),
    applyDeadline: () => new Promise(() => {}),
    readState: () => new Promise(() => {}),
    loadUpgrades: () => new Promise(() => {}),
    adopt: () => {},
    refresh: () => {},
    now: () => Date.now(),
    schedule: (run, ms) => {
      const timer = setTimeout(run, ms);
      return () => clearTimeout(timer);
    },
    newRequestId: () => "request",
  });
}

describe("GameScreen.dispose (#403)", () => {
  for (const hand of [false, true]) {
    test(`removes every window and document listener it added${hand ? ", with hand play" : ""}`, async () => {
      const windowLedger = new ListenerLedger();
      const documentLedger = new ListenerLedger();
      windowLedger.watch(page as unknown as EventTarget);
      documentLedger.watch(page.document as unknown as EventTarget);
      const host = page.document.createElement("div");
      page.document.body.append(host);
      const container = page.document.createElement("div");
      host.append(container);
      const handPlay = hand ? quietHandPlay() : null;
      const errors = console.error;
      console.error = () => {};
      try {
        const screen = new GameScreen(container as unknown as HTMLElement, {
          live: hand,
          logs: new LogBook(),
          onBack: () => {},
          onOpenFile: () => {},
          handPlay,
        });
        const demo = sampleDemo();
        // Two games in a row, as a lobby plays them: the second replaces the first.
        for (let game = 0; game < 2; game++) {
          const controller = screen.start(
            demo.teams,
            demo.frames,
            demo.outcome,
          );
          controller.togglePlay();
          await new Promise((resolve) => setTimeout(resolve, 50));
        }
        expect(
          windowLedger.names.length + documentLedger.names.length,
        ).toBeGreaterThan(0);
        screen.dispose();
        // The art's failed downloads settle after the screen is gone.
        await new Promise((resolve) => setTimeout(resolve, 50));
      } finally {
        console.error = errors;
        handPlay?.dispose();
      }
      expect(windowLedger.names).toEqual([]);
      expect(documentLedger.names).toEqual([]);
      host.remove();
    });
  }
});
