/**
 * The leaderboard's model, without the DOM: the page's address, the tabs'
 * queries, what each row and result says, and the stores that load the pages.
 * `leaderboardScreen.ts` draws what these return.
 */
import { ApiError } from "../../api/errors";
import type { components } from "../../api/generated/game";

export type Leaderboard = components["schemas"]["Leaderboard"];
export type LeaderboardEntry = components["schemas"]["LeaderboardEntry"];
export type LeaderboardCompetitor =
  components["schemas"]["LeaderboardCompetitor"];
export type CompetitorResult = components["schemas"]["CompetitorResult"];
export type CompetitorKind = LeaderboardEntry["kind"];
type IslandAccount = components["schemas"]["IslandAccount"];

/** How many rows a page of the leaderboard shows, and **Show more** adds. */
export const pageSize = 50;

/** The page's query parameter: `?leaderboard`, or `?leaderboard=<competitor_id>`. */
export const leaderboardParam = "leaderboard";

export const updatedLine = "Updated every minute";
export const emptyText = "Nobody has played a rated game yet.";
export const emptyHint =
  "Turn on Rated when you create a lobby, and the first result shows here.";
export const notFoundText = "This competitor isn't on the leaderboard.";
export const notFoundHint =
  "Its owner may have deleted it, or it hasn't played a rated game.";
export const houseTag = "house bot";

// --- The address ---

/** What the page shows: the leaderboard, or one competitor's page. */
export type LeaderboardRoute =
  | { view: "board" }
  | { view: "competitor"; competitorId: string };

/** The route in a page's query string, or undefined when it has no `?leaderboard`. */
export function readLeaderboardRoute(
  search: string,
): LeaderboardRoute | undefined {
  const params = new URLSearchParams(search);
  if (!params.has(leaderboardParam)) return undefined;
  const competitorId = params.get(leaderboardParam)?.trim();
  return competitorId
    ? { view: "competitor", competitorId }
    : { view: "board" };
}

/**
 * The query string for a route, keeping the page's other parameters: `?leaderboard`
 * for the leaderboard, `?leaderboard=<id>` for a competitor, and none of it for
 * undefined, the screens the page had before.
 */
export function leaderboardSearch(
  search: string,
  route: LeaderboardRoute | undefined,
): string {
  const params = new URLSearchParams(search);
  params.delete(leaderboardParam);
  const parts = params.toString() ? [params.toString()] : [];
  if (route?.view === "board") parts.push(leaderboardParam);
  if (route?.view === "competitor") {
    parts.push(`${leaderboardParam}=${encodeURIComponent(route.competitorId)}`);
  }
  return parts.length > 0 ? `?${parts.join("&")}` : "";
}

/** The relative link to a route, for an `<a href>` that can be shared or opened in a new tab. */
export function leaderboardHref(route: LeaderboardRoute): string {
  return leaderboardSearch("", route);
}

// --- The leaderboard ---

export type LeaderboardTab = "all" | CompetitorKind;

/** The tabs, in order, and the `kind` each one lists. */
export const leaderboardTabs: readonly {
  tab: LeaderboardTab;
  label: string;
}[] = [
  { tab: "all", label: "All" },
  { tab: "bot", label: "Bots" },
  { tab: "manual", label: "Hand players" },
  { tab: "house", label: "House bots" },
];

export interface LeaderboardQuery {
  kind?: CompetitorKind;
  limit: number;
  offset: number;
}

/** `GET /leaderboard`'s query for a tab's rows from `offset`. */
export function leaderboardQuery(
  tab: LeaderboardTab,
  offset: number,
): LeaderboardQuery {
  return tab === "all"
    ? { limit: pageSize, offset }
    : { kind: tab, limit: pageSize, offset };
}

/**
 * The line under a name: "by Dana" for a bot, "by hand" for hand play, and
 * none for a house bot, which carries the house bot tag instead.
 */
export function ownerLine(entry: {
  kind: CompetitorKind;
  owner: string | null;
}): string | undefined {
  if (entry.kind === "bot")
    return entry.owner ? `by ${entry.owner}` : undefined;
  if (entry.kind === "manual") return "by hand";
  return undefined;
}

/** Wins, draws and losses in one column, as the table shows them at phone width. */
export function wdl(entry: {
  wins: number;
  draws: number;
  losses: number;
}): string {
  return `${entry.wins}–${entry.draws}–${entry.losses}`;
}

/** "Showing 1–50 of 132" under the table. */
export function showingText(shown: number, total: number): string {
  if (shown === 0) return "";
  return `Showing 1–${shown} of ${total}`;
}

/** Adds a page to the rows already shown, leaving out any competitor already in them. */
export function appendPage(
  shown: readonly LeaderboardEntry[],
  page: readonly LeaderboardEntry[],
): LeaderboardEntry[] {
  const seen = new Set(shown.map((entry) => entry.competitor_id));
  return [...shown, ...page.filter((entry) => !seen.has(entry.competitor_id))];
}

export interface BoardRow {
  entry: LeaderboardEntry;
  /** One of the signed-in person's own competitors. */
  mine: boolean;
  owner: string | undefined;
  house: boolean;
  wdl: string;
}

export interface PodiumStep extends BoardRow {
  /** The step on the podium: 1 in the middle and tallest, 2 on its left, 3 on its right. */
  place: 1 | 2 | 3;
}

export interface BoardState {
  tab: LeaderboardTab;
  status: "loading" | "ready" | "error";
  entries: LeaderboardEntry[];
  total: number;
  /** A further page is on its way. */
  loadingMore: boolean;
  /** Why the last load failed, shown with **Try again**. */
  error: string | undefined;
}

export interface BoardView {
  /** The first three, on the podium, in the leaderboard's order. */
  podium: PodiumStep[];
  /** Everyone after them, in the table. */
  rows: BoardRow[];
  empty: boolean;
  showing: string;
  showMore: boolean;
}

function row(entry: LeaderboardEntry, own: ReadonlySet<string>): BoardRow {
  return {
    entry,
    mine: own.has(entry.competitor_id),
    owner: ownerLine(entry),
    house: entry.kind === "house",
    wdl: wdl(entry),
  };
}

/**
 * What the leaderboard shows: the first three on the podium, the rest in the
 * table, and whether **Show more** has more to add. Ranks are the API's, so
 * a tie shows twice (1, 2, 2, 4), also on the podium.
 */
export function boardView(
  state: BoardState,
  own: ReadonlySet<string>,
): BoardView {
  const rows = state.entries.map((entry) => row(entry, own));
  return {
    podium: rows
      .slice(0, 3)
      .map((top, index) => ({ ...top, place: (index + 1) as 1 | 2 | 3 })),
    rows: rows.slice(3),
    empty: state.status === "ready" && state.total === 0,
    showing: showingText(state.entries.length, state.total),
    showMore: state.status === "ready" && state.entries.length < state.total,
  };
}

/** The IDs of the signed-in person's competitors, from the game's `GET /account`. */
export function ownCompetitorIds(account: IslandAccount): Set<string> {
  return new Set([
    account.hand.id,
    ...account.bot_names.map((name) => name.id),
  ]);
}

// --- A competitor's page ---

export interface ResultOpponent {
  name: string;
  /** Null for a competitor deleted or hidden since, which isn't a link. */
  competitorId: string | null;
  rating: number;
}

export interface ResultLine {
  result: CompetitorResult["result"];
  /** "Won", "Lost" or "Drew". */
  word: string;
  /** "against", "to" or "with". */
  joiner: string;
  opponents: ResultOpponent[];
}

const resultWords: Record<
  CompetitorResult["result"],
  { word: string; joiner: string }
> = {
  win: { word: "Won", joiner: "against" },
  loss: { word: "Lost", joiner: "to" },
  draw: { word: "Drew", joiner: "with" },
};

/** A result's sentence, in parts the page draws: "Won against" and every opponent. */
export function resultLine(result: CompetitorResult): ResultLine {
  return {
    result: result.result,
    ...resultWords[result.result],
    opponents: result.opponents.map((opponent) => ({
      name: opponent.name,
      competitorId: opponent.competitor_id,
      rating: opponent.rating_before,
    })),
  };
}

/** "A", "A and B", "A, B and C". */
export function joinNames(names: readonly string[]): string {
  if (names.length <= 1) return names.join("");
  return `${names.slice(0, -1).join(", ")} and ${names.at(-1)}`;
}

/** The result as one sentence: "Won against Kraken (1512) and Bot (level 2) (1500)". */
export function resultText(result: CompetitorResult): string {
  const line = resultLine(result);
  const opponents = joinNames(
    line.opponents.map((opponent) => `${opponent.name} (${opponent.rating})`),
  );
  return `${line.word} ${line.joiner} ${opponents}`;
}

export interface RatingChange {
  /** "1500 → 1520". */
  text: string;
  /** "+20", "−12" or "±0". */
  delta: string;
  direction: "up" | "down" | "flat";
}

export function ratingChange(before: number, after: number): RatingChange {
  const difference = after - before;
  return {
    text: `${before} → ${after}`,
    delta:
      difference > 0
        ? `+${difference}`
        : difference < 0
          ? `−${-difference}`
          : "±0",
    direction: difference > 0 ? "up" : difference < 0 ? "down" : "flat",
  };
}

/** "20 Nov 2026, 14:32", in the viewer's time zone unless one is given. */
export function formatWhen(iso: string, timeZone?: string): string {
  return new Intl.DateTimeFormat("en-GB", {
    day: "numeric",
    month: "short",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    ...(timeZone ? { timeZone } : {}),
  }).format(new Date(iso));
}

/** "20 Nov 2026". */
export function formatDay(iso: string, timeZone?: string): string {
  return new Intl.DateTimeFormat("en-GB", {
    day: "numeric",
    month: "short",
    year: "numeric",
    ...(timeZone ? { timeZone } : {}),
  }).format(new Date(iso));
}

/**
 * The line under a competitor's name: "by Dana · last played 20 Nov 2026", and
 * only the date for a house bot, whose tag says what it is.
 */
export function competitorSubtitle(
  competitor: LeaderboardCompetitor,
  timeZone?: string,
): string {
  const who = ownerLine(competitor);
  const played = `last played ${formatDay(competitor.last_played_at, timeZone)}`;
  return who ? `${who} · ${played}` : played;
}

/** The tag beside a competitor's name on its page. */
export function kindTag(kind: CompetitorKind): string | undefined {
  if (kind === "bot") return "bot";
  if (kind === "house") return houseTag;
  return undefined;
}

/** The six numbers at the top of a competitor's page. */
export function competitorStats(
  competitor: LeaderboardCompetitor,
): { label: string; value: number }[] {
  return [
    { label: "Rank", value: competitor.rank },
    { label: "Rating", value: competitor.rating },
    { label: "Games", value: competitor.games },
    { label: "Won", value: competitor.wins },
    { label: "Drawn", value: competitor.draws },
    { label: "Lost", value: competitor.losses },
  ];
}

// --- Loading ---

/** The game's leaderboard routes, which need no credential. */
export interface LeaderboardApi {
  list(query: LeaderboardQuery): Promise<Leaderboard>;
  /** Rejects with an ApiError, 404 for a competitor the leaderboard doesn't list. */
  competitor(competitorId: string): Promise<LeaderboardCompetitor>;
}

/** The sentence for a load that failed. */
export function loadErrorText(error: unknown, what: string): string {
  if (error instanceof ApiError) {
    if (error.kind === "transport") {
      return `Couldn't reach the server to load ${what}. Check your connection and try again.`;
    }
    if (error.status === 429 && error.detail) return error.detail;
    return `Couldn't load ${what} (error ${error.status}). Try again in a moment.`;
  }
  return `Couldn't load ${what}. Try again in a moment.`;
}

/**
 * The leaderboard's rows for the chosen tab, with **Show more**. An answer
 * that arrives after another tab was chosen is dropped.
 */
export class BoardStore {
  private current: BoardState = {
    tab: "all",
    status: "loading",
    entries: [],
    total: 0,
    loadingMore: false,
    error: undefined,
  };
  private request = 0;
  private started = false;

  constructor(
    private readonly api: Pick<LeaderboardApi, "list">,
    private readonly onChange: (state: BoardState) => void,
  ) {}

  get state(): BoardState {
    return this.current;
  }

  /** Loads the first page once; later calls keep what is shown. */
  start(): void {
    if (this.started) return;
    this.started = true;
    void this.load(this.current.tab);
  }

  select(tab: LeaderboardTab): void {
    if (tab === this.current.tab && this.started) return;
    this.started = true;
    void this.load(tab);
  }

  /** Loads the first page of the current tab again, after a failure. */
  retry(): void {
    if (this.current.entries.length > 0) void this.showMore();
    else void this.load(this.current.tab);
  }

  async showMore(): Promise<void> {
    const { tab, entries, loadingMore } = this.current;
    if (loadingMore) return;
    const request = ++this.request;
    this.set({ loadingMore: true, error: undefined });
    try {
      const page = await this.api.list(leaderboardQuery(tab, entries.length));
      if (request !== this.request) return;
      this.set({
        status: "ready",
        entries: appendPage(this.current.entries, page.entries),
        total: page.total,
        loadingMore: false,
      });
    } catch (error) {
      if (request !== this.request) return;
      this.set({
        loadingMore: false,
        error: loadErrorText(error, "more of the leaderboard"),
      });
    }
  }

  private async load(tab: LeaderboardTab): Promise<void> {
    const request = ++this.request;
    this.set({
      tab,
      status: "loading",
      entries: [],
      total: 0,
      loadingMore: false,
      error: undefined,
    });
    try {
      const page = await this.api.list(leaderboardQuery(tab, 0));
      if (request !== this.request) return;
      this.set({ status: "ready", entries: page.entries, total: page.total });
    } catch (error) {
      if (request !== this.request) return;
      this.set({
        status: "error",
        error: loadErrorText(error, "the leaderboard"),
      });
    }
  }

  private set(change: Partial<BoardState>): void {
    this.current = { ...this.current, ...change };
    this.onChange(this.current);
  }
}

export type CompetitorState =
  | { status: "ready"; competitor: LeaderboardCompetitor }
  | { status: "not-found" }
  | { status: "error"; message: string };

/**
 * A competitor's page. A competitor the leaderboard doesn't list is 404, and an
 * ID that isn't one (a link mistyped by hand) is 422: both are "not found".
 */
export async function loadCompetitor(
  api: Pick<LeaderboardApi, "competitor">,
  competitorId: string,
): Promise<CompetitorState> {
  try {
    return {
      status: "ready",
      competitor: await api.competitor(competitorId),
    };
  } catch (error) {
    if (
      error instanceof ApiError &&
      (error.status === 404 || error.status === 422)
    ) {
      return { status: "not-found" };
    }
    return {
      status: "error",
      message: loadErrorText(error, "this competitor"),
    };
  }
}
