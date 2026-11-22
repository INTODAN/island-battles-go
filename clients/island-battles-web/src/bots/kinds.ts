/**
 * The kinds of bot that play a player's seat in a game, as `GenerationState.bot` reports the
 * one copied at the start. Only a localhost bot is relayed by the player's client; the server
 * plays the others, and nothing plays a manual player, who plays by hand.
 */

import type { components } from "../api/generated/game";

export type BotChoice = components["schemas"]["BotChoice"];
export type RelayedBot = components["schemas"]["RelayedBot"];
type RegisteredBot = components["schemas"]["RegisteredBot"];

/**
 * A hosted bot's URL as typed: trimmed, with `https://` put in front when it names no scheme, so
 * `bot.example.com/turn` reaches `https://bot.example.com/turn`. A typed `http://` or another
 * scheme stays for the game, or the URL check, to judge; `https:/` or `https//`, typos of one,
 * are read as `https://`. Empty, or only slashes, is empty.
 */
export function botUrlFromInput(text: string): string {
  const url = text.trim();
  if (/^[a-z][a-z0-9+.-]*:\/\//i.test(url)) return url;
  const typo = /^(https?)(?::\/*|\/+)(?=[^/])/i.exec(url);
  if (typo) return `${typo[1]}://${url.slice(typo[0].length)}`;
  const rest = url.replace(/^\/+/, "");
  return rest === "" ? "" : `https://${rest}`;
}

/**
 * A registered bot (#412): a bot name with its URL, rated or not, that anyone may choose for
 * their own team or seat as a bot, by its competitor ID. The server plays it at the name's URL,
 * without its owner joining.
 */
export interface ListedBot {
  competitorId: string;
  name: string;
  /** The display name of the account that owns it. */
  owner: string | null;
  /** Where the server plays it. */
  url: string;
  /** Its rating, rounded, once it has played a rated game; null while it is unrated. */
  rating: number | null;
}

/** The registered bots of `GET /bots/registered`, in its order. */
export function listedBots(bots: readonly RegisteredBot[]): ListedBot[] {
  return bots.map((bot) => ({
    competitorId: bot.competitor_id,
    name: bot.name,
    owner: bot.owner,
    url: bot.url,
    rating: bot.rating,
  }));
}

/** Adds `more` to `bots`, each bot once, keeping the first's order; a bot read again replaces its old copy. */
export function mergeListedBots(
  bots: readonly ListedBot[],
  more: readonly ListedBot[],
): ListedBot[] {
  const fresh = new Map(more.map((bot) => [bot.competitorId, bot]));
  const kept = bots.map((bot) => fresh.get(bot.competitorId) ?? bot);
  const known = new Set(bots.map((bot) => bot.competitorId));
  return [...kept, ...more.filter((bot) => !known.has(bot.competitorId))];
}

/** "rated 1520" or "unrated", for a registered bot. */
export const listedBotRating = (bot: ListedBot): string =>
  bot.rating === null ? "unrated" : `rated ${bot.rating}`;

/** "Kraken, by Dana", as the leaderboard and the registered bots name a bot. */
export const listedBotLabel = (bot: ListedBot): string =>
  bot.owner ? `${bot.name}, by ${bot.owner}` : bot.name;

/** Whether the server plays this bot without the player's tab: a hosted or an uploaded bot. */
export function playedByServer(bot: BotChoice): boolean {
  return bot.kind === "hosted" || bot.kind === "uploaded";
}

/**
 * A few words naming the bot that plays your seat, for the game view's player list, with the bot
 * name it plays as in a rated game: "Kraken, your localhost bot on port 41414, …". A named hosted
 * bot may be someone else's, a bot from the leaderboard, so it isn't called yours.
 */
export function describeGameBot(bot: BotChoice): string {
  const your = bot.name ? `${bot.name}, your` : "your";
  switch (bot.kind) {
    case "localhost":
      return `${bot.name ? your : "Your"} localhost bot on port ${bot.port ?? 41414}, relayed by this tab`;
    case "default":
      return `Default bot, level ${bot.level ?? "?"}, played by the server`;
    case "hosted":
      return bot.name
        ? `Played by the server: ${bot.name}, hosted${bot.host ? ` at ${bot.host}` : ""}`
        : `Played by the server: a hosted bot${bot.host ? ` at ${bot.host}` : ""}`;
    case "uploaded":
      return `Played by the server: ${your} uploaded bot`;
    case "manual":
      return "You play by hand";
  }
}

/** The line under a localhost bot seat's team for the host who relays it. */
export function describeRelayedBot(seat: RelayedBot): string {
  return `Your localhost bot seat on port ${seat.port}, relayed by this tab`;
}
