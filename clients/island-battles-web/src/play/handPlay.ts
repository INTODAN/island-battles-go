// Hand play: a hand player's orders and turn clock. It has no DOM and no Babylon imports; every
// dependency is injected (the game store, the API calls, the clock and the timers), so `bun test`
// runs it on a fake clock. `index.ts` builds the real dependencies.

import { ApiError } from "../api/errors";
import type { components } from "../api/generated/game";
import type { GenerationState } from "../realtime/install";
import { createStore, type ReadableStore } from "../realtime/store";
import {
  type Amount,
  type Board,
  buildLedger,
  checkOrder,
  isStaleTurn,
  isTransientError,
  type Ledger,
  type OrderSpec,
  playsByHand,
  type Refusal,
  type ResolvedOrder,
  refusalSentence,
  rejectionSentence,
  teamOut,
  type UpgradeTable,
  type UpgradeType,
  withOrder,
} from "./rules";

export type RegisterGenerationCommands =
  components["schemas"]["RegisterGenerationCommands"];
type PublicGenerationCommand = components["schemas"]["PublicGenerationCommand"];

/**
 * - `idle`: no game yet, before its first state, or a player who doesn't play by hand;
 * - `playing`: your team is in the game and it has no outcome: orders can be given;
 * - `eliminated`: your team has no castle and no soldier group left; the game goes on without you;
 * - `ended`: the game has an outcome.
 */
export type PlayStatus = "idle" | "playing" | "eliminated" | "ended";

/**
 * One order, as a snapshot: hand play never changes a `PlayOrder` it handed out. Follow an order
 * by its `id` in `PlayState.orders`, with `OrderResult.settled`, or with `onSettled`.
 */
export interface PlayOrder {
  /** Unique within a hand play, increasing in the order the orders were given. */
  readonly id: number;
  readonly spec: OrderSpec;
  /**
   * The soldiers of a send or a burst: an estimate while it waits to be sent (a share is resolved
   * when it is sent), then the number sent. Null for an upgrade.
   */
  readonly soldiers: number | null;
  /** The generation the order was sent to, or the open one while it waits. */
  readonly generationId: number;
  /** `sending` until the server accepts it (`accepted`) or it is refused or dropped (`rejected`). */
  readonly state: "sending" | "accepted" | "rejected";
  /** For a rejected order: why, in a plain sentence. */
  readonly sentence?: string;
}

/** The open turn's timing, for a pace indicator. */
export interface PlayTurn {
  gameId: number;
  generationId: number;
  turnNumber: number;
  /**
   * The turn's pace, `GenerationState.turn_pace_milliseconds`; null in a game that waits for
   * every player (not real-time).
   */
  paceMs: number | null;
  /**
   * Local epoch ms when this tab ends the turn: `turn_deadline_at` (by the local clock) or the pace
   * after the state arrived, whichever is first. Null without a pace or after the outcome. It is
   * fixed for the turn.
   */
  endsAt: number | null;
  /** `endsAt - paceMs`: where a pace bar starts. Null when `endsAt` is. */
  startsAt: number | null;
}

/** What hand play shows: `HandPlay.state`. */
export interface PlayState {
  status: PlayStatus;
  /**
   * Your team, or null with no game. Like `castles` and `turn`, it only means something while
   * `status` isn't `idle`: a player who doesn't play by hand has them filled but makes no request.
   */
  teamColor: number | null;
  /** The open turn, or null with no game. Only meaningful while `status` isn't `idle`. */
  turn: PlayTurn | null;
  /**
   * Your castles in the open generation, by castle ID, counting the orders of this turn that the
   * server accepted and those still being sent: a castle is here exactly while your team owns it.
   * `soldiers` is the server's count, not the animated board's. Only meaningful while `status`
   * isn't `idle`.
   */
  castles: Ledger;
  /**
   * The orders of the open turn, in the order given: those being sent, and those settled since
   * the open generation arrived. A turn's settled orders leave when the next generation arrives,
   * so to show every rejection use `onSettled`.
   */
  orders: readonly PlayOrder[];
  /** `GET /game/upgrades`, once loaded: every type's description, levels, effects and costs. */
  upgrades: UpgradeTable | null;
}

/** The answer of `check`. */
export type CheckResult =
  | { ok: true; order: ResolvedOrder }
  | { ok: false; refusal: Refusal; sentence: string };

/** The answer of `send`, `upgrade` and `burst`. */
export type OrderResult =
  /** Refused here, as the server would: nothing was sent. */
  | { ok: false; refusal: Refusal; sentence: string }
  /**
   * Given: `order` is its first snapshot (`sending`), and `settled` resolves with the last one,
   * `accepted` or `rejected`. `settled` never rejects; it also resolves when hand play is disposed.
   */
  | { ok: true; order: PlayOrder; settled: Promise<PlayOrder> };

export interface HandPlayDeps {
  /** The player's game state: `realtime.game`. */
  game: ReadableStore<GenerationState | null>;
  /** `POST /generation/commands`. Resolves with the caller's state; rejects with `ApiError`. */
  postCommands(
    body: RegisterGenerationCommands,
    signal: AbortSignal,
  ): Promise<GenerationState>;
  /** `POST /game/from-lobby/{lobby_id}/deadline`. Resolves with the caller's state. */
  applyDeadline(signal: AbortSignal): Promise<GenerationState>;
  /** `GET /game/from-lobby/{lobby_id}/generation`. */
  readState(signal: AbortSignal): Promise<GenerationState>;
  /** `GET /game/upgrades`'s `upgrades`. */
  loadUpgrades(signal: AbortSignal): Promise<UpgradeTable>;
  /** Installs a state an HTTP call answered, as any state read: `realtime.adoptGame`. */
  adopt(state: GenerationState): void;
  /** Reads the followed game again: `realtime.refreshGame`. */
  refresh(): void;
  /** Epoch milliseconds. */
  now(): number;
  /**
   * Runs `run` after `ms` and returns the function that cancels it. In the browser it must keep
   * time in a hidden tab (`workerSchedule`).
   */
  schedule(run: () => void, ms: number): () => void;
  /** A new UUID for a batch's `request_id`. */
  newRequestId(): string;
  /** How long a request may take before it counts as unanswered. Default 2000. */
  requestTimeoutMs?: number;
  /**
   * How long an order may be tried, from its first attempt: it is not sent again if the retry delay
   * plus `requestTimeoutMs` for the new attempt could run past this. Default 5000.
   */
  orderBudgetMs?: number;
}

export interface HandPlay {
  readonly state: ReadableStore<PlayState>;
  /**
   * Sends soldiers from one of your castles to any other castle. `amount` is an exact number or
   * `{share}` of the origin's unreserved soldiers, resolved when the order is sent.
   */
  send(
    originCastleId: number,
    destinationCastleId: number,
    amount: Amount,
  ): OrderResult;
  /** Buys the next level of `upgradeType` for one of your castles, paid from its soldiers. */
  upgrade(castleId: number, upgradeType: UpgradeType): OrderResult;
  /**
   * Orders a burst of one of your castles: it adds `amount` soldiers in the next generation and
   * owes them back from its production. It takes no soldiers now, so `{share}` is of the castle's
   * burst potential (`{share: 1}` is the largest burst). While `GET /game/upgrades` hasn't loaded
   * (`PlayState.upgrades` is null) the potential is unknown, so a `{share}` is refused with
   * `upgrades-not-loaded`; an exact amount is let through and the server checks it.
   */
  burst(castleId: number, amount: Amount): OrderResult;
  /**
   * Whether an order would be given now, without giving it: the same check as the three above,
   * with the soldiers resolved. For disabled controls and their reasons.
   */
  check(spec: OrderSpec): CheckResult;
  /**
   * Calls `listener` once for every order when it is accepted or rejected. Returns the
   * unsubscribe. After `dispose()` no listener is called, not even for the orders that `dispose()`
   * drops (their `settled` promises still resolve, as `rejected`).
   */
  onSettled(listener: (order: PlayOrder) => void): () => void;
  /**
   * Stops: no more requests or timers; the orders not settled yet settle as rejected, which
   * resolves their `settled` promises but calls no `onSettled` listener. `state` keeps its last
   * value (orders still `sending` stay so): nothing is published after `dispose()`.
   */
  dispose(): void;
}

/** Delay of the `attempt`th retry (from 0): 100 ms, doubling up to 1 s. */
export function retryDelay(attempt: number): number {
  return Math.min(100 * 2 ** attempt, 1000);
}

/** The deadline call aims this long after `turn_deadline_at`, which `Date.parse` rounds down. */
export const deadlineMarginMs = 10;
/** After a deadline call the server answered with an error it won't fix by itself, ask this often. */
export const slowDeadlineRetryMs = 5000;
/** When a turn ended with no request from this tab for this long, it calls once to stay heard. */
export const presenceGapMs = 2000;

const noAnswerSentence =
  "The server didn't answer; the order may not have been given.";
const lateSentence = "The turn ended before the order arrived.";
const leftSentence = "You left the game.";

interface Entry {
  id: number;
  spec: OrderSpec;
  state: PlayOrder["state"];
  sentence?: string;
  soldiers: number | null;
  generationId: number;
  /**
   * queued: waits in the queue; posting: a request is out; retrying: waits to post the same body
   * again; awaiting-turn: its turn ended (stale 409), waits for the next generation's state.
   */
  phase: "queued" | "posting" | "retrying" | "awaiting-turn";
  resolved?: ResolvedOrder;
  body?: RegisterGenerationCommands;
  /** When the current body was first posted, for `orderBudgetMs`. */
  started: number;
  attempt: number;
  /** Identifies the entry's current request, so late answers of earlier ones are ignored. */
  token: number;
  /** Sent once more already after its turn ended. */
  resent: boolean;
  /** A read of the state is out, while `awaiting-turn`. */
  reading: boolean;
  cancel?: () => void;
  resolve(order: PlayOrder): void;
}

interface Clock {
  key: string;
  /** Hand play calls the deadline for this turn. */
  running: boolean;
  /** `endsAt` came from `turn_deadline_at`, not from the pace. */
  byDeadline: boolean;
  timer?: () => void;
  fired: boolean;
  calls: number;
  retries: number;
  /** The token of the deadline call in flight. */
  token?: number;
  refreshed: boolean;
}

const turnKey = (state: GenerationState) =>
  `${state.game_id}:${state.generation_id}`;

function statusOf(state: GenerationState | null): PlayStatus {
  if (state === null || state.bot.kind !== "manual") return "idle";
  if (state.outcome) return "ended";
  if (teamOut(state.generation, state.team_color)) return "eliminated";
  return "playing";
}

function command(order: ResolvedOrder): PublicGenerationCommand {
  switch (order.kind) {
    case "send":
      return {
        command_type: "send_soldier_group",
        parameters: {
          origin_castle_id: order.origin,
          destination_castle_id: order.destination,
          soldier_amount: order.soldiers,
        },
      };
    case "upgrade":
      return {
        command_type: "upgrade_castle",
        parameters: {
          castle_id: order.castleId,
          upgrade_type: order.upgradeType,
        },
      };
    case "burst":
      return {
        command_type: "burst_castle",
        parameters: {
          castle_id: order.castleId,
          soldier_amount: order.soldiers,
        },
      };
  }
}

const soldiersOf = (order: ResolvedOrder) =>
  order.kind === "upgrade" ? null : order.soldiers;

/**
 * Plays a hand player's seat. It follows `deps.game` and stays `idle`, making no request, until a
 * state says the player plays by hand (`bot.kind` `manual`). Then:
 *
 * - **Orders** are checked as the server checks them, and each is sent at once as its own batch
 *   with `complete: false`, one request at a time, in order. The server ends the turn; hand play
 *   never completes it.
 * - **The turn clock** calls `POST …/deadline` when each turn of a real-time game ends, and again
 *   while the server still shows it open; it is what the server hears from an idle player, so its
 *   timers must run in a hidden tab (`deps.schedule`).
 */
export function createHandPlay(deps: HandPlayDeps): HandPlay {
  const timeoutMs = deps.requestTimeoutMs ?? 2000;
  const budgetMs = deps.orderBudgetMs ?? 5000;
  const store = createStore<PlayState>({
    status: "idle",
    teamColor: null,
    turn: null,
    castles: new Map(),
    orders: [],
    upgrades: null,
  });
  const listeners = new Set<(order: PlayOrder) => void>();
  const aborts = new Set<AbortController>();

  let disposed = false;
  let held: GenerationState | null = null;
  let heldKey: string | undefined;
  let turn: PlayTurn | null = null;
  let table: UpgradeTable | null = null;
  let tableLoading = false;
  let tableAttempt = 0;
  let accepted: { key: string; orders: ResolvedOrder[] } = {
    key: "",
    orders: [],
  };
  let queue: Entry[] = [];
  let inflight: Entry | null = null;
  let shown: Entry[] = [];
  let nextId = 1;
  let tokens = 0;
  let clock: Clock | undefined;
  /** False once a call scheduled by `turn_deadline_at` came early: this game's clocks disagree. */
  let trustDeadline = true;
  let trustGame: number | undefined;
  let lastRequestAt = Number.NEGATIVE_INFINITY;

  /** Runs one request with a timeout on `deps.schedule`; the timeout aborts it. */
  function request<T>(
    run: (signal: AbortSignal) => Promise<T>,
    ms = timeoutMs,
  ): Promise<T> {
    const controller = new AbortController();
    aborts.add(controller);
    lastRequestAt = deps.now();
    return new Promise<T>((resolve, reject) => {
      const cancel = deps.schedule(() => {
        aborts.delete(controller);
        controller.abort(new Error("timeout"));
        reject(
          new ApiError({
            kind: "transport",
            status: 0,
            detail: "The server didn't answer in time.",
          }),
        );
      }, ms);
      let started: Promise<T>;
      try {
        started = run(controller.signal);
      } catch (error) {
        started = Promise.reject(error);
      }
      started.then(
        (value) => {
          cancel();
          aborts.delete(controller);
          resolve(value);
        },
        (error: unknown) => {
          cancel();
          aborts.delete(controller);
          reject(error);
        },
      );
    });
  }

  const board = (): Board | null =>
    held ? { generation: held.generation, team: held.team_color, table } : null;

  const acceptedOrders = () =>
    held && accepted.key === turnKey(held) ? accepted.orders : [];

  /** The ledger the server holds: the open generation and the orders it accepted. */
  function exactLedger(current: Board): Ledger {
    return buildLedger(current, acceptedOrders());
  }

  /**
   * The ledger the player sees: also the order in flight while its body names the open
   * generation (once a newer generation is open, it is in that board or will be sent again), and
   * the queued orders that still pass, each resolved against what the ones ahead leave.
   */
  function optimisticLedger(current: Board): Ledger {
    let ledger = exactLedger(current);
    if (
      inflight?.resolved &&
      inflight.phase !== "awaiting-turn" &&
      inflight.body?.generation_id === held?.generation_id
    ) {
      ledger = withOrder(current, ledger, inflight.resolved);
    }
    for (const entry of queue) {
      const checked = checkOrder(current, ledger, entry.spec);
      if ("order" in checked) {
        entry.soldiers = soldiersOf(checked.order);
        ledger = withOrder(current, ledger, checked.order);
      }
    }
    return ledger;
  }

  function snapshot(entry: Entry): PlayOrder {
    return Object.freeze({
      id: entry.id,
      spec: entry.spec,
      soldiers: entry.soldiers,
      generationId: entry.generationId,
      state: entry.state,
      ...(entry.sentence === undefined ? {} : { sentence: entry.sentence }),
    });
  }

  function publish(): void {
    if (disposed) return;
    const current = board();
    const castles = current ? optimisticLedger(current) : new Map();
    const entries = [...shown, ...(inflight ? [inflight] : []), ...queue].sort(
      (left, right) => left.id - right.id,
    );
    store.set({
      status: statusOf(held),
      teamColor: held?.team_color ?? null,
      turn,
      castles,
      orders: entries.map(snapshot),
      upgrades: table,
    });
  }

  function settle(
    entry: Entry,
    state: "accepted" | "rejected",
    sentence?: string,
  ): void {
    entry.cancel?.();
    delete entry.cancel;
    entry.token = ++tokens;
    entry.state = state;
    if (sentence !== undefined) entry.sentence = sentence;
    const order = snapshot(entry);
    if (!disposed) shown.push(entry);
    entry.resolve(order);
    if (!disposed) for (const listener of [...listeners]) listener(order);
  }

  /** Rejects every order not settled yet that waits in the queue. */
  function rejectQueued(sentence: string): void {
    const dropped = queue;
    queue = [];
    for (const entry of dropped) settle(entry, "rejected", sentence);
  }

  const statusSentence = () =>
    refusalSentence({
      kind: "not-playing",
      status: statusOf(held) as "idle" | "eliminated" | "ended",
    });

  // --- Orders ---

  /** Sends the next queued order once nothing is in flight. */
  function pump(): void {
    while (!disposed && inflight === null && queue.length > 0) {
      const current = board();
      if (!held || !current || statusOf(held) !== "playing") {
        rejectQueued(statusSentence());
        break;
      }
      const entry = queue.shift() as Entry;
      const checked = checkOrder(current, exactLedger(current), entry.spec);
      entry.generationId = held.generation_id;
      if ("refusal" in checked) {
        settle(entry, "rejected", refusalSentence(checked.refusal));
        continue;
      }
      entry.resolved = checked.order;
      entry.soldiers = soldiersOf(checked.order);
      entry.body = {
        game_id: held.game_id,
        generation_id: held.generation_id,
        request_id: deps.newRequestId(),
        complete: false,
        commands: [command(checked.order)],
      };
      entry.started = deps.now();
      entry.attempt = 0;
      inflight = entry;
      post(entry);
    }
    publish();
  }

  function post(entry: Entry): void {
    const body = entry.body as RegisterGenerationCommands;
    const token = ++tokens;
    entry.token = token;
    entry.phase = "posting";
    const current = () =>
      !disposed && inflight === entry && entry.token === token;
    request((signal) => deps.postCommands(body, signal)).then(
      (answer) => {
        if (!current()) return;
        deps.adopt(answer);
        if (!current()) return;
        const open = deps.game.get();
        if (
          open &&
          open.game_id === body.game_id &&
          open.generation_id === body.generation_id &&
          entry.resolved
        ) {
          const key = turnKey(open);
          if (accepted.key !== key) accepted = { key, orders: [] };
          accepted.orders.push(entry.resolved);
        }
        inflight = null;
        settle(entry, "accepted");
        pump();
      },
      (error: unknown) => {
        if (!current()) return;
        failed(entry, error);
      },
    );
  }

  function failed(entry: Entry, error: unknown): void {
    if (isTransientError(error)) {
      if (statusOf(held) !== "playing") {
        inflight = null;
        settle(entry, "rejected", statusSentence());
        pump();
        return;
      }
      const delay = retryDelay(entry.attempt++);
      if (deps.now() + delay + timeoutMs - entry.started > budgetMs) {
        // Another attempt could run past the order's budget (its retry delay and its own
        // timeout both count): the orders behind it would go out many seconds late, against a
        // board the player no longer sees.
        inflight = null;
        settle(entry, "rejected", noAnswerSentence);
        rejectQueued(noAnswerSentence);
        publish();
        return;
      }
      entry.phase = "retrying";
      entry.cancel = deps.schedule(() => {
        delete entry.cancel;
        if (!disposed && inflight === entry) post(entry);
      }, delay);
      return;
    }
    const apiError = error as ApiError;
    if (isStaleTurn(apiError) && !entry.resent) {
      entry.resent = true;
      entry.phase = "awaiting-turn";
      entry.started = deps.now();
      entry.attempt = 0;
      publish();
      afterTurn(entry);
      return;
    }
    inflight = null;
    settle(entry, "rejected", rejectionSentence(apiError));
    pump();
  }

  /**
   * An order whose turn ended before it arrived: once a newer generation is open, it goes back to
   * the head of the queue, to be checked against it and sent once more. The request that was
   * refused played the turn, so the new generation is usually pushed already; if not, it is read.
   */
  function afterTurn(entry: Entry): void {
    if (disposed || inflight !== entry || entry.phase !== "awaiting-turn")
      return;
    const body = entry.body as RegisterGenerationCommands;
    const newer = () =>
      held !== null &&
      held.game_id === body.game_id &&
      held.generation_id > body.generation_id;
    if (newer()) {
      entry.cancel?.();
      delete entry.cancel;
      delete entry.body;
      delete entry.resolved;
      entry.phase = "queued";
      entry.reading = false;
      inflight = null;
      queue.unshift(entry);
      pump();
      return;
    }
    if (entry.cancel || entry.reading) return;
    entry.reading = true;
    const token = ++tokens;
    entry.token = token;
    const current = () =>
      !disposed && inflight === entry && entry.token === token;
    const again = () => {
      entry.reading = false;
      const delay = retryDelay(entry.attempt++);
      if (deps.now() + delay + timeoutMs - entry.started > budgetMs) {
        // As a transport failure does: the orders behind it would go out just as late.
        inflight = null;
        settle(entry, "rejected", lateSentence);
        rejectQueued(lateSentence);
        publish();
        return;
      }
      entry.cancel = deps.schedule(() => {
        delete entry.cancel;
        afterTurn(entry);
      }, delay);
    };
    request((signal) => deps.readState(signal)).then(
      (answer) => {
        if (!current()) return;
        deps.adopt(answer);
        if (!current()) return;
        if (newer()) {
          entry.reading = false;
          afterTurn(entry);
        } else {
          again();
        }
      },
      () => {
        if (current()) again();
      },
    );
  }

  function give(spec: OrderSpec): OrderResult {
    const checked = check(spec);
    if (!checked.ok) return checked;
    let resolve: (order: PlayOrder) => void = () => {};
    const settled = new Promise<PlayOrder>((done) => {
      resolve = done;
    });
    const entry: Entry = {
      id: nextId++,
      spec,
      state: "sending",
      soldiers: soldiersOf(checked.order),
      generationId: held?.generation_id ?? 0,
      phase: "queued",
      started: 0,
      attempt: 0,
      token: 0,
      resent: false,
      reading: false,
      resolve,
    };
    queue.push(entry);
    const order = snapshot(entry);
    pump();
    return { ok: true, order, settled };
  }

  function check(spec: OrderSpec): CheckResult {
    const status = disposed ? "idle" : statusOf(held);
    const current = board();
    if (status !== "playing" || !current) {
      const refusal: Refusal = {
        kind: "not-playing",
        status: status as "idle",
      };
      return { ok: false, refusal, sentence: refusalSentence(refusal) };
    }
    const checked = checkOrder(current, optimisticLedger(current), spec);
    if ("refusal" in checked) {
      return {
        ok: false,
        refusal: checked.refusal,
        sentence: refusalSentence(checked.refusal),
      };
    }
    return { ok: true, order: checked.order };
  }

  // --- The upgrade table ---

  function loadTable(): void {
    if (disposed || table || tableLoading) return;
    tableLoading = true;
    request((signal) => deps.loadUpgrades(signal), 5000).then(
      (loaded) => {
        tableLoading = false;
        if (disposed) return;
        table = loaded;
        publish();
      },
      () => {
        if (disposed) return;
        const delay = Math.min(1000 * 2 ** tableAttempt++, 30_000);
        deps.schedule(() => {
          tableLoading = false;
          loadTable();
        }, delay);
      },
    );
  }

  // --- The turn clock ---

  const clockCurrent = (subject: Clock) =>
    !disposed &&
    clock === subject &&
    held !== null &&
    turnKey(held) === subject.key &&
    playsByHand(held);

  function fire(subject: Clock): void {
    delete subject.timer;
    subject.fired = true;
    if (!clockCurrent(subject) || subject.token !== undefined) return;
    const token = ++tokens;
    subject.token = token;
    subject.calls++;
    const mine = () => !disposed && subject.token === token;
    request((signal) => deps.applyDeadline(signal)).then(
      (answer) => {
        if (disposed) return;
        deps.adopt(answer);
        if (!mine()) return;
        delete subject.token;
        // A new turn's state started its own clock.
        if (!clockCurrent(subject)) return;
        // The turn is still open: the call went early.
        if (subject.calls === 1 && subject.byDeadline) trustDeadline = false;
        subject.timer = deps.schedule(
          () => fire(subject),
          retryDelay(subject.retries++),
        );
      },
      (error: unknown) => {
        if (!mine()) return;
        delete subject.token;
        if (!clockCurrent(subject)) return;
        if (isTransientError(error)) {
          subject.timer = deps.schedule(
            () => fire(subject),
            retryDelay(subject.retries++),
          );
          return;
        }
        // 401, 403, 404 and the like: realtime's own read tells whether the game or the session
        // is gone (and empties the store, which stops this clock). Until then keep calling
        // slowly: the relay has stood down, so no one else ends this player's turns.
        if (!subject.refreshed) {
          subject.refreshed = true;
          deps.refresh();
        }
        subject.timer = deps.schedule(() => fire(subject), slowDeadlineRetryMs);
      },
    );
  }

  /** One deadline call with no follow-up, so that the server keeps hearing from this player. */
  function presenceCall(): void {
    request((signal) => deps.applyDeadline(signal)).then(
      (answer) => {
        if (!disposed) deps.adopt(answer);
      },
      () => {},
    );
  }

  /** A new turn: its timing, and its clock. */
  function newTurn(state: GenerationState): void {
    const previous = clock;
    if (previous) {
      previous.timer?.();
      delete previous.timer;
      if (
        previous.running &&
        !previous.fired &&
        playsByHand(state) &&
        deps.now() - lastRequestAt >= presenceGapMs
      ) {
        // Another client ended that turn before this tab's call: call once anyway, so the
        // server doesn't take a silent player out.
        presenceCall();
      }
    }
    if (trustGame !== state.game_id) {
      trustGame = state.game_id;
      trustDeadline = true;
    }
    const key = turnKey(state);
    const pace = state.turn_pace_milliseconds;
    if (pace === null || state.turn_deadline_at === null || state.outcome) {
      turn = {
        gameId: state.game_id,
        generationId: state.generation_id,
        turnNumber: state.turn_number,
        paceMs: pace,
        endsAt: null,
        startsAt: null,
      };
      clock = {
        key,
        running: false,
        byDeadline: false,
        fired: false,
        calls: 0,
        retries: 0,
        refreshed: false,
      };
      return;
    }
    const arrival = deps.now();
    const byPace = arrival + pace;
    const deadline = Date.parse(state.turn_deadline_at) + deadlineMarginMs;
    const byDeadline =
      trustDeadline && Number.isFinite(deadline) && deadline < byPace;
    const endsAt = byDeadline ? deadline : byPace;
    turn = {
      gameId: state.game_id,
      generationId: state.generation_id,
      turnNumber: state.turn_number,
      paceMs: pace,
      endsAt,
      startsAt: endsAt - pace,
    };
    const running = playsByHand(state);
    const next: Clock = {
      key,
      running,
      byDeadline,
      fired: false,
      calls: 0,
      retries: 0,
      refreshed: false,
    };
    clock = next;
    if (running) {
      next.timer = deps.schedule(
        () => fire(next),
        Math.max(0, endsAt - deps.now()),
      );
    }
  }

  // --- Following the game ---

  function stopAll(sentence: string): void {
    if (inflight) {
      const entry = inflight;
      inflight = null;
      settle(entry, "rejected", sentence);
    }
    rejectQueued(sentence);
  }

  function onState(state: GenerationState | null): void {
    if (disposed) return;
    held = state;
    if (state === null) {
      heldKey = undefined;
      turn = null;
      clock?.timer?.();
      clock = undefined;
      stopAll(leftSentence);
      shown = [];
      accepted = { key: "", orders: [] };
      publish();
      return;
    }
    if (state.bot.kind === "manual") loadTable();
    const key = turnKey(state);
    if (key !== heldKey) {
      heldKey = key;
      shown = [];
      accepted = { key, orders: [] };
      newTurn(state);
    } else if (state.outcome && turn?.endsAt != null) {
      // The game ended on the open generation (a turn timeout, an abandoned game): no turn ends
      // after this, so the pace bar has nothing to count down to.
      turn = { ...turn, endsAt: null, startsAt: null };
      if (clock) {
        clock.timer?.();
        delete clock.timer;
      }
    }
    if (statusOf(state) !== "playing") {
      rejectQueued(statusSentence());
      // The request in flight settles by its answer; one waiting to go again stops here.
      if (inflight && inflight.phase !== "posting") {
        const entry = inflight;
        inflight = null;
        settle(entry, "rejected", statusSentence());
      }
    }
    if (inflight?.phase === "awaiting-turn") afterTurn(inflight);
    pump();
  }

  const unsubscribe = deps.game.subscribe(onState);
  onState(deps.game.get());

  return {
    state: store,
    send: (origin, destination, amount) =>
      give({ kind: "send", origin, destination, amount }),
    upgrade: (castleId, upgradeType) =>
      give({ kind: "upgrade", castleId, upgradeType }),
    burst: (castleId, amount) => give({ kind: "burst", castleId, amount }),
    check,
    onSettled(listener) {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
    dispose() {
      if (disposed) return;
      disposed = true;
      unsubscribe();
      clock?.timer?.();
      clock = undefined;
      for (const controller of aborts) controller.abort(new Error("disposed"));
      aborts.clear();
      stopAll(leftSentence);
      listeners.clear();
    },
  };
}
