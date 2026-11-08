// The bots' logs for the turn shown, and the visibility rule: while a game runs a player sees
// only their own team's logs, and once it has an outcome everyone's. Pure, so `bun test` covers
// it.

import type { components } from "../../api/generated/game";

export type GenerationLogs = components["schemas"]["GenerationLogs"];

export const OTHER_TEAMS_HIDDEN =
  "Logs of other teams appear when the game ends";

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
  options: { ended: boolean; live: boolean },
): string | null {
  if (!logs || logs.hidden_team_colors.length === 0) return null;
  if (!options.live) {
    return options.ended ? null : SAVED_BEFORE_END;
  }
  return options.ended ? null : OTHER_TEAMS_HIDDEN;
}
