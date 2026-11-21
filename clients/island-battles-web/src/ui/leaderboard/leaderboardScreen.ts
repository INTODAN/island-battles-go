import { copyText, h } from "../dom";
import type { Screen } from "../lobby/nameScreen";
import { routeLink } from "./links";
import {
  type BoardRow,
  type BoardState,
  BoardStore,
  boardView,
  botUrl,
  type CompetitorState,
  competitorStats,
  competitorSubtitle,
  emptyHint,
  emptyText,
  formatWhen,
  houseTag,
  kindTag,
  type LeaderboardApi,
  type LeaderboardCompetitor,
  type LeaderboardRoute,
  leaderboardTabs,
  loadCompetitor,
  notFoundHint,
  notFoundText,
  type PodiumStep,
  playAgainstHint,
  playAgainstLabel,
  ratingChange,
  resultLine,
  updatedLine,
} from "./model";
import type { PlayTarget } from "./playAgainst";
import { ratingsSections } from "./ratings";
import "./leaderboard.css";

export interface LeaderboardScreen extends Screen {
  /** Shows another route of the leaderboard, keeping the board's tab and rows. */
  setRoute(route: LeaderboardRoute): void;
}

export interface LeaderboardScreenOptions {
  route: LeaderboardRoute;
  api: LeaderboardApi;
  /** The viewer's own competitors, highlighted; empty for a guest or without a session. */
  ownCompetitors: Promise<ReadonlySet<string>>;
  /** Shows a route: a link, a tab or **Back**. Undefined leaves the leaderboard. */
  navigate(route: LeaderboardRoute | undefined): void;
  /**
   * **Play against it** on a listed bot's page: makes the unrated lobby and opens its room, or
   * the name screen first without a session. Rejects with the sentence to show under the button.
   * Without it, the page has no such action.
   */
  playAgainst?(target: PlayTarget): Promise<void>;
}

const youTag = () => h("span", { class: "tag you" }, "you");
/** A listed bot's URL: text to copy, never a link that opens it. */
const urlText = (url: string, className = "bot-url") =>
  h("span", { class: className, attrs: { translate: "no" } }, url);
const houseBotTag = () => h("span", { class: "tag house" }, houseTag);

/** "How ratings work": the opening paragraph, then each point with its title. */
function howRatings(): HTMLElement {
  const [intro, ...points] = ratingsSections;
  return h(
    "section",
    { class: "card how", attrs: { "aria-labelledby": "how-ratings" } },
    h("h2", { id: "how-ratings" }, intro?.title ?? "How ratings work"),
    ...(intro?.paragraphs ?? []).map((paragraph) => h("p", {}, paragraph)),
    h(
      "ul",
      {},
      ...points.map((point) =>
        h(
          "li",
          { id: `ratings-${point.id}` },
          h("strong", {}, `${point.title}:`),
          ` ${point.paragraphs.join(" ")}`,
          ...(point.items ?? []).map((item) => ` ${item}`),
        ),
      ),
    ),
  );
}

/**
 * The leaderboard and a competitor's page (`?leaderboard`,
 * `?leaderboard=<competitor_id>`), a screen of the app shell that needs no
 * session. Server text is written with `textContent` only.
 */
export function mountLeaderboard(
  options: LeaderboardScreenOptions,
): LeaderboardScreen {
  const { api, navigate } = options;
  let own: ReadonlySet<string> = new Set();
  let disposed = false;
  let route = options.route;
  /** Which competitor load is current: an older one's answer is dropped. */
  let competitorRequest = 0;
  /** Where the board was scrolled when a competitor's page replaced it. */
  let boardScroll = 0;

  const el = h("div", { class: "board" });
  const link = (
    to: LeaderboardRoute,
    attributes: { class?: string },
    text: string,
  ) => routeLink(to, navigate, attributes, text);
  const competitorLink = (competitorId: string, name: string, className = "") =>
    link({ view: "competitor", competitorId }, { class: className }, name);

  // --- The board ---

  const store = new BoardStore(api, () => renderBoard());
  const tabButtons = leaderboardTabs.map(({ tab, label }) => {
    const button = h(
      "button",
      {
        type: "button",
        id: `board-tab-${tab}`,
        attrs: { role: "tab", "aria-controls": "board-panel" },
      },
      label,
    );
    button.addEventListener("click", () => store.select(tab));
    return { tab, button };
  });
  const tabs = h(
    "div",
    {
      class: "board-tabs",
      attrs: { role: "tablist", "aria-label": "Which competitors" },
    },
    ...tabButtons.map(({ button }) => button),
  );
  // Arrow keys move between the tabs, as in any tab list.
  tabs.addEventListener("keydown", (event) => {
    const step =
      event.key === "ArrowRight" ? 1 : event.key === "ArrowLeft" ? -1 : 0;
    if (step === 0) return;
    const index = tabButtons.findIndex(({ tab }) => tab === store.state.tab);
    const next =
      tabButtons[(index + step + tabButtons.length) % tabButtons.length];
    if (!next) return;
    event.preventDefault();
    store.select(next.tab);
    next.button.focus();
  });
  const content = h("div", { class: "board-content" });
  const showing = h("span", { class: "hint" });
  const moreError = h("p", { class: "field-error", attrs: { role: "alert" } });
  const moreButton = h(
    "button",
    { type: "button", class: "secondary" },
    "Show more",
  );
  moreButton.addEventListener("click", () => void store.showMore());
  const more = h("div", { class: "board-more" }, showing, moreButton);
  const panel = h(
    "div",
    {
      id: "board-panel",
      class: "board-panel",
      attrs: { role: "tabpanel", "aria-live": "polite" },
    },
    content,
    moreError,
    more,
  );
  const backButton = h(
    "button",
    { type: "button", class: "secondary small" },
    "Back",
  );
  backButton.addEventListener("click", () => navigate(undefined));
  const board = h(
    "div",
    { class: "board-page" },
    h(
      "div",
      { class: "greeting board-head" },
      h(
        "div",
        {},
        h("h1", {}, "Leaderboard"),
        h("p", { class: "lead" }, updatedLine),
      ),
      backButton,
    ),
    h(
      "div",
      { class: "board-body side" },
      h("div", { class: "card board-main" }, tabs, panel),
      howRatings(),
    ),
  );

  function nameCell(row: BoardRow): HTMLElement {
    return h(
      "td",
      { class: "name" },
      competitorLink(row.entry.competitor_id, row.entry.name),
      row.mine ? youTag() : null,
      h(
        "span",
        { class: "owner" },
        row.house ? houseBotTag() : (row.owner ?? ""),
      ),
      row.url ? urlText(row.url) : null,
    );
  }

  function table(rows: BoardRow[]): HTMLElement {
    const number = (value: number | string, extra = "") =>
      h("td", { class: `num ${extra}`.trim() }, String(value));
    const heading = (text: string, extra = "") =>
      h("th", { class: `num ${extra}`.trim(), attrs: { scope: "col" } }, text);
    return h(
      "table",
      { class: "board-table" },
      h(
        "thead",
        {},
        h(
          "tr",
          {},
          h("th", { attrs: { scope: "col" } }, "#"),
          h("th", { attrs: { scope: "col" } }, "Name"),
          heading("Rating"),
          heading("Games"),
          heading("Won", "wide"),
          heading("Drawn", "wide"),
          heading("Lost", "wide"),
          h(
            "th",
            { class: "num wdl", attrs: { scope: "col" } },
            h("abbr", { title: "Won, drawn and lost" }, "W–D–L"),
          ),
        ),
      ),
      h(
        "tbody",
        {},
        ...rows.map((row) =>
          h(
            "tr",
            { class: row.mine ? "mine" : "" },
            h("td", { class: "rank" }, String(row.entry.rank)),
            nameCell(row),
            number(row.entry.rating, "rating"),
            number(row.entry.games),
            number(row.entry.wins, "wide"),
            number(row.entry.draws, "wide"),
            number(row.entry.losses, "wide"),
            number(row.wdl, "wdl"),
          ),
        ),
      ),
    );
  }

  function podiumStep(step: PodiumStep): HTMLElement {
    return h(
      "li",
      {
        class: `podium-step place-${step.place}${step.mine ? " mine" : ""}`,
      },
      h(
        "span",
        {
          class: `medal rank-${Math.min(step.entry.rank, 3)}`,
          attrs: { "aria-label": `Rank ${step.entry.rank}` },
        },
        String(step.entry.rank),
      ),
      h(
        "div",
        { class: "podium-name" },
        competitorLink(step.entry.competitor_id, step.entry.name, "comp-name"),
        step.mine ? youTag() : null,
        step.house ? houseBotTag() : null,
        step.owner ? h("div", { class: "hint" }, step.owner) : null,
        step.url ? urlText(step.url, "bot-url podium-url") : null,
      ),
      h("div", { class: "big" }, String(step.entry.rating)),
    );
  }

  function renderBoard(): void {
    if (disposed) return;
    const state: BoardState = store.state;
    for (const { tab, button } of tabButtons) {
      button.setAttribute("aria-selected", String(tab === state.tab));
      button.tabIndex = tab === state.tab ? 0 : -1;
    }
    panel.setAttribute("aria-labelledby", `board-tab-${state.tab}`);
    const view = boardView(state, own);
    moreButton.disabled = state.loadingMore;
    moreButton.textContent = state.loadingMore ? "Loading…" : "Show more";
    moreButton.hidden = !view.showMore;
    showing.textContent = view.showing;
    more.hidden = state.status !== "ready" || view.empty;
    moreError.textContent = state.status === "ready" ? (state.error ?? "") : "";
    if (state.status === "loading") {
      content.replaceChildren(
        h(
          "p",
          { class: "hint board-status", attrs: { role: "status" } },
          "Loading the leaderboard…",
        ),
      );
      return;
    }
    if (state.status === "error") {
      const retry = h(
        "button",
        { type: "button", class: "secondary" },
        "Try again",
      );
      retry.addEventListener("click", () => store.retry());
      content.replaceChildren(
        h(
          "div",
          { class: "board-status" },
          h("p", { attrs: { role: "alert" } }, state.error ?? ""),
          h("div", { class: "actions" }, retry),
        ),
      );
      return;
    }
    if (view.empty) {
      content.replaceChildren(
        h(
          "div",
          { class: "empty-state" },
          h("strong", {}, emptyText),
          h("p", {}, emptyHint),
        ),
      );
      return;
    }
    const podium = h(
      "ol",
      { class: "podium", attrs: { "aria-label": "Top three" } },
      ...view.podium.map(podiumStep),
    );
    content.replaceChildren(
      ...(view.rows.length > 0 ? [podium, table(view.rows)] : [podium]),
    );
  }

  // --- A competitor's page ---

  /** A listed bot's URL and **Play against it**, under its name; none for hand play or a house bot. */
  function playSection(competitor: LeaderboardCompetitor): HTMLElement | null {
    const url = botUrl(competitor);
    if (!url) return null;
    const target = { name: competitor.name, url };
    const copy = h(
      "button",
      { type: "button", class: "secondary small" },
      "Copy URL",
    );
    copy.addEventListener("click", async () => {
      copy.textContent = (await copyText(url)) ? "Copied" : "Copy failed";
      setTimeout(() => {
        copy.textContent = "Copy URL";
      }, 1500);
    });
    const section = h(
      "section",
      { class: "card play-against", attrs: { "aria-label": "Its hosted bot" } },
      h(
        "p",
        { class: "bot-url-line" },
        h("span", { class: "hint" }, "Plays at "),
        urlText(url),
        " ",
        copy,
      ),
    );
    const play = options.playAgainst;
    if (play) {
      const button = h(
        "button",
        { type: "button", class: "primary" },
        playAgainstLabel,
      );
      const error = h("p", { class: "field-error", attrs: { role: "alert" } });
      button.addEventListener("click", async () => {
        button.disabled = true;
        button.textContent = "Starting…";
        error.textContent = "";
        try {
          await play(target);
        } catch (cause) {
          error.textContent =
            cause instanceof Error && cause.message
              ? cause.message
              : "Something went wrong. Please try again.";
        } finally {
          button.disabled = false;
          button.textContent = playAgainstLabel;
        }
      });
      section.append(
        h(
          "div",
          { class: "play-against-action" },
          button,
          h("p", { class: "hint" }, playAgainstHint(competitor.name)),
        ),
        error,
      );
    }
    return section;
  }

  function competitorPage(competitor: LeaderboardCompetitor): HTMLElement {
    const tag = kindTag(competitor.kind);
    const back = link(
      { view: "board" },
      { class: "button secondary small" },
      "Back to the leaderboard",
    );
    const results = competitor.history.map((result) => {
      const line = resultLine(result);
      const change = ratingChange(result.rating_before, result.rating_after);
      const opponents: (Node | string)[] = [];
      line.opponents.forEach((opponent, index) => {
        if (index > 0) {
          opponents.push(index === line.opponents.length - 1 ? " and " : ", ");
        }
        opponents.push(
          opponent.competitorId
            ? competitorLink(opponent.competitorId, opponent.name)
            : opponent.name,
          ` (${opponent.rating})`,
        );
      });
      return h(
        "li",
        {},
        h("time", { dateTime: result.ended_at }, formatWhen(result.ended_at)),
        h(
          "span",
          { class: "result-text" },
          h("span", { class: `res ${line.result}` }, line.word),
          ` ${line.joiner} `,
          ...opponents,
        ),
        h(
          "span",
          { class: "change" },
          change.text,
          h("span", { class: `delta ${change.direction}` }, change.delta),
        ),
      );
    });
    return h(
      "div",
      { class: "board-page" },
      h(
        "div",
        { class: "greeting board-head" },
        h(
          "div",
          {},
          h(
            "h1",
            {},
            competitor.name,
            tag ? h("span", { class: "tag kind" }, tag) : null,
            own.has(competitor.competitor_id) ? youTag() : null,
          ),
          h("p", { class: "lead" }, competitorSubtitle(competitor)),
        ),
        back,
      ),
      h(
        "div",
        { class: "board-body" },
        playSection(competitor),
        h(
          "div",
          { class: "card" },
          h(
            "dl",
            { class: "stats" },
            ...competitorStats(competitor).map((stat) =>
              h(
                "div",
                { class: "stat" },
                h("dt", {}, stat.label),
                h("dd", {}, String(stat.value)),
              ),
            ),
          ),
        ),
        h(
          "section",
          { class: "card", attrs: { "aria-labelledby": "latest-results" } },
          h("h2", { id: "latest-results" }, "Latest results"),
          h(
            "p",
            { class: "hint" },
            "Newest first. Results stay here after a game's replay is deleted.",
          ),
          results.length > 0
            ? h("ul", { class: "results" }, ...results)
            : h("p", {}, "No results yet."),
        ),
      ),
    );
  }

  function messagePanel(
    lines: HTMLElement[],
    actions: HTMLElement[],
  ): HTMLElement {
    return h(
      "div",
      { class: "panel narrow board-message" },
      h("h1", {}, "Leaderboard"),
      ...lines,
      h("div", { class: "actions" }, ...actions),
    );
  }

  function competitorState(
    state: CompetitorState | undefined,
    competitorId: string,
  ): HTMLElement {
    const toBoard = h(
      "button",
      { type: "button", class: "primary" },
      "See the leaderboard",
    );
    toBoard.addEventListener("click", () => navigate({ view: "board" }));
    const back = h("button", { type: "button", class: "secondary" }, "Back");
    back.addEventListener("click", () => navigate(undefined));
    if (!state) {
      return messagePanel(
        [h("p", { class: "lead", attrs: { role: "status" } }, "Loading…")],
        [toBoard, back],
      );
    }
    if (state.status === "ready") return competitorPage(state.competitor);
    if (state.status === "not-found") {
      return messagePanel(
        [
          h("p", { class: "board-message-text" }, notFoundText),
          h("p", { class: "hint" }, notFoundHint),
        ],
        [toBoard, back],
      );
    }
    const retry = h(
      "button",
      { type: "button", class: "primary" },
      "Try again",
    );
    retry.addEventListener("click", () => showCompetitor(competitorId));
    toBoard.className = "secondary";
    return messagePanel(
      [
        h(
          "p",
          { class: "board-message-text", attrs: { role: "alert" } },
          state.message,
        ),
      ],
      [retry, toBoard],
    );
  }

  /** The competitor's page on screen, drawn again once the viewer's own competitors are known. */
  let shown: { id: string; state: CompetitorState | undefined } = {
    id: "",
    state: undefined,
  };

  function showCompetitor(competitorId: string): void {
    const request = ++competitorRequest;
    shown = { id: competitorId, state: undefined };
    el.replaceChildren(competitorState(undefined, competitorId));
    void loadCompetitor(api, competitorId).then((state) => {
      if (disposed || request !== competitorRequest) return;
      shown = { id: competitorId, state };
      el.replaceChildren(competitorState(state, competitorId));
    });
  }

  // --- Routing ---

  const scroller = () => el.parentElement;

  function show(next: LeaderboardRoute, first: boolean): void {
    const previous = route;
    route = next;
    if (next.view === "board") {
      competitorRequest++;
      el.replaceChildren(board);
      store.start();
      renderBoard();
      if (!first && previous.view !== "board") {
        const restore = boardScroll;
        queueMicrotask(() => scroller()?.scrollTo?.({ top: restore }));
      }
      return;
    }
    if (previous.view === "board" && !first) {
      boardScroll = scroller()?.scrollTop ?? 0;
    }
    showCompetitor(next.competitorId);
    if (!first) queueMicrotask(() => scroller()?.scrollTo?.({ top: 0 }));
  }

  show(route, true);
  void options.ownCompetitors.then((ids) => {
    if (disposed || ids.size === 0) return;
    own = ids;
    if (route.view === "board") renderBoard();
    else if (shown.state?.status === "ready") {
      el.replaceChildren(competitorState(shown.state, shown.id));
    }
  });

  return {
    el,
    setRoute(next) {
      if (
        next.view === route.view &&
        (next.view === "board" ||
          (route.view === "competitor" &&
            next.competitorId === route.competitorId))
      ) {
        return;
      }
      show(next, false);
    },
    dispose() {
      disposed = true;
      competitorRequest++;
    },
  };
}
