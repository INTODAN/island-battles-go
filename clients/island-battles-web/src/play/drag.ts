// Drag to send (#343): the rules of a drag from one of your castles to another castle, and of the
// amount it sends, with no DOM, no Babylon and no timers. The board's controls
// (`src/scene/dragControls.ts`) feed it pointer, key and slider events and run its effects; the
// amount bar and the line draw its state.
//
// The issue's "released" state is not a state here: a release returns to `idle` with a `send` or
// `dropped` effect, and the line that stays after a send belongs to the drawing.

import { type Amount, type Ledger, resolveAmount } from "./rules";

export interface Point {
  x: number;
  y: number;
}

/** What a drag sends: a share of the origin's unreserved soldiers ({share: 1} is all), or exact soldiers. */
export type DragAmount =
  | { kind: "share"; share: number }
  /** `typed`: the digits typed for it, "" when it was set another way. */
  | { kind: "exact"; soldiers: number; typed: string };

export const ALL: DragAmount = { kind: "share", share: 1 };
export const HALF: DragAmount = { kind: "share", share: 0.5 };

/** How far a press on your castle must move, in CSS pixels, before it is a drag. */
export const DRAG_THRESHOLD_PX = 8;

/** What the model is told about the hand player with each event. */
export interface DragContext {
  /** The player's status is `playing` and the view shows the newest turn. */
  canOrder: boolean;
  /** Your castles in the open generation (`PlayState.castles`). */
  ledger: Ledger;
}

interface Hold {
  key: "w" | "s";
  since: number;
  last: number;
}

export type DragPhase =
  | { phase: "idle" }
  | { phase: "pressed"; pointerId: number; origin: number; start: Point }
  | {
      phase: "dragging";
      pointerId: number;
      origin: number;
      pointer: Point;
      /** The castle under the pointer, never the origin. */
      target: number | null;
      hold: Hold | null;
    };

export type DragState = DragPhase & {
  /** The amount: during a drag, that drag's; otherwise the one the next drag starts with. */
  amount: DragAmount;
  /**
   * The pointer the camera ignores: a press on your castle holds it until it is released, whatever
   * happens to the drag meanwhile, so the board never turns for that gesture. Only a second finger
   * on the board gives it back.
   */
  held: number | null;
  /**
   * The gesture going on became a drag or was cancelled: it is no click or tap. Cleared by the
   * press that starts the next gesture.
   */
  taken: boolean;
};

export type DragEvent =
  | {
      type: "press";
      pointerId: number;
      point: Point;
      /** Your castle or any other castle under the press, or null. */
      castle: number | null;
      /** A mouse or pen's main button, or a finger with no other on the board. */
      primary: boolean;
      /** No other button or finger was down: a new gesture starts. */
      newGesture: boolean;
    }
  | { type: "move"; pointerId: number; point: Point; target: number | null }
  /** The castle under a pointer that didn't move changed (the camera zoomed). */
  | { type: "target"; target: number | null }
  | {
      type: "up";
      pointerId: number;
      target: number | null;
      /** A real DOM pointer event; Babylon makes up a release for a cancelled pointer and a blur. */
      real: boolean;
      button: number;
    }
  /** Esc, a right button, a cancelled pointer, the window losing focus, a hidden tab, cleanup. */
  | { type: "cancel" }
  /** Another finger on the board. */
  | { type: "second-pointer" }
  | { type: "key"; key: string; now: number }
  | { type: "key-up"; key: string }
  | { type: "tick"; now: number }
  /** The slider or the Half and All buttons. */
  | { type: "amount"; amount: DragAmount }
  /** The ledger, the status or the live-ness changed. */
  | { type: "context" };

export type DropReason =
  | "released-elsewhere"
  | "cancelled"
  | "origin-lost"
  | "not-live"
  | "second-pointer";

export type DragEffect =
  | { type: "send"; origin: number; target: number; amount: Amount }
  | { type: "dropped"; reason: DropReason };

export interface DragStep {
  state: DragState;
  effects: DragEffect[];
  /** Whether a key event was the drag's (the caller calls `preventDefault()`). */
  handled: boolean;
}

export function initialDragState(): DragState {
  return { phase: "idle", amount: ALL, held: null, taken: false };
}

/** The soldiers `amount` comes to with `unreserved` soldiers: 0 when there are none. */
export function resolved(amount: DragAmount, unreserved: number): number {
  if (unreserved <= 0) return 0;
  if (amount.kind === "exact") return Math.min(amount.soldiers, unreserved);
  return Math.min(unreserved, resolveAmount(amount, unreserved));
}

/**
 * What `send` gets: a share stays a share, resolved by hand play when the order is given, so all
 * is still all when the castle grew; an exact number is capped at the unreserved soldiers.
 */
export function toAmount(amount: DragAmount, unreserved: number): Amount {
  if (amount.kind === "share") return { share: amount.share };
  return Math.max(1, Math.min(amount.soldiers, unreserved));
}

/** The chip at the origin during a drag, such as "12 of 23"; null otherwise. */
export function chipText(state: DragState, ledger: Ledger): string | null {
  if (state.phase !== "dragging") return null;
  const castle = ledger.get(state.origin);
  if (!castle) return null;
  return `${resolved(state.amount, castle.unreserved)} of ${castle.unreserved}`;
}

/** The amount slider, as the bar shows it. */
export interface SliderView {
  /** `share` before a drag (0 to 100 %), `soldiers` during one (1 to the unreserved). */
  mode: "share" | "soldiers";
  min: number;
  max: number;
  value: number;
  text: string;
  disabled: boolean;
}

export function sliderView(state: DragState, ledger: Ledger): SliderView {
  if (state.phase === "dragging") {
    const unreserved = ledger.get(state.origin)?.unreserved ?? 0;
    return {
      mode: "soldiers",
      min: 1,
      max: Math.max(1, unreserved),
      value: Math.max(1, resolved(state.amount, unreserved)),
      text: `${resolved(state.amount, unreserved)} of ${unreserved}`,
      disabled: unreserved <= 1,
    };
  }
  const { amount } = state;
  if (amount.kind === "exact") {
    return {
      mode: "share",
      min: 0,
      max: 100,
      value: 0,
      text: `Send ${amount.soldiers}`,
      disabled: false,
    };
  }
  const percent = Math.round(amount.share * 100);
  return {
    mode: "share",
    min: 0,
    max: 100,
    value: percent,
    text: percent >= 100 ? "Send all" : `Send ${percent}%`,
    disabled: false,
  };
}

/**
 * The amount a slider position stands for. Before a drag: the right end is all, the left end one
 * soldier, a share in between. During one: the right end is all, any other position exact soldiers.
 */
export function sliderAmount(view: SliderView, value: number): DragAmount {
  if (value >= view.max) return ALL;
  if (view.mode === "share") {
    if (value <= view.min) return { kind: "exact", soldiers: 1, typed: "" };
    return { kind: "share", share: value / 100 };
  }
  return { kind: "exact", soldiers: Math.max(1, value), typed: "" };
}

/** How long `w` or `s`, held for `heldMs`, waits before its next step: faster the longer it is held. */
export function holdInterval(heldMs: number): number {
  if (heldMs < 400) return 400;
  const t = Math.min(1, (heldMs - 400) / 1600);
  return Math.round(120 - t * 90);
}

function exact(soldiers: number): DragAmount {
  return { kind: "exact", soldiers, typed: "" };
}

/** `w` raises by one soldier (reaching the top is all), `s` lowers by one, not below 1. */
function nudge(amount: DragAmount, key: "w" | "s", unreserved: number) {
  if (unreserved <= 0) return amount;
  const current = resolved(amount, unreserved);
  if (key === "w") {
    if (current + 1 >= unreserved) return ALL;
    return exact(current + 1);
  }
  return exact(Math.max(1, current - 1));
}

/** A digit or Backspace for the exact number typed during a drag. */
function typeKey(
  amount: DragAmount,
  key: string,
  unreserved: number,
): DragAmount {
  const typed = amount.kind === "exact" ? amount.typed : "";
  if (key === "Backspace") {
    // Nothing typed (after a, d or the slider): there is nothing to take back.
    if (typed === "") return amount;
    const shorter = typed.slice(0, -1);
    return shorter === "" ? ALL : withTyped(shorter, unreserved);
  }
  if (unreserved <= 0) return amount;
  const next = (typed + key).replace(/^0+/, "");
  if (next === "") return amount;
  return withTyped(next, unreserved);
}

function withTyped(typed: string, unreserved: number): DragAmount {
  const value = Number(typed);
  if (value >= unreserved) {
    return { kind: "exact", soldiers: unreserved, typed: String(unreserved) };
  }
  return { kind: "exact", soldiers: value, typed };
}

type Settable = Partial<Pick<DragState, "amount" | "held" | "taken">>;

function toIdle(
  state: DragState,
  extra: Settable = {},
): DragState & { phase: "idle" } {
  return {
    phase: "idle",
    amount: state.amount,
    held: state.held,
    taken: state.taken,
    ...extra,
  };
}

/** Whether `event` releases the held pointer: its main button, or a release Babylon made up. */
function releases(state: DragState, event: DragEvent): boolean {
  return (
    event.type === "up" &&
    event.pointerId === state.held &&
    (!event.real || event.button === 0)
  );
}

function distance(a: Point, b: Point): number {
  return Math.hypot(a.x - b.x, a.y - b.y);
}

/**
 * The rules of #343. Every way out of a drag, sent or dropped, resets the amount to all; a press
 * that never became a drag (a click or tap) keeps it.
 */
export function step(
  state: DragState,
  event: DragEvent,
  context: DragContext,
): DragStep {
  const done = (
    next: DragState,
    effects: DragEffect[] = [],
    handled = false,
  ): DragStep => ({ state: next, effects, handled });
  /** Ends the press or drag without sending; the pointer stays held until it is released. */
  const drop = (reason: DropReason, extra: Settable = {}) => {
    if (state.phase === "idle") return done(toIdle(state, extra));
    const wasDrag = state.phase === "dragging";
    return done(
      toIdle(state, {
        taken: true,
        ...(wasDrag ? { amount: ALL } : {}),
        ...extra,
      }),
      wasDrag ? [{ type: "dropped", reason }] : [],
    );
  };

  // A press or drag ends when its origin is no longer yours, or orders can't be given. The
  // pointer stays held until its release.
  if (state.phase !== "idle" && event.type !== "press") {
    const release = releases(state, event) ? { held: null } : {};
    if (!context.canOrder) return drop("not-live", release);
    if (!context.ledger.has(state.origin)) return drop("origin-lost", release);
  }

  switch (event.type) {
    case "press": {
      if (!event.newGesture) {
        // A second button of the held mouse (a right-click) cancels; anything else is ignored.
        if (
          state.phase !== "idle" &&
          event.pointerId === state.pointerId &&
          !event.primary
        ) {
          return drop("cancelled");
        }
        return done(state);
      }
      // A new gesture: whatever came before has ended, even if its release was lost.
      const fresh = toIdle(state, { held: null, taken: false });
      const ownCastle =
        event.castle !== null && context.ledger.has(event.castle);
      if (!event.primary || !ownCastle || !context.canOrder) {
        // Leaves a drag that lost its release, as a cancel does.
        if (state.phase === "dragging") {
          return done({ ...fresh, amount: ALL }, [
            { type: "dropped", reason: "cancelled" },
          ]);
        }
        return done(fresh);
      }
      return done(
        {
          ...fresh,
          ...(state.phase === "dragging" ? { amount: ALL } : {}),
          phase: "pressed",
          pointerId: event.pointerId,
          origin: event.castle as number,
          start: event.point,
          held: event.pointerId,
        },
        state.phase === "dragging"
          ? [{ type: "dropped", reason: "cancelled" }]
          : [],
      );
    }
    case "move": {
      if (state.phase === "pressed" && event.pointerId === state.pointerId) {
        if (distance(event.point, state.start) <= DRAG_THRESHOLD_PX) {
          return done(state);
        }
        return done({
          phase: "dragging",
          pointerId: state.pointerId,
          origin: state.origin,
          pointer: event.point,
          target: event.target === state.origin ? null : event.target,
          hold: null,
          amount: state.amount,
          held: state.held,
          taken: true,
        });
      }
      if (state.phase === "dragging" && event.pointerId === state.pointerId) {
        return done({
          ...state,
          pointer: event.point,
          target: event.target === state.origin ? null : event.target,
        });
      }
      return done(state);
    }
    case "target": {
      if (state.phase !== "dragging") return done(state);
      const target = event.target === state.origin ? null : event.target;
      return done(target === state.target ? state : { ...state, target });
    }
    case "up": {
      // The release of a second mouse button leaves the main one down.
      if (!releases(state, event)) return done(state);
      const released = { held: null };
      if (!event.real) return drop("cancelled", released);
      if (state.phase === "pressed") return done(toIdle(state, released));
      if (state.phase !== "dragging") return done(toIdle(state, released));
      const target = event.target === state.origin ? null : event.target;
      if (target === null) return drop("released-elsewhere", released);
      const unreserved = context.ledger.get(state.origin)?.unreserved ?? 0;
      return done(toIdle(state, { ...released, amount: ALL, taken: true }), [
        {
          type: "send",
          origin: state.origin,
          target,
          amount: toAmount(state.amount, unreserved),
        },
      ]);
    }
    case "cancel":
      return drop("cancelled");
    case "second-pointer":
      if (state.phase === "idle" && state.held === null) return done(state);
      return drop("second-pointer", { held: null });
    case "key": {
      if (state.phase === "idle") return done(state);
      if (event.key === "Escape")
        return { ...drop("cancelled"), handled: true };
      if (state.phase !== "dragging") return done(state);
      const unreserved = context.ledger.get(state.origin)?.unreserved ?? 0;
      const key = event.key.length === 1 ? event.key.toLowerCase() : event.key;
      if (key === "d") return done({ ...state, amount: ALL }, [], true);
      if (key === "a") return done({ ...state, amount: HALF }, [], true);
      if (key === "w" || key === "s") {
        return done(
          {
            ...state,
            amount: nudge(state.amount, key, unreserved),
            hold: { key, since: event.now, last: event.now },
          },
          [],
          true,
        );
      }
      if (/^[0-9]$/.test(key) || key === "Backspace") {
        return done(
          { ...state, amount: typeKey(state.amount, key, unreserved) },
          [],
          true,
        );
      }
      return done(state);
    }
    case "key-up": {
      if (state.phase !== "dragging" || !state.hold) return done(state);
      const key = event.key.toLowerCase();
      return done(key === state.hold.key ? { ...state, hold: null } : state);
    }
    case "tick": {
      if (state.phase !== "dragging" || !state.hold) return done(state);
      const unreserved = context.ledger.get(state.origin)?.unreserved ?? 0;
      let { amount } = state;
      let last = state.hold.last;
      while (event.now - last >= holdInterval(last - state.hold.since)) {
        last += holdInterval(last - state.hold.since);
        amount = nudge(amount, state.hold.key, unreserved);
      }
      if (last === state.hold.last) return done(state);
      return done({ ...state, amount, hold: { ...state.hold, last } });
    }
    case "amount":
      return done({ ...state, amount: event.amount });
    case "context":
      return done(state);
  }
}

/**
 * The amount bar as the board's controls see it (the DOM bar is `src/ui/game/amountBar.ts`): they
 * draw it with `render` (null hides it) and hear its changes, already turned into amounts.
 */
export interface AmountControl {
  render(view: SliderView | null): void;
  onChange(listener: (amount: DragAmount) => void): () => void;
}
