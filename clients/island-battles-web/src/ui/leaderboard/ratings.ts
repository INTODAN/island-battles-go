/**
 * "How ratings work", the leaderboard's explanation of the rating rule, in a
 * player's words. The numbers are the backend's (`backend/rating/` and
 * `models.InitialRating`); `ratings.test.ts` reads them from the Go sources,
 * so a change there fails here until the text says it too.
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
 * The explanation: the first section is the opening paragraph under the
 * heading, and each other one is a point of the list, led by its title and a
 * colon, which is why its text starts in lower case.
 */
export const ratingsSections: readonly RulesSection[] = [
  {
    id: "basics",
    title: "How ratings work",
    paragraphs: [
      `Everyone starts at ${initialRating}. Winning a rated game raises your rating and losing lowers it: more when the result is a surprise, less when it was expected.`,
    ],
  },
  {
    id: "rated-games",
    title: "Which games count",
    paragraphs: [
      "the host turned Rated on, everyone is signed in, and each team is a different competitor.",
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
    id: "new-competitors",
    title: "New competitors",
    paragraphs: [
      `in each of your first ${provisionalGames} rated games, ${provisionalK} points are at stake; after that, ${establishedK}. So your first games move your rating twice as fast.`,
    ],
  },
  {
    id: "teams",
    title: "More than two teams",
    paragraphs: ["only the winner gains, and each other team loses a little."],
  },
  {
    id: "draws",
    title: "No winner",
    paragraphs: [
      "the teams still in the game at the end draw with each other, and a team that dropped out before (knocked out, out of time, resigned or left) loses to each of them.",
    ],
  },
  {
    id: "unfinished",
    title: "Unfinished games",
    paragraphs: ["a game that never finishes changes nothing."],
  },
];
