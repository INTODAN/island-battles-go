// The account page's model (#369): what it shows, what is disabled and each sentence, from the
// game's `GET /account` and the state of the page's requests. Pure (no DOM, no requests), so
// `bun test` covers it; `accountDialog.ts` only draws it.

import { ApiError } from "../../api/errors";
import type { components } from "../../api/generated/game";

export type IslandAccount = components["schemas"]["IslandAccount"];
export type Competitor = components["schemas"]["Competitor"];

/** The rule of an account's display name and of a bot name, as the lobby and the game check it. */
export const nameRule = "3 to 24 letters, digits, spaces, - and _.";

const minNameLength = 3;
const maxNameLength = 24;
const nameCharacters = /^[\p{L}\p{Nd} _-]+$/u;
/** House bots are shown as "Bot (level N)", so a bot name can't start like one. */
const houseBotPrefix = "bot (level";

/** The account's display name rule; a sentence for a name that breaks it, else undefined. */
export function validateAccountName(text: string): string | undefined {
  const name = text.trim();
  if (name === "") return "Enter a display name first.";
  const length = [...name].length;
  if (
    length < minNameLength ||
    length > maxNameLength ||
    !nameCharacters.test(name)
  ) {
    return `Use ${nameRule}`;
  }
  return undefined;
}

/** A bot name's rule: the display name's, and it can't start with "Bot (level". */
export function validateBotName(text: string): string | undefined {
  const name = text.trim();
  if (name === "") return "Enter a bot name first.";
  const length = [...name].length;
  if (
    length < minNameLength ||
    length > maxNameLength ||
    !nameCharacters.test(name)
  ) {
    return `Use ${nameRule}`;
  }
  if (name.toLowerCase().startsWith(houseBotPrefix)) {
    return 'A bot name can\'t start with "Bot (level", which the house bots use.';
  }
  return undefined;
}

/** An example of a hosted bot's URL, in the field and in sentences. */
export const exampleBotUrl = "https://bot.example.com/turn";

/**
 * A bot name's URL, before it is sent: something that reads as an https:// URL. The game checks
 * the rest (port 443, no credentials, a public host) and its sentence shows when it refuses.
 */
export function validateBotUrl(text: string): string | undefined {
  const url = text.trim();
  if (url === "") {
    return `Enter the URL your hosted bot runs at, such as ${exampleBotUrl}.`;
  }
  let parsed: URL;
  try {
    parsed = new URL(url);
  } catch {
    return `Enter a whole URL, such as ${exampleBotUrl}.`;
  }
  // http:// is left to the game, which takes it only for a bot on its own machine, in development.
  if (parsed.protocol !== "https:" && parsed.protocol !== "http:") {
    return `Use an https:// URL, such as ${exampleBotUrl}.`;
  }
  return undefined;
}

/** Under the URL of a name registered before names had URLs. */
export const noBotUrlSentence =
  "Add the URL your bot runs at to play it in rated lobbies.";

/** Beside the new name's fields: what the URL is for, and that it becomes public. */
export const botUrlPublicSentence =
  "Rated games play the name at this URL. Once the name has played a rated game, the leaderboard shows the URL, so others can play against your bot.";

/** Under the field that changes a name's URL. */
export function botUrlEditHint(competitor: Competitor): string {
  return competitor.games > 0
    ? `The leaderboard shows ${competitor.name}'s new URL within a minute. Its rating stays; a game already started keeps the URL it started with.`
    : "Its rating stays. Once the name has played a rated game, the leaderboard shows its URL, so others can play against your bot.";
}

/** A rating as shown: rounded. */
export const ratingText = (rating: number): string =>
  String(Math.round(rating));

/** How many rated games a competitor played, as shown beside its rating. */
export function gamesText(games: number): string {
  if (games === 0) return "no rated games yet";
  return games === 1 ? "1 rated game" : `${games} rated games`;
}

export interface CompetitorRow {
  id: string;
  name: string;
  rating: string;
  games: string;
  /** A bot name's hosted bot URL, shown as text; null for hand play and a name without one. */
  url: string | null;
  /**
   * For a bot name paused after it missed a turn while it played away (#412): the sentence with
   * its reason, shown with **Resume**. Null otherwise.
   */
  paused: string | null;
}

/** The line of a paused bot name: which turn it missed, and what the pause stops. */
export function pausedSentence(reason: string): string {
  return `Paused: ${serverSentence(reason)} It plays in nobody else's rated game until you resume it; your own games with it go on.`;
}

const row = (competitor: Competitor): CompetitorRow => ({
  id: competitor.id,
  name: competitor.name,
  rating: ratingText(competitor.rating),
  games: gamesText(competitor.games),
  url: competitor.kind === "bot" ? competitor.url : null,
  paused:
    competitor.kind === "bot" && competitor.away_paused
      ? pausedSentence(competitor.away_paused.reason)
      : null,
});

export interface AddBotNameView {
  /** The field and Add are disabled: at the limit, or while a name is being added. */
  disabled: boolean;
  /** At the limit of bot names: the sentence that says so, shown first and in bold. */
  limit: string | undefined;
  /** The sentence under the field. */
  hint: string;
}

export interface AccountView {
  /** The hand play: the account's display name, its rating and games. */
  hand: CompetitorRow;
  bots: CompetitorRow[];
  /** "3 of 5", beside the Bot names heading. */
  count: string;
  add: AddBotNameView;
}

/** The limit sentence, shown in place of the rule at the most bot names. */
export const botNameLimitSentence = (max: number): string =>
  `You have ${max} bot names, the most an account can have.`;

/** What the page shows for an account, and whether a name can be added. */
export function accountView(
  account: IslandAccount,
  adding = false,
): AccountView {
  const count = account.bot_names.length;
  const max = account.max_bot_names;
  const full = count >= max;
  return {
    hand: row(account.hand),
    bots: account.bot_names.map(row),
    count: `${count} of ${max}`,
    add: full
      ? {
          disabled: true,
          limit: botNameLimitSentence(max),
          hint: "Delete one to register another.",
        }
      : {
          disabled: adding,
          limit: undefined,
          hint: `${nameRule} You have ${count} of ${max}.`,
        },
  };
}

/** An account with a bot name added, as the 201 answered it. */
export const withBotName = (
  account: IslandAccount,
  competitor: Competitor,
): IslandAccount => ({
  ...account,
  bot_names: [...account.bot_names, competitor],
});

/** An account with one of its bot names as the server answered it, such as with a new URL. */
export const withChangedBotName = (
  account: IslandAccount,
  competitor: Competitor,
): IslandAccount => ({
  ...account,
  bot_names: account.bot_names.map((name) =>
    name.id === competitor.id ? competitor : name,
  ),
});

/** An account without one of its bot names. */
export const withoutBotName = (
  account: IslandAccount,
  competitorId: string,
): IslandAccount => ({
  ...account,
  bot_names: account.bot_names.filter(
    (competitor) => competitor.id !== competitorId,
  ),
});

export interface Confirmation {
  question: string;
  text: string;
  /** The label of the button that does it. The other button reads "Keep it". */
  confirm: string;
}

/** The confirmation before a bot name is deleted, with its rating. */
export function deleteBotNameConfirmation(
  competitor: Competitor,
): Confirmation {
  const played =
    competitor.games === 0
      ? "no rated games yet"
      : `after ${gamesText(competitor.games)}`;
  return {
    question: `Delete the bot name ${competitor.name}?`,
    text: `Its rating (${ratingText(competitor.rating)}, ${played}) is deleted with it and can't be restored. Anyone can then register the name.`,
    confirm: `Delete ${competitor.name}`,
  };
}

/** The confirmation before the account is deleted. */
export function deleteAccountConfirmation(
  account: IslandAccount | null,
): Confirmation {
  const count = account?.bot_names.length;
  const names =
    count === undefined
      ? "your bot names"
      : count === 0
        ? undefined
        : count === 1
          ? "your bot name"
          : `your ${count} bot names`;
  return {
    question: "Delete your account?",
    text: `${names ? `Your display name, ${names}` : "Your display name"} and every rating are deleted, and can't be restored. You can still play as a guest.`,
    confirm: "Delete account",
  };
}

const problemPrefix = "urn:island-battles:problem:";

/** A server's detail as a sentence: a capital first letter and a full stop. */
export function serverSentence(detail: string): string {
  const text = detail.trim();
  if (text === "") return text;
  const sentence = text[0]?.toUpperCase() + text.slice(1);
  return /[.!?]$/.test(sentence) ? sentence : `${sentence}.`;
}

const unreachable =
  "Cannot reach the server. Check your connection and try again.";
export const sessionEndedSentence =
  "Your session has ended. Sign in again to continue.";

function common(error: unknown): string | undefined {
  if (!(error instanceof ApiError)) {
    return error instanceof Error ? error.message : undefined;
  }
  if (error.kind === "transport") return unreachable;
  if (error.status === 401) return sessionEndedSentence;
  if (error.problemType === `${problemPrefix}sign-in-required`) {
    return "Sign in with Google to have an account.";
  }
  return undefined;
}

const fallback = (error: unknown): string =>
  error instanceof ApiError && error.detail
    ? serverSentence(error.detail)
    : "Something went wrong. Please try again.";

/**
 * The sentence under the bot name fields for a refused or failed add or change of URL: the
 * server's own for a taken name, or a name or URL that breaks its rule.
 */
export function botNameProblem(error: unknown): string {
  return common(error) ?? fallback(error);
}

/** The sentence under the display name field for a refused or failed rename or first sign-in. */
export function displayNameProblem(error: unknown): string {
  const known = common(error);
  if (known) return known;
  if (error instanceof ApiError) {
    if (error.code === "display_name_taken") {
      return "Another account has that display name. Choose another.";
    }
    if (error.code === "invalid_input") return `Use ${nameRule}`;
  }
  return fallback(error);
}

/** The sentence for any other failed request of the page: loading, deleting. */
export function accountProblem(error: unknown): string {
  return common(error) ?? fallback(error);
}

/**
 * What the page says when the game's part of the account is deleted but the lobby's call failed:
 * the bot names and ratings are gone, and deleting again finishes it.
 */
export function partlyDeletedProblem(error: unknown): string {
  return `Your bot names and every rating are deleted, but your account isn't yet. ${accountProblem(error)} Press Delete account again to finish.`;
}

/** True when a failed request means the session ended, so the page closes. */
export const endsSession = (error: unknown): boolean =>
  error instanceof ApiError && error.status === 401;
