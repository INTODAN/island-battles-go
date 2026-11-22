import { type ListedBot, listedBotLabel } from "../../bots/kinds";
import { defaultBotPort, parseBotPort } from "../../bots/localhost";
import type { Snapshot } from "../../realtime";
import type {
  AddSeatBody,
  BotSeatSettings,
  MapTemplateSummary,
  MemberBot,
} from "./actions";
import {
  describeGameLength,
  describePace,
  describeTurnTimeout,
  turnPaceOf,
} from "./form";

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
  /** Whether a person had signed in with Google when they joined. Always false for a seat. */
  signedIn: boolean;
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
 * machine (sent as a number, or as digits by the lobby page), a hosted bot's URL, an uploaded
 * package or a bot from the leaderboard, by its competitor ID.
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
  if (typeof settings?.bot_competitor_id === "string") {
    return { kind: "hosted", competitor_id: settings.bot_competitor_id };
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
  const { kind, level, url, package_id, port, name, competitor_id } =
    bot as Record<string, unknown>;
  // The bot name a localhost, hosted or uploaded bot plays as, in a rated lobby.
  const named = typeof name === "string" && name !== "" ? { name } : {};
  if (kind === "manual") return { kind };
  if (kind === "localhost") {
    return typeof port === "number"
      ? { kind, port, ...named }
      : { kind, ...named };
  }
  if (kind === "default" && typeof level === "number") return { kind, level };
  // A bot from the leaderboard, by its competitor ID, which plays at its name's URL (#412).
  if (kind === "hosted" && typeof competitor_id === "string") {
    return { kind, competitor_id };
  }
  if (kind === "hosted" && typeof url === "string") {
    return { kind, url, ...named };
  }
  // A named hosted bot without a URL plays at its name's URL (#400).
  if (kind === "hosted" && named.name) return { kind, ...named };
  if (kind === "uploaded" && typeof package_id === "string") {
    return { kind, package_id, ...named };
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
 * `package-<id>`, `competitor-<id>` for a bot from the leaderboard, `hosted`
 * with the URL typed next to it, or `localhost` with the port typed next to
 * it, which `playerId`, the host who adds it, relays. Null when the value
 * needs a URL that is empty, a port that isn't one, or the host's player ID.
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
  if (value.startsWith("competitor-") && value.length > "competitor-".length) {
    return { bot_competitor_id: value.slice("competitor-".length) };
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
  /** The lobby's `rated` setting: its game counts for the leaderboard. */
  rated: boolean;
}

/**
 * A few words naming a member's bot. A localhost bot seat's, with its row's `relay`, runs on the
 * machine of the host who added it, whose tab alone relays it; nobody does once they left. A bot
 * that plays as one of its player's bot names is named by it: "Kraken (own bot, localhost)". A
 * bot from the leaderboard is named from `listed` with its owner, "Kraken, by Dana (registered
 * bot)", while the room knows it.
 */
export function describeBot(
  bot: MemberBot | undefined,
  relay?: SeatRelay | null,
  listed: readonly ListedBot[] = [],
): string {
  if (!bot) return "No bot yet";
  if (bot.kind === "hosted" && bot.competitor_id) {
    const found = listed.find(
      (candidate) => candidate.competitorId === bot.competitor_id,
    );
    return found
      ? `${listedBotLabel(found)} (registered bot)`
      : "A registered bot";
  }
  if (bot.name) {
    switch (bot.kind) {
      case "localhost":
        return bot.port && bot.port !== defaultBotPort
          ? `${bot.name} (own bot, localhost:${bot.port})`
          : `${bot.name} (own bot, localhost)`;
      case "hosted":
        return bot.url
          ? `${bot.name} (hosted bot, ${urlHost(bot.url)})`
          : `${bot.name} (hosted bot)`;
      case "uploaded":
        return `${bot.name} (uploaded bot)`;
    }
  }
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

/** The radio value of a bot from the leaderboard; its competitor ID is in the select beside it. */
export const leaderboardValue = "leaderboard";

/**
 * The radio value for a choice, "" for none. A hosted bot's URL is not in it, nor a bot from the
 * leaderboard's competitor ID.
 */
export function choiceValue(bot: MemberBot | undefined): string {
  if (!bot) return "";
  switch (bot.kind) {
    case "localhost":
      return "localhost";
    case "default":
      return `level-${bot.level}`;
    case "hosted":
      return bot.competitor_id ? leaderboardValue : "hosted";
    case "uploaded":
      return `package-${bot.package_id}`;
    case "manual":
      return "manual";
  }
}

/**
 * The choice a radio value stands for, or null for none. `hosted` takes the
 * URL typed next to it, and is null while that is empty. `localhost` takes the
 * port next to it, left out when it is the default one. A localhost, hosted or
 * uploaded bot takes `name`, the bot name the saved choice kept (`keptName` in
 * `botPicker.ts`), left out when it is ""; the other kinds have none.
 */
export function choiceFromValue(
  value: string,
  url = "",
  port: number = defaultBotPort,
  name = "",
): MemberBot | null {
  const named = name ? { name } : {};
  if (value === "localhost") {
    return port === defaultBotPort
      ? { kind: "localhost", ...named }
      : { kind: "localhost", port, ...named };
  }
  if (value === "manual") return { kind: "manual" };
  if (value === "hosted") {
    const trimmed = url.trim();
    return trimmed ? { kind: "hosted", url: trimmed, ...named } : null;
  }
  if (value.startsWith("package-") && value.length > "package-".length) {
    return {
      kind: "uploaded",
      package_id: value.slice("package-".length),
      ...named,
    };
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
  const rated = snapshot.lobby.game_settings?.rated === true;
  const rows = (snapshot.members ?? [])
    .filter((member) => member.active)
    .sort((left, right) => left.join_sequence - right.join_sequence)
    .map((member): MemberRow & { teamId: string | null } => {
      const seat = member.kind === "seat";
      const chosen = seat
        ? seatChoice(member.seat_settings)
        : memberSettingsBot(member.member_settings);
      // A rated lobby plays a named hosted bot at its name's URL, whatever url the choice has.
      const bot =
        rated && chosen?.kind === "hosted" && chosen.name
          ? { kind: chosen.kind, name: chosen.name }
          : chosen;
      return {
        memberId: member.id,
        playerId: member.player_id,
        name: member.display_name,
        kind: seat ? "seat" : "human",
        ready: member.ready,
        connected: seat || member.disconnected_until === null,
        isYou: member.player_id === playerId,
        isHost: member.player_id === snapshot.lobby.host_player_id,
        signedIn: !seat && member.signed_in === true,
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
    rated,
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

/** Your blocker in a rated lobby while you aren't signed in. */
export const ratedSignInBlocker = "Sign in to play in this rated lobby.";
/** Your blocker in a rated lobby while your bot plays under no bot name: a default bot, or a bot without one. */
export const ratedBotNameBlocker =
  "Play by hand, or choose one of your hosted bot names or a registered bot.";
/** Your blocker in a rated lobby while you chose a named localhost or uploaded bot (#400). */
export const ratedHostedBlocker =
  "Rated bots run at their own URL: choose one of your hosted bot names, or play by hand.";
/** Your blocker in a rated lobby while your bot name has no URL yet, as the server says it. */
export const botNameUrlBlocker = (name: string): string =>
  `Your bot name ${name} has no URL yet. Add its hosted bot's URL on the account page.`;
/** The rule of a rated lobby's bot seats, as the server says it. */
export const ratedSeatRule =
  "a rated game allows only default bot seats, each level once, and registered bots, each once.";
/** The tag beside a person who isn't signed in, in a rated lobby. */
export const guestTag = "guest";

/** One of your bot names, with its hosted bot's URL, null for a name registered before names had URLs. */
export interface BotName {
  name: string;
  url: string | null;
  /** Its competitor ID, which the leaderboard lists it by. */
  id?: string;
}

/** What `ratedBlockers` knows of your account: your bot names, once the game's `GET /account` answered. */
export interface RatedAccount {
  botNames: readonly BotName[];
}

/** One of your bot names, found without regard to case, as the server compares them. */
export function findBotName(
  names: readonly BotName[],
  name: string,
): BotName | undefined {
  const key = name.toLowerCase();
  return names.find((candidate) => candidate.name.toLowerCase() === key);
}

/** What a rated lobby is still missing before its start, as the room shows it. */
export interface RatedBlockers {
  /** What keeps you from getting ready, shown above the Ready button, or null. */
  you: string | null;
  /** For the host: the bot seats that a rated game doesn't allow, or null. */
  host: string | null;
  /** The member IDs of the other people who aren't signed in, tagged "guest". */
  guests: string[];
}

const noRatedBlockers = (): RatedBlockers => ({
  you: null,
  host: null,
  guests: [],
});

/**
 * The default bot levels that the lobby's seats have, each once, in seat order: on a team or
 * not, as the server counts them.
 */
export function seatLevels(room: RoomModel): number[] {
  const levels: number[] = [];
  for (const member of [
    ...room.teams.flatMap((team) => team.members),
    ...room.unassigned,
  ]) {
    if (
      member.kind === "seat" &&
      member.bot?.kind === "default" &&
      member.bot.level !== undefined &&
      !levels.includes(member.bot.level)
    ) {
      levels.push(member.bot.level);
    }
  }
  return levels;
}

/** The competitor IDs of the lobby's seats of bots from the leaderboard, on a team or not. */
export function seatCompetitors(room: RoomModel): string[] {
  return [...room.teams.flatMap((team) => team.members), ...room.unassigned]
    .filter((member) => member.kind === "seat")
    .flatMap((member) =>
      member.bot?.kind === "hosted" && member.bot.competitor_id
        ? [member.bot.competitor_id]
        : [],
    );
}

/**
 * The seats that a rated game doesn't allow, by name, as the server finds them: every seat that
 * is neither a default bot level nor a bot from the leaderboard, and every seat of a level or a
 * bot after its first.
 */
function ratedSeatsToRemove(room: RoomModel): string[] {
  const remove: string[] = [];
  const levels = new Set<number>();
  const bots = new Set<string>();
  for (const member of [
    ...room.teams.flatMap((team) => team.members),
    ...room.unassigned,
  ]) {
    if (member.kind !== "seat") continue;
    const level = member.bot?.kind === "default" ? member.bot.level : undefined;
    const bot =
      member.bot?.kind === "hosted" ? member.bot.competitor_id : undefined;
    if (level !== undefined && !levels.has(level)) levels.add(level);
    else if (bot !== undefined && !bots.has(bot)) bots.add(bot);
    else remove.push(member.name);
  }
  return remove;
}

/**
 * What a rated lobby still needs before it can start, in the server's own terms (its ready gate,
 * `backend/services/rated_lobbies.go`): for you, to sign in, or to choose one of your hosted bot
 * names or a bot from the leaderboard, or play by hand (also when you chose a default bot, or a
 * named localhost or uploaded bot), another name once yours was deleted, or a URL for a name
 * without one; for the host, the seats to remove. Nothing for a lobby that isn't rated. `account`
 * holds your bot names once read, or null; a name is checked against them only then. The server
 * stays the authority: it alone knows two members of one account, whether a bot from the
 * leaderboard is still listed or paused, and the daily caps, for example.
 */
export function ratedBlockers(
  room: RoomModel,
  account: RatedAccount | null,
): RatedBlockers {
  if (!room.rated) return noRatedBlockers();
  const blockers = noRatedBlockers();
  const people = [
    ...room.teams.flatMap((team) => team.members),
    ...room.unassigned,
  ].filter((member) => member.kind === "human");
  blockers.guests = people
    .filter((member) => !member.isYou && !member.signedIn)
    .map((member) => member.memberId);
  const you = room.you;
  if (you?.kind === "human") {
    const bot = you.bot;
    const found =
      bot?.name && account ? findBotName(account.botNames, bot.name) : null;
    if (!you.signedIn) blockers.you = ratedSignInBlocker;
    else if (!bot || bot.kind === "manual") {
      // Hand play is allowed; without a bot, the server's "choose your bot first" applies.
    } else if (bot.kind === "hosted" && bot.competitor_id) {
      // A bot from the leaderboard: the server checks that it is listed and not paused.
    } else if (!bot.name) blockers.you = ratedBotNameBlocker;
    else if (bot.kind !== "hosted") blockers.you = ratedHostedBlocker;
    else if (found === undefined) {
      blockers.you = `Your bot name ${bot.name} no longer exists. Choose another.`;
    } else if (found && found.url === null) {
      blockers.you = botNameUrlBlocker(found.name);
    }
  }
  if (room.isHost) {
    const remove = ratedSeatsToRemove(room);
    if (remove.length > 0) {
      blockers.host = `Remove ${remove.join(", ")}: ${ratedSeatRule}`;
    }
  }
  return blockers;
}

/** Whether any member plays by hand, which makes the game real-time. Only people can: a bot seat never plays by hand. */
export function anyPlaysByHand(room: RoomModel): boolean {
  return [
    ...room.teams.flatMap((team) => team.members),
    ...room.unassigned,
  ].some((member) => member.bot?.kind === "manual");
}

/** "Real time: a turn every 1 s": what a game with a hand player is, from the lobby's turn pace. */
export function describeRealTime(
  settings: Record<string, unknown> | undefined,
): string {
  return `Real time: a turn every ${describePace(turnPaceOf(settings))}`;
}

/**
 * One line naming the map and the turn limit a lobby's settings chose, its game length when
 * that isn't 300 turns, and "Rated" for a rated lobby. A game where someone plays by hand is
 * real-time and has no turn limit, so `realTime` swaps the limit's phrase for the turn pace's.
 */
export function describeSettings(
  settings: Record<string, unknown> | undefined,
  templates: MapTemplateSummary[],
  realTime = false,
): string {
  const limit = realTime
    ? describeRealTime(settings)
    : describeTurnTimeout(settings);
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
  return [
    map,
    limit,
    describeGameLength(settings),
    settings?.rated === true ? "Rated" : "",
  ]
    .filter(Boolean)
    .join(" · ");
}
