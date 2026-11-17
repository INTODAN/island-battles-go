import { describe, expect, test } from "bun:test";
import {
  ALL_TEAMS_HIDDEN,
  chooseLogs,
  type GenerationLogs,
  hiddenNotice,
  LogBook,
  LogFetchScheduler,
  OTHER_TEAMS_HIDDEN,
  openGenerationId,
  SAVED_BEFORE_END,
} from "./logs";

const logs = (id: number, hidden: number[]): GenerationLogs => ({
  generation_id: id,
  hidden_team_colors: hidden,
  players: [],
});

const withLine = (
  base: GenerationLogs,
  team: number,
  message: string,
): GenerationLogs => ({
  ...base,
  players: [
    {
      team_color: team,
      display_name: "Agent check",
      lines: [{ level: "info", logger: "bot", message }],
    },
  ],
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
    const running = { ended: false, open: false };
    const ended = { ended: true, open: false };
    expect(book.needsFetch(5, running)).toBe(true);
    book.add(logs(5, [2]));
    expect(book.needsFetch(5, running)).toBe(false);
    expect(book.needsFetch(5, ended)).toBe(true);
    book.add(logs(5, []));
    expect(book.needsFetch(5, ended)).toBe(false);
  });

  test("reads an open turn's logs again once the turn has ended (#311)", () => {
    const book = new LogBook();
    // Read while turn 5 was the newest: the bots hadn't logged it yet.
    expect(book.add(logs(5, [2]), { provisional: true })).toBe(true);
    expect(book.isProvisional(5)).toBe(true);
    expect(book.needsFetch(5, { ended: false, open: true })).toBe(false);
    expect(book.get(5)).toBeDefined();
    expect(book.settled(5)).toBeUndefined();
    // A newer turn arrived: turn 5 has ended, so it is read again.
    expect(book.needsFetch(5, { ended: false, open: false })).toBe(true);
    const final = withLine(logs(5, [2]), 1, "Turn 5: holding");
    expect(book.add(final)).toBe(true);
    expect(book.isProvisional(5)).toBe(false);
    expect(book.settled(5)).toBe(final);
    expect(book.needsFetch(5, { ended: false, open: false })).toBe(false);
    // The game's end still reveals the other teams.
    expect(book.needsFetch(5, { ended: true, open: false })).toBe(true);
    expect(book.add(logs(5, []))).toBe(true);
    expect(book.needsFetch(5, { ended: true, open: false })).toBe(false);
  });

  test("an open turn's logs replace each other, but never final ones", () => {
    const book = new LogBook();
    book.add(logs(5, [2]), { provisional: true });
    const again = withLine(logs(5, [2]), 1, "Turn 5: thinking");
    expect(book.add(again, { provisional: true })).toBe(true);
    expect(book.get(5)).toBe(again);
    const final = withLine(logs(5, [2]), 1, "Turn 5: done");
    book.add(final);
    expect(book.add(logs(5, [2]), { provisional: true })).toBe(false);
    expect(book.get(5)).toBe(final);
  });

  test("any answer replaces an open turn's logs, even one hiding more teams", () => {
    const book = new LogBook();
    book.add(logs(5, [2]), { provisional: true });
    expect(book.add(logs(5, [2, 3]))).toBe(true);
    expect(book.isProvisional(5)).toBe(false);
    expect(book.settled(5)).toEqual(logs(5, [2, 3]));
    expect(book.needsFetch(5, { ended: false, open: false })).toBe(false);
  });

  test("the game ending reads an open turn's logs again", () => {
    const book = new LogBook();
    book.add(logs(9, [2]), { provisional: true });
    expect(book.needsFetch(9, { ended: true, open: false })).toBe(true);
    expect(book.add(logs(9, []))).toBe(true);
    expect(book.isProvisional(9)).toBe(false);
  });

  test("a history's open turn is provisional", () => {
    const book = new LogBook();
    book.addAll([logs(1, [2]), logs(2, [2]), logs(3, [2])], 3);
    expect(book.isProvisional(1)).toBe(false);
    expect(book.isProvisional(2)).toBe(false);
    expect(book.isProvisional(3)).toBe(true);
  });
});

describe("openGenerationId", () => {
  test("is the newest generation of a running game", () => {
    const frames = [{ id: 4 }, { id: 5 }, { id: 6 }];
    expect(openGenerationId(frames, false)).toBe(6);
    expect(openGenerationId(frames, true)).toBeUndefined();
    expect(openGenerationId([], false)).toBeUndefined();
  });
});

describe("chooseLogs", () => {
  const frames = [{ id: 10 }, { id: 11 }, { id: 12 }, { id: 13 }];
  const view = (
    index: number,
    extra: Partial<Parameters<typeof chooseLogs>[0]> = {},
  ) => ({
    frames,
    index,
    ended: false,
    live: true,
    watching: false,
    ...extra,
  });

  test("an ended turn shows its own logs", () => {
    const book = new LogBook();
    book.add(logs(11, [2]));
    expect(chooseLogs(view(1), book)).toEqual({
      target: 1,
      shown: 1,
      logs: logs(11, [2]),
      openNote: null,
    });
    // Not held yet: loading.
    expect(chooseLogs(view(0), book)).toMatchObject({
      target: 0,
      shown: 0,
      logs: undefined,
    });
  });

  test("an ended turn read while it was open loads again instead of showing no lines", () => {
    const book = new LogBook();
    book.add(logs(12, [2]), { provisional: true });
    expect(chooseLogs(view(2), book)).toMatchObject({
      target: 2,
      shown: 2,
      logs: undefined,
    });
  });

  test("the open turn shows the latest turn that has ended", () => {
    const book = new LogBook();
    book.add(logs(12, [2]));
    expect(chooseLogs(view(3), book)).toEqual({
      target: 2,
      shown: 2,
      logs: logs(12, [2]),
      openNote:
        "Turn 4 is still open, so this shows turn 3, the latest that has ended.",
    });
  });

  test("while the latest ended turn loads, the open turn keeps showing an earlier one", () => {
    const book = new LogBook();
    book.add(logs(10, [2]));
    // Turn 3 was read while it was open: that answer isn't shown as final.
    book.add(logs(12, [2]), { provisional: true });
    expect(chooseLogs(view(3), book)).toEqual({
      target: 2,
      shown: 0,
      logs: logs(10, [2]),
      openNote:
        "Turn 4 is still open, so this shows turn 1 while the logs of turn 3 load.",
    });
    expect(chooseLogs(view(3), new LogBook())).toMatchObject({
      target: 2,
      shown: 2,
      logs: undefined,
    });
  });

  test("a first turn that is still open requests nothing", () => {
    const choice = chooseLogs(
      {
        frames: [{ id: 10 }],
        index: 0,
        ended: false,
        live: true,
        watching: false,
      },
      new LogBook(),
    );
    expect(choice.target).toBe(-1);
    expect(choice.shown).toBe(-1);
    expect(choice.openNote).toContain("Turn 1 is still open");
  });

  test("the newest turn shows its own logs once the game has ended, in a demo, or to a watcher", () => {
    const book = new LogBook();
    book.add(logs(13, []));
    for (const extra of [
      { ended: true },
      { live: false },
      { watching: true },
    ]) {
      expect(chooseLogs(view(3, extra), book)).toEqual({
        target: 3,
        shown: 3,
        logs: logs(13, []),
        openNote: null,
      });
    }
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

  test("while the turn keeps changing, fetches the turn on screen once a second", () => {
    const { requests, advance, show } = setup();
    show(1, true);
    // Following a fast game: a new turn every 200 ms, so none stays 300 ms.
    for (let id = 2; id <= 21; id++) {
      show(id, true);
      advance(200);
    }
    expect(requests).toEqual([
      [1, false],
      [6, false],
      [11, false],
      [16, false],
      [21, false],
    ]);
  });

  test("closing the panel cancels the longer wait too", () => {
    const { requests, advance, show } = setup();
    show(1, true);
    // Turns change for under a second: the longer wait is running.
    for (let id = 2; id <= 5; id++) {
      show(id, true);
      advance(200);
    }
    show(5, false);
    advance(2000);
    expect(requests).toEqual([[1, false]]);
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
