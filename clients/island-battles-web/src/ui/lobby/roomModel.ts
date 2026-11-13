import type { Snapshot } from "../../realtime";
import type {
  AddSeatBody,
  BotSeatSettings,
  MapTemplateSummary,
  MemberBot,
} from "./actions";
import { describeTurnTimeout } from "./form";

export interface MemberRow {
  /** The member ID; for a seat, the seat ID that removing it takes. */
  memberId: string;
  playerId: string;
  name: string;
  /** A person, or a seat the host added: a bot the game plays. */
  kind: "human" | "seat";
  ready: boolean;
  /** False while a person's lease is lapsed. Seats are always connected. */
  connected: boolean;
  isYou: boolean;
  isHost: boolean;
  /** A person's bot choice, or a seat's bot. */
  bot: MemberBot | undefined;
}

/** The bot level a seat was added with, from its settings. */
export function seatLevel(
  settings: Record<string, unknown> | null | undefined,
): number | undefined {
  const level = settings?.bot_level;
  return typeof level === "number" ? level : undefined;
}

/** A seat's bot, from its settings: a default level, a hosted bot's URL or an uploaded package. */
function seatChoice(
  settings: Record<string, unknown> | null | undefined,
): MemberBot | undefined {
  const level = seatLevel(settings);
  if (level !== undefined) return { kind: "default", level };
  if (typeof settings?.bot_url === "string") {
    return { kind: "hosted", url: settings.bot_url };
  }
  if (typeof settings?.bot_package_id === "string") {
    return { kind: "uploaded", package_id: settings.bot_package_id };
  }
  return undefined;
}

/**
 * A person's bot, from their lobby member settings, `{"bot": ...}`, or none
 * until they choose one.
 */
export function memberSettingsBot(
  settings: Record<string, unknown> | null | undefined,
): MemberBot | undefined {
  const bot = settings?.bot;
  if (typeof bot !== "object" || bot === null) return undefined;
  const { kind, level, url, package_id } = bot as Record<string, unknown>;
  if (kind === "localhost") return { kind };
  if (kind === "default" && typeof level === "number") return { kind, level };
  if (kind === "hosted" && typeof url === "string") return { kind, url };
  if (kind === "uploaded" && typeof package_id === "string") {
    return { kind, package_id };
  }
  return undefined;
}

/** The body that adds a bot seat with `settings` to a team; the request ID makes a retry safe. */
export function seatBody(
  teamId: string,
  settings: BotSeatSettings,
  requestId: string,
): AddSeatBody {
  return {
    request_id: requestId,
    team_id: teamId,
    seat_settings: settings,
  };
}

/**
 * The seat settings that an Add bot select's value stands for: `level-N`,
 * `package-<id>`, or `hosted` with the URL typed next to it. Null when the
 * value needs a URL that is empty.
 */
export function seatSettingsFromValue(
  value: string,
  url: string,
): BotSeatSettings | null {
  const level = /^level-([1-9][0-9]*)$/.exec(value);
  if (level) return { bot_level: Number(level[1]) };
  if (value.startsWith("package-") && value.length > "package-".length) {
    return { bot_package_id: value.slice("package-".length) };
  }
  if (value === "hosted") {
    const trimmed = url.trim();
    return trimmed ? { bot_url: trimmed } : null;
  }
  return null;
}

/** The host name of a bot's URL, or the URL itself when it can't be read. */
export function urlHost(url: string): string {
  try {
    return new URL(url).hostname || url;
  } catch {
    return url;
  }
}

/** The people in a list of members: seats are bots, so they are not counted. */
export function countPeople(members: MemberRow[]): number {
  return members.filter((member) => member.kind === "human").length;
}

export interface TeamRow {
  id: string;
  name: string;
  /** The team colour, 1 to N, as the board draws it. */
  colour: number;
  /**
   * The team's member: a team holds one person or bot. Only a lobby created
   * before that rule can list more.
   */
  members: MemberRow[];
}

/** A team with a member, person or bot, has no room for anyone else. */
export function teamTaken(team: TeamRow): boolean {
  return team.members.length > 0;
}

export interface RoomModel {
  teams: TeamRow[];
  /** Members who have not chosen a team yet. */
  unassigned: MemberRow[];
  you: MemberRow | undefined;
  /** The team `you` is on. */
  yourTeamId: string | null;
  isHost: boolean;
  phase: string;
}

/** A few words naming a member's bot. */
export function describeBot(bot: MemberBot | undefined): string {
  if (!bot) return "No bot yet";
  switch (bot.kind) {
    case "localhost":
      return "Own bot (localhost)";
    case "default":
      return `Default bot, level ${bot.level ?? "?"}`;
    case "hosted":
      return `Hosted bot (${urlHost(bot.url ?? "")})`;
    case "uploaded":
      return "Uploaded bot";
  }
}

/** The radio value for a choice, "" for none. A hosted bot's URL is not in it. */
export function choiceValue(bot: MemberBot | undefined): string {
  if (!bot) return "";
  switch (bot.kind) {
    case "localhost":
      return "localhost";
    case "default":
      return `level-${bot.level}`;
    case "hosted":
      return "hosted";
    case "uploaded":
      return `package-${bot.package_id}`;
  }
}

/**
 * The choice a radio value stands for, or null for none. `hosted` takes the
 * URL typed next to it, and is null while that is empty.
 */
export function choiceFromValue(value: string, url = ""): MemberBot | null {
  if (value === "localhost") return { kind: "localhost" };
  if (value === "hosted") {
    const trimmed = url.trim();
    return trimmed ? { kind: "hosted", url: trimmed } : null;
  }
  if (value.startsWith("package-") && value.length > "package-".length) {
    return { kind: "uploaded", package_id: value.slice("package-".length) };
  }
  const level = /^level-([1-9][0-9]*)$/.exec(value);
  return level ? { kind: "default", level: Number(level[1]) } : null;
}

/** Arranges a lobby snapshot, with the members' bots, for the room screen. */
export function buildRoom(snapshot: Snapshot, playerId: string): RoomModel {
  const rows = (snapshot.members ?? [])
    .filter((member) => member.active)
    .sort((left, right) => left.join_sequence - right.join_sequence)
    .map((member): MemberRow & { teamId: string | null } => {
      const seat = member.kind === "seat";
      return {
        memberId: member.id,
        playerId: member.player_id,
        name: member.display_name,
        kind: seat ? "seat" : "human",
        ready: member.ready,
        connected: seat || member.disconnected_until === null,
        isYou: member.player_id === playerId,
        isHost: member.player_id === snapshot.lobby.host_player_id,
        bot: seat
          ? seatChoice(member.seat_settings)
          : memberSettingsBot(member.member_settings),
        teamId: member.team_id,
      };
    });
  const teams = [...(snapshot.teams ?? [])]
    .sort((left, right) => left.order - right.order)
    .map(
      (team, index): TeamRow => ({
        id: team.id,
        name: team.name,
        colour: index + 1,
        members: rows.filter((row) => row.teamId === team.id),
      }),
    );
  const you = rows.find((row) => row.isYou);
  return {
    teams,
    unassigned: rows.filter(
      (row) => !teams.some((team) => team.id === row.teamId),
    ),
    you,
    yourTeamId: you?.teamId ?? null,
    isHost: you?.isHost ?? false,
    phase: snapshot.lobby.phase,
  };
}

/** Only the host changes the map and turn limit, and only while the lobby waits. */
export function canChangeSettings(room: RoomModel): boolean {
  return room.isHost && room.phase === "waiting";
}

/**
 * The first condition the server would refuse a start for, as one sentence,
 * or null when the lobby can start. Checked in the server's order of concern;
 * the server stays the authority.
 */
export function startBlocker(
  room: RoomModel,
  startRules: Snapshot["start_rules"] | undefined,
): string | null {
  const members = [
    ...room.teams.flatMap((team) => team.members),
    ...room.unassigned,
  ];
  if (members.length < 2) return "Waiting for another player or bot.";
  if (room.unassigned.length > 0) return "Everyone needs a team.";
  const away = members.find((member) => !member.connected);
  if (away) return `Waiting for ${away.name} to reconnect.`;
  const unready = members.find(
    (member) => !member.ready && member.kind === "human",
  );
  if (unready) return `Waiting for ${unready.name} to be ready.`;
  if (
    startRules?.every_team_needs_member &&
    room.teams.some((team) => team.members.length === 0)
  ) {
    return "Every team needs a player or a bot.";
  }
  return null;
}

/** One line naming the map and the turn limit a lobby's settings chose. */
export function describeSettings(
  settings: Record<string, unknown> | undefined,
  templates: MapTemplateSummary[],
): string {
  const limit = describeTurnTimeout(settings);
  let map = "Map: generated for the players";
  const file = settings?.map;
  if (typeof file === "object" && file !== null && "map_name" in file) {
    map = `Map: ${String((file as { map_name: unknown }).map_name)}`;
  } else if (typeof settings?.map_template_id === "number") {
    const id = settings.map_template_id;
    const found = templates.find((item) => item.map_template_id === id);
    map = found ? `Map: ${found.map_name}` : `Map: default map #${id}`;
  }
  return `${map} · ${limit}`;
}
