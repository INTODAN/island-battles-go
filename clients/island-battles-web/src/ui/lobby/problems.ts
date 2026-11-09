import { ApiError } from "../../api/errors";

export interface ShownProblem {
  /** A plain sentence for the person. */
  message: string;
  /** Teams to highlight: for `empty_teams`, the teams that still need a player. */
  teamIds: string[];
  /** The player is no longer in the lobby; the screen goes back to the menu. */
  lostMembership: boolean;
  /** The session ended; the screen goes back to the name step. */
  signedOut: boolean;
}

/** Where a request was made from, for the codes whose meaning depends on it. */
export type ProblemContext =
  | "join"
  | "team"
  | "ready"
  | "start"
  | "bot"
  | "seat"
  | "create"
  | "watch";

const isBotNotChosen = (error: ApiError) =>
  error.problemType?.endsWith(":bot-not-chosen") === true;

/** Sentences for the lobby's problem codes, from the lobby page's wording. */
const sentences: Record<string, string> = {
  lobby_full: "This lobby is full. Every seat is taken.",
  team_full: "That team is taken. Please choose another team.",
  phase_conflict: "The lobby has already started or closed.",
  not_ready: "Every player needs a team and must be ready before starting.",
  empty_teams: "Every team needs a player before the game can start.",
  not_host: "Only the host can start the lobby.",
  stale_membership:
    "You're no longer connected to that lobby. Use its code to join again.",
  not_found: "No lobby found for that code.",
  watcher_kicked: "The host removed you from this game's watchers.",
  watchers_full:
    "This game has as many watchers as it allows. Try again later.",
  unavailable: "The lobby server is temporarily unavailable. Please try again.",
  idempotency_conflict:
    "That create request was already used for a different lobby. Try again.",
};

/** The sentences for adding or removing a bot seat, which replace the lobby's where they differ. */
const seatSentences: Record<string, string> = {
  not_host: "Only the host can add or remove bots.",
  team_full: "That team already has a player or a bot.",
  phase_conflict: "The lobby has already started, so its bots can't change.",
  invalid_seat_settings: "That bot level was refused. Choose another level.",
  idempotency_conflict:
    "The lobby changed while that bot was being added. Try again.",
};

const botNotChosen =
  "Choose your bot first: your localhost bot or a default level.";

/** Turns a failed call into what the screen shows next to the control that caused it. */
export function describeProblem(
  error: unknown,
  context: ProblemContext,
): ShownProblem {
  const shown: ShownProblem = {
    message: "Something went wrong. Please try again.",
    teamIds: [],
    lostMembership: false,
    signedOut: false,
  };
  if (!(error instanceof ApiError)) {
    if (error instanceof Error) shown.message = error.message;
    return shown;
  }
  if (error.kind === "transport") {
    shown.message =
      "Cannot reach the server. Check your connection and try again.";
    return shown;
  }
  if (error.status === 401) {
    shown.signedOut = true;
    shown.message = "Your session expired. Enter your name to continue.";
    return shown;
  }
  const code = error.code;
  if (context === "watch" && code === "not_found") {
    shown.message = "No game found for that code.";
    return shown;
  }
  if (
    code === "stale_membership" ||
    (code === "not_found" && context !== "join" && context !== "create")
  ) {
    shown.lostMembership = true;
    shown.message = sentences.stale_membership ?? shown.message;
    return shown;
  }
  if (code === "game_requirement" || isBotNotChosen(error)) {
    if (isBotNotChosen(error)) {
      // At the start the server names the members who have no bot.
      shown.message =
        context === "start" && error.detail ? error.detail : botNotChosen;
    } else {
      shown.message = error.detail ?? "The game is not ready yet.";
    }
    return shown;
  }
  if (code === "empty_teams") shown.teamIds = error.teamIds ?? [];
  if (code === "invalid_game_settings") {
    shown.message =
      `The map or turn limit was refused. ${error.detail ?? ""}`.trim();
    return shown;
  }
  if (code === "invalid_input") {
    shown.message = error.detail ?? "Check what you entered and try again.";
    return shown;
  }
  const known =
    code === undefined
      ? undefined
      : context === "seat"
        ? (seatSentences[code] ?? sentences[code])
        : sentences[code];
  if (known) {
    shown.message = known;
    return shown;
  }
  shown.message = error.detail ?? error.title ?? shown.message;
  return shown;
}
