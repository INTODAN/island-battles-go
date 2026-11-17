import { describe, expect, test } from "bun:test";
import { type GenerationLogs, LogBook, LogFetchScheduler } from "./logs";

const logs = (id: number, message?: string): GenerationLogs => ({
  generation_id: id,
  players: [
    {
      display_name: "Alex",
      team_color: 1,
      lines: message ? [{ level: "info", logger: "bot", message }] : [],
    },
  ],
});

describe("LogBook", () => {
  test("reads the logs of a turn once, after it is over", () => {
    const book = new LogBook();
    expect(book.needsFetch(5, true)).toBe(true);
    expect(book.add(logs(5, "attack"))).toBe(true);
    expect(book.needsFetch(5, true)).toBe(false);
    expect(book.get(5)?.players[0]?.lines[0]?.message).toBe("attack");
  });

  test("reads the open turn's logs again once it is over", () => {
    const book = new LogBook();
    expect(book.add(logs(5), false)).toBe(true);
    // While the turn stays open, it isn't read again.
    expect(book.needsFetch(5, false)).toBe(false);
    expect(book.needsFetch(5, true)).toBe(true);
    expect(book.add(logs(5, "final line"), true)).toBe(true);
    expect(book.needsFetch(5, true)).toBe(false);
    // A read from while it was open, answering late, doesn't replace the final lines.
    expect(book.add(logs(5), false)).toBe(false);
    expect(book.get(5)?.players[0]?.lines).toHaveLength(1);
  });

  test("a history's open turn is not settled", () => {
    const book = new LogBook();
    book.addAll([logs(4, "a"), logs(5)], 5);
    expect(book.needsFetch(4, true)).toBe(false);
    expect(book.needsFetch(5, true)).toBe(true);
    book.addAll([logs(6)]);
    expect(book.needsFetch(6, true)).toBe(false);
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
    const show = (id: number, open: boolean, settled = false) =>
      scheduler.update({ open, generationId: id, settled, needsFetch: true });
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
      settled: false,
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

  test("fetches the open turn once more when it is over with the panel open", () => {
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
