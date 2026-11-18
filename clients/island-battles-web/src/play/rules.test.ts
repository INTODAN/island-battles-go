import { describe, expect, test } from "bun:test";
import { ApiError } from "../api/errors";
import {
  type Amount,
  type Board,
  buildLedger,
  checkOrder,
  isStaleTurn,
  isTransientError,
  type OrderSpec,
  playsByHand,
  type Refusal,
  refusalSentence,
  rejectionSentence,
  resolveAmount,
  teamOut,
  type UpgradeTable,
  validAmount,
} from "./rules";
import { board, handState, upgradeTable } from "./testing";

const at = (
  table: UpgradeTable | null = upgradeTable,
  generation = board(),
): Board => ({
  generation,
  team: 1,
  table,
});

/** The refusal of `spec` against the board and the given earlier orders' ledger, or the order. */
function judge(
  spec: OrderSpec,
  current: Board = at(),
  ledger = buildLedger(current),
) {
  return checkOrder(current, ledger, spec);
}

function refusal(
  spec: OrderSpec,
  current?: Board,
  ledger?: ReturnType<typeof buildLedger>,
): Refusal {
  const result = judge(spec, current, ledger);
  if (!("refusal" in result))
    throw new Error(`accepted: ${JSON.stringify(result)}`);
  return result.refusal;
}

const send = (
  origin: number,
  destination: number,
  amount: Amount,
): OrderSpec => ({
  kind: "send",
  origin,
  destination,
  amount,
});

describe("amounts", () => {
  test("a share is rounded down, at least 1, and exact numbers stay", () => {
    expect(resolveAmount({ share: 0.5 }, 7)).toBe(3);
    expect(resolveAmount({ share: 1 }, 23)).toBe(23);
    expect(resolveAmount({ share: 0.01 }, 7)).toBe(1);
    expect(resolveAmount({ share: 0.5 }, 0)).toBe(1);
    expect(resolveAmount(12, 3)).toBe(12);
  });

  test("k / n of n is k, not k - 1 from rounding", () => {
    for (let n = 1; n <= 200; n++) {
      for (let k = 1; k <= n; k++) {
        expect(resolveAmount({ share: k / n }, n)).toBe(k);
      }
    }
  });

  test("valid amounts are whole numbers from 1 and shares in (0, 1]", () => {
    expect(validAmount(1)).toBe(true);
    expect(validAmount(0)).toBe(false);
    expect(validAmount(-2)).toBe(false);
    expect(validAmount(1.5)).toBe(false);
    expect(validAmount(Number.NaN)).toBe(false);
    expect(validAmount({ share: 1 })).toBe(true);
    expect(validAmount({ share: 0 })).toBe(false);
    expect(validAmount({ share: 1.2 })).toBe(false);
  });
});

describe("the local check of a send", () => {
  test("zero soldiers", () => {
    expect(refusal(send(1, 4, 0))).toEqual({ kind: "bad-amount" });
    expect(refusalSentence({ kind: "bad-amount" })).toBe(
      "Choose at least one soldier.",
    );
  });

  test("the same castle as origin and destination", () => {
    expect(refusal(send(1, 1, 5))).toEqual({ kind: "same-castle" });
  });

  test("a destination that isn't on the board", () => {
    expect(refusal(send(1, 99, 5))).toEqual({
      kind: "castle-not-found",
      castleId: 99,
    });
  });

  test("a castle that isn't yours, another team's or neutral", () => {
    expect(refusal(send(3, 4, 5))).toEqual({
      kind: "castle-not-owned",
      castleId: 3,
      owner: 2,
    });
    const neutral = refusal(send(4, 1, 5));
    expect(neutral).toEqual({
      kind: "castle-not-owned",
      castleId: 4,
      owner: 0,
    });
    expect(refusalSentence(neutral)).toBe("That castle isn't yours.");
  });

  test("more than the unreserved soldiers", () => {
    const tooMany = refusal(send(1, 4, 31));
    expect(tooMany).toEqual({
      kind: "insufficient-soldiers",
      castleId: 1,
      unreserved: 30,
      needed: 31,
    });
    expect(refusalSentence(tooMany)).toBe(
      "The castle has only 30 soldiers left to send.",
    );
    expect(judge(send(1, 4, 30))).toEqual({
      order: { kind: "send", origin: 1, destination: 4, soldiers: 30 },
    });
  });

  test("earlier orders of the turn reserve soldiers", () => {
    const current = at();
    const ledger = buildLedger(current, [
      { kind: "send", origin: 1, destination: 4, soldiers: 20 },
      { kind: "upgrade", castleId: 1, upgradeType: "speed" },
    ]);
    expect(ledger.get(1)?.unreserved).toBe(0);
    const none = refusal(send(1, 4, { share: 1 }), current, ledger);
    expect(refusalSentence(none)).toBe(
      "The castle has no soldiers left to send.",
    );
  });

  test("a share is of the unreserved soldiers", () => {
    const current = at();
    const ledger = buildLedger(current, [
      { kind: "send", origin: 1, destination: 4, soldiers: 10 },
    ]);
    expect(judge(send(1, 4, { share: 0.5 }), current, ledger)).toEqual({
      order: { kind: "send", origin: 1, destination: 4, soldiers: 10 },
    });
  });
});

describe("the local check of an upgrade", () => {
  const upgrade = (
    castleId: number,
    upgradeType: "speed" | "burst_potential" | "clash_stop_generations",
  ): OrderSpec => ({
    kind: "upgrade",
    castleId,
    upgradeType,
  });

  test("a castle that isn't yours", () => {
    expect(refusal(upgrade(3, "speed")).kind).toBe("castle-not-owned");
  });

  test("an unknown type", () => {
    const unknown = refusal({
      kind: "upgrade",
      castleId: 1,
      upgradeType: "armour" as "speed",
    });
    expect(unknown).toEqual({ kind: "unknown-upgrade-type", type: "armour" });
  });

  test("past the maximum, counting this turn's upgrades", () => {
    const current = at();
    const ledger = buildLedger(current, [
      { kind: "upgrade", castleId: 1, upgradeType: "clash_stop_generations" },
    ]);
    expect(
      judge(upgrade(1, "clash_stop_generations"), current, ledger),
    ).toEqual({
      order: {
        kind: "upgrade",
        castleId: 1,
        upgradeType: "clash_stop_generations",
      },
    });
    const twice = buildLedger(current, [
      { kind: "upgrade", castleId: 1, upgradeType: "clash_stop_generations" },
      { kind: "upgrade", castleId: 1, upgradeType: "clash_stop_generations" },
    ]);
    const max = refusal(upgrade(1, "clash_stop_generations"), current, twice);
    expect(max).toEqual({
      kind: "upgrade-max-level",
      castleId: 1,
      type: "clash_stop_generations",
      level: 3,
    });
    expect(refusalSentence(max)).toBe("Stop is at its maximum level.");
  });

  test("several levels in one turn each cost the next level", () => {
    const current = at();
    const ledger = buildLedger(current, [
      { kind: "upgrade", castleId: 1, upgradeType: "speed" },
      { kind: "upgrade", castleId: 1, upgradeType: "speed" },
    ]);
    expect(ledger.get(1)).toMatchObject({
      unreserved: 0,
      levels: { speed: 3 },
      ordered: { speed: 2 },
    });
  });

  test("beyond the soldiers", () => {
    const short = refusal(
      upgrade(2, "speed"),
      at(),
      buildLedger(at(), [
        { kind: "upgrade", castleId: 2, upgradeType: "speed" },
      ]),
    );
    expect(short).toEqual({
      kind: "insufficient-soldiers",
      castleId: 2,
      unreserved: 2,
      needed: 20,
      upgrade: { type: "speed", level: 3 },
    });
    expect(refusalSentence(short)).toBe(
      "Speed 3 costs 20 soldiers; the castle has 2.",
    );
  });

  test("the exact balance is allowed", () => {
    const current = at();
    const ledger = buildLedger(current, [
      { kind: "send", origin: 1, destination: 4, soldiers: 20 },
    ]);
    expect("order" in judge(upgrade(1, "speed"), current, ledger)).toBe(true);
  });

  test("without the table, cost and maximum are left to the server", () => {
    const current = at(null);
    const ledger = buildLedger(current, [
      { kind: "send", origin: 2, destination: 4, soldiers: 12 },
    ]);
    expect("order" in judge(upgrade(2, "speed"), current, ledger)).toBe(true);
    expect(ledger.get(2)?.burstPotential).toBeNull();
  });
});

describe("the local check of a burst", () => {
  const burst = (
    castleId: number,
    amount: number | { share: number },
  ): OrderSpec => ({
    kind: "burst",
    castleId,
    amount,
  });

  test("zero soldiers and a castle that isn't yours", () => {
    expect(refusal(burst(2, 0)).kind).toBe("bad-amount");
    expect(refusal(burst(4, 1)).kind).toBe("castle-not-owned");
  });

  test("no burst potential", () => {
    const none = refusal(burst(1, 1));
    expect(none).toEqual({ kind: "no-burst-potential", castleId: 1 });
    expect(refusalSentence(none)).toBe(
      "This castle has no burst potential yet; upgrade Burst first.",
    );
  });

  test("above the potential", () => {
    const above = refusal(burst(2, 5));
    expect(above).toEqual({
      kind: "burst-above-potential",
      castleId: 2,
      potential: 4,
      amount: 5,
    });
    expect(refusalSentence(above)).toBe(
      "This castle can burst at most 4 soldiers.",
    );
    expect(judge(burst(2, 4))).toEqual({
      order: { kind: "burst", castleId: 2, soldiers: 4 },
    });
  });

  test("the potential counts this turn's Burst upgrades", () => {
    const current = at();
    const ledger = buildLedger(current, [
      { kind: "upgrade", castleId: 1, upgradeType: "burst_potential" },
    ]);
    expect(ledger.get(1)?.burstPotential).toBe(4);
    expect(judge(burst(1, 4), current, ledger)).toEqual({
      order: { kind: "burst", castleId: 1, soldiers: 4 },
    });
  });

  test("a share is of the burst potential", () => {
    expect(judge(burst(2, { share: 1 }))).toEqual({
      order: { kind: "burst", castleId: 2, soldiers: 4 },
    });
    expect(judge(burst(2, { share: 0.5 }))).toEqual({
      order: { kind: "burst", castleId: 2, soldiers: 2 },
    });
  });

  test("a castle with a debt", () => {
    const generation = board();
    const castle = generation.castles[1];
    if (castle) castle.burst_debt = 5;
    const debt = refusal(burst(2, 1), at(upgradeTable, generation));
    expect(debt).toEqual({ kind: "burst-debt", castleId: 2, debt: 5 });
    expect(refusalSentence(debt)).toBe(
      "This castle is still repaying its last burst: 5 soldiers left.",
    );
    expect(
      buildLedger(at(upgradeTable, generation)).get(2)?.burstRefusal,
    ).toEqual(debt);
  });

  test("a second burst in the turn", () => {
    const current = at();
    const ledger = buildLedger(current, [
      { kind: "burst", castleId: 2, soldiers: 2 },
    ]);
    const twice = refusal(burst(2, 1), current, ledger);
    expect(twice).toEqual({ kind: "burst-ordered", castleId: 2 });
    expect(ledger.get(2)?.burstRefusal).toEqual(twice);
    expect(ledger.get(2)?.unreserved).toBe(12);
  });

  test("the potential is checked before the debt, as the server does", () => {
    const generation = board();
    const castle = generation.castles[1];
    if (castle) castle.burst_debt = 5;
    expect(refusal(burst(2, 9), at(upgradeTable, generation)).kind).toBe(
      "burst-above-potential",
    );
  });
});

describe("the ledger", () => {
  test("holds your castles only, with every level, a missing type at level 1", () => {
    const generation = board();
    const castle = generation.castles[0];
    if (castle) castle.upgrades = { speed: 2 };
    const ledger = buildLedger(at(upgradeTable, generation));
    expect([...ledger.keys()]).toEqual([1, 2]);
    expect(ledger.get(1)?.levels).toEqual({
      burst_potential: 1,
      clash_stop_generations: 1,
      generation_amount: 1,
      speed: 2,
    });
    expect(ledger.get(2)?.burstRefusal).toBeNull();
  });
});

describe("who plays by hand", () => {
  test("a manual player in a real-time game still in it, before the outcome", () => {
    expect(playsByHand(handState(10))).toBe(true);
    expect(playsByHand(handState(10, { bot: { kind: "localhost" } }))).toBe(
      false,
    );
    expect(playsByHand(handState(10, { turn_pace_milliseconds: null }))).toBe(
      false,
    );
    expect(
      playsByHand(
        handState(10, {
          outcome: {
            reason: "last_team_standing",
            summary: "",
            winner_team_color: 2,
          },
        }),
      ),
    ).toBe(false);
    expect(playsByHand(handState(10, { team_color: 5 }))).toBe(false);
    expect(playsByHand(null)).toBe(false);
  });

  test("a team with a soldier group but no castle is still in", () => {
    const generation = board(10, {
      castles: [],
      soldier_groups: [
        {
          id: 1,
          team_color: 1,
          soldiers: 3,
          origin_castle_id: 1,
          destination_castle_id: 4,
          clash_radius: 0.25,
          upgrades: {},
          x: 0,
          y: 0,
        },
      ],
    });
    expect(teamOut(generation, 1)).toBe(false);
    expect(teamOut(generation, 2)).toBe(true);
  });
});

describe("the server's refusals", () => {
  const problem = (status: number, type?: string) =>
    new ApiError({
      kind: "problem",
      status,
      problemType: type,
      detail: "details",
    });

  test("each problem type has a sentence without numbers", () => {
    const cases: [number, string, string][] = [
      [409, "game-ended", "The game ended before the order arrived."],
      [409, "stale-generation", "The turn ended before the order arrived."],
      [422, "castle-not-found", "That castle isn't on the board."],
      [403, "castle-not-owned", "That castle isn't yours."],
      [422, "unknown-upgrade-type", "That upgrade doesn't exist."],
      [
        409,
        "upgrade-max-level",
        "That upgrade is already at its maximum level.",
      ],
      [
        409,
        "insufficient-soldiers",
        "The castle doesn't have enough soldiers left for that.",
      ],
      [
        409,
        "burst-unavailable",
        "This castle can't burst now: it is repaying a burst or already bursts this turn.",
      ],
      [
        409,
        "burst-above-potential",
        "That burst is larger than the castle's burst potential.",
      ],
      [422, "game-log-limit", "The server refused the order."],
    ];
    for (const [status, type, sentence] of cases) {
      expect(
        rejectionSentence(
          problem(status, `urn:island-battles:problem:${type}`),
        ),
      ).toBe(sentence);
    }
  });

  test("untyped answers", () => {
    expect(rejectionSentence(problem(409))).toBe(
      "The turn ended before the order arrived.",
    );
    expect(rejectionSentence(problem(401))).toBe(
      "You're no longer in this game.",
    );
    expect(rejectionSentence(problem(403))).toBe(
      "You're no longer in this game.",
    );
    expect(rejectionSentence(problem(404))).toBe(
      "You're no longer in this game.",
    );
    expect(rejectionSentence(problem(422))).toBe(
      "The server refused the order.",
    );
  });

  test("a stale turn is the stale-generation type first, else a 409 without a type", () => {
    expect(
      isStaleTurn(problem(409, "urn:island-battles:problem:stale-generation")),
    ).toBe(true);
    expect(isStaleTurn(problem(409))).toBe(true);
    expect(
      isStaleTurn(problem(409, "urn:island-battles:problem:game-ended")),
    ).toBe(false);
    expect(
      isStaleTurn(
        problem(409, "urn:island-battles:problem:insufficient-soldiers"),
      ),
    ).toBe(false);
    expect(isStaleTurn(problem(422))).toBe(false);
  });

  test("transient failures", () => {
    expect(
      isTransientError(new ApiError({ kind: "transport", status: 0 })),
    ).toBe(true);
    expect(isTransientError(problem(503))).toBe(true);
    expect(isTransientError(new ApiError({ kind: "http", status: 429 }))).toBe(
      true,
    );
    expect(isTransientError(new TypeError("failed"))).toBe(true);
    expect(isTransientError(problem(409))).toBe(false);
    expect(isTransientError(problem(404))).toBe(false);
  });
});
