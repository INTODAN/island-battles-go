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

/**
 * Keeps the logs of each generation by id, replacing an entry only with a fuller one.
 *
 * Logs read while their turn was still open (the newest generation of a running game) are
 * provisional: the bots haven't finished that turn, so the answer may lack lines they log
 * before it ends. Such an entry is read again once a newer generation exists, and any answer
 * replaces it.
 */
export class LogBook {
  private readonly byGeneration = new Map<
    number,
    { logs: GenerationLogs; provisional: boolean }
  >();

  get(generationId: number): GenerationLogs | undefined {
    return this.byGeneration.get(generationId)?.logs;
  }

  /** Whether the logs held for a generation were read while it was the open turn. */
  isProvisional(generationId: number): boolean {
    return this.byGeneration.get(generationId)?.provisional === true;
  }

  /**
   * The logs of an ended turn, if held: logs read while it was open are left out, since they
   * may lack lines and are read again.
   */
  settled(generationId: number): GenerationLogs | undefined {
    const held = this.byGeneration.get(generationId);
    return held && !held.provisional ? held.logs : undefined;
  }

  /**
   * Stores `logs`; returns whether the book changed. `provisional` marks logs read while their
   * turn was open.
   */
  add(logs: GenerationLogs, options: { provisional?: boolean } = {}): boolean {
    const provisional = options.provisional === true;
    const held = this.byGeneration.get(logs.generation_id);
    // Any answer replaces logs read while their turn was open, even one hiding more teams:
    // otherwise the entry would stay provisional, with its read already made.
    if (held?.provisional !== true && !isFuller(held?.logs, logs)) return false;
    this.byGeneration.set(logs.generation_id, { logs, provisional });
    return true;
  }

  clear(): void {
    this.byGeneration.clear();
  }

  /**
   * Stores the logs of a history. `openGenerationId` names its open turn, the newest generation
   * of a running game, whose logs are provisional.
   */
  addAll(all: readonly GenerationLogs[], openGenerationId?: number): void {
    for (const logs of all) {
      this.add(logs, { provisional: logs.generation_id === openGenerationId });
    }
  }

  /**
   * Whether the logs of a generation must be read from the server: they are missing, they were
   * read while the turn was open and it has ended since (`open` is false), or they hide teams
   * although the game has ended since.
   */
  needsFetch(
    generationId: number,
    state: { ended: boolean; open: boolean },
  ): boolean {
    const held = this.byGeneration.get(generationId);
    if (held === undefined) return true;
    if (held.provisional && !state.open) return true;
    return state.ended && held.logs.hidden_team_colors.length > 0;
  }
}

/** The open turn of a game: its newest generation while it runs, or undefined once it has ended. */
export function openGenerationId(
  frames: readonly { id: number }[],
  ended: boolean,
): number | undefined {
  return ended ? undefined : frames.at(-1)?.id;
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
 * The longest a request waits while the turn keeps changing, as it does while following a fast
 * game: then the turn on screen at that moment is requested.
 */
export const LOG_FETCH_MAX_WAIT_MS = 1000;

/**
 * Decides when the logs of the turn on screen are requested. Nothing is requested while the
 * panel is closed. Opening the panel requests the turn on screen at once. While it stays open,
 * a turn is requested after it has been shown for the debounce delay without changing, so
 * turns that flash by cost no request; while the turn keeps changing, the one on screen is
 * requested once the longer wait has passed, so a panel following a fast game still fills in,
 * with at most one request per wait. Pure apart from the injected timers.
 */
export class LogFetchScheduler {
  private wasOpen = false;
  private pending: { generationId: number; ended: boolean } | undefined;
  private handle: unknown;
  private maxWaitHandle: unknown;

  constructor(
    private readonly request: (generationId: number, ended: boolean) => void,
    private readonly timers: LogFetchTimers = browserTimers,
    private readonly delayMs: number = LOG_FETCH_DEBOUNCE_MS,
    private readonly maxWaitMs: number = LOG_FETCH_MAX_WAIT_MS,
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
    // Another turn: wait for it to stay, but no longer than the wait that is already running.
    if (this.handle !== undefined) this.timers.clear(this.handle);
    this.pending = { generationId: view.generationId, ended: view.ended };
    this.handle = this.timers.set(() => this.fire(), this.delayMs);
    if (this.maxWaitHandle === undefined) {
      this.maxWaitHandle = this.timers.set(() => this.fire(), this.maxWaitMs);
    }
  }

  cancel(): void {
    if (this.handle !== undefined) this.timers.clear(this.handle);
    if (this.maxWaitHandle !== undefined) this.timers.clear(this.maxWaitHandle);
    this.handle = undefined;
    this.maxWaitHandle = undefined;
    this.pending = undefined;
  }

  private fire(): void {
    const due = this.pending;
    this.cancel();
    if (due) this.request(due.generationId, due.ended);
  }
}

/** What the logs panel shows for the turn on screen. */
export interface LogsChoice {
  /** Index of the turn whose logs are read when missing, or -1 when none is. */
  target: number;
  /** Index of the turn whose logs are shown, or loading; -1 when none is. */
  shown: number;
  /** The logs shown; undefined while they load, or when no turn's are shown. */
  logs: GenerationLogs | undefined;
  /** Says that the turn on screen is still open, and which turn is shown instead; or null. */
  openNote: string | null;
}

/**
 * Picks the logs for the turn on screen. The open turn of a running game (its newest) has no
 * settled logs yet, since its bots are still at work, so the panel shows the latest turn that
 * has ended: the one before. While that turn's logs load, it keeps showing the newest earlier
 * turn whose logs are held, so a panel following live shows lines rather than a loading note.
 * A watcher sees no team's logs before the end, so nothing is redirected for them.
 */
export function chooseLogs(
  view: {
    frames: readonly { id: number }[];
    index: number;
    ended: boolean;
    live: boolean;
    watching: boolean;
  },
  book: LogBook,
): LogsChoice {
  const { frames, index } = view;
  const frame = frames[index];
  if (!frame) return { target: -1, shown: -1, logs: undefined, openNote: null };
  const isOpenTurn =
    view.live && frame.id === openGenerationId(frames, view.ended);
  if (!isOpenTurn || view.watching) {
    // Logs read while an ended turn was open show only once they have been read again.
    const logs = isOpenTurn ? book.get(frame.id) : book.settled(frame.id);
    return { target: index, shown: index, logs, openNote: null };
  }
  const turn = index + 1;
  if (index === 0) {
    return {
      target: -1,
      shown: -1,
      logs: undefined,
      openNote: `Turn ${turn} is still open: the bots’ lines for it appear here once it ends.`,
    };
  }
  const target = index - 1;
  for (let earlier = target; earlier >= 0; earlier--) {
    const logs = book.settled(frames[earlier]?.id ?? -1);
    if (!logs) continue;
    const openNote =
      earlier === target
        ? `Turn ${turn} is still open, so this shows turn ${earlier + 1}, the latest that has ended.`
        : `Turn ${turn} is still open, so this shows turn ${earlier + 1} while the logs of turn ${target + 1} load.`;
    return { target, shown: earlier, logs, openNote };
  }
  return {
    target,
    shown: target,
    logs: undefined,
    openNote: `Turn ${turn} is still open, so this shows turn ${target + 1}, the latest that has ended.`,
  };
}
