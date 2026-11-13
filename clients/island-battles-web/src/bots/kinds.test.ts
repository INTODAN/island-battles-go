import { describe, expect, test } from "bun:test";
import { describeGameBot, playedByServer } from "./kinds";

describe("the bot that plays your seat", () => {
  test("is played by the server unless it is your localhost bot or a default bot", () => {
    expect(playedByServer({ kind: "localhost" })).toBe(false);
    expect(playedByServer({ kind: "default", level: 1 })).toBe(false);
    expect(playedByServer({ kind: "hosted", host: "bot.example.com" })).toBe(
      true,
    );
    expect(playedByServer({ kind: "uploaded" })).toBe(true);
  });

  test("is named for the game view's player list", () => {
    expect(describeGameBot({ kind: "localhost" })).toBe(
      "Your localhost bot, relayed by this tab",
    );
    expect(describeGameBot({ kind: "default", level: 2 })).toBe(
      "Default bot, level 2, played by the server",
    );
    expect(describeGameBot({ kind: "hosted", host: "bot.example.com" })).toBe(
      "Played by the server: your hosted bot at bot.example.com",
    );
    expect(describeGameBot({ kind: "uploaded" })).toBe(
      "Played by the server: your uploaded bot",
    );
  });
});
