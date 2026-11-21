import { describe, expect, test } from "bun:test";
import {
  type RatingChange,
  ratedGameText,
  ratingLines,
  ratingLineText,
} from "./ratings";

const change = (
  team_color: number,
  name: string,
  before: number,
  after: number,
  kind: RatingChange["kind"] = "bot",
): RatingChange => ({
  team_color,
  competitor_id: `00000000-0000-4000-8000-00000000000${team_color}`,
  kind,
  name,
  before,
  after,
});

describe("the rating changes at the end", () => {
  test("list the winner first, with the ratings rounded", () => {
    const lines = ratingLines({
      winner_team_color: 2,
      ratings: [
        change(1, "Bot (level 2)", 1500, 1480.4, "house"),
        change(2, "Kraken", 1500.2, 1519.6),
      ],
    });
    expect(lines.map(ratingLineText)).toEqual([
      "Kraken 1500 → 1520 (+20)",
      "Bot (level 2) 1500 → 1480 (−20)",
    ]);
    expect(lines[0]).toMatchObject({ teamColor: 2, direction: "up" });
    expect(lines[1]).toMatchObject({ teamColor: 1, direction: "down" });
  });

  test("a draw lists the teams in colour order, and an unchanged rating says ±0", () => {
    const lines = ratingLines({
      winner_team_color: null,
      ratings: [
        change(3, "Dana", 1500, 1500, "manual"),
        change(1, "Squid", 1612.6, 1601.1),
      ],
    });
    expect(lines.map(ratingLineText)).toEqual([
      "Squid 1613 → 1601 (−12)",
      "Dana 1500 → 1500 (±0)",
    ]);
    expect(lines[1]?.direction).toBe("flat");
  });

  test("a bot name is told apart by its owner too, as the leaderboard says it", () => {
    const lines = ratingLines({
      winner_team_color: 1,
      ratings: [
        { ...change(1, "Kraken", 1500, 1520), owner: "Dana" },
        { ...change(2, "Kraken2", 1500, 1480), owner: "Sam" },
        { ...change(3, "Dana", 1500, 1490, "manual"), owner: "Dana" },
      ],
    });
    expect(lines.map(ratingLineText)).toEqual([
      "Kraken, by Dana 1500 → 1520 (+20)",
      "Kraken2, by Sam 1500 → 1480 (−20)",
      "Dana 1500 → 1490 (−10)",
    ]);
    expect(lines[0]).toMatchObject({ name: "Kraken", owner: "by Dana" });
    // Hand play and a house bot have no owner line; nor does a bot without one.
    expect(lines[2]?.owner).toBeUndefined();
    expect(
      ratingLines({
        winner_team_color: 1,
        ratings: [change(1, "Squid", 1500, 1500)],
      })[0]?.owner,
    ).toBeUndefined();
  });

  test("a game that isn't rated has none", () => {
    expect(ratingLines({ winner_team_color: 1 })).toEqual([]);
    expect(ratingLines(null)).toEqual([]);
    expect(ratedGameText).toBe("Rated game");
  });
});
