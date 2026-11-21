import { describe, expect, test } from "bun:test";
import {
  describeGameBot,
  describeRelayedBot,
  listedBotLabel,
  listedBots,
  playedByServer,
} from "./kinds";

describe("the bot that plays your seat", () => {
  test("is played by the server unless it is your localhost bot or a default bot", () => {
    expect(playedByServer({ kind: "localhost" })).toBe(false);
    expect(playedByServer({ kind: "default", level: 1 })).toBe(false);
    expect(playedByServer({ kind: "hosted", host: "bot.example.com" })).toBe(
      true,
    );
    expect(playedByServer({ kind: "uploaded" })).toBe(true);
    expect(playedByServer({ kind: "manual" })).toBe(false);
  });

  test("is named for the game view's player list", () => {
    expect(describeGameBot({ kind: "localhost" })).toBe(
      "Your localhost bot on port 41414, relayed by this tab",
    );
    expect(describeGameBot({ kind: "localhost", port: 41415 })).toBe(
      "Your localhost bot on port 41415, relayed by this tab",
    );
    expect(
      describeRelayedBot({
        player_id: "seat",
        team_color: 2,
        display_name: "Localhost bot (port 41416)",
        port: 41416,
      }),
    ).toBe("Your localhost bot seat on port 41416, relayed by this tab");
    expect(describeGameBot({ kind: "default", level: 2 })).toBe(
      "Default bot, level 2, played by the server",
    );
    expect(describeGameBot({ kind: "hosted", host: "bot.example.com" })).toBe(
      "Played by the server: a hosted bot at bot.example.com",
    );
    expect(describeGameBot({ kind: "uploaded" })).toBe(
      "Played by the server: your uploaded bot",
    );
    expect(describeGameBot({ kind: "manual" })).toBe("You play by hand");
  });
});

test("a rated game names the bot name your bot plays as", () => {
  expect(describeGameBot({ kind: "localhost", name: "Kraken" })).toBe(
    "Kraken, your localhost bot on port 41414, relayed by this tab",
  );
  expect(
    describeGameBot({
      kind: "hosted",
      host: "bot.example.com",
      name: "Kraken",
    }),
  ).toBe("Played by the server: Kraken, hosted at bot.example.com");
  expect(describeGameBot({ kind: "uploaded", name: "Squid" })).toBe(
    "Played by the server: Squid, your uploaded bot",
  );
});

test("the bots from the leaderboard are its bot names with a URL, named with their owner", () => {
  const entry = {
    rank: 1,
    rating: 1520,
    games: 3,
    wins: 2,
    draws: 0,
    losses: 1,
    last_played_at: "2026-11-20T14:32:00Z",
  };
  const bots = listedBots([
    {
      ...entry,
      competitor_id: "c-kraken",
      kind: "bot",
      name: "Kraken",
      owner: "Dana",
      url: "https://kraken.example.org/bot",
    },
    {
      ...entry,
      competitor_id: "c-old",
      kind: "bot",
      name: "Old",
      owner: "Dana",
      url: null,
    },
    {
      ...entry,
      competitor_id: "c-hand",
      kind: "manual",
      name: "Dana",
      owner: null,
      url: null,
    },
    {
      ...entry,
      competitor_id: "c-house",
      kind: "house",
      name: "Bot (level 2)",
      owner: null,
      url: null,
    },
  ]);
  expect(bots).toEqual([
    { competitorId: "c-kraken", name: "Kraken", owner: "Dana" },
  ]);
  expect(listedBotLabel(bots[0] as (typeof bots)[0])).toBe("Kraken, by Dana");
  expect(
    listedBotLabel({ competitorId: "c-x", name: "Squid", owner: null }),
  ).toBe("Squid");
});
