// The bots' logs for the turn shown, and the visibility rule: while a game runs a player sees
// only their own team's logs, and once it has an outcome everyone's. Pure, so `bun test` covers
// it.

import type { components } from "../../api/generated/game";

export type GenerationLogs = components["schemas"]["GenerationLogs"];

export const OTHER_TEAMS_HIDDEN =
  "Logs of other teams appear when the game ends";

export const ALL_TEAMS_HIDDEN = "Bot logs appear when the game ends";

export const SAVED_BEFORE_END =
  "This demo was saved before the game ended, so it holds only the saver’s team’s logs";

/** Whether `candidate` shows more than `held`: fewer hidden teams, or `held` is missing. */
export function isFuller(
  held: GenerationLogs | undefined,
  candidate: GenerationLogs,
): boolean {
  return (
    held === undefined ||
    candidate.hidden_team_colors.length < held.hidden_team_colors.length
  );
}

/** Keeps the logs of each generation by id, replacing an entry only with a fuller one. */
export class LogBook {
  private readonly byGeneration = new Map<number, GenerationLogs>();

  get(generationId: number): GenerationLogs | undefined {
    return this.byGeneration.get(generationId);
  }

  /** Stores `logs`; returns whether the book changed. */
  add(logs: GenerationLogs): boolean {
    if (!isFuller(this.byGeneration.get(logs.generation_id), logs)) {
      return false;
    }
    this.byGeneration.set(logs.generation_id, logs);
    return true;
  }

  clear(): void {
    this.byGeneration.clear();
  }

  addAll(all: readonly GenerationLogs[]): void {
    for (const logs of all) this.add(logs);
  }

  /**
   * Whether the logs of a generation must be read from the server: they are missing, or they
   * hide teams although the game has ended since.
   */
  needsFetch(generationId: number, ended: boolean): boolean {
    const held = this.byGeneration.get(generationId);
    return held === undefined || (ended && held.hidden_team_colors.length > 0);
  }
}

/**
 * The line to show above the logs when some teams' logs are left out, or null. `live` is false
 * for a saved demo, whose hidden logs will not appear later.
 */
export function hiddenNotice(
  logs: GenerationLogs | undefined,
  options: { ended: boolean; live: boolean; watching?: boolean },
): string | null {
  if (!logs || logs.hidden_team_colors.length === 0) return null;
  if (!options.live) {
    return options.ended ? null : SAVED_BEFORE_END;
  }
  if (options.ended) return null;
  // A watcher belongs to no team, so every team is hidden until the end.
  return options.watching ? ALL_TEAMS_HIDDEN : OTHER_TEAMS_HIDDEN;
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
  private pending: { generationId: number; ended: boolean } | undefined;
  private handle: unknown;

  constructor(
    private readonly request: (generationId: number, ended: boolean) => void,
    private readonly timers: LogFetchTimers = browserTimers,
    private readonly delayMs: number = LOG_FETCH_DEBOUNCE_MS,
  ) {}

  /** Call on every view update and whenever the panel opens or closes. */
  update(view: {
    open: boolean;
    generationId: number;
    ended: boolean;
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
      this.request(view.generationId, view.ended);
      return;
    }
    if (this.pending && this.pending.generationId === view.generationId) {
      // The same turn is still on screen: keep the running timer, with the latest state.
      this.pending.ended = view.ended;
      return;
    }
    this.cancel();
    this.pending = { generationId: view.generationId, ended: view.ended };
    this.handle = this.timers.set(() => {
      const due = this.pending;
      this.pending = undefined;
      this.handle = undefined;
      if (due) this.request(due.generationId, due.ended);
    }, this.delayMs);
  }

  cancel(): void {
    if (this.handle !== undefined) this.timers.clear(this.handle);
    this.handle = undefined;
    this.pending = undefined;
  }
}
