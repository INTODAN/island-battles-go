import { describe, expect, test } from "bun:test";
import {
  describeGameBot,
  describeRelayedBot,
  type ListedBot,
  listedBotLabel,
  listedBotRating,
  listedBots,
  mergeListedBots,
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

test("the registered bots are named with their owner and rating, rated or not", () => {
  const bots = listedBots([
    {
      competitor_id: "c-kraken",
      name: "Kraken",
      owner: "Dana",
      url: "https://kraken.example.org/bot",
      rated: true,
      rating: 1520,
      games: 3,
    },
    {
      competitor_id: "c-eel",
      name: "Eel",
      owner: "Sam",
      url: "https://eel.example.org/bot",
      rated: false,
      rating: null,
      games: 0,
    },
  ]);
  expect(bots).toEqual([
    {
      competitorId: "c-kraken",
      name: "Kraken",
      owner: "Dana",
      url: "https://kraken.example.org/bot",
      rating: 1520,
    },
    {
      competitorId: "c-eel",
      name: "Eel",
      owner: "Sam",
      url: "https://eel.example.org/bot",
      rating: null,
    },
  ]);
  const [kraken, eel] = bots as [ListedBot, ListedBot];
  expect(listedBotLabel(kraken)).toBe("Kraken, by Dana");
  expect(listedBotLabel({ ...eel, owner: null })).toBe("Eel");
  expect(listedBotRating(kraken)).toBe("rated 1520");
  expect(listedBotRating(eel)).toBe("unrated");
  // A search's finds join the bots read so far, each once, a fresh copy replacing an old one.
  const renamed = { ...kraken, rating: 1530 };
  expect(mergeListedBots([kraken], [eel, renamed])).toEqual([renamed, eel]);
});
