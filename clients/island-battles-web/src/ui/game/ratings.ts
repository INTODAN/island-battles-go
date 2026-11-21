// How a rated game changed each team's rating (#370), as the outcome banner lists it under the
// summary: the winner first, then the other teams in colour order. Pure, so `bun test` covers it.

import { ownerLine, ratingChange } from "../leaderboard/model";
import type { GameOutcome } from "./history";

export type RatingChange = NonNullable<GameOutcome["ratings"]>[number];

/** One line of the banner: the competitor's name, drawn in its team's colour, and its change. */
export interface RatingLine {
  teamColor: number;
  /** The competitor's name: a bot name, a hand player's display name, or "Bot (level 2)". */
  name: string;
  /** "by Dana": a bot name's owner, as the leaderboard says it; none for hand play and a house bot. */
  owner?: string;
  /** "1500 → 1520 (+20)", with the ratings rounded. */
  change: string;
  direction: "up" | "down" | "flat";
}

/** "Kraken, by Dana 1500 → 1520 (+20)": a line as one sentence, for a screen reader and the tests. */
export const ratingLineText = (line: RatingLine): string =>
  `${line.name}${line.owner ? `, ${line.owner}` : ""} ${line.change}`;

/**
 * The rating changes of a rated game's outcome, the winner first and then by team colour; none
 * for a game that isn't rated. The ratings are decimals, shown rounded, and the change is that of
 * the rounded numbers, so it always adds up.
 */
export function ratingLines(
  outcome: Pick<GameOutcome, "ratings" | "winner_team_color"> | null,
): RatingLine[] {
  const winner = outcome?.winner_team_color ?? null;
  return [...(outcome?.ratings ?? [])]
    .sort(
      (left, right) =>
        Number(right.team_color === winner) -
          Number(left.team_color === winner) ||
        left.team_color - right.team_color,
    )
    .map((rating) => {
      const change = ratingChange(
        Math.round(rating.before),
        Math.round(rating.after),
      );
      const owner = ownerLine({
        kind: rating.kind,
        owner: rating.owner ?? null,
      });
      return {
        teamColor: rating.team_color,
        name: rating.name,
        // Two bots are told apart by their owners too, not only by colour.
        ...(rating.kind === "bot" && owner ? { owner } : {}),
        change: `${change.text} (${change.delta})`,
        direction: change.direction,
      };
    });
}

/** The teams box's line for a rated game. */
export const ratedGameText = "Rated game";
