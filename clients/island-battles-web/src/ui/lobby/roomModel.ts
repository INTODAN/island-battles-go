import { defaultBotPort, parseBotPort } from "../../bots/localhost";
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
  /**
   * For a localhost bot seat only: who relays it, the host who added it (its settings'
   * `bot_relay_player_id`), or null once they are no longer in the lobby, which stops the start.
   */
  relay?: SeatRelay | null;
}

/** The person whose tab relays a localhost bot seat: the host who added it. */
export interface SeatRelay {
  playerId: string;
  name: string;
  isYou: boolean;
}

/** The bot level a seat was added with, from its settings. */
export function seatLevel(
  settings: Record<string, unknown> | null | undefined,
): number | undefined {
  const level = settings?.bot_level;
  return typeof level === "number" ? level : undefined;
}

/**
 * A seat's bot, from its settings: a default level, a localhost bot on a port of the host's
 * machine (sent as a number, or as digits by the lobby page), a hosted bot's URL or an uploaded
 * package.
 */
function seatChoice(
  settings: Record<string, unknown> | null | undefined,
): MemberBot | undefined {
  const level = seatLevel(settings);
  if (level !== undefined) return { kind: "default", level };
  const port = parseBotPort(
    settings?.bot_localhost_port as string | number | undefined,
  );
  if (port !== null) return { kind: "localhost", port };
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
  const { kind, level, url, package_id, port } = bot as Record<string, unknown>;
  if (kind === "manual") return { kind };
  if (kind === "localhost") {
    return typeof port === "number" ? { kind, port } : { kind };
  }
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
 * `package-<id>`, `hosted` with the URL typed next to it, or `localhost` with
 * the port typed next to it, which `playerId`, the host who adds it, relays.
 * Null when the value needs a URL that is empty, a port that isn't one, or the
 * host's player ID.
 */
export function seatSettingsFromValue(
  value: string,
  url: string,
  port = "",
  playerId = "",
): BotSeatSettings | null {
  const level = /^level-([1-9][0-9]*)$/.exec(value);
  if (level) return { bot_level: Number(level[1]) };
  if (value === "localhost") {
    const parsed = parseBotPort(port);
    return parsed === null || !playerId
      ? null
      : { bot_localhost_port: parsed, bot_relay_player_id: playerId };
  }
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

/**
 * A few words naming a member's bot. A localhost bot seat's, with its row's `relay`, runs on the
 * machine of the host who added it, whose tab alone relays it; nobody does once they left.
 */
export function describeBot(
  bot: MemberBot | undefined,
  relay?: SeatRelay | null,
): string {
  if (!bot) return "No bot yet";
  switch (bot.kind) {
    case "localhost":
      if (relay === null)
        return `Localhost bot on port ${bot.port ?? defaultBotPort}, whoever added it left, so the start is refused until it is removed`;
      if (relay)
        return `Localhost bot on ${relay.isYou ? "your" : `${relay.name}'s`} port ${bot.port ?? defaultBotPort}`;
      return bot.port && bot.port !== defaultBotPort
        ? `Own bot (localhost:${bot.port})`
        : "Own bot (localhost)";
    case "default":
      return `Default bot, level ${bot.level ?? "?"}`;
    case "hosted":
      return `Hosted bot (${urlHost(bot.url ?? "")})`;
    case "uploaded":
      return "Uploaded bot";
    case "manual":
      return "Plays by hand";
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
    case "manual":
      return "manual";
  }
}

/**
 * The choice a radio value stands for, or null for none. `hosted` takes the
 * URL typed next to it, and is null while that is empty. `localhost` takes the
 * port next to it, left out when it is the default one.
 */
export function choiceFromValue(
  value: string,
  url = "",
  port: number = defaultBotPort,
): MemberBot | null {
  if (value === "localhost") {
    return port === defaultBotPort
      ? { kind: "localhost" }
      : { kind: "localhost", port };
  }
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

/**
 * The ports of the localhost bots that your tab relays: your own localhost bot's and those of
 * the localhost bot seats that you added.
 */
export function yourPorts(model: RoomModel): number[] {
  const members = [
    ...model.teams.flatMap((team) => team.members),
    ...model.unassigned,
  ];
  return members.flatMap((member) =>
    member.bot?.kind === "localhost" &&
    (member.kind === "seat" ? member.relay?.isYou === true : member.isYou)
      ? [member.bot.port ?? defaultBotPort]
      : [],
  );
}

/**
 * The localhost bot seats of a room, with their ports and who relays them: the host who added
 * each, or null once they left.
 */
export function localhostSeats(
  model: RoomModel,
): { name: string; port: number; relay: SeatRelay | null }[] {
  return model.teams.flatMap((team) =>
    team.members.flatMap((member) =>
      member.kind === "seat" && member.bot?.kind === "localhost"
        ? [
            {
              name: member.name,
              port: member.bot.port ?? defaultBotPort,
              relay: member.relay ?? null,
            },
          ]
        : [],
    ),
  );
}

/** The localhost bot seats that you added, which your tab relays. */
export function yourSeats(
  model: RoomModel,
): { name: string; port: number; relay: SeatRelay | null }[] {
  return localhostSeats(model).filter((seat) => seat.relay?.isYou === true);
}

/**
 * Who relays a localhost bot seat, from its settings: the person among the lobby's active members
 * whose player ID is its `bot_relay_player_id`, or null.
 */
function seatRelay(
  settings: Record<string, unknown> | null | undefined,
  members: Snapshot["members"],
  playerId: string,
): SeatRelay | null {
  const relay = settings?.bot_relay_player_id;
  const member = (members ?? []).find(
    (candidate) =>
      candidate.player_id === relay &&
      candidate.active &&
      candidate.kind !== "seat",
  );
  return member
    ? {
        playerId: member.player_id,
        name: member.display_name,
        isYou: member.player_id === playerId,
      }
    : null;
}

/** Arranges a lobby snapshot, with the members' bots, for the room screen. */
export function buildRoom(snapshot: Snapshot, playerId: string): RoomModel {
  const rows = (snapshot.members ?? [])
    .filter((member) => member.active)
    .sort((left, right) => left.join_sequence - right.join_sequence)
    .map((member): MemberRow & { teamId: string | null } => {
      const seat = member.kind === "seat";
      const bot = seat
        ? seatChoice(member.seat_settings)
        : memberSettingsBot(member.member_settings);
      return {
        memberId: member.id,
        playerId: member.player_id,
        name: member.display_name,
        kind: seat ? "seat" : "human",
        ready: member.ready,
        connected: seat || member.disconnected_until === null,
        isYou: member.player_id === playerId,
        isHost: member.player_id === snapshot.lobby.host_player_id,
        bot,
        ...(seat && bot?.kind === "localhost"
          ? {
              relay: seatRelay(
                member.seat_settings,
                snapshot.members,
                playerId,
              ),
            }
          : {}),
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
  // Only the player who added a localhost bot seat may relay it.
  const orphan = members.find(
    (member) =>
      member.kind === "seat" &&
      member.bot?.kind === "localhost" &&
      member.relay === null,
  );
  if (orphan) {
    return `Remove ${orphan.name}: the player who added it left. The host can add it again from their own tab.`;
  }
  return null;
}

/** One line naming the map and the turn limit a lobby's settings chose. */
export function describeSettings(
  settings: Record<string, unknown> | undefined,
  templates: MapTemplateSummary[],
): string {
  const limit = describeTurnTimeout(settings);
  // Only a lobby created before every lobby needed a map has none.
  let map = "No map chosen";
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
