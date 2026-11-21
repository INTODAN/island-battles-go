/**
 * "How ratings work", the leaderboard's explanation of the rating rule, in a
 * player's words. The numbers are the backend's (`backend/rating/`,
 * `models.InitialRating` and the away bots' caps in `backend/services/`);
 * `ratings.test.ts` reads them from the Go sources, so a change there fails
 * here until the text says it too.
 */
import type { RulesSection } from "../rules/rules";

/** Every competitor's rating before its first rated game (`models.InitialRating`). */
export const initialRating = 1500;

/** How many rated games a competitor plays with the bigger K (`rating.ProvisionalGames`). */
export const provisionalGames = 30;

/** The points at stake in each of a competitor's first rated games (`rating.ProvisionalK`). */
export const provisionalK = 40;

/** The points at stake once those games are played (`rating.EstablishedK`). */
export const establishedK = 20;

/**
 * How many rated games two competitors, at least one of them away, play in a UTC day: one with
 * each on the lower team colour (`capRefusal` in `backend/services/rated_away.go`).
 */
export const pairGamesPerDay = 2;

/** How many rated games a bot plays away in a UTC day (`services.MaxAwayGamesPerDay`). */
export const awayGamesPerDay = 10;

/**
 * The explanation: the first section is the opening paragraph under the
 * heading, and each other one is a point of the list, led by its title and a
 * colon, which is why its text starts in lower case.
 */
export const ratingsSections: readonly RulesSection[] = [
  {
    id: "basics",
    title: "How ratings work",
    paragraphs: [
      `Everyone starts at ${initialRating}, and is listed after their first rated game. Winning a rated game raises your rating and losing lowers it: more when the result is a surprise, less when it was expected. Ratings are shown rounded.`,
    ],
  },
  {
    id: "rated-games",
    title: "Which games count",
    paragraphs: [
      "the host turned Rated on, everyone is signed in and plays by hand, with one of their hosted bot names or with a bot from the leaderboard, and each team is a different account's competitor.",
    ],
  },
  {
    id: "competitors",
    title: "Bots and hands",
    paragraphs: [
      "each bot name has its own rating, and so does playing by hand. The game's own bots are rated too, as house bots.",
    ],
  },
  {
    id: "bot-urls",
    title: "Rated bots",
    paragraphs: [
      "a rated bot runs at its bot name's URL, which the leaderboard shows once the bot is listed. Anyone can then play against it from its page, rated or unrated, or choose it in a rated lobby, for their own team or a bot seat.",
    ],
  },
  {
    id: "away",
    title: "Playing away",
    paragraphs: [
      "a bot plays away when it sits in a bot seat, or plays for someone who doesn't own it. Its owner needn't be there. The bot is rated, not the person who chose it, and its page marks the result away.",
    ],
  },
  {
    id: "daily-limits",
    title: "Daily limits",
    paragraphs: [
      `two competitors, at least one of them away, play at most ${pairGamesPerDay} rated games against each other a day, one with each of them on the lower team colour: swap their teams for the second. A bot plays away in at most ${awayGamesPerDay} rated games a day. Days are UTC days.`,
    ],
  },
  {
    id: "missed-turns",
    title: "Missed turns",
    paragraphs: [
      "a bot that misses a turn while it plays away loses that game, as anyone who runs out of time does, and is paused: it plays away in no rated game until its owner resumes it on the account page. The owner's own games with it go on.",
    ],
  },
  {
    id: "new-competitors",
    title: "New competitors",
    paragraphs: [
      `in each of your first ${provisionalGames} rated games, ${provisionalK} points are at stake; after that, ${establishedK}. So your first games move your rating twice as fast.`,
    ],
  },
  {
    id: "teams",
    title: "More than two teams",
    paragraphs: [
      "when a team wins, only it gains, and each other team loses a little, to the winner only.",
    ],
  },
  {
    id: "draws",
    title: "No winner",
    paragraphs: [
      "the teams still in the game at the end draw with each other, and a team that dropped out before (knocked out, out of time, resigned or left) loses to each of them. If no team is left, every team draws with every other. A draw raises the lower rating and lowers the higher one.",
    ],
  },
  {
    id: "unfinished",
    title: "Unfinished games",
    paragraphs: ["a game that never finishes changes nothing."],
  },
];
