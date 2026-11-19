import { describe, expect, test } from "bun:test";
import {
  burstAmount,
  castleMenuModel,
  doubleClickFocuses,
  isRightClick,
  LOADING_REASON,
  MenuGestures,
  type MenuPlay,
  type MenuPointer,
  menuAllowed,
  menuReason,
  placeMenu,
  TOUCH_DOUBLE_TAP_MS,
} from "./castleMenu";

describe("the menu's gestures", () => {
  const pointer = (extra: Partial<MenuPointer>): MenuPointer => ({
    button: 0,
    pointerType: "mouse",
    castleId: 1,
    x: 100,
    y: 100,
    at: 0,
    ...extra,
  });

  test("a right-click on a castle opens it; a right-click that moved 8 px or more doesn't", () => {
    const gestures = new MenuGestures();
    gestures.down(pointer({ button: 2 }));
    expect(gestures.tap(pointer({ button: 2, x: 105 }))).toBe("right-click");
    gestures.down(pointer({ button: 2 }));
    expect(gestures.tap(pointer({ button: 2, x: 109 }))).toBeNull();
  });

  test("a right-click needs its press, and a castle", () => {
    const gestures = new MenuGestures();
    expect(gestures.tap(pointer({ button: 2 }))).toBeNull();
    gestures.down(pointer({ button: 2 }));
    expect(gestures.tap(pointer({ button: 2, castleId: null }))).toBeNull();
  });

  test("a double-click opens it when both presses were on the same castle", () => {
    const gestures = new MenuGestures();
    expect(gestures.tap(pointer({}))).toBeNull();
    expect(gestures.doubleTap(pointer({ at: 200 }))).toBe("double-click");
  });

  test("Babylon's double tap over two castles opens nothing", () => {
    const gestures = new MenuGestures();
    gestures.tap(pointer({ castleId: 1 }));
    expect(gestures.doubleTap(pointer({ castleId: 2, at: 200 }))).toBeNull();
    gestures.tap(pointer({ castleId: null }));
    expect(gestures.doubleTap(pointer({ castleId: 1, at: 200 }))).toBeNull();
  });

  test("a double-tap with a finger, within Babylon's 300 ms or within the longer touch window", () => {
    const gestures = new MenuGestures();
    gestures.tap(pointer({ pointerType: "touch" }));
    expect(gestures.doubleTap(pointer({ pointerType: "touch", at: 250 }))).toBe(
      "double-tap",
    );
    // 300 to 450 ms: Babylon raises a second tap; it is a double-tap on the same castle.
    gestures.tap(pointer({ pointerType: "touch", at: 1000 }));
    expect(
      gestures.tap(
        pointer({ pointerType: "touch", at: 1000 + TOUCH_DOUBLE_TAP_MS }),
      ),
    ).toBe("double-tap");
    // Slower, or a mouse, or another castle: two single taps.
    gestures.tap(pointer({ pointerType: "touch", at: 3000 }));
    expect(
      gestures.tap(
        pointer({ pointerType: "touch", at: 3001 + TOUCH_DOUBLE_TAP_MS }),
      ),
    ).toBeNull();
    gestures.tap(pointer({ at: 5000 }));
    expect(gestures.tap(pointer({ at: 5350 }))).toBeNull();
    gestures.tap(pointer({ pointerType: "touch", at: 7000 }));
    expect(
      gestures.tap(pointer({ pointerType: "touch", castleId: 2, at: 7350 })),
    ).toBeNull();
  });

  test("a third tap doesn't open it again", () => {
    const gestures = new MenuGestures();
    gestures.tap(pointer({ pointerType: "touch" }));
    expect(gestures.tap(pointer({ pointerType: "touch", at: 350 }))).toBe(
      "double-tap",
    );
    expect(gestures.tap(pointer({ pointerType: "touch", at: 500 }))).toBeNull();
  });
});

import type { PlayOrder } from "./handPlay";
import {
  type Board,
  buildLedger,
  checkOrder,
  type Generation,
  type OrderSpec,
  type ResolvedOrder,
  refusalSentence,
  type UpgradeTable,
} from "./rules";
import { board, upgradeTable } from "./testing";

/**
 * A hand play's state and check for team 1 on `generation` after `orders`, built with the module's
 * own ledger and check, as `createHandPlay` builds them.
 */
function play(
  options: {
    generation?: Generation;
    orders?: ResolvedOrder[];
    table?: UpgradeTable | null;
  } = {},
): MenuPlay {
  const current: Board = {
    generation: options.generation ?? board(),
    team: 1,
    table: options.table === undefined ? upgradeTable : options.table,
  };
  const ordered = options.orders ?? [];
  const castles = buildLedger(current, ordered);
  const orders: PlayOrder[] = ordered.map((order, index) => ({
    id: index + 1,
    spec:
      order.kind === "burst"
        ? { kind: "burst", castleId: order.castleId, amount: order.soldiers }
        : order.kind === "upgrade"
          ? order
          : {
              kind: "send",
              origin: order.origin,
              destination: order.destination,
              amount: order.soldiers,
            },
    soldiers: order.kind === "upgrade" ? null : order.soldiers,
    generationId: current.generation.id,
    state: "accepted",
  }));
  return {
    state: { castles, upgrades: current.table, orders },
    check(spec: OrderSpec) {
      const result = checkOrder(current, castles, spec);
      return "order" in result
        ? { ok: true, order: result.order }
        : {
            ok: false,
            refusal: result.refusal,
            sentence: refusalSentence(result.refusal),
          };
    },
  };
}

function model(menuPlay: MenuPlay, castleId = 1) {
  const result = castleMenuModel(menuPlay, castleId);
  if (!result) throw new Error(`no menu for castle ${castleId}`);
  return result;
}

const row = (menuPlay: MenuPlay, type: string, castleId = 1) => {
  const found = model(menuPlay, castleId).upgrades.find(
    (entry) => entry.type === type,
  );
  if (!found) throw new Error(`no row ${type}`);
  return found;
};

/** Generation 10 with castle 1 changed. */
function withCastle1(change: Partial<Generation["castles"][number]>) {
  const generation = board();
  generation.castles[0] = {
    ...(generation.castles[0] as Generation["castles"][number]),
    ...change,
  };
  return generation;
}

describe("opening the menu", () => {
  const castles = new Map([[1, {}]]);

  test("opens on your castle while playing on the newest turn", () => {
    expect(
      menuAllowed({ status: "playing", showsNewestTurn: true, castles }, 1),
    ).toBe(true);
  });

  test("not on a castle that isn't yours", () => {
    expect(
      menuAllowed({ status: "playing", showsNewestTurn: true, castles }, 3),
    ).toBe(false);
  });

  test("not scrubbed back, nor once the game ended or the team is out", () => {
    expect(
      menuAllowed({ status: "playing", showsNewestTurn: false, castles }, 1),
    ).toBe(false);
    for (const status of ["idle", "eliminated", "ended"] as const) {
      expect(menuAllowed({ status, showsNewestTurn: true, castles }, 1)).toBe(
        false,
      );
    }
  });

  test("a right-click opens it only when it moved less than 8 px", () => {
    expect(isRightClick({ x: 10, y: 10 }, { x: 10, y: 10 })).toBe(true);
    expect(isRightClick({ x: 10, y: 10 }, { x: 15, y: 15 })).toBe(true);
    expect(isRightClick({ x: 10, y: 10 }, { x: 18, y: 10 })).toBe(false);
    expect(isRightClick({ x: 10, y: 10 }, { x: 40, y: 30 })).toBe(false);
  });

  test("a double-click focuses the camera except for a hand player who plays", () => {
    expect(doubleClickFocuses(null)).toBe(true);
    expect(doubleClickFocuses("idle")).toBe(true);
    expect(doubleClickFocuses("playing")).toBe(false);
    expect(doubleClickFocuses("eliminated")).toBe(true);
    expect(doubleClickFocuses("ended")).toBe(true);
  });
});

describe("the menu's heading", () => {
  test("names the castle and its unreserved soldiers", () => {
    const menu = model(play({ orders: [send(1, 4, 5)] }));
    expect(menu.title).toBe("Castle 2");
    expect(menu.soldiers).toBe(30);
    expect(menu.unreserved).toBe(25);
    expect(menu.subtitle).toBe("25 soldiers free to spend");
  });

  test("is null for a castle that isn't yours", () => {
    expect(castleMenuModel(play(), 3)).toBeNull();
    expect(castleMenuModel(play(), 4)).toBeNull();
    expect(castleMenuModel(play(), 99)).toBeNull();
  });
});

function send(origin: number, destination: number, soldiers: number) {
  return { kind: "send", origin, destination, soldiers } as const;
}
function upgrade(castleId: number, upgradeType: string) {
  return { kind: "upgrade", castleId, upgradeType } as ResolvedOrder;
}

describe("upgrade rows", () => {
  test("one row per type, in the labels' order, with name, description, levels, effect and cost", () => {
    const menu = model(play());
    expect(menu.upgrades.map((entry) => entry.name)).toEqual([
      "Speed",
      "Stop",
      "Growth",
      "Burst",
    ]);
    const speed = row(play(), "speed");
    expect(speed).toMatchObject({
      level: 1,
      next: 2,
      effect: "Speed 1 → 2: 0.5 → 1 units a turn",
      cost: 10,
      disabled: null,
      orderedText: null,
    });
    expect(speed.description).toContain("sail");
    expect(row(play(), "generation_amount").effect).toBe(
      "Growth 1 → 2: 1 → 2 soldiers a turn",
    );
    expect(row(play(), "clash_stop_generations").effect).toBe(
      "Stop 1 → 2: 3 → 2 turns stopped",
    );
  });

  test("the issue's example: Speed 2 → 3 is 1 → 1.5 units a turn and costs 20", () => {
    const speed = row(
      play({ generation: withCastle1({ upgrades: { speed: 2 } }) }),
      "speed",
    );
    expect(speed.effect).toBe("Speed 2 → 3: 1 → 1.5 units a turn");
    expect(speed.cost).toBe(20);
  });

  test("disabled at the maximum level", () => {
    const speed = row(
      play({ generation: withCastle1({ upgrades: { speed: 4 } }) }),
      "speed",
    );
    expect(speed).toMatchObject({
      level: 4,
      next: null,
      cost: null,
      effect: "Speed 4: 2 units a turn",
      disabled: "Maximum level",
    });
  });

  test("disabled without enough soldiers, with the numbers", () => {
    // Castle 2 has 12 soldiers; Speed 2 costs 10, Speed 3 costs 20.
    const after = play({ orders: [upgrade(2, "speed")] });
    expect(row(after, "speed", 2).disabled).toBe(
      "Needs 20 soldiers; the castle has 2",
    );
    const poor = play({ generation: withCastle1({ soldiers: 9 }) });
    expect(row(poor, "speed").disabled).toBe(
      "Needs 10 soldiers; the castle has 9",
    );
  });

  test("an upgrade ordered this turn shows as ordered, and the row offers the level after it", () => {
    const once = play({ orders: [upgrade(1, "speed")] });
    expect(row(once, "speed")).toMatchObject({
      level: 2,
      levelAtTurnStart: 1,
      ordered: 1,
      next: 3,
      effect: "Speed 2 → 3: 1 → 1.5 units a turn",
      orderedText: "Speed 2 ordered this turn",
      cost: 20,
      disabled: null,
    });
    expect(model(once).unreserved).toBe(20);
    const twice = play({ orders: [upgrade(1, "speed"), upgrade(1, "speed")] });
    expect(row(twice, "speed").orderedText).toBe(
      "Speed 2 and 3 ordered this turn",
    );
    expect(row(twice, "speed").disabled).toBe(
      "Needs 30 soldiers; the castle has 0",
    );
  });

  test("every row waits while the upgrade table loads", () => {
    const loading = play({ table: null });
    for (const entry of model(loading).upgrades) {
      expect(entry.disabled).toBe(LOADING_REASON);
      expect(entry.cost).toBeNull();
    }
    expect(model(loading).burst.disabled).toBe(LOADING_REASON);
  });
});

describe("the burst row", () => {
  test("a castle at Burst 1 has no potential", () => {
    const burst = model(play()).burst;
    expect(burst).toMatchObject({
      potential: 0,
      max: 0,
      disabled: "No burst potential yet",
    });
  });

  test("castle 2 at Burst 2 can burst 1 to 4", () => {
    expect(model(play(), 2).burst).toMatchObject({
      potential: 4,
      max: 4,
      disabled: null,
      ordered: null,
    });
  });

  test("the potential counts the upgrades ordered this turn", () => {
    const burst = model(
      play({ orders: [upgrade(1, "burst_potential")] }),
    ).burst;
    expect(burst).toMatchObject({ potential: 4, max: 4, disabled: null });
  });

  test("disabled while repaying a burst", () => {
    const debt = play({
      generation: withCastle1({
        burst_debt: 5,
        upgrades: { burst_potential: 3 },
      }),
    });
    expect(model(debt).burst).toMatchObject({
      debt: 5,
      disabled: "Repaying a burst: 5 soldiers left",
    });
  });

  test("disabled after a burst this turn, which it shows", () => {
    const burst = model(
      play({ orders: [{ kind: "burst", castleId: 2, soldiers: 3 }] }),
      2,
    ).burst;
    expect(burst).toMatchObject({
      disabled: "Already ordered this turn",
      ordered: 3,
    });
  });

  test("the slider's amount stays within 1 and the potential", () => {
    expect(burstAmount(null, 8)).toBe(8);
    expect(burstAmount(3, 8)).toBe(3);
    expect(burstAmount(12, 8)).toBe(8);
    expect(burstAmount(0, 8)).toBe(1);
    expect(burstAmount(5, 0)).toBe(1);
  });
});

describe("the menu's reasons", () => {
  test("other refusals get hand play's sentence", () => {
    expect(menuReason({ kind: "not-playing", status: "ended" })).toBe(
      "The game has ended.",
    );
  });
});

describe("placing the menu", () => {
  const size = { width: 200, height: 300 };
  const boardSize = { width: 800, height: 600 };

  test("to the right of the castle, centred on it", () => {
    expect(placeMenu({ x: 100, y: 300 }, size, boardSize)).toEqual({
      left: 128,
      top: 150,
    });
  });

  test("to the left when the right has no room", () => {
    expect(placeMenu({ x: 700, y: 300 }, size, boardSize)).toEqual({
      left: 472,
      top: 150,
    });
  });

  test("always inside the board", () => {
    const corner = placeMenu({ x: 790, y: 590 }, size, boardSize);
    expect(corner.left + size.width).toBeLessThanOrEqual(boardSize.width);
    expect(corner.top + size.height).toBeLessThanOrEqual(boardSize.height);
    const top = placeMenu({ x: 300, y: -50 }, size, boardSize);
    expect(top.top).toBe(6);
    const narrow = placeMenu({ x: 200, y: 300 }, size, {
      width: 260,
      height: 600,
    });
    expect(narrow.left).toBeGreaterThanOrEqual(6);
    expect(narrow.left + size.width).toBeLessThanOrEqual(260);
  });
});
