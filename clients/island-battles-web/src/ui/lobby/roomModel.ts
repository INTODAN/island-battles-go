import type { Snapshot } from "../../realtime";
import type { BotChoice, LobbyBots, MapTemplateSummary } from "./actions";
import { describeTurnTimeout } from "./form";

export interface MemberRow {
  memberId: string;
  playerId: string;
  name: string;
  ready: boolean;
  /** False while the member's lease is lapsed. */
  connected: boolean;
  isYou: boolean;
  isHost: boolean;
  bot: BotChoice | undefined;
}

export interface TeamRow {
  id: string;
  name: string;
  /** The team colour, 1 to N, as the board draws it. */
  colour: number;
  capacity: number;
  members: MemberRow[];
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
    .map((member): MemberRow & { teamId: string | null } => ({
      memberId: member.id,
      playerId: member.player_id,
      name: member.display_name,
      ready: member.ready,
      connected: member.disconnected_until === null,
      isYou: member.player_id === playerId,
      isHost: member.player_id === snapshot.lobby.host_player_id,
      bot: botOf.get(member.player_id),
      teamId: member.team_id,
    }));
  const teams = [...(snapshot.teams ?? [])]
    .sort((left, right) => left.order - right.order)
    .map(
      (team, index): TeamRow => ({
        id: team.id,
        name: team.name,
        colour: index + 1,
        capacity: team.capacity,
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
