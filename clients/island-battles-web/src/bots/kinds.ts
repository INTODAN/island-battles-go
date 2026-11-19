/**
 * The kinds of bot that play a player's seat in a game, as `GenerationState.bot` reports the
 * one copied at the start. Only a localhost bot is relayed by the player's client; the server
 * plays the others, and nothing plays a manual player, who plays by hand.
 */

import type { components } from "../api/generated/game";

export type BotChoice = components["schemas"]["BotChoice"];
export type RelayedBot = components["schemas"]["RelayedBot"];

/** Whether the server plays this bot without the player's tab: a hosted or an uploaded bot. */
export function playedByServer(bot: BotChoice): boolean {
  return bot.kind === "hosted" || bot.kind === "uploaded";
}

/** A few words naming the bot that plays your seat, for the game view's player list. */
export function describeGameBot(bot: BotChoice): string {
  switch (bot.kind) {
    case "localhost":
      return `Your localhost bot on port ${bot.port ?? 41414}, relayed by this tab`;
    case "default":
      return `Default bot, level ${bot.level ?? "?"}, played by the server`;
    case "hosted":
      return `Played by the server: your hosted bot${bot.host ? ` at ${bot.host}` : ""}`;
    case "uploaded":
      return "Played by the server: your uploaded bot";
    case "manual":
      return "You play by hand";
  }
}

/** The line under a localhost bot seat's team for the host who relays it. */
export function describeRelayedBot(seat: RelayedBot): string {
  return `Your localhost bot seat on port ${seat.port}, relayed by this tab`;
}
