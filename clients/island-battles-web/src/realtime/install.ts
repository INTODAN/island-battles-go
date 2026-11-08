import type { components as GameComponents } from "../api/generated/game";
import type { components as LobbyComponents } from "../api/generated/lobby";

export type Snapshot = LobbyComponents["schemas"]["Snapshot"];
export type GenerationState = GameComponents["schemas"]["GenerationState"];
/** What a watcher outside the roster sees of a game: no player-only fields. */
export type WatchState = GameComponents["schemas"]["WatchState"];
export type WatchAccess = LobbyComponents["schemas"]["WatchAccess"];

/** Structural equality of JSON values, ignoring the `$schema` annotation HTTP bodies carry. */
export function sameJson(left: unknown, right: unknown): boolean {
  if (left === right) return true;
  if (
    typeof left !== "object" ||
    typeof right !== "object" ||
    left === null ||
    right === null
  ) {
    return false;
  }
  if (Array.isArray(left) || Array.isArray(right)) {
    return (
      Array.isArray(left) &&
      Array.isArray(right) &&
      left.length === right.length &&
      left.every((item, index) => sameJson(item, right[index]))
    );
  }
  const keys = (value: object) =>
    Object.keys(value).filter(
      (key) =>
        key !== "$schema" &&
        (value as Record<string, unknown>)[key] !== undefined,
    );
  const leftKeys = keys(left);
  return (
    leftKeys.length === keys(right).length &&
    leftKeys.every((key) =>
      sameJson(
        (left as Record<string, unknown>)[key],
        (right as Record<string, unknown>)[key],
      ),
    )
  );
}

/**
 * Decides which generation state to hold. Returns the state to install, or
 * null to keep `held`:
 * - another game's state, and any older generation, are dropped;
 * - once the game has an outcome, nothing changes: the outcome is final, so a
 *   later state for the same generation without it cannot remove it;
 * - a duplicate changes nothing;
 * - a later state for the held generation never reopens a submitted turn.
 * Generation IDs have gaps, so only their order is compared.
 */
export function nextGeneration(
  held: GenerationState | null,
  incoming: GenerationState,
): GenerationState | null {
  if (held === null) return incoming;
  if (incoming.game_id !== held.game_id) return null;
  if (incoming.generation_id < held.generation_id) return null;
  if (held.outcome) return null;
  const next =
    incoming.generation_id === held.generation_id &&
    held.submitted &&
    !incoming.submitted
      ? { ...incoming, submitted: true }
      : incoming;
  return sameJson(next, held) ? null : next;
}

/**
 * Decides which watch state to hold, by the rules of `nextGeneration`: another
 * game's state and older generations are dropped, nothing changes once the game
 * has an outcome, and a duplicate changes nothing.
 */
export function nextWatch(
  held: WatchState | null,
  incoming: WatchState,
): WatchState | null {
  if (held === null) return incoming;
  if (incoming.game_id !== held.game_id) return null;
  if (incoming.generation_id < held.generation_id) return null;
  if (held.outcome) return null;
  return sameJson(incoming, held) ? null : incoming;
}

/**
 * Decides which lobby snapshot to hold, for the lobby being followed. Returns
 * the snapshot to install, or null to keep `held`. A snapshot of another lobby
 * or an older version is dropped; an identical one changes nothing.
 */
export function nextSnapshot(
  held: Snapshot | null,
  incoming: Snapshot,
  lobbyId: string,
): Snapshot | null {
  if (incoming.lobby.id !== lobbyId) return null;
  if (held === null) return incoming;
  if (incoming.lobby.version < held.lobby.version) return null;
  return sameJson(incoming, held) ? null : incoming;
}
