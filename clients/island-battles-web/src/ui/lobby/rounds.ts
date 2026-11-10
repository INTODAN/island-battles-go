// Which screen a lobby member sees, by the lobby's round. A lobby plays one game per round:
// once the game has ended, a member reopens the lobby, which waits again in its next round.
// The app remembers the last round whose game view the member closed with "Back to lobby".

/** What the app knows when it decides. */
export interface RoundRouteInput {
  /** The lobby's phase. */
  phase: string;
  /** The lobby's round. */
  round: number;
  /** The last round whose game view the member closed in this lobby, or null. */
  closedRound: number | null;
  /** The round whose game view is showing now, or null. */
  viewingRound: number | null;
}

export type RoundRoute =
  /**
   * The game view of `round`. `reopened`: the member is still looking at the result of that
   * round while the lobby is open again, so the view says so next to "Back to lobby".
   */
  | { view: "game"; round: number; reopened: boolean }
  /**
   * The room. `readOnly`: the lobby's game is running and the member closed its view, so the
   * room shows who plays, with "Return to game".
   */
  | { view: "room"; readOnly: boolean };

/**
 * Decides the screen:
 * - a started round that the member hasn't closed shows its game, also when another member
 *   started it while the member was still looking at the previous result;
 * - a member still looking at a result stays on it after another member reopened the lobby;
 * - otherwise the room, read-only while a game runs.
 */
export function routeRound(input: RoundRouteInput): RoundRoute {
  const { phase, round, closedRound, viewingRound } = input;
  if (phase === "started" && round !== closedRound) {
    return { view: "game", round, reopened: false };
  }
  if (
    viewingRound !== null &&
    viewingRound !== closedRound &&
    viewingRound < round &&
    phase === "waiting"
  ) {
    return { view: "game", round: viewingRound, reopened: true };
  }
  return { view: "room", readOnly: phase === "started" };
}

/**
 * What "Back to lobby" does in the game view: once the game has ended it reopens the lobby
 * for the next game; while it runs it only shows the room, and the game goes on. When no
 * state of the game is known yet, such as a start that never made a game, it asks to reopen:
 * the server refuses with `game_running` if the game runs, and the room shows read-only.
 */
export function backToLobby(
  game: "ended" | "running" | "unknown",
): "reopen" | "show room" {
  return game === "running" ? "show room" : "reopen";
}
