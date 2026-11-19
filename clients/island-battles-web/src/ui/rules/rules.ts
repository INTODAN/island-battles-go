/**
 * The rules of Island Battles as the client's Game rules page shows them.
 *
 * This is text, written by hand from the game's simulation in
 * `backend/simulation/` and the numbers in `backend/models/` and
 * `backend/services/`. When a rule changes there (a new upgrade, a new
 * command, another cost or limit), change it here in the same pull request:
 * see `.agents/skills/game-rules/SKILL.md`. `rules.test.ts` checks the numbers
 * and the lists of upgrades and commands against the backend, but not the
 * sentences.
 */
import type { components } from "../../api/generated/game";

export type UpgradeType = components["schemas"]["UpgradeType"];
export type PlayerCommandType =
  components["schemas"]["PublicGenerationCommand"]["command_type"];

/** The game length when the lobby's host sets none, in generations (`DefaultMaxGenerations` in `backend/models/lobby_settings.go`). */
export const defaultGameLength = 300;

/** The shortest and the longest game length a lobby may set (`MinMaxGenerations` and `MaxMaxGenerations`). */
export const minGameLength = 50;
export const maxGameLength = 1200;

/** A turn's time limit when the lobby's host sets none (`DefaultTurnTimeoutSeconds`), in seconds. */
export const defaultTurnSeconds = 30;

/** A real-time game's turn pace when the lobby's host sets none (`DefaultTurnPaceMilliseconds`), in milliseconds. */
export const defaultTurnPaceMilliseconds = 1000;

/** How close enemy groups come before they clash, in world units (`DefaultClashRadius`). */
export const clashRadius = 0.25;

export interface UpgradeLevel {
  /** The level's effect, in the unit `UpgradeRule.unit` names. */
  value: number;
  /** Soldiers a castle pays to reach this level from the level below. */
  cost: number;
}

export interface UpgradeRule {
  name: string;
  /** What the upgrade does, in a sentence or two. */
  effect: string;
  /** What a level's value counts, for the table's heading. */
  unit: string;
  /** Every level from 1, in order; level 1 is free and every castle has it. */
  levels: UpgradeLevel[];
}

/**
 * Every upgrade type, as `upgradeLevels` in `backend/models/upgrades.go`
 * defines it. A type added to the API without a rule here fails `tsc`.
 */
export const upgradeRules: Record<UpgradeType, UpgradeRule> = {
  generation_amount: {
    name: "Production",
    effect:
      "Soldiers the castle produces every turn while a team owns it. Neutral castles produce nothing.",
    unit: "Soldiers per turn",
    levels: [
      { value: 1, cost: 0 },
      { value: 2, cost: 10 },
      { value: 3, cost: 20 },
      { value: 4, cost: 30 },
    ],
  },
  speed: {
    name: "Speed",
    effect:
      "How far the soldier groups sent from the castle travel each turn. A group keeps the speed it left with, so an upgrade only speeds up groups sent after it.",
    unit: "World units per turn",
    levels: [
      { value: 0.5, cost: 0 },
      { value: 1, cost: 10 },
      { value: 1.5, cost: 20 },
      { value: 2, cost: 30 },
    ],
  },
  clash_stop_generations: {
    name: "Recovery",
    effect:
      "How many turns the castle's groups stay stopped after a close clash, counting the turn of the clash. Lower is better: at level 3 a group moves again on the next turn.",
    unit: "Turns stopped",
    levels: [
      { value: 3, cost: 0 },
      { value: 2, cost: 10 },
      { value: 1, cost: 20 },
    ],
  },
  burst_potential: {
    name: "Burst potential",
    effect:
      "The most soldiers one burst can bring into the castle at once. At level 1 the castle can't burst.",
    unit: "Most soldiers per burst",
    levels: [
      { value: 0, cost: 0 },
      { value: 4, cost: 10 },
      { value: 8, cost: 20 },
      { value: 16, cost: 30 },
    ],
  },
};

/** The order the page lists the upgrades in. */
export const upgradeOrder: readonly UpgradeType[] = [
  "generation_amount",
  "speed",
  "clash_stop_generations",
  "burst_potential",
];

export interface CommandRule {
  name: string;
  description: string;
}

/** What a player (or their bot) can order in a turn: every player command of the API. */
export const commandRules: Record<PlayerCommandType, CommandRule> = {
  send_soldier_group: {
    name: "Send soldiers",
    description:
      "Send any number of a castle's soldiers, up to all of them, to any other castle: your own to reinforce it, or an enemy or neutral one to attack it. They leave as one group.",
  },
  upgrade_castle: {
    name: "Upgrade a castle",
    description:
      "Raise one of the castle's upgrades by one level, paying the level's cost from the castle's soldiers. A castle can buy several levels in one turn, one at a time, and may spend every soldier it has.",
  },
  burst_castle: {
    name: "Burst",
    description:
      "Bring up to the castle's burst potential of extra soldiers into it at once: they arrive with its production this turn, counting the upgrades bought this turn. The burst is a debt: the castle's later production repays it before the castle grows again. A castle that still owes soldiers can't burst, nor can it burst twice in one turn. Capturing a castle clears its debt.",
  },
};

/** The order the page lists the commands in. */
export const commandOrder: readonly PlayerCommandType[] = [
  "send_soldier_group",
  "upgrade_castle",
  "burst_castle",
];

/**
 * What happens when a turn ends, in order. The keys
 * after `commands` are the commands that `EndOfTurnCommands` in
 * `backend/simulation/commands.go` runs, in its order.
 */
export const turnSteps: readonly { step: string; text: string }[] = [
  {
    step: "commands",
    text: "Everyone's orders happen: groups leave their castles, upgrades are bought and bursts are ordered.",
  },
  {
    step: "move_soldier_groups",
    text: "Every group moves towards its castle, and groups that meet enemies on the way clash.",
  },
  {
    step: "increase_soldier_amount",
    text: "Every castle a team owns produces soldiers, and a burst ordered this turn arrives.",
  },
  {
    step: "remove_arrived_soldier_groups",
    text: "Groups that reached their castle arrive: they reinforce it, or fight its soldiers.",
  },
];

/** A part of the page: a heading, paragraphs, and optionally a list. */
export interface RulesSection {
  id: string;
  title: string;
  paragraphs: string[];
  items?: string[];
  /** Number the items, for steps that happen in order. */
  ordered?: boolean;
}

/**
 * The page's sections, in order. The commands and the upgrade table come
 * after "Your turn" and "Upgrades" from `commandRules` and `upgradeRules`.
 */
export const rulesSections: readonly RulesSection[] = [
  {
    id: "goal",
    title: "The goal",
    paragraphs: [
      "Island Battles is played by two or more teams on a map of castles. Capture castles, outgrow your enemies, and be the last team left with castles or soldiers.",
      "You can play by hand: choose Play by hand in the lobby room, and in the game the Controls button lists how to give orders. Or let a bot play for you: one of the game's default bots, or one you write.",
    ],
  },
  {
    id: "board",
    title: "The board",
    paragraphs: [
      "Every castle belongs to a team or is neutral, and holds a number of soldiers. Each team starts with the castles the map gives it, and the map says how many soldiers every castle starts with.",
      "Soldiers travel between castles in groups, in a straight line, and can't be called back or turned once they've left.",
    ],
  },
  {
    id: "turns",
    title: "Your turn",
    paragraphs: [
      "Everyone plays each turn at the same time. Orders that a castle can't afford, counting what your team has already ordered this turn, are refused, and nothing of that batch is kept.",
      `In a game of bots, a turn waits until every player has finished it. Finish yours before its time runs out: the lobby's host picks the turn limit, ${defaultTurnSeconds} seconds unless they choose another or no limit.`,
      `When someone plays by hand, the game is real-time: each turn ends after the turn pace, ${defaultTurnPaceMilliseconds / 1000} second unless the host chooses another, with the orders given by then. A player who gave none passes, and nobody loses for a missed turn. A turn ends sooner when every player has finished it.`,
      "When the turn ends, it runs in this order:",
    ],
    items: turnSteps.map((step) => step.text),
    ordered: true,
  },
  {
    id: "clashes",
    title: "Clashes",
    paragraphs: [
      `Groups of different teams clash when they come within ${clashRadius} world units of each other on their way. Groups of the same team pass each other, and a group never fights a castle on its way, only the castle it was sent to.`,
      "In a clash, every team loses as many soldiers as all its enemies in that clash have together. A team survives only if it has more soldiers than all the teams against it combined, and keeps the difference. Groups that meet at the same moment all fight in one clash.",
      "A close clash stops every group in it where they met. A clash is close when two groups of different teams in it were close in size: the smaller had at least half as many soldiers as the bigger one, counted before the fight. A stopped group stays there for as many turns as its Recovery upgrade says, and can still be attacked by other groups.",
    ],
  },
  {
    id: "castles",
    title: "Reaching a castle",
    paragraphs: [
      "A group that reaches your own castle joins its soldiers. A group that reaches an enemy or neutral castle fights the soldiers inside: with more attackers than defenders, it captures the castle and the survivors stay as its new soldiers; otherwise the castle stays its owner's, with that many fewer soldiers.",
      "Groups that arrive at the same castle in one turn land one after another, each meeting the castle as the previous one left it. A captured castle keeps its upgrades, loses any burst debt, and produces for its new team from the next turn.",
    ],
  },
  {
    id: "upgrades",
    title: "Upgrades",
    paragraphs: [
      "Each castle has its own upgrades, and every castle starts at level 1 of each unless the map says otherwise. A level costs soldiers from that castle, and levels are bought one at a time. The groups a castle sends keep the castle's levels of the moment they left.",
    ],
  },
  {
    id: "end",
    title: "How the game ends",
    paragraphs: [],
    items: [
      "A team that has no castles and no soldier groups left is out. When only one team is left, it wins. Neutral castles don't need to be captured.",
      "When the last teams are knocked out in the same turn, the game is a draw.",
      `The lobby's host picks the game's length N: ${defaultGameLength} generations unless they choose another, from ${minGameLength} to ${maxGameLength}. The game ends when generation N is reached, so turn N - 1 is the last one played. Then the team with the most castles wins, and teams tied for the most castles draw.`,
      "In a game of bots, when a turn's time runs out before every player has finished it, every team with a player who hadn't finished loses. Of the other teams still in the game, the one with the most castles wins, and a tie is a draw.",
    ],
  },
];
