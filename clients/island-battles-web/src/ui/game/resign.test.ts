import { describe, expect, test } from "bun:test";
import { ApiError } from "../../api/errors";
import type { components } from "../../api/generated/game";
import {
  confirmationKey,
  requestResign,
  resignControl,
  resignQuestion,
  resignRetryDelaysMs,
} from "./resign";

type GenerationState = components["schemas"]["GenerationState"];

const state = { game_id: 1 } as unknown as GenerationState;
const problem = (status: number, problemType?: string, detail?: string) =>
  new ApiError({ kind: "problem", status, problemType, detail });
const transport = () =>
  new ApiError({ kind: "transport", status: 0, detail: "offline" });

function deps(outcomes: Array<GenerationState | ApiError>) {
  const waits: number[] = [];
  let calls = 0;
  return {
    waits,
    get calls() {
      return calls;
    },
    call: async () => {
      const next = outcomes[Math.min(calls, outcomes.length - 1)];
      calls++;
      if (next instanceof ApiError) throw next;
      return next as GenerationState;
    },
    sleep: async (milliseconds: number) => {
      waits.push(milliseconds);
    },
  };
}

describe("resignControl", () => {
  test("shows the button only while the hand play plays", () => {
    for (const status of ["idle", "eliminated", "ended"] as const) {
      expect(
        resignControl({ status, sending: false, resigned: false }),
      ).toEqual({ kind: "hidden" });
    }
    expect(
      resignControl({ status: "playing", sending: false, resigned: false }),
    ).toEqual({ kind: "button", label: "Resign", disabled: false });
  });

  test("is disabled while the request runs", () => {
    expect(
      resignControl({ status: "playing", sending: true, resigned: false }),
    ).toEqual({ kind: "button", label: "Resigning…", disabled: true });
  });

  test("says 'You resigned' until the turn plays, then goes away with the team", () => {
    expect(
      resignControl({ status: "playing", sending: false, resigned: true }),
    ).toEqual({ kind: "resigned", text: "You resigned" });
    // The turn played: the team is out (or the game over), and nothing is left to show.
    expect(
      resignControl({ status: "eliminated", sending: false, resigned: true }),
    ).toEqual({ kind: "hidden" });
    expect(
      resignControl({ status: "ended", sending: false, resigned: true }),
    ).toEqual({ kind: "hidden" });
  });

  test("the question is the issue's", () => {
    expect(resignQuestion).toBe("Resign? Your team loses this game.");
  });
});

describe("confirmationKey", () => {
  test("Esc keeps playing, only while the confirmation is open", () => {
    const esc = { key: "Escape", defaultPrevented: false };
    expect(confirmationKey(true, esc)).toBe("keep-playing");
    expect(confirmationKey(false, esc)).toBeNull();
  });

  test("another feature's Esc is left alone, and other keys do nothing", () => {
    expect(
      confirmationKey(true, { key: "Escape", defaultPrevented: true }),
    ).toBeNull();
    expect(
      confirmationKey(true, { key: "Enter", defaultPrevented: false }),
    ).toBeNull();
  });
});

describe("requestResign", () => {
  test("returns the server's state", async () => {
    const d = deps([state]);
    expect(await requestResign(d)).toEqual({ kind: "resigned", state });
    expect(d.calls).toBe(1);
  });

  test("tries a transport failure again, twice at most, with waits", async () => {
    const answered = deps([transport(), state]);
    expect(await requestResign(answered)).toEqual({
      kind: "resigned",
      state,
    });
    expect(answered.waits).toEqual([resignRetryDelaysMs[0] as number]);

    const silent = deps([transport()]);
    const result = await requestResign(silent);
    expect(result.kind).toBe("unreachable");
    expect(silent.calls).toBe(3);
    expect(silent.waits).toEqual(resignRetryDelaysMs);
  });

  test("tries a server fault and throttling again", async () => {
    const d = deps([problem(503), problem(429), state]);
    expect((await requestResign(d)).kind).toBe("resigned");
    expect(d.calls).toBe(3);
  });

  test("game-ended and team-out mean already resigned", async () => {
    for (const type of ["game-ended", "team-out"]) {
      const d = deps([problem(409, `urn:island-battles:problem:${type}`)]);
      expect(await requestResign(d)).toEqual({ kind: "already" });
      expect(d.calls).toBe(1);
    }
  });

  test("a refusal is shown with its sentence and not tried again", async () => {
    const d = deps([
      problem(409, "urn:island-battles:problem:not-real-time", "no"),
    ]);
    const result = await requestResign(d);
    expect(result.kind).toBe("refused");
    expect(d.calls).toBe(1);
    const other = await requestResign(
      deps([problem(403, undefined, "You are not in this game.")]),
    );
    expect(other).toEqual({
      kind: "refused",
      sentence: "You are not in this game.",
    });
    const signedOut = await requestResign(deps([problem(401)]));
    expect(signedOut).toEqual({
      kind: "refused",
      sentence: "Your session has ended. Sign in again.",
    });
  });
});
