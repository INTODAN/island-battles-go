import { describe, expect, test } from "bun:test";
import { ApiError } from "../../api/errors";
import {
  appendPage,
  type BoardState,
  BoardStore,
  boardView,
  type CompetitorResult,
  competitorStats,
  competitorSubtitle,
  formatWhen,
  type Leaderboard,
  type LeaderboardCompetitor,
  type LeaderboardEntry,
  type LeaderboardQuery,
  leaderboardHref,
  leaderboardQuery,
  leaderboardSearch,
  loadCompetitor,
  loadErrorText,
  ownCompetitorIds,
  ownerLine,
  pageSize,
  ratingChange,
  readLeaderboardRoute,
  resultLine,
  resultText,
  showingText,
  wdl,
} from "./model";

let next = 0;
function entry(change: Partial<LeaderboardEntry> = {}): LeaderboardEntry {
  next += 1;
  return {
    rank: next,
    competitor_id: `c${next}`,
    kind: "bot",
    name: `Bot ${next}`,
    owner: "Dana",
    rating: 1600 - next,
    games: 10,
    wins: 6,
    draws: 1,
    losses: 3,
    last_played_at: "2026-11-20T14:32:00Z",
    ...change,
  };
}

function entries(count: number, from = 0): LeaderboardEntry[] {
  return Array.from({ length: count }, (_, index) =>
    entry({ rank: from + index + 1, competitor_id: `c${from + index + 1}` }),
  );
}

const ready = (change: Partial<BoardState>): BoardState => ({
  tab: "all",
  status: "ready",
  entries: [],
  total: 0,
  offset: change.entries?.length ?? 0,
  exhausted: false,
  loadingMore: false,
  error: undefined,
  ...change,
});

const tick = () => new Promise((resolve) => setTimeout(resolve, 0));

describe("the page's address", () => {
  test("?leaderboard opens the leaderboard, and ?leaderboard=<id> a competitor", () => {
    expect(readLeaderboardRoute("?leaderboard")).toEqual({ view: "board" });
    expect(readLeaderboardRoute("?leaderboard=")).toEqual({ view: "board" });
    expect(readLeaderboardRoute("?leaderboard=%20")).toEqual({
      view: "board",
    });
    expect(readLeaderboardRoute("?leaderboard=c-1")).toEqual({
      view: "competitor",
      competitorId: "c-1",
    });
    expect(readLeaderboardRoute("?join=ABCD&leaderboard=c-1")).toEqual({
      view: "competitor",
      competitorId: "c-1",
    });
  });

  test("a page without it shows the screens it showed before", () => {
    expect(readLeaderboardRoute("")).toBeUndefined();
    expect(readLeaderboardRoute("?join=ABCD")).toBeUndefined();
    expect(readLeaderboardRoute("?leaderboards")).toBeUndefined();
  });

  test("the address keeps the page's other parameters, and Back removes only its own", () => {
    expect(leaderboardSearch("", { view: "board" })).toBe("?leaderboard");
    expect(
      leaderboardSearch("?leaderboard", {
        view: "competitor",
        competitorId: "c 1/2",
      }),
    ).toBe("?leaderboard=c%201%2F2");
    expect(leaderboardSearch("?leaderboard=c1", undefined)).toBe("");
    expect(leaderboardSearch("?debug=1&leaderboard=c1", undefined)).toBe(
      "?debug=1",
    );
    expect(leaderboardSearch("?debug=1", { view: "board" })).toBe(
      "?debug=1&leaderboard",
    );
  });

  test("links are relative, so they work under /play/ and in development", () => {
    expect(leaderboardHref({ view: "board" })).toBe("?leaderboard");
    expect(leaderboardHref({ view: "competitor", competitorId: "c1" })).toBe(
      "?leaderboard=c1",
    );
  });
});

describe("the leaderboard", () => {
  test("each tab asks for its kind, 50 rows at a time", () => {
    expect(leaderboardQuery("all", 0)).toEqual({ limit: 50, offset: 0 });
    expect(leaderboardQuery("bot", 50)).toEqual({
      kind: "bot",
      limit: 50,
      offset: 50,
    });
    expect(leaderboardQuery("manual", 0).kind).toBe("manual");
    expect(leaderboardQuery("house", 0).kind).toBe("house");
    expect(pageSize).toBe(50);
  });

  test("a bot reads 'by' its owner, hand play 'by hand', and a house bot has no owner line", () => {
    expect(ownerLine({ kind: "bot", owner: "Dana" })).toBe("by Dana");
    expect(ownerLine({ kind: "manual", owner: null })).toBe("by hand");
    expect(ownerLine({ kind: "house", owner: null })).toBeUndefined();
  });

  test("wins, draws and losses fold into one W–D–L column", () => {
    expect(wdl({ wins: 10, draws: 1, losses: 7 })).toBe("10–1–7");
  });

  test("says how many rows show of how many", () => {
    expect(showingText(50, 132)).toBe("Showing 1–50 of 132");
    expect(showingText(0, 0)).toBe("");
  });

  test("the first three stand on the podium, and the rest fill the table", () => {
    const list = entries(5);
    const view = boardView(
      ready({ entries: list, total: 5 }),
      new Set(["c2", "c5"]),
    );
    expect(view.podium.map((step) => [step.entry.rank, step.place])).toEqual([
      [1, 1],
      [2, 2],
      [3, 3],
    ]);
    expect(view.podium.map((step) => step.mine)).toEqual([false, true, false]);
    expect(view.rows.map((row) => [row.entry.rank, row.mine])).toEqual([
      [4, false],
      [5, true],
    ]);
    expect(view.empty).toBe(false);
    expect(view.showMore).toBe(false);
  });

  test("ranks are shown as the API gives them, ties and the skip after them too", () => {
    const list = [
      entry({ rank: 1, rating: 1520 }),
      entry({ rank: 2, rating: 1500 }),
      entry({ rank: 2, rating: 1500 }),
      entry({ rank: 4, rating: 1490 }),
      entry({ rank: 4, rating: 1490 }),
      entry({ rank: 6, rating: 1480 }),
    ];
    const view = boardView(ready({ entries: list, total: 6 }), new Set());
    expect(view.podium.map((step) => step.entry.rank)).toEqual([1, 2, 2]);
    expect(view.rows.map((row) => row.entry.rank)).toEqual([4, 4, 6]);
  });

  test("a house bot is tagged, and its row has no owner", () => {
    const [house] = boardView(
      ready({
        entries: [entry({ kind: "house", owner: null, name: "Bot (level 2)" })],
        total: 1,
      }),
      new Set(),
    ).podium;
    expect(house?.house).toBe(true);
    expect(house?.owner).toBeUndefined();
  });

  test("an empty leaderboard says nobody has played, and offers no Show more", () => {
    const view = boardView(ready({ total: 0 }), new Set());
    expect(view.empty).toBe(true);
    expect(view.showMore).toBe(false);
    expect(view.podium).toEqual([]);
    expect(view.rows).toEqual([]);
    // Not while it loads or after a failure: those have their own lines.
    expect(boardView(ready({ status: "loading" }), new Set()).empty).toBe(
      false,
    );
    expect(boardView(ready({ status: "error" }), new Set()).empty).toBe(false);
  });

  test("Show more shows while fewer rows show than the total", () => {
    const view = boardView(
      ready({ entries: entries(50), total: 132 }),
      new Set(),
    );
    expect(view.showMore).toBe(true);
    expect(view.showing).toBe("Showing 1–50 of 132");
    expect(view.rows).toHaveLength(47);
  });

  test("a page adds only competitors not shown yet", () => {
    const shown = entries(3);
    const [, , third] = shown;
    if (!third) throw new Error("no third entry");
    const page = [third, ...entries(2, 3)];
    expect(appendPage(shown, page).map((item) => item.competitor_id)).toEqual([
      "c1",
      "c2",
      "c3",
      "c4",
      "c5",
    ]);
  });

  test("your own competitors are your hand play and your bot names", () => {
    const competitor = (id: string, kind: "bot" | "manual") => ({
      id,
      kind,
      name: id,
      rating: 1500,
      games: 0,
      wins: 0,
      draws: 0,
      losses: 0,
      last_played_at: null,
    });
    expect(
      ownCompetitorIds({
        account_id: "a1",
        display_name: "Dana",
        max_bot_names: 5,
        hand: competitor("hand", "manual"),
        bot_names: [competitor("kraken", "bot"), competitor("squid", "bot")],
      }),
    ).toEqual(new Set(["hand", "kraken", "squid"]));
  });
});

/** A fake `GET /leaderboard` of `total` competitors, answering when `release` is called. */
function fakeList(total: number) {
  const all = entries(total);
  const calls: LeaderboardQuery[] = [];
  const waiting: (() => void)[] = [];
  let fail: ApiError | undefined;
  return {
    calls,
    failNext(error: ApiError) {
      fail = error;
    },
    release() {
      for (const resolve of waiting.splice(0)) resolve();
    },
    list(query: LeaderboardQuery): Promise<Leaderboard> {
      calls.push(query);
      const error = fail;
      fail = undefined;
      return new Promise((resolve, reject) => {
        waiting.push(() => {
          if (error) {
            reject(error);
            return;
          }
          const kind = all.filter(
            (item) => !query.kind || item.kind === query.kind,
          );
          resolve({
            total: kind.length,
            entries: kind.slice(query.offset, query.offset + query.limit),
          });
        });
      });
    },
  };
}

describe("loading the leaderboard", () => {
  test("loads the first 50, and Show more adds the next page", async () => {
    const api = fakeList(120);
    const store = new BoardStore(api, () => {});
    store.start();
    expect(store.state.status).toBe("loading");
    api.release();
    await tick();
    expect(store.state.entries).toHaveLength(50);
    expect(store.state.total).toBe(120);

    const more = store.showMore();
    expect(store.state.loadingMore).toBe(true);
    api.release();
    await more;
    expect(api.calls.map((query) => query.offset)).toEqual([0, 50]);
    expect(store.state.entries).toHaveLength(100);
    expect(store.state.entries[50]?.rank).toBe(51);

    const last = store.showMore();
    api.release();
    await last;
    expect(store.state.entries).toHaveLength(120);
    expect(boardView(store.state, new Set()).showMore).toBe(false);
  });

  test("after a page repeats a competitor, the next page still starts at the next 50", async () => {
    // Pages cached in different minutes: the board moved between them, so
    // the second page repeats c50, which swapped places with c51.
    const first = entries(50);
    const fifty = first[49];
    if (!fifty) throw new Error("no 50th entry");
    const pages: Record<number, Leaderboard> = {
      0: { total: 132, entries: first },
      50: { total: 132, entries: [fifty, ...entries(49, 51)] },
      100: { total: 132, entries: entries(32, 100) },
    };
    const calls: number[] = [];
    const store = new BoardStore(
      {
        list(query) {
          calls.push(query.offset);
          return Promise.resolve(
            pages[query.offset] ?? { total: 132, entries: [] },
          );
        },
      },
      () => {},
    );
    store.start();
    await tick();
    await store.showMore();
    expect(store.state.entries).toHaveLength(99);
    expect(boardView(store.state, new Set()).showMore).toBe(true);
    await store.showMore();
    expect(calls).toEqual([0, 50, 100]);
    expect(store.state.entries).toHaveLength(131);
    // Every page was asked: Show more is gone instead of asking offset 131 forever.
    expect(boardView(store.state, new Set()).showMore).toBe(false);
  });

  test("a page that comes back empty ends Show more, whatever the total says", async () => {
    const calls: number[] = [];
    const store = new BoardStore(
      {
        list(query) {
          calls.push(query.offset);
          return Promise.resolve({
            total: 120,
            entries: query.offset === 0 ? entries(50) : [],
          });
        },
      },
      () => {},
    );
    store.start();
    await tick();
    expect(boardView(store.state, new Set()).showMore).toBe(true);
    await store.showMore();
    expect(calls).toEqual([0, 50]);
    expect(boardView(store.state, new Set()).showMore).toBe(false);
  });

  test("a tab starts from its first page, and an older tab's answer is dropped", async () => {
    const api = fakeList(10);
    const store = new BoardStore(api, () => {});
    store.start();
    store.select("house");
    expect(store.state.tab).toBe("house");
    api.release();
    await tick();
    expect(api.calls).toEqual([
      { limit: 50, offset: 0 },
      { kind: "house", limit: 50, offset: 0 },
    ]);
    // The fake has bots only, so the house bots' answer is empty, and the
    // first, older answer for All didn't replace it.
    expect(store.state.status).toBe("ready");
    expect(store.state.total).toBe(0);
    expect(boardView(store.state, new Set()).empty).toBe(true);
  });

  test("a failed load says why, and Try again loads it again", async () => {
    const api = fakeList(3);
    const store = new BoardStore(api, () => {});
    api.failNext(
      new ApiError({ kind: "transport", status: 0, detail: "offline" }),
    );
    store.start();
    api.release();
    await tick();
    expect(store.state.status).toBe("error");
    expect(store.state.error).toContain("Couldn't reach the server");
    store.retry();
    api.release();
    await tick();
    expect(store.state.status).toBe("ready");
    expect(store.state.entries).toHaveLength(3);
  });

  test("a failed Show more keeps the rows and says why", async () => {
    const api = fakeList(60);
    const store = new BoardStore(api, () => {});
    store.start();
    api.release();
    await tick();
    api.failNext(new ApiError({ kind: "http", status: 500 }));
    const more = store.showMore();
    api.release();
    await more;
    expect(store.state.status).toBe("ready");
    expect(store.state.entries).toHaveLength(50);
    expect(store.state.error).toBe(
      "Couldn't load more of the leaderboard (error 500). Try again in a moment.",
    );
  });

  test("a throttled load shows the server's busy sentence", () => {
    expect(
      loadErrorText(
        new ApiError({
          kind: "http",
          status: 429,
          detail: "The server is busy. Try again in a moment.",
        }),
        "the leaderboard",
      ),
    ).toBe("The server is busy. Try again in a moment.");
  });
});

const uuid = "7c9e6679-7425-40de-944b-e07fc1f90ae7";

const kraken: LeaderboardCompetitor = {
  competitor_id: "c1",
  kind: "bot",
  name: "Kraken",
  owner: "Dana",
  rank: 1,
  rating: 1609,
  games: 52,
  wins: 34,
  draws: 3,
  losses: 15,
  last_played_at: "2026-11-20T14:32:00Z",
  history: [],
};

function result(change: Partial<CompetitorResult>): CompetitorResult {
  return {
    ended_at: "2026-11-20T14:32:00Z",
    result: "win",
    rating_before: 1599,
    rating_after: 1609,
    opponents: [
      {
        competitor_id: "h3",
        name: "Bot (level 3)",
        owner: null,
        rating_before: 1590,
      },
    ],
    ...change,
  };
}

describe("a competitor's page", () => {
  test("says a result with every opponent and their rating before the game", () => {
    expect(resultText(result({}))).toBe("Won against Bot (level 3) (1590)");
    expect(
      resultText(
        result({
          result: "loss",
          opponents: [
            {
              competitor_id: "c2",
              name: "Mira",
              owner: null,
              rating_before: 1551,
            },
          ],
        }),
      ),
    ).toBe("Lost to Mira (1551)");
    expect(
      resultText(
        result({
          result: "draw",
          opponents: [
            {
              competitor_id: "c3",
              name: "Leviathan",
              owner: "Ola_K",
              rating_before: 1560,
            },
            {
              competitor_id: null,
              name: "Deleted bot",
              owner: null,
              rating_before: 1480,
            },
            {
              competitor_id: "c4",
              name: "Coral",
              owner: "Ben",
              rating_before: 1520,
            },
          ],
        }),
      ),
    ).toBe("Drew with Leviathan (1560), Deleted bot (1480) and Coral (1520)");
  });

  test("an opponent with an ID links to its page, and a deleted one doesn't", () => {
    const line = resultLine(
      result({
        opponents: [
          {
            competitor_id: "c3",
            name: "Leviathan",
            owner: "Ola_K",
            rating_before: 1560,
          },
          {
            competitor_id: null,
            name: "Deleted player",
            owner: null,
            rating_before: 1500,
          },
        ],
      }),
    );
    expect(line.opponents.map((opponent) => opponent.competitorId)).toEqual([
      "c3",
      null,
    ]);
    expect(line.word).toBe("Won");
    expect(line.joiner).toBe("against");
  });

  test("shows each rating change, signed", () => {
    expect(ratingChange(1500, 1520)).toEqual({
      text: "1500 → 1520",
      delta: "+20",
      direction: "up",
    });
    expect(ratingChange(1611, 1599)).toEqual({
      text: "1611 → 1599",
      delta: "−12",
      direction: "down",
    });
    expect(ratingChange(1500, 1500).delta).toBe("±0");
  });

  test("dates read as day, month, year and time", () => {
    expect(formatWhen("2026-11-20T14:32:00Z", "UTC")).toBe(
      "20 Nov 2026, 14:32",
    );
  });

  test("names the owner and the day it last played, and the six numbers", () => {
    expect(competitorSubtitle(kraken, "UTC")).toBe(
      "by Dana · last played 20 Nov 2026",
    );
    expect(
      competitorSubtitle({ ...kraken, kind: "manual", owner: null }, "UTC"),
    ).toBe("by hand · last played 20 Nov 2026");
    expect(
      competitorSubtitle({ ...kraken, kind: "house", owner: null }, "UTC"),
    ).toBe("last played 20 Nov 2026");
    expect(competitorStats(kraken)).toEqual([
      { label: "Rank", value: 1 },
      { label: "Rating", value: 1609 },
      { label: "Games", value: 52 },
      { label: "Won", value: 34 },
      { label: "Drawn", value: 3 },
      { label: "Lost", value: 15 },
    ]);
  });

  test("loads the competitor, and a 404 or an ID that isn't one is not found", async () => {
    const asked: string[] = [];
    const answer = (error?: ApiError) => ({
      competitor: (id: string) => {
        asked.push(id);
        return error
          ? Promise.reject(error)
          : Promise.resolve({ ...kraken, competitor_id: id });
      },
    });
    expect(await loadCompetitor(answer(), uuid)).toEqual({
      status: "ready",
      competitor: { ...kraken, competitor_id: uuid },
    });
    for (const status of [404, 422]) {
      expect(
        await loadCompetitor(
          answer(new ApiError({ kind: "problem", status })),
          uuid,
        ),
      ).toEqual({ status: "not-found" });
    }
    asked.length = 0;
    for (const id of ["nope", "c9", `${uuid}x`, " "]) {
      expect(await loadCompetitor(answer(), id)).toEqual({
        status: "not-found",
      });
    }
    expect(asked).toEqual([]);
    expect(
      await loadCompetitor(
        answer(new ApiError({ kind: "http", status: 502 })),
        uuid,
      ),
    ).toEqual({
      status: "error",
      message:
        "Couldn't load this competitor (error 502). Try again in a moment.",
    });
  });
});
