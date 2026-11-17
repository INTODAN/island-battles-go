import { describe, expect, test } from "bun:test";
import {
  ALL_TEAMS_HIDDEN,
  type GenerationLogs,
  hiddenNotice,
  LogBook,
  LogFetchScheduler,
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

describe("LogFetchScheduler", () => {
  function setup() {
    const requests: Array<[number, boolean]> = [];
    let now = 0;
    let next = 1;
    const timers = new Map<number, { at: number; run: () => void }>();
    const scheduler = new LogFetchScheduler(
      (id, ended) => requests.push([id, ended]),
      {
        set: (run, delay) => {
          timers.set(next, { at: now + delay, run });
          return next++;
        },
        clear: (handle) => void timers.delete(handle as number),
      },
    );
    const advance = (ms: number) => {
      now += ms;
      for (const [key, timer] of [...timers]) {
        if (timer.at <= now) {
          timers.delete(key);
          timer.run();
        }
      }
    };
    const show = (id: number, open: boolean, ended = false) =>
      scheduler.update({ open, generationId: id, ended, needsFetch: true });
    return { requests, advance, show, scheduler };
  }

  test("makes no request while the panel is closed", () => {
    const { requests, advance, show } = setup();
    for (let id = 1; id <= 50; id++) {
      show(id, false);
      advance(1000);
    }
    show(50, false, true);
    advance(1000);
    expect(requests).toEqual([]);
  });

  test("opening the panel fetches the turn on screen once", () => {
    const { requests, advance, show, scheduler } = setup();
    show(7, false);
    show(7, true);
    // The logs arrive, so the redraw no longer needs them.
    scheduler.update({
      open: true,
      generationId: 7,
      ended: false,
      needsFetch: false,
    });
    advance(1000);
    expect(requests).toEqual([[7, false]]);
  });

  test("while open, fetches only turns that stayed 300 ms", () => {
    const { requests, advance, show } = setup();
    show(1, true);
    expect(requests).toEqual([[1, false]]);
    for (let id = 2; id <= 6; id++) {
      show(id, true);
      advance(100);
    }
    expect(requests).toEqual([[1, false]]);
    show(7, true);
    advance(299);
    expect(requests).toHaveLength(1);
    advance(1);
    expect(requests).toEqual([
      [1, false],
      [7, false],
    ]);
  });

  test("fetches the final turn once when the game ends with the panel open", () => {
    const { requests, advance, show } = setup();
    show(1, true);
    show(9, true);
    advance(100);
    show(10, true);
    show(10, true, true);
    show(10, true, true);
    advance(300);
    expect(requests).toEqual([
      [1, false],
      [10, true],
    ]);
  });

  test("closing the panel cancels a waiting request", () => {
    const { requests, advance, show } = setup();
    show(1, true);
    show(2, true);
    show(2, false);
    advance(1000);
    expect(requests).toEqual([[1, false]]);
  });
});
