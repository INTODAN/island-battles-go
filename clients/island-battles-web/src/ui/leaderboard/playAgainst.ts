/**
 * **Play against it** on a listed bot's page (#404): an unrated lobby with the viewer as host and
 * a hosted bot seat at the bot's URL on the other team. No route of its own: the lobby's create,
 * team and seat calls, as the menu and the room make them. The game is unrated, since a lobby
 * that isn't rated changes no rating, and a seat is never a competitor.
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

/** A listed bot to play against: its name and the URL the leaderboard shows. */
export interface PlayTarget {
  name: string;
  url: string;
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
export const signInToPlayNotice = (name: string): string =>
  `Choose a name to play against ${name}. A lobby with ${name} as a bot opens next.`;

/** When the tab is in a lobby already, which one tab holds at a time. */
export const inLobbyProblem = (name: string): string =>
  `You're in a lobby already. Leave it to play against ${name}.`;

export const noHostedBotsProblem = (name: string): string =>
  `This server doesn't play hosted bots, so you can't play against ${name} here.`;

export const noMapProblem =
  "There is no default map to play on. Create a lobby with a map file instead.";

/**
 * Makes the lobby: checks that the server plays hosted bots, creates an unrated lobby on a default
 * map, takes the first team if the lobby didn't give you one, and adds the bot as a hosted bot
 * seat on another team. A failure before the lobby exists rejects with an Error to show. Once it
 * exists, the room shows it, so a failed team or seat call resolves with a sentence for the room.
 */
export async function startPlayAgainst(
  target: PlayTarget,
  options: {
    actions: PlayAgainstActions;
    playerId: string;
    newId(): string;
  },
): Promise<{ notice?: string }> {
  const { actions } = options;
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
      rated: false,
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
      seatBody(other.id, { bot_url: target.url }, options.newId()),
    );
  } catch (cause) {
    // A refused URL (its host no longer public, say) is the game's own sentence.
    const why =
      cause instanceof ApiError &&
      cause.code === "invalid_seat_settings" &&
      cause.detail
        ? serverSentence(cause.detail)
        : describeProblem(cause, "seat").message;
    return {
      notice: `${target.name} wasn't added: ${why} Add it with Add bot, as a hosted bot at ${target.url}.`,
    };
  }
  return {};
}
