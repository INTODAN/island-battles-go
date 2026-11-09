import type { Snapshot } from "../../realtime";
import type {
  AddSeatBody,
  BotChoice,
  LobbyBots,
  MapTemplateSummary,
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
  /** A person's bot choice, or a seat's default level. */
  bot: BotChoice | undefined;
}

/** The bot level a seat was added with, from its settings. */
export function seatLevel(
  settings: Record<string, unknown> | null | undefined,
): number | undefined {
  const level = settings?.bot_level;
  return typeof level === "number" ? level : undefined;
}

/** A seat's bot as a default choice of its level, or none if its settings name no level. */
function seatChoice(
  settings: Record<string, unknown> | null | undefined,
): BotChoice | undefined {
  const level = seatLevel(settings);
  return level === undefined ? undefined : { kind: "default", level };
}

/** The body that adds a bot seat of `level` to a team; the request ID makes a retry safe. */
export function seatBody(
  teamId: string,
  level: number,
  requestId: string,
): AddSeatBody {
  return {
    request_id: requestId,
    team_id: teamId,
    seat_settings: { bot_level: level },
  };
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
export function describeBot(bot: BotChoice | undefined): string {
  if (!bot) return "No bot yet";
  return bot.kind === "localhost"
    ? "Own bot (localhost)"
    : `Default bot, level ${bot.level ?? "?"}`;
}

/** The select value for a choice, "" for none. */
export function choiceValue(bot: BotChoice | undefined): string {
  if (!bot) return "";
  return bot.kind === "localhost" ? "localhost" : `level-${bot.level}`;
}

/** The choice a select value stands for, or null for none. */
export function choiceFromValue(value: string): BotChoice | null {
  if (value === "localhost") return { kind: "localhost" };
  const level = /^level-([1-9][0-9]*)$/.exec(value);
  return level ? { kind: "default", level: Number(level[1]) } : null;
}

/** Arranges a lobby snapshot and the members' bot choices for the room screen. */
export function buildRoom(
  snapshot: Snapshot,
  bots: LobbyBots | null,
  playerId: string,
): RoomModel {
  const botOf = new Map(
    (bots?.members ?? []).map((member) => [member.player_id, member.bot]),
  );
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
          : botOf.get(member.player_id),
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
