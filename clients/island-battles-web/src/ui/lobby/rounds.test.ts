import { describe, expect, test } from "bun:test";
import { backToLobby, type RoundRouteInput, routeRound } from "./rounds";

const at = (input: Partial<RoundRouteInput>): RoundRouteInput => ({
  phase: "waiting",
  round: 1,
  closedRound: null,
  viewingRound: null,
  ...input,
});

describe("routeRound", () => {
  test("a waiting lobby shows the room", () => {
    expect(routeRound(at({}))).toEqual({ view: "room", readOnly: false });
  });

  test("a started round shows its game", () => {
    expect(routeRound(at({ phase: "started" }))).toEqual({
      view: "game",
      round: 1,
      reopened: false,
    });
  });

  test("closing a running game's view shows the room read-only", () => {
    expect(
      routeRound(at({ phase: "started", closedRound: 1, viewingRound: 1 })),
    ).toEqual({ view: "room", readOnly: true });
    expect(routeRound(at({ phase: "started", closedRound: 1 }))).toEqual({
      view: "room",
      readOnly: true,
    });
  });

  test("Return to game forgets the closed round and shows the game again", () => {
    expect(routeRound(at({ phase: "started", closedRound: null }))).toEqual({
      view: "game",
      round: 1,
      reopened: false,
    });
  });

  test("after reopening, the next round's room shows", () => {
    expect(routeRound(at({ round: 2, closedRound: 1 }))).toEqual({
      view: "room",
      readOnly: false,
    });
  });

  test("a member looking at the result stays there when another reopens the lobby", () => {
    expect(routeRound(at({ round: 2, viewingRound: 1 }))).toEqual({
      view: "game",
      round: 1,
      reopened: true,
    });
  });

  test("the next game is entered even from the previous result", () => {
    expect(
      routeRound(at({ phase: "started", round: 2, viewingRound: 1 })),
    ).toEqual({ view: "game", round: 2, reopened: false });
    expect(
      routeRound(at({ phase: "started", round: 2, closedRound: 1 })),
    ).toEqual({ view: "game", round: 2, reopened: false });
  });

  test("a closed round's game is never shown again", () => {
    expect(
      routeRound(at({ round: 2, closedRound: 1, viewingRound: 1 })),
    ).toEqual({ view: "room", readOnly: false });
  });

  test("a member who joins between games sees the room", () => {
    expect(routeRound(at({ round: 3 }))).toEqual({
      view: "room",
      readOnly: false,
    });
  });
});

describe("backToLobby", () => {
  test("reopens once the game has ended, and only shows the room before", () => {
    expect(backToLobby("ended")).toBe("reopen");
    expect(backToLobby("running")).toBe("show room");
  });

  test("asks to reopen when no game state is known, such as after a failed start", () => {
    expect(backToLobby("unknown")).toBe("reopen");
  });
});
