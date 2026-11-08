import { ApiError } from "../api/errors";
import type { components } from "../api/generated/game";
import { createStore, type ReadableStore } from "../realtime/store";
import {
  BotError,
  type BotTurn,
  type BotTurnResult,
  buildBotTurn,
  type GenerationState,
  type Upgrade,
} from "./localhost";

type RegisterCommands = components["schemas"]["RegisterGenerationCommands"];

/** The batch that completes a turn: `POST /generation/commands`. */
export type TurnSubmission = RegisterCommands & {
  complete: true;
  request_id: string;
};

/** What the relay is doing, for the game view to show. */
export type RelayStatus =
  /** Not relaying: no game, a default bot, or the game ended. */
  | { kind: "idle" }
  /** The bot is thinking about this generation. */
  | { kind: "playing"; generationId: number }
  /** The turn was submitted; the relay waits for the others. */
  | { kind: "submitted"; generationId: number }
  /** Something failed. `retrying` says the relay keeps trying by itself; otherwise `retry()` tries again. */
  | {
      kind: "error";
      generationId: number;
      message: string;
      retrying: boolean;
    };

export interface RelayDeps {
  /** The player's game state, from the realtime session. */
  game: ReadableStore<GenerationState | null>;
  /** Reads the upgrade cost table, `GET /game/upgrades`. Called once. */
  loadUpgrades(): Promise<Upgrade[]>;
  /** Posts a turn to the localhost bot. Rejects with `BotError`. */
  playBot(turn: BotTurn): Promise<BotTurnResult>;
  /** Submits the batch, `POST /generation/commands`. Rejects with `ApiError`. */
  submit(submission: TurnSubmission): Promise<void>;
  /** `POST /game/from-lobby/{lobby_id}/deadline`. */
  applyDeadline(): Promise<void>;
  /** Epoch milliseconds. */
  now(): number;
  /** A fresh UUID. */
  newRequestId(): string;
  /** Runs `run` after `ms` and returns the function that cancels it. */
  schedule(run: () => void, ms: number): () => void;
  /** How long after `turn_deadline_at` the relay asks the server to apply it. Default 1000. */
  deadlineGraceMs?: number;
  /** Pause before asking an unreachable bot again. Default 2000. */
  botRetryMs?: number;
}

export interface BotRelay {
  readonly status: ReadableStore<RelayStatus>;
  /** Plays the open turn again after an error that needs a human decision. */
  retry(): void;
  dispose(): void;
}

const isGameEnded = (error: unknown) =>
  error instanceof ApiError && error.problemType?.endsWith(":game-ended");

/** A failure worth sending again unchanged: no answer, or a server fault. */
export const isTransient = (error: unknown) =>
  error instanceof ApiError &&
  (error.kind === "transport" || error.status >= 500 || error.status === 429);

/** Delay before sending a failed submission again: 1 s, 2 s, 4 s, then every 8 s. */
export function submitRetryDelay(attempt: number): number {
  return Math.min(1000 * 2 ** attempt, 8000);
}

/** Delay before asking again for a deadline that is still open: 2 s, doubling up to 30 s. */
export function deadlineRetryDelay(attempt: number): number {
  return Math.min(2000 * 2 ** attempt, 30_000);
}

const turnKey = (state: GenerationState) =>
  `${state.game_id}:${state.generation_id}`;

/**
 * Relays the turns of a player whose bot is `localhost`: on each new
 * generation it builds a `BotTurn` from the player's state, posts it to the
 * bot, and submits the answer with `complete: true` and the new memory. A
 * player with a default bot submits nothing, since the backend plays it. The
 * relay also asks the server to apply a turn's deadline once it passes while
 * other players have yet to answer. It has no DOM imports, so another client
 * can reuse it.
 */
export function createBotRelay(deps: RelayDeps): BotRelay {
  const status = createStore<RelayStatus>({ kind: "idle" });
  const graceMs = deps.deadlineGraceMs ?? 1000;
  const botRetryMs = deps.botRetryMs ?? 2000;
  let disposed = false;
  let held: string | undefined;
  let run = 0;
  let cancels = new Set<() => void>();
  let upgrades: Upgrade[] | undefined;

  function reset(): void {
    run++;
    for (const cancel of cancels) cancel();
    cancels = new Set();
  }

  function sleep(ms: number): Promise<void> {
    return new Promise((resolve) => {
      cancels.add(deps.schedule(resolve, ms));
    });
  }

  /** Asks for the deadline to be applied until the turn moves on. */
  function watchDeadline(state: GenerationState): void {
    if (!state.turn_deadline_at) return;
    const key = turnKey(state);
    const myRun = run;
    let attempt = 0;
    const open = () => {
      const now = deps.game.get();
      return (
        !disposed &&
        run === myRun &&
        now !== null &&
        !now.outcome &&
        turnKey(now) === key
      );
    };
    const arm = (ms: number) => {
      cancels.add(
        deps.schedule(async () => {
          if (!open()) return;
          try {
            await deps.applyDeadline();
          } catch {
            // The next ask follows; the server may be briefly unavailable.
          }
          if (!open()) return;
          arm(deadlineRetryDelay(attempt++));
        }, ms),
      );
    };
    arm(Math.max(Date.parse(state.turn_deadline_at) + graceMs - deps.now(), 0));
  }

  async function play(state: GenerationState): Promise<void> {
    const myRun = ++run;
    const alive = () => !disposed && run === myRun;
    const generationId = state.generation_id;

    let result: BotTurnResult;
    status.set({ kind: "playing", generationId });
    for (;;) {
      try {
        upgrades ??= await deps.loadUpgrades();
        result = await deps.playBot(buildBotTurn(state, upgrades));
        break;
      } catch (error) {
        if (!alive()) return;
        const unreachable =
          (error instanceof BotError && error.kind === "unreachable") ||
          isTransient(error);
        status.set({
          kind: "error",
          generationId,
          message:
            error instanceof Error ? error.message : "The bot call failed.",
          retrying: unreachable,
        });
        if (!unreachable) return;
        await sleep(botRetryMs);
        if (!alive()) return;
        status.set({ kind: "playing", generationId });
      }
    }
    if (!alive()) return;

    const submission: TurnSubmission = {
      game_id: state.game_id,
      generation_id: generationId,
      request_id: deps.newRequestId(),
      commands: result.commands,
      logs: result.logs,
      memory: result.memory,
      complete: true,
    };
    for (let attempt = 0; ; attempt++) {
      try {
        await deps.submit(submission);
        break;
      } catch (error) {
        if (!alive()) return;
        if (isGameEnded(error)) {
          // The game's last event arrives on the channel and resets the relay.
          status.set({ kind: "idle" });
          return;
        }
        const transient = isTransient(error);
        status.set({
          kind: "error",
          generationId,
          message:
            error instanceof Error ? error.message : "The turn was not sent.",
          retrying: transient,
        });
        if (!transient) return;
        await sleep(submitRetryDelay(attempt));
        if (!alive()) return;
      }
    }
    status.set({ kind: "submitted", generationId });
    const now = deps.game.get();
    watchDeadline(now && turnKey(now) === turnKey(state) ? now : state);
  }

  function handle(state: GenerationState | null): void {
    if (disposed) return;
    if (state === null || state.outcome) {
      held = undefined;
      reset();
      status.set({ kind: "idle" });
      return;
    }
    const key = turnKey(state);
    if (key === held) return;
    held = key;
    reset();
    if (state.bot.kind !== "localhost") {
      status.set({ kind: "idle" });
      // The backend plays this player; the relay still keeps the game moving.
      watchDeadline(state);
    } else if (state.submitted) {
      // Submitted earlier, perhaps from another tab: wait for the others.
      status.set({ kind: "submitted", generationId: state.generation_id });
      watchDeadline(state);
    } else {
      void play(state);
    }
  }

  const unsubscribe = deps.game.subscribe(handle);
  handle(deps.game.get());

  return {
    status,
    retry() {
      const state = deps.game.get();
      const current = status.get();
      if (
        disposed ||
        !state ||
        state.outcome ||
        state.bot.kind !== "localhost" ||
        current.kind !== "error" ||
        current.retrying ||
        current.generationId !== state.generation_id
      ) {
        return;
      }
      void play(state);
    },
    dispose() {
      disposed = true;
      unsubscribe();
      reset();
    },
  };
}
