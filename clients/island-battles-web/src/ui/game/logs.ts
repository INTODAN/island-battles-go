// The bots' logs for the turn shown. Every team's logs are public, to players and watchers
// alike, while the game runs and after it. Pure, so `bun test` covers it.

import type { components } from "../../api/generated/game";

export type GenerationLogs = components["schemas"]["GenerationLogs"];

/**
 * Keeps the logs of each generation by id. Logs read while their turn was still open may lack
 * lines that bots log later, so they are not settled, and are read again once the turn is over.
 */
export class LogBook {
  private readonly byGeneration = new Map<
    number,
    { logs: GenerationLogs; settled: boolean }
  >();

  get(generationId: number): GenerationLogs | undefined {
    return this.byGeneration.get(generationId)?.logs;
  }

  /**
   * Stores `logs`, read when the turn was over (`settled`) or still open; returns whether the
   * book changed. Settled logs are never replaced by a read from while the turn was open, which
   * can answer after them.
   */
  add(logs: GenerationLogs, settled = true): boolean {
    const held = this.byGeneration.get(logs.generation_id);
    if (held?.settled && !settled) return false;
    this.byGeneration.set(logs.generation_id, { logs, settled });
    return true;
  }

  clear(): void {
    this.byGeneration.clear();
  }

  /**
   * Stores a history's logs. `openGenerationId` names the turn that was still open when the
   * history was read, if any: its logs are not settled.
   */
  addAll(all: readonly GenerationLogs[], openGenerationId?: number): void {
    for (const logs of all) {
      this.add(logs, logs.generation_id !== openGenerationId);
    }
  }

  /**
   * Whether the logs of a generation must be read from the server: they are missing, or they
   * were read while the turn was open and it is over now (`settled`).
   */
  needsFetch(generationId: number, settled: boolean): boolean {
    const held = this.byGeneration.get(generationId);
    return held === undefined || (settled && !held.settled);
  }
}

export interface LogFetchTimers {
  set: (callback: () => void, delayMs: number) => unknown;
  clear: (handle: unknown) => void;
}

const browserTimers: LogFetchTimers = {
  set: (callback, delayMs) => setTimeout(callback, delayMs),
  clear: (handle) => clearTimeout(handle as ReturnType<typeof setTimeout>),
};

/** How long a turn must stay on screen before its logs are requested. */
export const LOG_FETCH_DEBOUNCE_MS = 300;

/**
 * Decides when the logs of the turn on screen are requested. Nothing is requested while the
 * panel is closed. Opening the panel requests the turn on screen at once. While it stays open,
 * a turn is requested only after it has been shown for the debounce delay without changing, so
 * turns that flash by during live play cost no request. Pure apart from the injected timers.
 */
export class LogFetchScheduler {
  private wasOpen = false;
  private pending: { generationId: number; settled: boolean } | undefined;
  private handle: unknown;

  constructor(
    private readonly request: (generationId: number, settled: boolean) => void,
    private readonly timers: LogFetchTimers = browserTimers,
    private readonly delayMs: number = LOG_FETCH_DEBOUNCE_MS,
  ) {}

  /** Call on every view update and whenever the panel opens or closes. */
  update(view: {
    open: boolean;
    generationId: number;
    /** The turn is over: the game has ended or a later turn has arrived. */
    settled: boolean;
    needsFetch: boolean;
  }): void {
    const justOpened = view.open && !this.wasOpen;
    this.wasOpen = view.open;
    if (!view.open || !view.needsFetch) {
      this.cancel();
      return;
    }
    if (justOpened) {
      this.cancel();
      this.request(view.generationId, view.settled);
      return;
    }
    if (this.pending && this.pending.generationId === view.generationId) {
      // The same turn is still on screen: keep the running timer, with the latest state.
      this.pending.settled = view.settled;
      return;
    }
    this.cancel();
    this.pending = { generationId: view.generationId, settled: view.settled };
    this.handle = this.timers.set(() => {
      const due = this.pending;
      this.pending = undefined;
      this.handle = undefined;
      if (due) this.request(due.generationId, due.settled);
    }, this.delayMs);
  }

  cancel(): void {
    if (this.handle !== undefined) this.timers.clear(this.handle);
    this.handle = undefined;
    this.pending = undefined;
  }
}
