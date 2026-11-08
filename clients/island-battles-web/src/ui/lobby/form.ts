import type { components as GameComponents } from "../../api/generated/game";
import type { components as LobbyComponents } from "../../api/generated/lobby";

export type MapTemplateSummary =
  GameComponents["schemas"]["MapTemplateSummary"];
export type MapTemplate = GameComponents["schemas"]["MapTemplate"];
export type CreateLobbyBody = LobbyComponents["schemas"]["CreateLobbyBody"];

export const gameKey = "island-battles";

/** The board's team colours in order, as the replay and the 3D board name them. */
const colourNames = ["Blue", "Red", "Green", "Purple", "Amber", "Teal"];

export function teamName(colour: number): string {
  return colourNames[colour - 1] ?? `Team ${colour}`;
}

export const minTeams = 2;
/** The replay palette's size. */
export const maxTeams = 6;
export const maxTeamCapacity = 8;
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

export type MapChoice =
  | { kind: "generated" }
  | { kind: "template"; templateId: number }
  /** A map file's parsed JSON once `POST /map/validate` accepted it, or null with the problem to show. */
  | { kind: "file"; map: MapTemplate | null; error?: string };

export interface CreateFormValues {
  lobbyName: string;
  teamCount: number;
  teamCapacity: number;
  map: MapChoice;
  /** Seconds, or null for no limit. */
  turnTimeoutSeconds: number | null;
}

export type FormField =
  | "lobbyName"
  | "teamCount"
  | "teamCapacity"
  | "map"
  | "turnTimeout";

export type FormErrors = Partial<Record<FormField, string>>;

export type FormResult =
  | {
      ok: true;
      teams: { name: string; capacity: number }[];
      capacity: number;
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

/** The team count a map choice fixes, or null when the host picks it. */
export function fixedTeamCount(
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

/**
 * Checks the create form. A map decides the lobby's teams, so with a template
 * or a file the team count field is ignored and the map's count is used.
 */
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

  const fixed = fixedTeamCount(values.map, templates);
  const teamCount = fixed ?? values.teamCount;
  const choice = values.map;
  if (
    choice.kind === "template" &&
    fixed === null &&
    !templates.some((map) => map.map_template_id === choice.templateId)
  ) {
    errors.map = "That map is no longer on the list. Pick another.";
  }
  if (choice.kind === "file") {
    if (choice.error) errors.map = choice.error;
    else if (!choice.map)
      errors.map = "Choose a map file, or pick another map.";
  }
  if (
    !Number.isInteger(teamCount) ||
    teamCount < minTeams ||
    teamCount > maxTeams
  ) {
    if (fixed === null) {
      errors.teamCount = `Choose from ${minTeams} to ${maxTeams} teams.`;
    } else if (!errors.map) {
      errors.map = `This map is made for ${fixed} teams; lobbies have ${minTeams} to ${maxTeams}.`;
    }
  }
  if (
    !Number.isInteger(values.teamCapacity) ||
    values.teamCapacity < 1 ||
    values.teamCapacity > maxTeamCapacity
  ) {
    errors.teamCapacity = `Choose from 1 to ${maxTeamCapacity} players per team.`;
  }
  if (
    values.turnTimeoutSeconds !== null &&
    !turnTimeouts.some((option) => option.seconds === values.turnTimeoutSeconds)
  ) {
    errors.turnTimeout = "Pick one of the listed turn limits.";
  }
  if (Object.keys(errors).length > 0) return { ok: false, errors };

  const gameSettings: Record<string, unknown> = {
    turn_timeout_seconds: values.turnTimeoutSeconds,
  };
  if (values.map.kind === "template") {
    gameSettings.map_template_id = values.map.templateId;
  } else if (values.map.kind === "file" && values.map.map) {
    gameSettings.map = values.map.map;
  }
  return {
    ok: true,
    teams: Array.from({ length: teamCount }, (_, index) => ({
      name: teamName(index + 1),
      capacity: values.teamCapacity,
    })),
    capacity: teamCount * values.teamCapacity,
    gameSettings,
  };
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
