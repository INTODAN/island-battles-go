import { describe, expect, test } from "bun:test";
import {
  ALL,
  chipText,
  type DragContext,
  type DragEvent,
  type DragState,
  HALF,
  holdInterval,
  initialDragState,
  resolved,
  sliderAmount,
  sliderView,
  step,
  toAmount,
} from "./drag";
import type { CastleLedger, Ledger } from "./rules";

const YOURS = 1;
const OTHER_YOURS = 2;
const NEUTRAL = 7;
const ENEMY = 8;

function ledger(unreserved: Record<number, number>): Ledger {
  return new Map(
    Object.entries(unreserved).map(([id, count]) => [
      Number(id),
      { id: Number(id), soldiers: count, unreserved: count } as CastleLedger,
    ]),
  );
}

const live = (counts: Record<number, number> = { 1: 23, 2: 5 }) =>
  ({ canOrder: true, ledger: ledger(counts) }) satisfies DragContext;

/** Runs events in order and returns the last state and every effect. */
function run(
  events: DragEvent[],
  context: DragContext = live(),
  from: DragState = initialDragState(),
) {
  let state = from;
  const effects = [];
  let handled = false;
  for (const event of events) {
    const result = step(state, event, context);
    state = result.state;
    effects.push(...result.effects);
    handled = result.handled;
  }
  return { state, effects, handled };
}

const press = (
  castle: number | null,
  options: Partial<Extract<DragEvent, { type: "press" }>> = {},
): DragEvent => ({
  type: "press",
  pointerId: 1,
  point: { x: 100, y: 100 },
  castle,
  primary: true,
  newGesture: true,
  ...options,
});
const move = (x: number, target: number | null = null, pointerId = 1) =>
  ({ type: "move", pointerId, point: { x, y: 100 }, target }) as DragEvent;
const up = (
  target: number | null,
  options: Partial<Extract<DragEvent, { type: "up" }>> = {},
): DragEvent => ({
  type: "up",
  pointerId: 1,
  target,
  real: true,
  button: 0,
  ...options,
});
const key = (name: string, now = 0): DragEvent => ({
  type: "key",
  key: name,
  now,
});
/** A press on your castle and a move past the threshold. */
const startDrag = [press(YOURS), move(140)];

describe("starting a drag", () => {
  test("a press on your castle holds the pointer from the press, before any move", () => {
    const { state } = run([press(YOURS)]);
    expect(state).toMatchObject({ phase: "pressed", held: 1, origin: YOURS });
  });

  test("8 px is still a press, more is a drag", () => {
    expect(run([press(YOURS), move(108)]).state.phase).toBe("pressed");
    expect(run([press(YOURS), move(108.1)]).state.phase).toBe("dragging");
  });

  test("a press on a neutral or enemy castle or on the sea is the camera's", () => {
    for (const castle of [NEUTRAL, ENEMY, null]) {
      const { state } = run([press(castle), move(200)]);
      expect(state).toMatchObject({ phase: "idle", held: null, taken: false });
    }
  });

  test("a right-button press or a second finger never starts one", () => {
    expect(run([press(YOURS, { primary: false })]).state.held).toBeNull();
  });

  test("not live, a press on your castle is the camera's", () => {
    const { state } = run([press(YOURS), move(200)], {
      ...live(),
      canOrder: false,
    });
    expect(state).toMatchObject({ phase: "idle", held: null });
  });

  test("a press that never became a drag is a click: nothing sent, and not taken", () => {
    const { state, effects } = run([press(YOURS), move(105), up(YOURS)]);
    expect(effects).toEqual([]);
    expect(state).toMatchObject({ phase: "idle", held: null, taken: false });
  });

  test("the target is never the origin", () => {
    const { state } = run([...startDrag, move(150, YOURS)]);
    expect(state).toMatchObject({ phase: "dragging", target: null });
  });
});

describe("ending a drag", () => {
  test("releasing over another castle sends all, as a share resolved when sent", () => {
    const { state, effects } = run([
      ...startDrag,
      move(200, NEUTRAL),
      up(NEUTRAL),
    ]);
    expect(effects).toEqual([
      { type: "send", origin: YOURS, target: NEUTRAL, amount: { share: 1 } },
    ]);
    expect(state).toMatchObject({
      phase: "idle",
      held: null,
      taken: true,
      amount: ALL,
    });
  });

  test("a castle of your own is a target too", () => {
    const { effects } = run([...startDrag, up(OTHER_YOURS)]);
    expect(effects[0]).toMatchObject({ type: "send", target: OTHER_YOURS });
  });

  test("releasing on the sea or on the origin drops it", () => {
    for (const target of [null, YOURS]) {
      const { effects, state } = run([...startDrag, up(target)]);
      expect(effects).toEqual([
        { type: "dropped", reason: "released-elsewhere" },
      ]);
      expect(state.held).toBeNull();
    }
  });

  test("Esc drops it, and the key is the drag's; the pointer stays held until released", () => {
    const { state, effects, handled } = run([...startDrag, key("Escape")]);
    expect(effects).toEqual([{ type: "dropped", reason: "cancelled" }]);
    expect(handled).toBe(true);
    expect(state).toMatchObject({ phase: "idle", held: 1, taken: true });
    const after = run([up(NEUTRAL)], live(), state);
    expect(after.effects).toEqual([]);
    expect(after.state.held).toBeNull();
  });

  test("Esc with no drag is not the drag's", () => {
    expect(run([key("Escape")]).handled).toBe(false);
  });

  test("a right button pressed during the drag drops it and keeps the main button held", () => {
    const { state, effects } = run([
      ...startDrag,
      press(null, { primary: false, newGesture: false }),
      up(NEUTRAL, { button: 2 }),
    ]);
    expect(effects).toEqual([{ type: "dropped", reason: "cancelled" }]);
    expect(state).toMatchObject({ phase: "idle", held: 1, taken: true });
  });

  test("a release Babylon made up (pointercancel, blur) never sends", () => {
    const { state, effects } = run([
      ...startDrag,
      up(NEUTRAL, { real: false }),
    ]);
    expect(effects).toEqual([{ type: "dropped", reason: "cancelled" }]);
    expect(state.held).toBeNull();
  });

  test("a second finger on the board drops it and gives the gesture back to the camera", () => {
    const { state, effects } = run([...startDrag, { type: "second-pointer" }]);
    expect(effects).toEqual([{ type: "dropped", reason: "second-pointer" }]);
    expect(state).toMatchObject({ phase: "idle", held: null, taken: true });
  });

  test("a captured origin drops it at the next event; the pointer stays held", () => {
    const dragging = run(startDrag).state;
    const { state, effects } = run(
      [{ type: "context" }],
      live({ 2: 5 }),
      dragging,
    );
    expect(effects).toEqual([{ type: "dropped", reason: "origin-lost" }]);
    expect(state).toMatchObject({ phase: "idle", held: 1, amount: ALL });
  });

  test("a captured origin at the release sends nothing and lets the pointer go", () => {
    const dragging = run(startDrag).state;
    const { state, effects } = run([up(NEUTRAL)], live({ 2: 5 }), dragging);
    expect(effects).toEqual([{ type: "dropped", reason: "origin-lost" }]);
    expect(state.held).toBeNull();
  });

  test("leaving live drops it", () => {
    const dragging = run(startDrag).state;
    const { effects } = run(
      [{ type: "context" }],
      { ...live(), canOrder: false },
      dragging,
    );
    expect(effects).toEqual([{ type: "dropped", reason: "not-live" }]);
  });

  test("the press that starts the next gesture clears the taken flag and a lost hold", () => {
    const cancelled = run([...startDrag, key("Escape")]).state;
    const { state } = run([press(null)], live(), cancelled);
    expect(state).toMatchObject({ taken: false, held: null });
  });
});

describe("the amount", () => {
  test("a drag starts at all", () => {
    expect(run(startDrag).state.amount).toEqual(ALL);
  });

  test("a during the drag sends half, and the next drag sends all again", () => {
    const first = run([...startDrag, key("a"), up(NEUTRAL)]);
    expect(first.effects[0]).toMatchObject({ amount: { share: 0.5 } });
    const second = run([...startDrag, up(NEUTRAL)], live(), first.state);
    expect(second.effects[0]).toMatchObject({ amount: { share: 1 } });
  });

  test("a dropped drag resets the amount too", () => {
    const { state } = run([...startDrag, key("a"), key("Escape")]);
    expect(state.amount).toEqual(ALL);
  });

  test("a change before a drag applies to the next drag only", () => {
    const first = run([
      { type: "amount", amount: HALF },
      ...startDrag,
      up(NEUTRAL),
    ]);
    expect(first.effects[0]).toMatchObject({ amount: { share: 0.5 } });
    const second = run([...startDrag, up(NEUTRAL)], live(), first.state);
    expect(second.effects[0]).toMatchObject({ amount: { share: 1 } });
  });

  test("a tap on your castle keeps a change made before it", () => {
    const { state } = run([
      { type: "amount", amount: HALF },
      press(YOURS),
      up(YOURS),
    ]);
    expect(state.amount).toEqual(HALF);
  });

  test("keys before a drag do nothing", () => {
    const { state, handled } = run([key("a")]);
    expect(state.amount).toEqual(ALL);
    expect(handled).toBe(false);
  });

  test("the slider during a drag changes that drag", () => {
    const { effects } = run([
      ...startDrag,
      { type: "amount", amount: { kind: "exact", soldiers: 9, typed: "" } },
      up(NEUTRAL),
    ]);
    expect(effects[0]).toMatchObject({ amount: 9 });
  });

  test("typing 7 sends exactly 7", () => {
    const { effects, handled } = run([...startDrag, key("7"), up(NEUTRAL)]);
    expect(handled).toBe(false); // the up's
    expect(effects[0]).toMatchObject({ amount: 7 });
    expect(run([...startDrag, key("7")]).handled).toBe(true);
  });

  test("typed digits are capped at the unreserved soldiers and can't be 0", () => {
    expect(run([...startDrag, key("7"), key("5")]).state.amount).toEqual({
      kind: "exact",
      soldiers: 23,
      typed: "23",
    });
    expect(run([...startDrag, key("0")]).state.amount).toEqual(ALL);
    expect(run([...startDrag, key("1"), key("0")]).state.amount).toMatchObject({
      soldiers: 10,
    });
  });

  test("Backspace edits the typed number, and back to all when it is empty", () => {
    expect(
      run([...startDrag, key("1"), key("2"), key("Backspace")]).state.amount,
    ).toMatchObject({
      soldiers: 1,
      typed: "1",
    });
    expect(
      run([...startDrag, key("1"), key("Backspace")]).state.amount,
    ).toEqual(ALL);
  });

  test("Backspace with nothing typed leaves the amount as it is", () => {
    expect(run([...startDrag, key("Backspace")]).state.amount).toEqual(ALL);
    expect(
      run([...startDrag, key("a"), key("Backspace")]).state.amount,
    ).toEqual(HALF);
    expect(
      run([...startDrag, key("s"), key("Backspace")]).state.amount,
    ).toMatchObject({ kind: "exact", soldiers: 22 });
  });

  test("d is all, a is half", () => {
    expect(run([...startDrag, key("7"), key("d")]).state.amount).toEqual(ALL);
    expect(run([...startDrag, key("A")]).state.amount).toEqual(HALF);
  });

  test("w and s step by one soldier; w reaching the top is all, s stops at 1", () => {
    expect(run([...startDrag, key("s")]).state.amount).toMatchObject({
      soldiers: 22,
    });
    expect(run([...startDrag, key("a"), key("w")]).state.amount).toMatchObject({
      soldiers: 12,
    });
    expect(run([...startDrag, key("s"), key("w")]).state.amount).toEqual(ALL);
    const low = run([...startDrag, key("1"), key("s")]).state.amount;
    expect(low).toMatchObject({ soldiers: 1 });
  });

  test("w and s repeat faster the longer they are held, and stop when let go", () => {
    expect(holdInterval(0)).toBe(400);
    expect(holdInterval(400)).toBe(120);
    expect(holdInterval(5000)).toBe(30);
    const held = run([...startDrag, key("s", 0)]).state;
    // 400 ms to the first repeat, then 120 ms and shorter.
    expect(
      resolved(
        run([{ type: "tick", now: 399 }], live(), held).state.amount,
        23,
      ),
    ).toBe(22);
    expect(
      resolved(
        run([{ type: "tick", now: 400 }], live(), held).state.amount,
        23,
      ),
    ).toBe(21);
    const longer = run([{ type: "tick", now: 1000 }], live(), held).state;
    expect(resolved(longer.amount, 23)).toBeLessThan(18);
    const released = run(
      [
        { type: "key-up", key: "s" },
        { type: "tick", now: 5000 },
      ],
      live(),
      held,
    ).state;
    expect(resolved(released.amount, 23)).toBe(22);
  });

  test("a share is resolved when sent; an exact number is capped then, not rewritten", () => {
    expect(toAmount(ALL, 23)).toEqual({ share: 1 });
    expect(toAmount({ kind: "exact", soldiers: 12, typed: "" }, 5)).toBe(5);
    expect(toAmount({ kind: "exact", soldiers: 12, typed: "" }, 0)).toBe(1);
    expect(resolved(HALF, 23)).toBe(11);
    expect(resolved(HALF, 1)).toBe(1);
    expect(resolved(ALL, 0)).toBe(0);
  });

  test("the chip shows the number and the total, following the castle's count", () => {
    const dragging = run([...startDrag, key("1"), key("2")]).state;
    expect(chipText(dragging, ledger({ 1: 23 }))).toBe("12 of 23");
    expect(chipText(dragging, ledger({ 1: 8 }))).toBe("8 of 8");
    expect(chipText(run(startDrag).state, ledger({ 1: 24 }))).toBe("24 of 24");
    expect(chipText(initialDragState(), ledger({ 1: 23 }))).toBeNull();
  });
});

describe("the slider", () => {
  test("before a drag it shows the share", () => {
    expect(sliderView(initialDragState(), ledger({}))).toMatchObject({
      mode: "share",
      value: 100,
      text: "Send all",
    });
    const half = { ...initialDragState(), amount: HALF };
    expect(sliderView(half, ledger({}))).toMatchObject({
      value: 50,
      text: "Send 50%",
    });
  });

  test("before a drag its left end is one soldier and its right end all", () => {
    const view = sliderView(initialDragState(), ledger({}));
    expect(sliderAmount(view, 0)).toEqual({
      kind: "exact",
      soldiers: 1,
      typed: "",
    });
    expect(sliderAmount(view, 100)).toEqual(ALL);
    expect(sliderAmount(view, 30)).toEqual({ kind: "share", share: 0.3 });
    const one = { ...initialDragState(), amount: sliderAmount(view, 0) };
    expect(sliderView(one, ledger({}))).toMatchObject({
      value: 0,
      text: "Send 1",
    });
  });

  test("during a drag it moves in whole soldiers of the origin", () => {
    const dragging = run([...startDrag, key("a")]).state;
    const view = sliderView(dragging, ledger({ 1: 23 }));
    expect(view).toMatchObject({
      mode: "soldiers",
      min: 1,
      max: 23,
      value: 11,
      text: "11 of 23",
    });
    expect(sliderAmount(view, 7)).toEqual({
      kind: "exact",
      soldiers: 7,
      typed: "",
    });
    expect(sliderAmount(view, 23)).toEqual(ALL);
  });

  test("its thumb returns to the right end after a drag", () => {
    const { state } = run([...startDrag, key("a"), up(NEUTRAL)]);
    expect(sliderView(state, ledger({ 1: 23 }))).toMatchObject({
      mode: "share",
      value: 100,
    });
  });

  test("a castle with one soldier or none disables it", () => {
    expect(sliderView(run(startDrag).state, ledger({ 1: 1 })).disabled).toBe(
      true,
    );
  });
});
