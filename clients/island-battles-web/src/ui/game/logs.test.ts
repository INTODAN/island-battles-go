import { describe, expect, test } from "bun:test";
import {
  ALL_TEAMS_HIDDEN,
  type GenerationLogs,
  hiddenNotice,
  LogBook,
  OTHER_TEAMS_HIDDEN,
  SAVED_BEFORE_END,
} from "./logs";

const logs = (id: number, hidden: number[]): GenerationLogs => ({
  generation_id: id,
  hidden_team_colors: hidden,
  players: [],
});

describe("LogBook", () => {
  test("keeps the fuller logs of a generation", () => {
    const book = new LogBook();
    expect(book.add(logs(5, [2]))).toBe(true);
    expect(book.add(logs(5, []))).toBe(true);
    expect(book.add(logs(5, [2]))).toBe(false);
    expect(book.get(5)?.hidden_team_colors).toEqual([]);
  });

  test("reads logs that are missing, or that hid teams before the game ended", () => {
    const book = new LogBook();
    expect(book.needsFetch(5, false)).toBe(true);
    book.add(logs(5, [2]));
    expect(book.needsFetch(5, false)).toBe(false);
    expect(book.needsFetch(5, true)).toBe(true);
    book.add(logs(5, []));
    expect(book.needsFetch(5, true)).toBe(false);
  });
});

describe("hiddenNotice", () => {
  test("says other teams' logs appear when the game ends", () => {
    expect(hiddenNotice(logs(1, [2]), { ended: false, live: true })).toBe(
      OTHER_TEAMS_HIDDEN,
    );
    expect(OTHER_TEAMS_HIDDEN).toBe(
      "Logs of other teams appear when the game ends",
    );
  });

  test("a watcher is told every team's logs appear when the game ends", () => {
    const options = { ended: false, live: true, watching: true };
    expect(hiddenNotice(logs(1, [1, 2]), options)).toBe(ALL_TEAMS_HIDDEN);
    expect(hiddenNotice(logs(1, []), { ...options, ended: true })).toBeNull();
  });

  test("says nothing when no team is hidden, or the game has ended", () => {
    expect(hiddenNotice(logs(1, []), { ended: false, live: true })).toBeNull();
    expect(hiddenNotice(logs(1, [2]), { ended: true, live: true })).toBeNull();
    expect(hiddenNotice(undefined, { ended: false, live: true })).toBeNull();
  });

  test("a demo saved during the game says it holds only one team's logs", () => {
    expect(hiddenNotice(logs(1, [2]), { ended: false, live: false })).toBe(
      SAVED_BEFORE_END,
    );
  });
});
