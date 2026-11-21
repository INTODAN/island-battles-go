/**
 * **Play against it** on a listed bot's page (#404, #412): a lobby with the viewer as host and the
 * bot as a seat on the other team, `{"bot_competitor_id": …}`, which the server plays at the bot
 * name's URL. No route of its own: the lobby's create, team and seat calls, as the menu and the
 * room make them. The viewer chooses a rated lobby, whose game rates the bot, playing away, and
 * whoever plays the host's team, or an unrated one, which changes no rating. Only a signed-in
 * viewer can host a rated lobby.
 */
import { ApiError } from "../../api/errors";
import { serverSentence } from "../account/accountModel";
import type { LobbyActions, MapTemplateSummary } from "../lobby/actions";
import {
  defaultGameLength,
  defaultTurnPace,
  defaultTurnTimeout,
  maxNameLength,
  maxTeams,
  minTeams,
  validateMapSettings,
} from "../lobby/form";
import { describeProblem } from "../lobby/problems";
import { seatBody } from "../lobby/roomModel";

/** A listed bot to play against, and in which kind of lobby. */
export interface PlayTarget {
  name: string;
  /** Its competitor ID, which the seat names it by. */
  competitorId: string;
  /** The URL the leaderboard shows. */
  url: string;
  /** A rated lobby, whose game counts for the leaderboard, or an unrated one. */
  rated: boolean;
}

export type PlayAgainstActions = Pick<
  LobbyActions,
  "botOptions" | "mapTemplates" | "create" | "selectTeam" | "addSeat"
>;

/** The lobby's name: "Against Kraken". */
export const playAgainstLobbyName = (name: string): string =>
  [...`Against ${name}`].slice(0, maxNameLength).join("");

/** The default map to play on: the first for two teams, else the first a lobby can have. */
export function playAgainstMap(
  maps: readonly MapTemplateSummary[],
): MapTemplateSummary | undefined {
  return (
    maps.find((map) => map.team_count === 2) ??
    maps.find((map) => map.team_count >= minTeams && map.team_count <= maxTeams)
  );
}

/** Shown on the name screen while someone without a session is on the way to the lobby. */
export const signInToPlayNotice = (name: string, rated = false): string =>
  rated
    ? `Sign in with Google to play a rated game against ${name}. A rated lobby with ${name} as a bot opens next.`
    : `Choose a name to play against ${name}. A lobby with ${name} as a bot opens next.`;

/** When a guest asks for a rated lobby, which only a signed-in host can make. */
export const ratedSignInProblem = (name: string): string =>
  `Sign in with Google to play a rated game against ${name}, or play an unrated one.`;

/** When the tab is in a lobby already, which one tab holds at a time. */
export const inLobbyProblem = (name: string): string =>
  `You're in a lobby already. Leave it to play against ${name}.`;

export const noHostedBotsProblem = (name: string): string =>
  `This server doesn't play hosted bots, so you can't play against ${name} here.`;

export const noMapProblem =
  "There is no default map to play on. Create a lobby with a map file instead.";

/**
 * Makes the lobby: checks that a rated one has a signed-in host and that the server plays hosted
 * bots, creates the lobby, rated or not, on a default map, takes the first team if the lobby
 * didn't give you one, and adds the bot as a seat on another team, by its competitor ID. A failure
 * before the lobby exists rejects with an Error to show. Once it exists, the room shows it, so a
 * failed team or seat call resolves with a sentence for the room.
 */
export async function startPlayAgainst(
  target: PlayTarget,
  options: {
    actions: PlayAgainstActions;
    playerId: string;
    /** The viewer signed in with Google, so they can host a rated lobby. */
    signedIn: boolean;
    newId(): string;
  },
): Promise<{ notice?: string }> {
  const { actions } = options;
  if (target.rated && !options.signedIn) {
    throw new Error(ratedSignInProblem(target.name));
  }
  const fail = (cause: unknown, context: "create" | "seat") =>
    new Error(describeProblem(cause, context).message);
  let offered: Awaited<ReturnType<PlayAgainstActions["botOptions"]>>;
  let maps: MapTemplateSummary[];
  try {
    [offered, maps] = await Promise.all([
      actions.botOptions(),
      actions.mapTemplates(),
    ]);
  } catch (cause) {
    throw fail(cause, "create");
  }
  if (!offered.serverBots) throw new Error(noHostedBotsProblem(target.name));
  const map = playAgainstMap(maps);
  if (!map) throw new Error(noMapProblem);
  const settings = validateMapSettings(
    {
      map: { kind: "template", templateId: map.map_template_id },
      turnTimeoutSeconds: defaultTurnTimeout,
      turnPaceMilliseconds: defaultTurnPace,
      maxGenerations: defaultGameLength,
      rated: target.rated,
    },
    maps,
  );
  if (!settings.ok) throw new Error(noMapProblem);
  let snapshot: Awaited<ReturnType<PlayAgainstActions["create"]>>;
  try {
    snapshot = await actions.create({
      name: playAgainstLobbyName(target.name),
      teams: settings.teams,
      game_settings: settings.gameSettings,
      request_id: options.newId(),
    });
  } catch (cause) {
    throw fail(cause, "create");
  }
  const teams = [...(snapshot.teams ?? [])].sort(
    (left, right) => left.order - right.order,
  );
  const you = (snapshot.members ?? []).find(
    (member) => member.active && member.player_id === options.playerId,
  );
  let yourTeam = you?.team_id ?? null;
  try {
    if (!yourTeam) {
      yourTeam = teams[0]?.id ?? null;
      if (yourTeam) await actions.selectTeam(yourTeam);
    }
    const other = teams.find((team) => team.id !== yourTeam);
    if (!other) throw new Error("The lobby has no other team.");
    await actions.addSeat(
      seatBody(
        other.id,
        { bot_competitor_id: target.competitorId },
        options.newId(),
      ),
    );
  } catch (cause) {
    // A refused bot (paused, or no longer listed) is the game's own sentence.
    const why =
      cause instanceof ApiError &&
      cause.code === "invalid_seat_settings" &&
      cause.detail
        ? serverSentence(cause.detail)
        : describeProblem(cause, "listed-seat").message;
    return {
      notice: `${target.name} wasn't added: ${why} Add it with Add bot, as a bot from the leaderboard.`,
    };
  }
  return {};
}
