// The board's side of drag to send (#343, #373): `installDragControls` and the guards of
// `installBoardControls`, driven by the real Babylon camera input in a `NullEngine`.
//
// What is real: the scene, the camera with `BoardPointersInput`, the pointer observable, picking,
// the drag model and the controls. What is made up: the page (`window`, `document`,
// `PointerEvent`), the battlefield (`./battlefield` is mocked to hand out a small field of three
// castles), and hand play, which records the orders it is given. The castles stand at (-3, 0),
// (3, 0) and (0, 3.5) in world units; castles 1 and 2 are yours, 8 is an enemy's.

import {
  afterAll,
  afterEach,
  beforeEach,
  describe,
  expect,
  mock,
  test,
} from "bun:test";
import { ArcRotateCamera } from "@babylonjs/core/Cameras/arcRotateCamera";
import { NullEngine } from "@babylonjs/core/Engines/nullEngine";
import {
  PointerEventTypes,
  PointerInfo,
} from "@babylonjs/core/Events/pointerEvents";
import { Matrix, Vector3 } from "@babylonjs/core/Maths/math.vector";
import "@babylonjs/core/Meshes/Builders/boxBuilder";
import { CreateBox } from "@babylonjs/core/Meshes/Builders/boxBuilder";
import { TransformNode } from "@babylonjs/core/Meshes/transformNode";
import { Scene } from "@babylonjs/core/scene";
import "@babylonjs/core/Culling/ray";
import { BoardPointersInput } from "./camera";
import { toMap } from "./coordinates";

const fields = new WeakMap<object, unknown>();
// A copy: Bun updates the module's own namespace when it is mocked.
const realBattlefield = { ...(await import("./battlefield")) };
/** How many times the line's ribbon was written. */
let ribbonWrites = 0;
mock.module("./battlefield", () => ({
  ...realBattlefield,
  setRibbon: (...args: Parameters<typeof realBattlefield.setRibbon>) => {
    ribbonWrites++;
    return realBattlefield.setRibbon(...args);
  },
  battlefieldOf: (scene: object) => fields.get(scene),
  setLabel: () => {},
  anchorFor: () => null,
}));
// `mock.module` is process-wide and `bun test` runs every file in one process: put the real module
// back when this file is done, so no other file sees the mock.
afterAll(() => {
  mock.module("./battlefield", () => realBattlefield);
});
const { installDragControls } = await import("./dragControls");
const { installBoardControls } = await import("./picking");

/** Stands in for the browser's `PointerEvent`, so `instanceof PointerEvent` can tell a real one. */
class FakePointerEvent {
  constructor(init: object) {
    Object.assign(this, init);
  }
  preventDefault() {}
}

const { POINTERDOWN, POINTERMOVE, POINTERUP, POINTERTAP, POINTERDOUBLETAP } =
  PointerEventTypes;

const WIDTH = 800;
const HEIGHT = 600;
const CASTLES: Record<number, { x: number; z: number }> = {
  1: { x: -3, z: 0 },
  2: { x: 3, z: 0 },
  8: { x: 0, z: 3.5 },
};
const MINE = 1;
const ENEMY = 8;
const REJECTED_COLOR = "#e5402f";

type Listener = (event: unknown) => void;
type Order = [origin: number, target: number, amount: unknown];

const globals = globalThis as Record<string, unknown>;
const saved: Record<string, unknown> = {};
const windowListeners = new Map<string, Set<Listener>>();

/** Puts the page's globals in place for one test; `restoreGlobals` puts them back. */
function installGlobals(canvas: object) {
  for (const name of [
    "PointerEvent",
    "HTMLInputElement",
    "HTMLTextAreaElement",
    "HTMLSelectElement",
    "HTMLElement",
    "document",
    "window",
  ]) {
    saved[name] = globals[name];
  }
  globals.PointerEvent = FakePointerEvent;
  for (const name of [
    "HTMLInputElement",
    "HTMLTextAreaElement",
    "HTMLSelectElement",
    "HTMLElement",
  ]) {
    globals[name] = class {};
  }
  const tooltip = {
    hidden: true,
    className: "",
    style: {},
    offsetWidth: 100,
    offsetHeight: 40,
    textContent: "",
    replaceChildren() {},
    remove() {},
  };
  globals.document = {
    hidden: false,
    elementFromPoint: () => canvas,
    createElement: () => tooltip,
    addEventListener() {},
    removeEventListener() {},
  };
  windowListeners.clear();
  globals.window = {
    addEventListener: (name: string, listener: Listener) => {
      if (!windowListeners.has(name)) windowListeners.set(name, new Set());
      windowListeners.get(name)?.add(listener);
    },
    removeEventListener: (name: string, listener: Listener) => {
      windowListeners.get(name)?.delete(listener);
    },
  };
}

function restoreGlobals() {
  for (const [name, value] of Object.entries(saved)) {
    if (value === undefined) delete globals[name];
    else globals[name] = value;
  }
}

let scenes: Scene[] = [];
beforeEach(() => {
  scenes = [];
});
afterEach(() => {
  for (const scene of scenes) scene.getEngine().dispose();
  restoreGlobals();
});

interface BoardOptions {
  /** What `canOrder` answers; the test may change it. */
  live?: boolean;
  /** Install the board's controls (with a castle menu) instead of the drag's alone. */
  board?: boolean;
}

/** A scene with a camera, the three castles, a hand player and the drag's controls installed. */
function setup(options: BoardOptions = {}) {
  const engine = new NullEngine({
    renderWidth: WIDTH,
    renderHeight: HEIGHT,
    textureSize: 512,
    deterministicLockstep: false,
    lockstepMaxSteps: 1,
  });
  const scene = new Scene(engine);
  scenes.push(scene);
  const camera = new ArcRotateCamera(
    "camera",
    -Math.PI / 2,
    0.96,
    15,
    Vector3.Zero(),
    scene,
  );
  camera.inputs.removeByType("ArcRotateCameraPointersInput");
  const input = new BoardPointersInput();
  camera.inputs.add(input);
  camera.pinchDeltaPercentage = 0.005;
  camera.upperBetaLimit = 1.3;
  camera.lowerBetaLimit = 0.45;
  camera.panningSensibility = 1500;
  camera.attachControl(true);
  const canvas = {
    getBoundingClientRect: () => ({
      left: 0,
      top: 0,
      width: WIDTH,
      height: HEIGHT,
    }),
    parentElement: { append() {} },
    addEventListener() {},
    removeEventListener() {},
  };
  (
    engine as unknown as { getRenderingCanvas: () => unknown }
  ).getRenderingCanvas = () => canvas;
  installGlobals(canvas);

  const castles = Object.entries(CASTLES).map(([id, world]) => ({
    id: Number(id),
    ...toMap(world),
    // What a castle's tooltip reads.
    team_color: 1,
    soldiers: 10,
    upgrades: {},
    burst_debt: 0,
  }));
  const overlayColors: string[] = [];
  const field = {
    scene,
    camera,
    bounds: { halfWidth: 6, halfDepth: 4 },
    teams: [{ color: 1, name: "Blue" }],
    generation: { id: 10, castles },
    castles: new Map(
      castles.map((castle) => [
        castle.id,
        {
          team: 1,
          view: { root: new TransformNode("root", scene), level: 1 },
        },
      ]),
    ),
    labels: { remove() {} },
    materials: {
      overlay: (color: string) => {
        overlayColors.push(color);
        return null;
      },
      flat: () => null,
    },
  };
  fields.set(scene, field);
  // A pickable model under each castle, as the real board has.
  for (const [id, world] of Object.entries(CASTLES)) {
    const model = CreateBox(`castle-${id}`, { size: 1 }, scene);
    model.position.set(world.x, 0.3, world.z);
    model.metadata = { kind: "castle", id: Number(id) };
  }
  scene.render();

  /** Where a castle is on the canvas, in pixels. */
  const at = (id: number) => {
    const world = CASTLES[id] as { x: number; z: number };
    const point = Vector3.Project(
      new Vector3(world.x, 0.3, world.z),
      Matrix.Identity(),
      scene.getTransformMatrix(),
      camera.viewport.toGlobal(WIDTH, HEIGHT),
    );
    return { x: point.x, y: point.y };
  };

  // Hand play: your castles' soldiers, the turn that is open, and the orders given.
  const ledger = new Map([
    [1, { id: 1, soldiers: 23, unreserved: 23 }],
    [2, { id: 2, soldiers: 5, unreserved: 5 }],
  ]);
  const subscribers = new Set<(state: unknown) => void>();
  let openTurn = 10;
  const playState = () => ({
    status: "playing",
    teamColor: 1,
    turn: { generationId: openTurn },
    castles: ledger,
  });
  const notify = () => {
    for (const subscriber of subscribers) subscriber(playState());
  };
  const orders: Order[] = [];
  let refusal: string | null = null;
  let settle: (outcome: unknown) => void = () => {};
  const handPlay = {
    state: {
      get: playState,
      subscribe: (listener: (state: unknown) => void) => {
        subscribers.add(listener);
        return () => subscribers.delete(listener);
      },
    },
    send: (origin: number, target: number, amount: unknown) => {
      orders.push([origin, target, amount]);
      notify();
      if (refusal !== null) return { ok: false, sentence: refusal };
      const order = {
        id: orders.length,
        generationId: openTurn,
        state: "sending",
      };
      return {
        ok: true,
        order,
        settled: new Promise((resolve) => {
          settle = resolve;
        }),
      };
    },
  };

  let live = options.live ?? true;
  const feedback: [string, number | undefined][] = [];
  const taken: boolean[] = [];
  const bars: unknown[] = [];
  const menus: number[] = [];
  const controls = {
    handPlay: handPlay as never,
    canOrder: () => live,
    amountControl: {
      render: (view: unknown) => bars.push(view),
      onChange: () => () => {},
    },
    showOrderFeedback: (sentence: string, id?: number) =>
      feedback.push([sentence, id]),
  };
  const remove = options.board
    ? installBoardControls(scene, {
        ...controls,
        onCastleMenu: (id) => {
          menus.push(id);
          return true;
        },
      })
    : installDragControls(scene, {
        ...controls,
        hideHover: () => {},
        setTaken: (value) => taken.push(value),
      });

  /** A pointer event as Babylon's input manager hands it to the observers. */
  const pointer = (
    type: number,
    init: Record<string, unknown>,
    real = true,
  ) => {
    const fields = {
      pointerType: "mouse",
      pointerId: 1,
      button: type === POINTERMOVE ? -1 : 0,
      buttons: 1,
      target: null,
      ...init,
    };
    const event = real
      ? new FakePointerEvent(fields)
      : { ...fields, preventDefault() {} };
    scene.onPointerObservable.notifyObservers(
      new PointerInfo(type, event as never, null),
      type,
    );
  };
  /** Fires a window listener of ours (capture phase), as the browser does. */
  const windowEvent = (name: string, init: Record<string, unknown> = {}) => {
    const event = Object.assign(init, {
      type: name,
      defaultPrevented: false,
      preventDefault() {
        event.defaultPrevented = true;
      },
    });
    for (const listener of [...(windowListeners.get(name) ?? [])]) {
      listener(event);
    }
    return event;
  };
  /** The camera after the inertia of what it was fed has run out. */
  const camera_ = () => {
    for (let frame = 0; frame < 60; frame++) scene.render();
    return {
      alpha: +camera.alpha.toFixed(4),
      beta: +camera.beta.toFixed(4),
      radius: +camera.radius.toFixed(3),
      x: +camera.target.x.toFixed(3),
      z: +camera.target.z.toFixed(3),
    };
  };
  /** The drag's meshes that are showing (the target ring stays, disabled, when unused). */
  const drawn = () =>
    scene.meshes
      .filter((mesh) => mesh.name.startsWith("drag-") && mesh.isEnabled())
      .map((mesh) => mesh.name);

  return {
    scene,
    field,
    at,
    pointer,
    windowEvent,
    orders,
    feedback,
    taken,
    bars,
    menus,
    overlayColors,
    remove,
    drawn,
    camera: camera_,
    held: () => input.held,
    openTurn: (id: number) => {
      openTurn = id;
      notify();
    },
    loseCastle: (id: number) => {
      ledger.delete(id);
      notify();
    },
    drawTurn: (id: number) => {
      field.generation = { ...field.generation, id };
      scene.render();
    },
    refuseWith: (sentence: string | null) => {
      refusal = sentence;
    },
    settle: (outcome: unknown) => settle(outcome),
    setLive: (value: boolean) => {
      live = value;
      scene.render();
    },
  };
}

type Board = ReturnType<typeof setup>;

/** Where the pointer is, offset by pixels. */
const at = (point: { x: number; y: number }, dx = 0, dy = 0) => ({
  clientX: point.x + dx,
  clientY: point.y + dy,
});
const released = (point: { clientX: number; clientY: number }) => ({
  ...point,
  buttons: 0,
});

/** A mouse drag from your castle over the enemy's, the button still down. */
function dragOverEnemy(board: Board) {
  const origin = board.at(MINE);
  board.pointer(POINTERDOWN, at(origin));
  board.pointer(POINTERMOVE, at(origin, 20, 0));
  board.pointer(POINTERMOVE, at(board.at(ENEMY)));
}

const settled = () => Promise.resolve().then(() => Promise.resolve());

describe("a drag and the camera", () => {
  test("a release over a castle sends, and the camera never turned", () => {
    const board = setup();
    const before = board.camera();
    dragOverEnemy(board);
    expect(board.drawn()).toEqual([
      "drag-line",
      "drag-line-casing",
      "drag-target-ring",
    ]);
    board.pointer(POINTERUP, released(at(board.at(ENEMY))));
    expect(board.orders).toEqual([[MINE, ENEMY, { share: 1 }]]);
    expect(board.camera()).toEqual(before);
    expect(board.held()).toBeNull();
  });

  test("Esc with the button still down: no send, and the held pointer's moves still don't turn the camera", () => {
    const board = setup();
    const origin = board.at(MINE);
    const before = board.camera();
    board.pointer(POINTERDOWN, at(origin));
    board.pointer(POINTERMOVE, at(origin, 150, 0));
    const pressed = board.windowEvent("keydown", {
      key: "Escape",
      target: null,
    });
    expect(pressed.defaultPrevented).toBe(true);
    expect(board.drawn()).toEqual([]);
    board.pointer(POINTERMOVE, at(origin, 151, 0));
    board.pointer(POINTERMOVE, at(board.at(ENEMY)));
    expect(board.camera()).toEqual(before);
    expect(board.held()).toBe(1);
    board.pointer(POINTERUP, released(at(board.at(ENEMY))));
    expect(board.orders).toEqual([]);
    expect(board.held()).toBeNull();
    // The gesture stays taken to its release, so it is no click.
    expect(board.taken.at(-1)).toBe(true);
  });

  test("after the cancelled gesture is released, a drag over the sea turns the camera again", () => {
    const board = setup();
    const origin = board.at(MINE);
    const before = board.camera();
    board.pointer(POINTERDOWN, at(origin));
    board.pointer(POINTERMOVE, at(origin, 150, 0));
    board.windowEvent("keydown", { key: "Escape", target: null });
    board.pointer(POINTERUP, released(at(origin, 150, 0)));
    const sea = { clientX: 5, clientY: 590 };
    board.pointer(POINTERDOWN, sea);
    board.pointer(POINTERMOVE, { clientX: 205, clientY: 590 });
    board.pointer(POINTERUP, { clientX: 205, clientY: 590, buttons: 0 });
    expect(board.camera().alpha).not.toBe(before.alpha);
    expect(board.taken.at(-1)).toBe(false);
  });
});

describe("what counts as a release", () => {
  test("a release Babylon made up, for a cancelled pointer or a lost focus, never sends", () => {
    const board = setup();
    dragOverEnemy(board);
    board.pointer(POINTERUP, released(at(board.at(ENEMY))), false);
    expect(board.orders).toEqual([]);
    expect(board.held()).toBeNull();
    expect(board.drawn()).toEqual([]);
  });

  test("only the main button's release sends", () => {
    const board = setup();
    dragOverEnemy(board);
    // A second button pressed and let go arrives as a press and a release of the same pointer.
    board.pointer(POINTERUP, { ...at(board.at(ENEMY)), button: 1, buttons: 1 });
    expect(board.orders).toEqual([]);
  });
});

describe("a second mouse button", () => {
  test("the right button pressed during a drag drops it, and the main button's release sends nothing", () => {
    const board = setup();
    const before = board.camera();
    dragOverEnemy(board);
    const enemy = at(board.at(ENEMY));
    board.pointer(POINTERDOWN, { ...enemy, button: 2, buttons: 3 });
    expect(board.drawn()).toEqual([]);
    board.pointer(POINTERMOVE, { ...at(board.at(ENEMY), 30, 0), buttons: 1 });
    board.pointer(POINTERUP, { ...enemy, button: 2, buttons: 1 });
    expect(board.held()).toBe(1);
    board.pointer(POINTERMOVE, { ...at(board.at(ENEMY), 100, 0), buttons: 1 });
    board.pointer(POINTERMOVE, { ...enemy, buttons: 1 });
    board.pointer(POINTERUP, { ...enemy, button: 0, buttons: 0 });
    expect(board.orders).toEqual([]);
    expect(board.camera()).toEqual(before);
    expect(board.taken.at(-1)).toBe(true);
  });

  test("a right button seen only in a move's buttons (Safari) drops the drag too", () => {
    const board = setup();
    dragOverEnemy(board);
    board.pointer(POINTERMOVE, { ...at(board.at(ENEMY), 5, 0), buttons: 3 });
    expect(board.drawn()).toEqual([]);
    board.pointer(POINTERUP, released(at(board.at(ENEMY))));
    expect(board.orders).toEqual([]);
  });
});

describe("a second finger", () => {
  const touch = { pointerType: "touch" };

  test("drops the drag and lets the camera have the first finger back", () => {
    const board = setup();
    const origin = board.at(MINE);
    board.pointer(POINTERDOWN, { ...touch, pointerId: 5, ...at(origin) });
    board.pointer(POINTERMOVE, {
      ...touch,
      pointerId: 5,
      ...at(origin, 300, 0),
    });
    expect(board.drawn().length).toBeGreaterThan(0);
    expect(board.held()).toBe(5);
    board.pointer(POINTERDOWN, {
      ...touch,
      pointerId: 6,
      ...at(board.at(2)),
    });
    expect(board.drawn()).toEqual([]);
    expect(board.held()).toBeNull();
    board.pointer(POINTERUP, {
      ...touch,
      pointerId: 6,
      ...at(origin, 300, 200),
      buttons: 0,
    });
    board.pointer(POINTERUP, {
      ...touch,
      pointerId: 5,
      ...at(board.at(ENEMY)),
      buttons: 0,
    });
    expect(board.orders).toEqual([]);
  });

  test("once both are up a finger on your castle drags again", () => {
    const board = setup();
    const origin = board.at(MINE);
    board.pointer(POINTERDOWN, { ...touch, pointerId: 5, ...at(origin) });
    board.pointer(POINTERDOWN, {
      ...touch,
      pointerId: 6,
      ...at(origin, 300, 200),
    });
    board.pointer(POINTERUP, {
      ...touch,
      pointerId: 6,
      ...at(origin, 300, 200),
      buttons: 0,
    });
    board.pointer(POINTERUP, {
      ...touch,
      pointerId: 5,
      ...at(origin),
      buttons: 0,
    });
    board.pointer(POINTERDOWN, { ...touch, pointerId: 7, ...at(origin) });
    board.pointer(POINTERMOVE, {
      ...touch,
      pointerId: 7,
      ...at(origin, 20, 0),
    });
    board.pointer(POINTERMOVE, {
      ...touch,
      pointerId: 7,
      ...at(board.at(ENEMY)),
    });
    board.pointer(POINTERUP, {
      ...touch,
      pointerId: 7,
      ...at(board.at(ENEMY)),
      buttons: 0,
    });
    expect(board.orders).toEqual([[MINE, ENEMY, { share: 1 }]]);
  });
});

describe("the cancel listener", () => {
  const touch = { pointerType: "touch" };

  function touchDrag(board: Board) {
    board.pointer(POINTERDOWN, {
      ...touch,
      pointerId: 5,
      ...at(board.at(MINE)),
    });
    board.pointer(POINTERMOVE, {
      ...touch,
      pointerId: 5,
      ...at(board.at(ENEMY)),
    });
  }

  test("a cancelled pointer that is not the held one leaves the drag alone", () => {
    const board = setup();
    touchDrag(board);
    board.windowEvent(
      "pointercancel",
      new FakePointerEvent({ pointerType: "touch", pointerId: 99 }) as never,
    );
    expect(board.drawn().length).toBeGreaterThan(0);
    board.pointer(POINTERUP, {
      ...touch,
      pointerId: 5,
      ...at(board.at(ENEMY)),
      buttons: 0,
    });
    expect(board.orders).toEqual([[MINE, ENEMY, { share: 1 }]]);
  });

  test("the held pointer's cancel drops the drag", () => {
    const board = setup();
    touchDrag(board);
    board.windowEvent(
      "pointercancel",
      new FakePointerEvent({ pointerType: "touch", pointerId: 5 }) as never,
    );
    expect(board.drawn()).toEqual([]);
    board.pointer(POINTERUP, {
      ...touch,
      pointerId: 5,
      ...at(board.at(ENEMY)),
      buttons: 0,
    });
    expect(board.orders).toEqual([]);
  });

  test("another element's blur leaves the drag alone, and the finger down stays counted", () => {
    const board = setup();
    touchDrag(board);
    board.windowEvent("blur", { target: { tagName: "INPUT" } });
    expect(board.drawn().length).toBeGreaterThan(0);
    // The first finger is still down: a second finger on your castle is a second pointer, not a
    // new drag.
    board.pointer(POINTERDOWN, { ...touch, pointerId: 6, ...at(board.at(2)) });
    expect(board.held()).toBeNull();
    expect(board.drawn()).toEqual([]);
    board.pointer(POINTERMOVE, {
      ...touch,
      pointerId: 6,
      ...at(board.at(2), 0, 40),
    });
    expect(board.drawn()).toEqual([]);
  });

  test("the window's own blur drops the drag", () => {
    const board = setup();
    touchDrag(board);
    board.windowEvent("blur", { target: globals.window });
    expect(board.drawn()).toEqual([]);
    board.pointer(POINTERUP, {
      ...touch,
      pointerId: 5,
      ...at(board.at(ENEMY)),
      buttons: 0,
    });
    expect(board.orders).toEqual([]);
  });
});

describe("the tap and double tap that a drag took", () => {
  /** A tap on your castle, as Babylon raises it after a press and release. */
  function tapOnCastle(board: Board, pointerType = "mouse") {
    const castle = board.at(MINE);
    board.scene.pointerX = castle.x;
    board.scene.pointerY = castle.y;
    board.pointer(POINTERTAP, { ...at(castle), pointerType, buttons: 0 });
  }
  function doubleTapOnCastle(board: Board) {
    const castle = board.at(MINE);
    board.scene.pointerX = castle.x;
    board.scene.pointerY = castle.y;
    board.pointer(POINTERDOUBLETAP, { ...at(castle), buttons: 0 });
  }
  /** A drag from your castle that Esc cancels with the button still down. */
  function cancelledDrag(board: Board) {
    const origin = board.at(MINE);
    board.pointer(POINTERDOWN, at(origin));
    board.pointer(POINTERMOVE, at(origin, 150, 0));
    board.windowEvent("keydown", { key: "Escape", target: null });
    board.pointer(POINTERUP, released(at(origin, 150, 0)));
  }

  test("without a drag, a tap then a double tap on one castle opens its menu", () => {
    const board = setup({ board: true });
    tapOnCastle(board);
    doubleTapOnCastle(board);
    expect(board.menus).toEqual([MINE]);
  });

  test("a finger's tap that follows a drag is no second tap of a double tap", () => {
    const board = setup({ board: true });
    tapOnCastle(board, "touch");
    cancelledDrag(board);
    // Without the guard this is a second tap on the castle just tapped, within 450 ms.
    tapOnCastle(board, "touch");
    expect(board.menus).toEqual([]);
  });

  test("a double tap that follows a drag opens no menu", () => {
    const board = setup({ board: true });
    tapOnCastle(board);
    cancelledDrag(board);
    doubleTapOnCastle(board);
    expect(board.menus).toEqual([]);
  });

  test("the next press ends it: the tap of an ordinary click counts again", () => {
    const board = setup({ board: true });
    cancelledDrag(board);
    const sea = { clientX: 5, clientY: 590 };
    board.pointer(POINTERDOWN, sea);
    board.pointer(POINTERUP, { ...sea, buttons: 0 });
    tapOnCastle(board);
    doubleTapOnCastle(board);
    expect(board.menus).toEqual([MINE]);
  });
});

describe("the line after an order", () => {
  /** Sends an order from your castle to the enemy's. */
  function send(board: Board) {
    dragOverEnemy(board);
    board.pointer(POINTERUP, released(at(board.at(ENEMY))));
  }

  test("stays, dimmed, until the board draws the turn after it", () => {
    const board = setup();
    send(board);
    board.scene.render();
    expect(board.drawn()).toEqual(["drag-line", "drag-line-casing"]);
    // The turn opens, but the board has not drawn it: the boat is not there yet.
    board.openTurn(11);
    board.scene.render();
    expect(board.drawn()).toEqual(["drag-line", "drag-line-casing"]);
    board.drawTurn(11);
    expect(board.drawn()).toEqual([]);
  });

  test("goes after three turns opened that the board never drew", () => {
    const board = setup();
    send(board);
    board.openTurn(11);
    board.openTurn(12);
    expect(board.drawn()).toEqual(["drag-line", "drag-line-casing"]);
    board.openTurn(13);
    expect(board.drawn()).toEqual([]);
  });

  test("goes when the view leaves live", () => {
    const board = setup();
    send(board);
    expect(board.drawn()).toEqual(["drag-line", "drag-line-casing"]);
    board.setLive(false);
    expect(board.drawn()).toEqual([]);
  });

  test("an order the server refuses flashes red, with its sentence and the order's id", async () => {
    const board = setup();
    send(board);
    board.settle({
      id: 1,
      state: "rejected",
      sentence: "Refused.",
      generationId: 10,
    });
    await settled();
    expect(board.feedback).toEqual([["Refused.", 1]]);
    expect(board.overlayColors).toContain(REJECTED_COLOR);
    expect(board.drawn()).toEqual(["drag-line", "drag-line-casing"]);
    // It was the pending line that flashed: a flash is not left pending as well.
    board.openTurn(11);
    board.openTurn(12);
    board.openTurn(13);
    board.drawTurn(11);
    expect(board.drawn()).toEqual(["drag-line", "drag-line-casing"]);
  });

  test("an order hand play refuses at once flashes red, with its sentence and no id", () => {
    const board = setup();
    board.refuseWith("Not enough soldiers.");
    send(board);
    expect(board.feedback).toEqual([["Not enough soldiers.", undefined]]);
    expect(board.overlayColors).toContain(REJECTED_COLOR);
  });

  test("a settling that comes after the controls are removed does nothing", async () => {
    const board = setup();
    send(board);
    dragOverEnemy(board);
    board.remove();
    expect(board.drawn()).toEqual([]);
    expect(board.held()).toBeNull();
    board.settle({
      id: 1,
      state: "rejected",
      sentence: "No.",
      generationId: 10,
    });
    await settled();
    expect(board.feedback).toEqual([]);
    for (const name of ["keydown", "keyup", "blur", "pointercancel"]) {
      expect(windowListeners.get(name)?.size ?? 0).toBe(0);
    }
  });
});

describe("when orders cannot be given", () => {
  test("a drag from your castle is a camera drag, and the bar stays hidden", () => {
    const board = setup({ live: false });
    const before = board.camera();
    dragOverEnemy(board);
    board.pointer(POINTERUP, released(at(board.at(ENEMY))));
    expect(board.orders).toEqual([]);
    expect(board.camera().alpha).not.toBe(before.alpha);
    expect(board.bars.every((view) => view === null)).toBe(true);
  });

  test("leaving live in the middle of a drag drops it: its release sends nothing", () => {
    const board = setup();
    dragOverEnemy(board);
    board.setLive(false);
    expect(board.drawn()).toEqual([]);
    board.pointer(POINTERUP, released(at(board.at(2))));
    expect(board.orders).toEqual([]);
  });
});

describe("the drag's drawing", () => {
  test("a castle gone from your ledger in the middle of a drag drops it", () => {
    const board = setup();
    dragOverEnemy(board);
    board.loseCastle(MINE);
    expect(board.drawn()).toEqual([]);
    board.pointer(POINTERUP, released(at(board.at(ENEMY))));
    expect(board.orders).toEqual([]);
  });

  test("a pointer that has not moved is not looked up again each frame", () => {
    const board = setup();
    dragOverEnemy(board);
    board.scene.render();
    const picks: number[] = [];
    const pick = board.scene.pick.bind(board.scene);
    board.scene.pick = ((...args: Parameters<typeof pick>) => {
      picks.push(1);
      return pick(...args);
    }) as typeof board.scene.pick;
    for (let frame = 0; frame < 5; frame++) board.scene.render();
    expect(picks).toEqual([]);
    board.pointer(POINTERMOVE, at(board.at(2)));
    board.scene.render();
    expect(picks.length).toBeGreaterThan(0);
  });
});

describe("what each frame leaves alone", () => {
  /** How many times the drag line's ribbon is written while `frames` render. */
  const lineWrites = (board: Board, frames: number) => {
    const before = ribbonWrites;
    for (let frame = 0; frame < frames; frame++) board.scene.render();
    // Two ribbons (the line and its casing) a drawing.
    return (ribbonWrites - before) / 2;
  };

  test("the line is not drawn again while nothing it shows has changed", () => {
    const board = setup();
    dragOverEnemy(board);
    board.scene.render();
    expect(lineWrites(board, 5)).toBe(0);
    // A move that changes the line draws it again (at the event or in the next frame).
    const before = ribbonWrites;
    board.pointer(POINTERMOVE, at(board.at(2)));
    board.scene.render();
    expect(ribbonWrites).toBeGreaterThan(before);
  });

  test("the pointer is looked up again when the canvas is resized under it", () => {
    const board = setup();
    dragOverEnemy(board);
    board.scene.render();
    const picks: number[] = [];
    const pick = board.scene.pick.bind(board.scene);
    board.scene.pick = ((...args: Parameters<typeof pick>) => {
      picks.push(1);
      return pick(...args);
    }) as typeof board.scene.pick;
    board.scene.render();
    expect(picks).toEqual([]);
    // The page resized the canvas: the engine now reports another height.
    (
      board.scene.getEngine() as unknown as { getRenderHeight: () => number }
    ).getRenderHeight = () => HEIGHT - 100;
    board.scene.render();
    expect(picks.length).toBeGreaterThan(0);
  });

  test("a new drag starts with no memory of where the last one ended", () => {
    const board = setup();
    // A wide view, so the top of the canvas looks above the horizon.
    board.field.camera.fov = 2;
    board.scene.render();
    const origin = board.at(MINE);
    // The first drag ends over the board and is cancelled with Esc.
    board.pointer(POINTERDOWN, at(origin));
    board.pointer(POINTERMOVE, at(origin, 40, 40));
    board.scene.render();
    expect(board.drawn()).toContain("drag-line");
    board.windowEvent("keydown", { key: "Escape", target: null });
    board.pointer(POINTERUP, released(at(origin, 40, 40)));
    // The second goes straight to the sky: there is no point on the board to draw to yet.
    board.pointer(POINTERDOWN, at(origin));
    board.pointer(POINTERMOVE, { clientX: origin.x, clientY: 0 });
    board.scene.render();
    expect(board.drawn()).toEqual([]);
  });
});
