import { describe, expect, test } from "bun:test";
import type { PlayState, PlayStatus, PlayTurn } from "../../play";
import { PLAYING, WATCHING } from "../../scene/animation/player";
import type { ViewState } from "./controller";
import {
  handView,
  jumpsOnReturn,
  NO_HAND,
  paceSegment,
  RETURN_TO_LIVE_TEXT,
  statusText,
  TEAM_OUT_TEXT,
} from "./handView";
import { generation } from "./testing";

const turn: PlayTurn = {
  gameId: 7,
  generationId: 12,
  turnNumber: 3,
  paceMs: 1000,
  endsAt: 101_000,
  startsAt: 100_000,
};

function play(status: PlayStatus, extra: Partial<PlayState> = {}): PlayState {
  return {
    status,
    teamColor: 1,
    turn,
    castles: new Map(),
    orders: [],
    upgrades: null,
    ...extra,
  };
}

/** A live view of three turns, on turn `index`, following when on the newest. */
function view(index = 2, extra: Partial<ViewState> = {}): ViewState {
  return {
    frames: [generation(1), generation(2), generation(3)],
    index,
    follow: index === 2,
    playing: false,
    outcome: null,
    live: true,
    ...extra,
  };
}

const outcome = {
  reason: "turn_timeout",
  summary: "Blue wins",
  winner_team_color: 1,
} as unknown as NonNullable<ViewState["outcome"]>;

describe("handView", () => {
  test("a watcher, a demo and a bot player see nothing of hand play", () => {
    const views = [
      view(),
      view(0),
      view(-1, { frames: [] }),
      view(2, { outcome }),
      view(0, { live: false, follow: false }),
    ];
    for (const shown of views) {
      expect(handView(null, shown)).toBe(NO_HAND);
      // A player whose bot plays: idle, with the turn and castles filled in.
      expect(
        handView(play("idle", { castles: new Map([[1, {} as never]]) }), shown),
      ).toBe(NO_HAND);
    }
    expect(NO_HAND.timing).toBe(WATCHING);
    expect(NO_HAND.hideSpeed).toBe(false);
  });

  test("a hand player following live: team marked, pace bar, no speed select, no buffer", () => {
    const shown = handView(play("playing"), view());
    expect(shown).toEqual({
      hand: true,
      yourTeam: 1,
      pace: turn,
      status: null,
      hideSpeed: true,
      timing: PLAYING,
      livePaceMs: 1000,
      jumpToLive: true,
    });
  });

  test("scrubbed back, the row offers to return to live and the speed select shows", () => {
    const shown = handView(play("playing"), view(0, { follow: false }));
    expect(shown.status).toBe("return-to-live");
    expect(statusText("return-to-live")).toBe(RETURN_TO_LIVE_TEXT);
    expect(shown.hideSpeed).toBe(false);
    expect(shown.timing).toBe(PLAYING);
  });

  test("paused on the newest turn needs no return", () => {
    expect(
      handView(play("playing"), view(2, { follow: false })).status,
    ).toBeNull();
  });

  test("no return button at the game's end or before a load", () => {
    // The outcome reached the view before hand play saw it.
    expect(
      handView(play("playing"), view(0, { outcome, follow: false })).status,
    ).toBeNull();
    expect(
      handView(play("playing"), view(-1, { frames: [], follow: false })).status,
    ).toBeNull();
    expect(
      handView(play("playing"), view(0, { live: false, follow: false })).status,
    ).toBeNull();
  });

  test("an eliminated player is told, and watches as a watcher does", () => {
    for (const index of [0, 2]) {
      const shown = handView(play("eliminated"), view(index));
      expect(shown.status).toBe("team-out");
      expect(shown.pace).toBeNull();
      expect(shown.hideSpeed).toBe(false);
      expect(shown.timing).toBe(WATCHING);
      expect(shown.livePaceMs).toBeNull();
      expect(shown.jumpToLive).toBe(false);
      expect(shown.yourTeam).toBe(1);
    }
    expect(statusText("team-out")).toBe(TEAM_OUT_TEXT);
  });

  test("after the outcome the team stays marked, and nothing else shows", () => {
    const shown = handView(play("ended"), view(2, { outcome, follow: false }));
    expect(shown.hand).toBe(true);
    expect(shown.status).toBeNull();
    // The bar keys on the status, whatever the turn holds.
    expect(shown.pace).toBeNull();
    expect(shown.timing).toBe(WATCHING);
  });

  test("no pace bar in a game that isn't real-time", () => {
    const waiting = { ...turn, paceMs: null, endsAt: null, startsAt: null };
    expect(handView(play("playing", { turn: waiting }), view()).pace).toBe(
      null,
    );
  });
});

describe("paceSegment", () => {
  test("fills from where the turn is to the end of its time", () => {
    expect(paceSegment(turn, 100_000)).toEqual({ from: 0, durationMs: 1000 });
    expect(paceSegment(turn, 100_250)).toEqual({
      from: 0.25,
      durationMs: 750,
    });
  });

  test("a turn already over is full at once, and one not started yet starts empty", () => {
    expect(paceSegment(turn, 101_400)).toEqual({ from: 1, durationMs: 0 });
    expect(paceSegment(turn, 99_000)).toEqual({ from: 0, durationMs: 2000 });
  });

  test("no bar without a pace", () => {
    expect(paceSegment({ ...turn, paceMs: null }, 100_000)).toBeNull();
    expect(paceSegment({ ...turn, endsAt: null }, 100_000)).toBeNull();
  });
});

describe("jumpsOnReturn", () => {
  test("a hand player following live jumps when turns piled up", () => {
    const playing = handView(play("playing"), view());
    expect(jumpsOnReturn(playing, true, 2)).toBe(true);
    expect(jumpsOnReturn(playing, true, 1)).toBe(false);
    expect(jumpsOnReturn(playing, false, 5)).toBe(false);
    expect(jumpsOnReturn(NO_HAND, true, 5)).toBe(false);
    expect(jumpsOnReturn(handView(play("eliminated"), view()), true, 5)).toBe(
      false,
    );
  });
});
