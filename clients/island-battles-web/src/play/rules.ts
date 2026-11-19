// The server's rules for a hand player's orders, as pure functions, so an order the server would
// refuse is refused at once with a plain sentence. They mirror `validateCommands` and
// `turnReservations` in backend/services (generation_input.go, turn_reservations.go): the same
// checks in the same order, against the open generation and the orders of this turn.

import { ApiError } from "../api/errors";
import type { components } from "../api/generated/game";
import type { GenerationState } from "../realtime/install";
import { upgradeName } from "../scene/format";

export type Generation = components["schemas"]["Generation"];
export type UpgradeType = components["schemas"]["UpgradeType"];
/** `GET /game/upgrades`'s `upgrades`: every type with its levels' effects and costs. */
export type UpgradeTable = components["schemas"]["Upgrade"][];

/** Every upgrade type, in the order the API lists them. */
export const upgradeTypes: readonly UpgradeType[] = [
  "burst_potential",
  "clash_stop_generations",
  "generation_amount",
  "speed",
];

/**
 * How many soldiers an order takes: an exact whole number (at least 1), or a share of the castle,
 * `{share: 0.5}` with `0 < share <= 1`. A share is resolved when the order is sent: rounded down,
 * at least 1 (`resolveAmount`).
 */
export type Amount = number | { share: number };

/** One order, as the hand player gives it. */
export type OrderSpec =
  | {
      kind: "send";
      /** A castle of your team. */
      origin: number;
      /** Any other castle. */
      destination: number;
      /** A share is of the origin's unreserved soldiers. */
      amount: Amount;
    }
  | { kind: "upgrade"; castleId: number; upgradeType: UpgradeType }
  | {
      kind: "burst";
      castleId: number;
      /**
       * Soldiers the burst adds. A burst takes no soldiers from the castle, so a share is of the
       * castle's burst potential: `{share: 1}` is the largest burst it can make.
       */
      amount: Amount;
    };

/** An order with its soldiers resolved to a whole number, as it is sent. */
export type ResolvedOrder =
  | { kind: "send"; origin: number; destination: number; soldiers: number }
  | { kind: "upgrade"; castleId: number; upgradeType: UpgradeType }
  | { kind: "burst"; castleId: number; soldiers: number };

/**
 * Why an order can't be given, with the numbers, so a control can word its own reason (such as a
 * disabled menu row's). `refusalSentence` words each one.
 */
export type Refusal =
  /** Hand play isn't `playing`: before the first turn, after the team is out, or after the outcome. */
  | { kind: "not-playing"; status: "idle" | "eliminated" | "ended" }
  /** An amount of 0, a negative or fractional number, or a share outside (0, 1]. */
  | { kind: "bad-amount" }
  /** A send whose origin and destination are the same castle. */
  | { kind: "same-castle" }
  /** The castle isn't in the open generation. */
  | { kind: "castle-not-found"; castleId: number }
  /** The castle belongs to another team (`owner`) or to none (`owner` 0). */
  | { kind: "castle-not-owned"; castleId: number; owner: number }
  /**
   * The castle's unreserved soldiers are fewer than `needed`: the soldiers of a send, or the cost
   * of the next level of `upgrade`.
   */
  | {
      kind: "insufficient-soldiers";
      castleId: number;
      unreserved: number;
      needed: number;
      upgrade?: { type: UpgradeType; level: number };
    }
  /** The type's level, counting this turn's upgrades, is already its maximum. */
  | {
      kind: "upgrade-max-level";
      castleId: number;
      type: UpgradeType;
      level: number;
    }
  | { kind: "unknown-upgrade-type"; type: string }
  /** The castle's burst potential is 0: its Burst upgrade is at level 1. */
  | { kind: "no-burst-potential"; castleId: number }
  /** A burst above the castle's burst potential, counting this turn's upgrades. */
  | {
      kind: "burst-above-potential";
      castleId: number;
      potential: number;
      amount: number;
    }
  /** The castle still repays its last burst: `debt` soldiers left. */
  | { kind: "burst-debt"; castleId: number; debt: number }
  /** A burst of this castle is already ordered this turn. */
  | { kind: "burst-ordered"; castleId: number }
  /**
   * A burst by `{share}` of the burst potential, which is unknown until `GET /game/upgrades` has
   * loaded. An exact amount is not refused for this: the server checks it.
   */
  | { kind: "upgrades-not-loaded"; castleId: number };

/** One castle of your team as the open turn's orders leave it. */
export interface CastleLedger {
  id: number;
  /** Soldiers in the open generation: the server's count, not the animated board's. */
  soldiers: number;
  /** Soldiers that this turn's orders haven't claimed yet: what a send or an upgrade can still use. */
  unreserved: number;
  /** Every type's level counting the upgrades ordered this turn (the next one buys `levels[type] + 1`). */
  levels: Record<UpgradeType, number>;
  /** How many levels of each type this turn's orders buy. */
  ordered: Record<UpgradeType, number>;
  /** Soldiers the castle still owes for its last burst; it can't burst while this is above 0. */
  burstDebt: number;
  /** The largest burst at `levels.burst_potential`; null until the upgrade table has loaded. */
  burstPotential: number | null;
  /** A burst of this castle is ordered this turn. */
  bursting: boolean;
  /** Null when a burst of 1 soldier would pass; otherwise why the castle can't burst now. */
  burstRefusal: Refusal | null;
}

/** Your team's castles, by castle ID. A castle is in it exactly while your team owns it. */
export type Ledger = ReadonlyMap<number, CastleLedger>;

/** What the checks read: the open generation, your team and the upgrade table. */
export interface Board {
  generation: Generation;
  team: number;
  /** Null until `GET /game/upgrades` has loaded: the checks that need it are then left to the server. */
  table: UpgradeTable | null;
}

/** A level of an upgrade type, 1 when the castle's map doesn't list the type (as the server reads it). */
export function levelOf(
  upgrades: Record<string, number>,
  type: string,
): number {
  return upgrades[type] ?? 1;
}

/** The type's entry in the table. */
export function upgradeOf(
  table: UpgradeTable | null,
  type: string,
): components["schemas"]["Upgrade"] | undefined {
  return table?.find((upgrade) => upgrade.type === type);
}

/** The soldiers it costs to reach `level` of `type`, or undefined when the table doesn't list it. */
export function upgradeCost(
  table: UpgradeTable | null,
  type: string,
  level: number,
): number | undefined {
  return upgradeOf(table, type)?.levels.find((entry) => entry.level === level)
    ?.soldier_cost;
}

/** The effect of `level` of `type` (such as Speed 3's 1.5 units a turn), or undefined. */
export function upgradeValue(
  table: UpgradeTable | null,
  type: string,
  level: number,
): number | undefined {
  return upgradeOf(table, type)?.levels.find((entry) => entry.level === level)
    ?.value;
}

/** Whether a team is out of the game: no castle and no soldier group (the server's end rule). */
export function teamOut(generation: Generation, team: number): boolean {
  return (
    !generation.castles.some((castle) => castle.team_color === team) &&
    !generation.soldier_groups.some((group) => group.team_color === team)
  );
}

/**
 * Whether this player plays the open turn by hand, so hand play keeps the turn clock: a `manual`
 * player in a real-time game that has no outcome, whose team is still in the game. The bot relay's
 * own deadline watch stands down exactly then (its `standDown`), so a tab makes one deadline call
 * per turn.
 */
export function playsByHand(state: GenerationState | null): boolean {
  return (
    state !== null &&
    state.bot.kind === "manual" &&
    state.turn_pace_milliseconds !== null &&
    !state.outcome &&
    !teamOut(state.generation, state.team_color)
  );
}

/** Whether an amount is a whole number of at least 1, or a share in (0, 1]. */
export function validAmount(amount: Amount): boolean {
  if (typeof amount === "number") {
    return Number.isSafeInteger(amount) && amount >= 1;
  }
  return Number.isFinite(amount.share) && amount.share > 0 && amount.share <= 1;
}

/**
 * The soldiers of an amount: an exact number as it is; a share of `of` rounded down, at least 1.
 * (A small epsilon keeps `(k / n) * n` from rounding down to `k - 1`.)
 */
export function resolveAmount(amount: Amount, of: number): number {
  if (typeof amount === "number") return amount;
  return Math.max(1, Math.floor(amount.share * of + 1e-9));
}

const emptyLevels = (value: number) =>
  Object.fromEntries(upgradeTypes.map((type) => [type, value])) as Record<
    UpgradeType,
    number
  >;

function burstPotential(
  table: UpgradeTable | null,
  levels: Record<UpgradeType, number>,
): number | null {
  return table === null
    ? null
    : (upgradeValue(table, "burst_potential", levels.burst_potential) ?? null);
}

function burstRefusal(castle: CastleLedger): Refusal | null {
  if (castle.burstPotential !== null && castle.burstPotential < 1) {
    return { kind: "no-burst-potential", castleId: castle.id };
  }
  if (castle.burstDebt > 0) {
    return { kind: "burst-debt", castleId: castle.id, debt: castle.burstDebt };
  }
  if (castle.bursting) return { kind: "burst-ordered", castleId: castle.id };
  return null;
}

/**
 * Your team's castles in `board.generation` after `orders`, replayed in order as the server
 * replays a turn's accepted commands: a send claims its soldiers, an upgrade raises the level and
 * claims its cost, a burst marks the castle as bursting. An order that doesn't fit (an optimistic
 * one) claims what is left, never below 0.
 */
export function buildLedger(
  board: Board,
  orders: readonly ResolvedOrder[] = [],
): Map<number, CastleLedger> {
  const ledger = new Map<number, CastleLedger>();
  for (const castle of board.generation.castles) {
    if (castle.team_color !== board.team) continue;
    const levels = emptyLevels(1);
    for (const type of upgradeTypes)
      levels[type] = levelOf(castle.upgrades, type);
    const entry: CastleLedger = {
      id: castle.id,
      soldiers: castle.soldiers,
      unreserved: castle.soldiers,
      levels,
      ordered: emptyLevels(0),
      burstDebt: castle.burst_debt,
      burstPotential: burstPotential(board.table, levels),
      bursting: false,
      burstRefusal: null,
    };
    ledger.set(castle.id, entry);
  }
  for (const order of orders) applyOrder(board.table, ledger, order);
  for (const castle of ledger.values())
    castle.burstRefusal = burstRefusal(castle);
  return ledger;
}

/** Applies one resolved order to the ledger in place. */
function applyOrder(
  table: UpgradeTable | null,
  ledger: Map<number, CastleLedger>,
  order: ResolvedOrder,
): void {
  if (order.kind === "send") {
    const origin = ledger.get(order.origin);
    if (origin)
      origin.unreserved = Math.max(0, origin.unreserved - order.soldiers);
    return;
  }
  const castle = ledger.get(order.castleId);
  if (!castle) return;
  if (order.kind === "burst") {
    castle.bursting = true;
    return;
  }
  const level = castle.levels[order.upgradeType] + 1;
  castle.levels[order.upgradeType] = level;
  castle.ordered[order.upgradeType] += 1;
  castle.unreserved = Math.max(
    0,
    castle.unreserved - (upgradeCost(table, order.upgradeType, level) ?? 0),
  );
  castle.burstPotential = burstPotential(table, castle.levels);
}

/** The ledger after one more order, for checking the orders that follow it. */
export function withOrder(
  board: Board,
  ledger: Ledger,
  order: ResolvedOrder,
): Map<number, CastleLedger> {
  const next = new Map<number, CastleLedger>();
  for (const [id, castle] of ledger) {
    next.set(id, {
      ...castle,
      levels: { ...castle.levels },
      ordered: { ...castle.ordered },
    });
  }
  applyOrder(board.table, next, order);
  for (const castle of next.values())
    castle.burstRefusal = burstRefusal(castle);
  return next;
}

/** The owner of a castle in the generation, or undefined when it isn't there. */
const ownerOf = (generation: Generation, castleId: number) =>
  generation.castles.find((castle) => castle.id === castleId)?.team_color;

function owned(
  board: Board,
  ledger: Ledger,
  castleId: number,
): CastleLedger | Refusal {
  const owner = ownerOf(board.generation, castleId);
  if (owner === undefined) return { kind: "castle-not-found", castleId };
  const castle = ledger.get(castleId);
  if (owner !== board.team || !castle) {
    return { kind: "castle-not-owned", castleId, owner };
  }
  return castle;
}

const isRefusal = (value: CastleLedger | Refusal): value is Refusal =>
  "kind" in value;

/**
 * Checks an order against the board and the ledger of this turn's orders, in the server's order,
 * and resolves its amount. Returns the order as it would be sent, or why the server would refuse
 * it. Checks that need the upgrade table are left to the server while `board.table` is null.
 */
export function checkOrder(
  board: Board,
  ledger: Ledger,
  spec: OrderSpec,
): { order: ResolvedOrder } | { refusal: Refusal } {
  switch (spec.kind) {
    case "send": {
      if (!validAmount(spec.amount)) return { refusal: { kind: "bad-amount" } };
      if (spec.origin === spec.destination) {
        return { refusal: { kind: "same-castle" } };
      }
      if (ownerOf(board.generation, spec.destination) === undefined) {
        return {
          refusal: { kind: "castle-not-found", castleId: spec.destination },
        };
      }
      const origin = owned(board, ledger, spec.origin);
      if (isRefusal(origin)) return { refusal: origin };
      const soldiers = resolveAmount(spec.amount, origin.unreserved);
      if (soldiers > origin.unreserved) {
        return {
          refusal: {
            kind: "insufficient-soldiers",
            castleId: origin.id,
            unreserved: origin.unreserved,
            needed: soldiers,
          },
        };
      }
      return {
        order: {
          kind: "send",
          origin: spec.origin,
          destination: spec.destination,
          soldiers,
        },
      };
    }
    case "upgrade": {
      if (!upgradeTypes.includes(spec.upgradeType)) {
        return {
          refusal: { kind: "unknown-upgrade-type", type: spec.upgradeType },
        };
      }
      const castle = owned(board, ledger, spec.castleId);
      if (isRefusal(castle)) return { refusal: castle };
      const upgrade = upgradeOf(board.table, spec.upgradeType);
      if (upgrade) {
        const level = castle.levels[spec.upgradeType];
        if (level >= upgrade.max_level) {
          return {
            refusal: {
              kind: "upgrade-max-level",
              castleId: castle.id,
              type: spec.upgradeType,
              level,
            },
          };
        }
        const cost = upgradeCost(board.table, spec.upgradeType, level + 1) ?? 0;
        if (cost > castle.unreserved) {
          return {
            refusal: {
              kind: "insufficient-soldiers",
              castleId: castle.id,
              unreserved: castle.unreserved,
              needed: cost,
              upgrade: { type: spec.upgradeType, level: level + 1 },
            },
          };
        }
      }
      return {
        order: {
          kind: "upgrade",
          castleId: spec.castleId,
          upgradeType: spec.upgradeType,
        },
      };
    }
    case "burst": {
      if (!validAmount(spec.amount)) return { refusal: { kind: "bad-amount" } };
      const castle = owned(board, ledger, spec.castleId);
      if (isRefusal(castle)) return { refusal: castle };
      const potential = castle.burstPotential;
      // Until the table has loaded the potential is unknown, so a share of it can't be resolved
      // (it would come out as 1 soldier). An exact amount goes on: the server checks it.
      if (potential === null && typeof spec.amount === "object") {
        return {
          refusal: { kind: "upgrades-not-loaded", castleId: castle.id },
        };
      }
      const soldiers = resolveAmount(spec.amount, potential ?? 0);
      if (potential !== null && soldiers > potential) {
        return {
          refusal:
            potential < 1
              ? { kind: "no-burst-potential", castleId: castle.id }
              : {
                  kind: "burst-above-potential",
                  castleId: castle.id,
                  potential,
                  amount: soldiers,
                },
        };
      }
      if (castle.burstDebt > 0) {
        return {
          refusal: {
            kind: "burst-debt",
            castleId: castle.id,
            debt: castle.burstDebt,
          },
        };
      }
      if (castle.bursting) {
        return { refusal: { kind: "burst-ordered", castleId: castle.id } };
      }
      return { order: { kind: "burst", castleId: spec.castleId, soldiers } };
    }
  }
}

const soldiersWord = (count: number) =>
  count === 1 ? "1 soldier" : `${count} soldiers`;

/** A plain sentence for a refusal, for a notice line or a menu. */
export function refusalSentence(refusal: Refusal): string {
  switch (refusal.kind) {
    case "not-playing":
      return refusal.status === "ended"
        ? "The game has ended."
        : refusal.status === "eliminated"
          ? "Your team is out of the game."
          : "The game hasn't started yet.";
    case "bad-amount":
      return "Choose at least one soldier.";
    case "same-castle":
      return "Choose another castle to send to.";
    case "castle-not-found":
      return "That castle isn't on the board.";
    case "castle-not-owned":
      return "That castle isn't yours.";
    case "insufficient-soldiers":
      if (refusal.upgrade) {
        return `${upgradeName(refusal.upgrade.type)} ${refusal.upgrade.level} costs ${soldiersWord(refusal.needed)}; the castle has ${refusal.unreserved}.`;
      }
      return refusal.unreserved === 0
        ? "The castle has no soldiers left to send."
        : `The castle has only ${soldiersWord(refusal.unreserved)} left to send.`;
    case "upgrade-max-level":
      return `${upgradeName(refusal.type)} is at its maximum level.`;
    case "unknown-upgrade-type":
      return "That upgrade doesn't exist.";
    case "no-burst-potential":
      return "This castle has no burst potential yet; upgrade Burst first.";
    case "burst-above-potential":
      return `This castle can burst at most ${soldiersWord(refusal.potential)}.`;
    case "burst-debt":
      return `This castle is still repaying its last burst: ${soldiersWord(refusal.debt)} left.`;
    case "burst-ordered":
      return "This castle already bursts this turn.";
    case "upgrades-not-loaded":
      return "The upgrade details haven't loaded yet; try the burst again in a moment.";
  }
}

const problemPrefix = "urn:island-battles:problem:";

/** The problem type of a turn that ended before the batch arrived (#353). */
export const staleGenerationProblemType = `${problemPrefix}stale-generation`;

/**
 * Whether a refused batch came after its turn ended: the stale generation's problem type, or, from
 * a server without that type, a 409 with no type (a hand player can't cause the other untyped
 * 409s: it never reuses a request ID and never completes a turn).
 */
export function isStaleTurn(error: ApiError): boolean {
  if (error.problemType === staleGenerationProblemType) return true;
  return (
    error.kind !== "transport" &&
    error.status === 409 &&
    error.problemType === undefined
  );
}

/** A failure worth sending the same request again for: no answer, a server fault or throttling. */
export function isTransientError(error: unknown): boolean {
  if (!(error instanceof ApiError)) return true;
  return (
    error.kind === "transport" || error.status >= 500 || error.status === 429
  );
}

/**
 * A plain sentence for the server's refusal of an order. It has no numbers: the server counted
 * orders that this tab may not know of (another tab, a reload), so ours could be wrong.
 */
export function rejectionSentence(error: ApiError): string {
  if (isStaleTurn(error)) return "The turn ended before the order arrived.";
  switch (error.problemType?.slice(problemPrefix.length)) {
    case "game-ended":
      return "The game ended before the order arrived.";
    case "team-out":
      return "Your team is out of the game.";
    case "castle-not-found":
      return "That castle isn't on the board.";
    case "castle-not-owned":
      return "That castle isn't yours.";
    case "unknown-upgrade-type":
      return "That upgrade doesn't exist.";
    case "upgrade-max-level":
      return "That upgrade is already at its maximum level.";
    case "insufficient-soldiers":
      return "The castle doesn't have enough soldiers left for that.";
    case "burst-unavailable":
      return "This castle can't burst now: it is repaying a burst or already bursts this turn.";
    case "burst-above-potential":
      return "That burst is larger than the castle's burst potential.";
  }
  if (
    error.problemType === undefined &&
    (error.status === 401 || error.status === 403 || error.status === 404)
  ) {
    return "You're no longer in this game.";
  }
  return "The server refused the order.";
}
