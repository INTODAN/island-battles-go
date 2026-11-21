/**
 * Checks the numbers of "How ratings work" against the backend's rating rule,
 * as `rules.test.ts` checks the Game rules page: it reads the Go sources.
 */
import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import {
  awayGamesPerDay,
  establishedK,
  initialRating,
  pairGamesPerDay,
  provisionalGames,
  provisionalK,
  ratingsSections,
} from "./ratings";

const repository = new URL("../../../../../", import.meta.url);
const read = (path: string) => readFileSync(new URL(path, repository), "utf8");

const changed =
  "The rating rule changed: update clients/island-battles-web/src/ui/leaderboard/ratings.ts, its numbers and its sentences.";

/** The value of `name = value` or `const name = value` in a Go file. */
function goConstant(path: string, name: string): number {
  const found = read(path).match(
    new RegExp(`\\b${name}\\s*(?:\\w+\\s*)?=\\s*([0-9.]+)`),
  );
  if (!found?.[1]) throw new Error(`${path} has no constant ${name}`);
  return Number(found[1]);
}

const text = ratingsSections
  .flatMap((section) => [
    section.title,
    ...section.paragraphs,
    ...(section.items ?? []),
  ])
  .join("\n");

describe("how ratings work", () => {
  test("starts everyone at the backend's initial rating", () => {
    expect(initialRating, changed).toBe(
      goConstant("backend/models/account.go", "InitialRating"),
    );
  });

  test("names the backend's K values and how many games use the bigger one", () => {
    const rule = "backend/rating/rating.go";
    expect(provisionalK, changed).toBe(goConstant(rule, "ProvisionalK"));
    expect(establishedK, changed).toBe(goConstant(rule, "EstablishedK"));
    expect(provisionalGames, changed).toBe(
      goConstant(rule, "ProvisionalGames"),
    );
    // "Twice as fast" holds only while the bigger K is twice the other.
    expect(provisionalK / establishedK, changed).toBe(2);
  });

  test("names the away bots' daily caps", () => {
    expect(awayGamesPerDay, changed).toBe(
      goConstant("backend/services/rated_lobbies.go", "MaxAwayGamesPerDay"),
    );
    // The pair cap is one game with each competitor on the lower colour: two, which the start's
    // refusal says once both are used.
    const caps = read("backend/services/rated_away.go");
    expect(caps, changed).toContain("have played both their rated games today");
    expect(caps, changed).toContain("first.TeamColor < second.TeamColor");
    expect(pairGamesPerDay, changed).toBe(2);
  });

  test("says each number in the text", () => {
    for (const number of [
      initialRating,
      provisionalGames,
      provisionalK,
      establishedK,
      pairGamesPerDay,
      awayGamesPerDay,
    ]) {
      expect(text).toContain(String(number));
    }
  });

  test("opens with the heading, and every section has text and a unique id", () => {
    expect(ratingsSections[0]?.title).toBe("How ratings work");
    const ids = ratingsSections.map((section) => section.id);
    expect(new Set(ids).size).toBe(ids.length);
    for (const section of ratingsSections) {
      expect(section.paragraphs.length).toBeGreaterThan(0);
    }
  });
});
