import type { components as GameComponents } from "../../api/generated/game";
import type { components as LobbyComponents } from "../../api/generated/lobby";

export type MapTemplateSummary =
  GameComponents["schemas"]["MapTemplateSummary"];
export type MapTemplate = GameComponents["schemas"]["MapTemplate"];
export type CreateLobbyBody = LobbyComponents["schemas"]["CreateLobbyBody"];
export type ChangeSettingsBody =
  LobbyComponents["schemas"]["ChangeSettingsBody"];

export const gameKey = "island-battles";

/** The board's team colours in order, as the replay and the 3D board name them. */
const colourNames = ["Blue", "Red", "Green", "Purple", "Amber", "Teal"];

export function teamName(colour: number): string {
  return colourNames[colour - 1] ?? `Team ${colour}`;
}

export const minTeams = 2;
/** The replay palette's size. */
export const maxTeams = 6;
export const maxNameLength = 40;

/** The turn timeouts the form offers: seconds, or null for no limit. */
export const turnTimeouts: { seconds: number | null; label: string }[] = [
  { seconds: 10, label: "10 s" },
  { seconds: 30, label: "30 s" },
  { seconds: 120, label: "2 min" },
  { seconds: 600, label: "10 min" },
  { seconds: null, label: "No limit" },
];
export const defaultTurnTimeout = 30;

/** A short phrase for a lobby's turn timeout, from its `game_settings`. */
export function describeTurnTimeout(
  settings: Record<string, unknown> | undefined,
): string {
  const seconds =
    settings && "turn_timeout_seconds" in settings
      ? settings.turn_timeout_seconds
      : defaultTurnTimeout;
  if (seconds === null) return "No turn limit";
  if (typeof seconds !== "number") return "";
  return `Turn limit: ${seconds % 60 === 0 ? `${seconds / 60} min` : `${seconds} s`}`;
}

/**
 * The turn paces the form offers, as `turn_pace_milliseconds`: how long each
 * turn of a real-time game lasts, a game where someone plays by hand.
 */
export const turnPaces: { milliseconds: number; label: string }[] = [
  { milliseconds: 500, label: "0.5 s" },
  { milliseconds: 1000, label: "1 s" },
  { milliseconds: 2000, label: "2 s" },
];
export const defaultTurnPace = 1000;

/** A lobby's turn pace from its `game_settings`; 1000 ms when it sets none. */
export function turnPaceOf(
  settings: Record<string, unknown> | undefined,
): number {
  const milliseconds = settings?.turn_pace_milliseconds;
  return typeof milliseconds === "number" ? milliseconds : defaultTurnPace;
}

/** A turn pace in seconds, as the lobby shows it: 500 is "0.5 s", 1000 is "1 s". */
export function describePace(milliseconds: number): string {
  return `${milliseconds / 1000} s`;
}

/** The Rated checkbox's line for a signed-in host. */
export const ratedHint =
  "Rated games count for the leaderboard. Everyone signs in, and plays by hand, with one of their bot names or with a bot from the leaderboard.";
/** The Rated checkbox's line for a guest, who can't turn it on. */
export const ratedGuestHint = "Sign in to host a rated lobby.";

/**
 * The Rated checkbox's state for a host: a signed-in host turns it on and off; a guest can't
 * turn it on, but one who became the host of a rated lobby (the host role passes on when the
 * host leaves) can turn it off.
 */
export function ratedField(
  signedIn: boolean,
  currentlyRated: boolean,
): { disabled: boolean; hint: string } {
  if (signedIn) return { disabled: false, hint: ratedHint };
  return {
    disabled: !currentlyRated,
    hint: currentlyRated ? ratedGuestOffHint : ratedGuestHint,
  };
}
/** The Rated checkbox's line for a guest who hosts a rated lobby: they may only turn it off. */
export const ratedGuestOffHint =
  "Only a signed-in host can turn Rated on. You can turn it off.";

/** The game lengths the form offers, as `max_generations`: the most turns of a game. */
export const gameLengths = [150, 300, 600, 1200];
export const defaultGameLength = 300;

/** A lobby's game length from its `game_settings`; 300 when it sets none. */
function gameLengthOf(settings: Record<string, unknown> | undefined): number {
  const turns = settings?.max_generations;
  return typeof turns === "number" ? turns : defaultGameLength;
}

/** A phrase for a lobby's game length, or "" when it is the default 300 turns. */
export function describeGameLength(
  settings: Record<string, unknown> | undefined,
): string {
  const turns = gameLengthOf(settings);
  return turns === defaultGameLength ? "" : `Game length: ${turns} turns`;
}

/** Every lobby plays on a default map or a map file, which sets its teams. */
export type MapChoice =
  /** No map yet: the default maps are still loading, or none was picked. */
  | { kind: "none" }
  | { kind: "template"; templateId: number }
  /** A map file's parsed JSON once `POST /map/validate` accepted it, or null with the problem to show. */
  | { kind: "file"; map: MapTemplate | null; error?: string };

/** The map, turn limit, turn pace, game length and Rated fields, which the create form and the room's Change settings form share. */
export interface MapSettingsValues {
  map: MapChoice;
  /** Seconds, or null for no limit. */
  turnTimeoutSeconds: number | null;
  /**
   * The lobby's current turn limit when the form changes a lobby's settings:
   * accepted even when the list lacks it, as one set through the API may be.
   */
  currentTurnTimeoutSeconds?: number | null;
  /** How long each turn of a real-time game lasts, `turn_pace_milliseconds`. */
  turnPaceMilliseconds: number;
  /** The lobby's current turn pace, accepted even when the list lacks it. */
  currentTurnPaceMilliseconds?: number;
  /** The most turns of the game, `max_generations`. */
  maxGenerations: number;
  /** The lobby's current game length, accepted even when the list lacks it. */
  currentMaxGenerations?: number;
  /**
   * The lobby's `rated` setting, the **Rated** checkbox: a rated game counts
   * for the leaderboard. Off when left out.
   */
  rated?: boolean;
}

export interface CreateFormValues extends MapSettingsValues {
  lobbyName: string;
}

export type FormField =
  | "lobbyName"
  | "map"
  | "turnTimeout"
  | "turnPace"
  | "gameLength";

export type FormErrors = Partial<Record<FormField, string>>;

export type FormResult =
  | {
      ok: true;
      /** One player per team, so the lobby holds as many players as teams. */
      teams: { name: string }[];
      gameSettings: Record<string, unknown>;
    }
  | { ok: false; errors: FormErrors };

/** Reads a map file's text far enough for the form: a name and the team count. */
export function readMapFile(text: string): MapTemplate {
  let map: unknown;
  try {
    map = JSON.parse(text);
  } catch {
    throw new Error("The map file is not valid JSON.");
  }
  if (typeof map !== "object" || map === null || Array.isArray(map)) {
    throw new Error("The map file must hold one map object.");
  }
  const candidate = map as Record<string, unknown>;
  if (
    typeof candidate.player_amount !== "number" ||
    !Number.isInteger(candidate.player_amount) ||
    candidate.player_amount < 1
  ) {
    throw new Error("The map file needs a whole player_amount of at least 1.");
  }
  if (
    typeof candidate.map_name !== "string" ||
    candidate.map_name.trim() === ""
  ) {
    throw new Error("The map file needs a map_name.");
  }
  return map as MapTemplate;
}

/** The number of teams a map choice is made for, or null while it isn't known. */
export function mapTeamCount(
  choice: MapChoice,
  templates: MapTemplateSummary[],
): number | null {
  if (choice.kind === "template") {
    return (
      templates.find((map) => map.map_template_id === choice.templateId)
        ?.team_count ?? null
    );
  }
  if (choice.kind === "file" && choice.map) return choice.map.player_amount;
  return null;
}

/** Checks the create form. The map decides the lobby's teams. */
export function validateCreateForm(
  values: CreateFormValues,
  templates: MapTemplateSummary[],
): FormResult {
  const errors: FormErrors = {};
  const lobbyName = values.lobbyName.trim();
  if (lobbyName === "") errors.lobbyName = "Give the lobby a name.";
  else if ([...lobbyName].length > maxNameLength) {
    errors.lobbyName = `Use at most ${maxNameLength} characters.`;
  }
  const settings = validateMapSettings(values, templates);
  if (!settings.ok)
    return { ok: false, errors: { ...errors, ...settings.errors } };
  if (Object.keys(errors).length > 0) return { ok: false, errors };
  return settings;
}

/**
 * The map and turn limit fields' values for a lobby's `game_settings`, which
 * the room's Change settings form starts from.
 */
export function lobbyMapSettings(
  settings: Record<string, unknown> | undefined,
): MapSettingsValues {
  const seconds =
    settings && "turn_timeout_seconds" in settings
      ? settings.turn_timeout_seconds
      : defaultTurnTimeout;
  const turnTimeoutSeconds =
    seconds === null
      ? null
      : typeof seconds === "number"
        ? seconds
        : defaultTurnTimeout;
  let map: MapChoice = { kind: "none" };
  if (typeof settings?.map === "object" && settings.map !== null) {
    map = { kind: "file", map: settings.map as MapTemplate };
  } else if (typeof settings?.map_template_id === "number") {
    map = { kind: "template", templateId: settings.map_template_id };
  }
  return {
    map,
    turnTimeoutSeconds,
    currentTurnTimeoutSeconds: turnTimeoutSeconds,
    turnPaceMilliseconds: turnPaceOf(settings),
    currentTurnPaceMilliseconds: turnPaceOf(settings),
    maxGenerations: gameLengthOf(settings),
    currentMaxGenerations: gameLengthOf(settings),
    rated: settings?.rated === true,
  };
}

/** Checks the map and turn limit fields, and builds the teams and `game_settings` they choose. */
export function validateMapSettings(
  values: MapSettingsValues,
  templates: MapTemplateSummary[],
): FormResult {
  const errors: FormErrors = {};
  const choice = values.map;
  const teamCount = mapTeamCount(choice, templates);
  if (choice.kind === "none") {
    errors.map = "Pick a default map, or load a map file.";
  } else if (choice.kind === "template" && teamCount === null) {
    errors.map = "That map is no longer on the list. Pick another.";
  } else if (choice.kind === "file" && (choice.error || !choice.map)) {
    errors.map = choice.error || "Choose a map file, or pick another map.";
  } else if (
    teamCount !== null &&
    (!Number.isInteger(teamCount) ||
      teamCount < minTeams ||
      teamCount > maxTeams)
  ) {
    errors.map = `This map is made for ${teamCount} teams; lobbies have ${minTeams} to ${maxTeams}.`;
  }
  if (
    values.turnTimeoutSeconds !== null &&
    values.turnTimeoutSeconds !== values.currentTurnTimeoutSeconds &&
    !turnTimeouts.some((option) => option.seconds === values.turnTimeoutSeconds)
  ) {
    errors.turnTimeout = "Pick one of the listed turn limits.";
  }
  if (
    values.turnPaceMilliseconds !== values.currentTurnPaceMilliseconds &&
    !turnPaces.some(
      (option) => option.milliseconds === values.turnPaceMilliseconds,
    )
  ) {
    errors.turnPace = "Pick one of the listed turn paces.";
  }
  if (
    values.maxGenerations !== values.currentMaxGenerations &&
    !gameLengths.includes(values.maxGenerations)
  ) {
    errors.gameLength = "Pick one of the listed game lengths.";
  }
  if (teamCount === null || Object.keys(errors).length > 0) {
    return { ok: false, errors };
  }

  const gameSettings: Record<string, unknown> = {
    turn_timeout_seconds: values.turnTimeoutSeconds,
    turn_pace_milliseconds: values.turnPaceMilliseconds,
    max_generations: values.maxGenerations,
  };
  if (values.rated) gameSettings.rated = true;
  if (choice.kind === "template") {
    gameSettings.map_template_id = choice.templateId;
  } else if (choice.kind === "file") {
    gameSettings.map = choice.map;
  }
  return {
    ok: true,
    teams: Array.from({ length: teamCount }, (_, index) => ({
      name: teamName(index + 1),
    })),
    gameSettings,
  };
}

/** The body of `PUT …/settings` for the fields' checked choice, its `rated` setting included. */
export function changeSettingsBody(
  result: Extract<FormResult, { ok: true }>,
): ChangeSettingsBody {
  return { teams: result.teams, game_settings: result.gameSettings };
}

/** The 8-character join code as the server reads it: trimmed and upper-cased. */
export function normalizeJoinCode(text: string): string {
  return text.trim().toUpperCase();
}

export function validateJoinCode(text: string): string | undefined {
  const code = normalizeJoinCode(text);
  if (code === "") return "Enter the lobby's join code.";
  if (!/^[A-Z0-9]{8}$/.test(code))
    return "A join code has 8 letters and digits.";
  return undefined;
}

export function validateDisplayName(text: string): string | undefined {
  const name = text.trim();
  if (name === "") return "Enter your name first.";
  if ([...name].length > maxNameLength) {
    return `Use at most ${maxNameLength} characters.`;
  }
  return undefined;
}

/**
 * Keeps one `request_id` for a create until it is known whether the server
 * saw it: a transport failure leaves the id in place so the same create is
 * resent, and the next attempt with the same body reuses it. Any other
 * outcome ends the attempt, and the next create gets a fresh id.
 */
export class CreateRequests {
  private pending: { key: string; requestId: string } | undefined;
  constructor(private readonly newId: () => string) {}

  /** The id to send for a create with this body. */
  idFor(body: unknown): string {
    const key = JSON.stringify(body);
    if (this.pending?.key !== key) {
      this.pending = { key, requestId: this.newId() };
    }
    return this.pending.requestId;
  }

  /** Call when the server answered (success or a refusal): the id is spent. */
  settled(): void {
    this.pending = undefined;
  }
}
