import { checkBotHealth, nextFreePort, portProblem } from "../../bots";
import {
  listedSeatValue,
  offeredChoice,
  searchSeatValue,
  seatOptions,
} from "../../bots/compute";
import {
  type ListedBot,
  listedBotLabel,
  listedBotRating,
  mergeListedBots,
} from "../../bots/kinds";
import { webClientUrl } from "../../platform";
import type { Realtime, Snapshot } from "../../realtime";
import { copyText, h } from "../dom";
import { rulesButton } from "../rules/rulesDialog";
import {
  type BotPackage,
  type DefaultBot,
  type LobbyActions,
  type MapTemplateSummary,
  retryTransport,
} from "./actions";
import { mountBotPicker } from "./botPicker";
import {
  changeSettingsBody,
  lobbyMapSettings,
  validateMapSettings,
} from "./form";
import { type MapFields, mountMapFields } from "./mapFields";
import type { Screen } from "./nameScreen";
import { describeProblem, type ProblemContext } from "./problems";
import {
  anyPlaysByHand,
  buildRoom,
  canChangeSettings,
  countPeople,
  describeBot,
  describeSettings,
  guestTag,
  type MemberRow,
  type RatedAccount,
  type RoomModel,
  ratedBlockers,
  seatBody,
  seatCompetitors,
  seatLevels,
  seatSettingsFromValue,
  startBlocker,
  type TeamRow,
  teamTaken,
  yourPorts,
  yourSeats,
} from "./roomModel";
import { mountWatchersPanel } from "./watchersPanel";

export interface RoomScreenOptions {
  realtime: Realtime;
  actions: LobbyActions;
  onSignedOut(): void;
  /** The player is no longer in the lobby (they left, or it is gone). */
  onLeft(notice: string): void;
  /**
   * While the lobby's game runs, the room shows read-only, for a player who
   * went back to it from the game; this button takes them back to the game.
   */
  onReturnToGame(): void;
  /** A sentence to show when the room opens, such as why reopening failed. */
  notice?: string;
  /**
   * Reads a signed-in member's bot names and their URLs, for a rated lobby's choices: the game's
   * `GET /account`.
   */
  readAccount?(): Promise<RatedAccount>;
  /** Opens the account page, from **Manage bot names**; resolves once it closes. */
  openAccount?(): Promise<void>;
}

export interface RoomScreen extends Screen {
  /** Shows a sentence about the lobby, such as why reopening it failed. */
  showNotice(message: string): void;
}

interface Problems {
  team: { teamId: string; message: string } | undefined;
  /** A refused add or remove of a bot seat, shown in its team. */
  seat: { teamId: string; message: string } | undefined;
  bot: string;
  ready: string;
  start: string;
  highlight: string[];
}

const noProblems = (): Problems => ({
  team: undefined,
  seat: undefined,
  bot: "",
  ready: "",
  start: "",
  highlight: [],
});

/** Used until `GET /bots/defaults` answers. */
const fallbackLevels: DefaultBot[] = [1, 2, 3].map((level) => ({
  level,
  name: `Level ${level}`,
  description: "",
}));

/**
 * Screen 3: the lobby room. Everything shown comes from the realtime stores and the server's
 * answers. While the lobby's game runs it is read-only, with "Return to game".
 */
export function mountRoomScreen(options: RoomScreenOptions): RoomScreen {
  const { realtime, actions } = options;
  let disposed = false;
  let busy = false;
  let problems = noProblems();
  let templates: MapTemplateSummary[] = [];
  let levels: DefaultBot[] = fallbackLevels;
  /** The server offers hosted and uploaded bots, so Add bot offers them too. */
  let serverBots = false;
  /** The lobby's packages, which the host may seat, as the bot picker last read them. */
  let lobbyPackages: BotPackage[] = [];
  /** Why uploaded bots can't be seated this month, from the bot picker; null while they can. */
  let computeSpent: string | null = null;
  /** The bot each team's Add bot select shows, by team ID: `level-N`, `package-<id>` or `hosted`. */
  const chosenSeat = new Map<string, string>();
  /** The URL typed for each team's hosted bot, by team ID. */
  const seatUrls = new Map<string, string>();
  /** The port typed for each team's localhost bot, by team ID. */
  const seatPorts = new Map<string, string>();
  /** Whether each localhost bot seat's port answers, by port, as last asked; the host's room asks. */
  let seatHealth = new Map<number, boolean>();
  let lastVersion = -1;
  /** Your bot names once read, for a rated lobby's choices and your blocker; null until then. */
  let account: RatedAccount | null = null;
  /** Whether your bot names have been asked for: once a rated lobby needs them. */
  let accountAsked = false;
  /**
   * The registered bots read so far, rated or not, for the bot picker, Add bot and the members'
   * bots: the first page, then each search's finds; empty until read.
   */
  let listed: ListedBot[] = [];
  /** The team whose Add bot searches the registered bots, one at a time; null for none. */
  let searchTeamId: string | null = null;
  /** The search's last finds, null while it runs, and whether it failed. */
  let searchFound: ListedBot[] | null = null;
  let searchFailed = false;
  let searchTimer: ReturnType<typeof setTimeout> | undefined;
  /** Counts the searches, so only the latest one's answer is shown. */
  let searchSerial = 0;
  const searchInput = h("input", {
    type: "search",
    class: "bot-search",
    placeholder: "Bot or owner name",
    attrs: { "aria-label": "Search the registered bots", maxlength: "100" },
  });
  searchInput.addEventListener("input", () => scheduleSearch(250));

  /** Searches the registered bots after `delay` ms, so typing sends one request. */
  function scheduleSearch(delay: number): void {
    clearTimeout(searchTimer);
    searchTimer = setTimeout(() => void runSearch(), delay);
  }

  /** Reads the registered bots that match the search box; the room remembers what it finds. */
  async function runSearch(): Promise<void> {
    const serial = ++searchSerial;
    searchFound = null;
    searchFailed = false;
    render();
    try {
      const found = await actions.listedBots(searchInput.value.trim());
      if (disposed || serial !== searchSerial) return;
      searchFound = found;
      listed = mergeListedBots(listed, found);
      picker.setListedBots(listed);
    } catch {
      if (disposed || serial !== searchSerial) return;
      searchFound = [];
      searchFailed = true;
    }
    render();
  }

  const title = h("h1", {});
  const settingsLine = h("p", { class: "lead" });
  const codeText = h("strong", {
    class: "join-code",
    attrs: { "aria-label": "Join code" },
  });
  const copyCode = h(
    "button",
    { type: "button", class: "secondary small" },
    "Copy code",
  );
  const copyLink = h(
    "button",
    { type: "button", class: "secondary small" },
    "Copy invite link",
  );
  const connection = h("span", { class: "pill", attrs: { role: "status" } });
  const teamsEl = h("div", { class: "teams" });
  const unassignedEl = h("div", { class: "card unassigned" });
  const hint = h("p", { class: "hint" });

  const readyButton = h("button", { type: "button", class: "primary" });
  /** What keeps you from getting ready in a rated lobby, above the Ready button. */
  const ratedLine = h("p", {
    class: "rated-blocker",
    hidden: true,
    attrs: { role: "status" },
  });
  const readyError = h("p", { class: "field-error", attrs: { role: "alert" } });
  const startButton = h(
    "button",
    { type: "button", class: "primary" },
    "Start game",
  );
  const startError = h("p", { class: "field-error", attrs: { role: "alert" } });
  const leaveButton = h(
    "button",
    { type: "button", class: "secondary" },
    "Leave lobby",
  );
  const returnButton = h(
    "button",
    { type: "button", class: "primary", hidden: true },
    "Return to game",
  );
  returnButton.addEventListener("click", () => options.onReturnToGame());
  const notice = h(
    "p",
    { class: "field-error", attrs: { role: "alert" } },
    options.notice ?? "",
  );
  const botError = h("p", { class: "field-error", attrs: { role: "alert" } });

  // --- Change settings: the host's map, turn limit, turn pace, game length and Rated, while the lobby waits ---
  const changeButton = h(
    "button",
    { type: "button", class: "secondary small", hidden: true },
    "Change settings",
  );
  const changePanel = h("section", {
    class: "card change-map",
    hidden: true,
    attrs: { "aria-label": "Change settings" },
  });
  const changeError = h("p", {
    class: "field-error",
    attrs: { role: "alert" },
  });
  const saveChange = h("button", { type: "submit", class: "primary" }, "Save");
  const cancelChange = h(
    "button",
    { type: "button", class: "secondary" },
    "Cancel",
  );
  /** The open form's fields, which start from the lobby's settings when it opens. */
  let changeFields: MapFields | null = null;

  function openChange(): void {
    const snapshot = realtime.lobby.get();
    if (!snapshot || changeFields) return;
    changeFields = mountMapFields({
      actions,
      idPrefix: "change-",
      initial: lobbyMapSettings(snapshot.lobby.game_settings),
      signedIn: realtime.session.get()?.account !== undefined,
    });
    changeError.textContent = "";
    const form = h(
      "form",
      { noValidate: true },
      h("h2", {}, "Change settings"),
      ...changeFields.nodes,
      h(
        "p",
        { class: "hint" },
        "Saving clears everyone's ready. With fewer teams, the players of the removed teams need a new team, and their bots are removed.",
      ),
      changeError,
      h("div", { class: "actions" }, saveChange, cancelChange),
    );
    form.addEventListener("submit", (event) => {
      event.preventDefault();
      void save();
    });
    changePanel.replaceChildren(form);
    changePanel.hidden = false;
    render();
  }

  function closeChange(): void {
    changeFields?.dispose();
    changeFields = null;
    changePanel.replaceChildren();
    changePanel.hidden = true;
  }

  async function save(): Promise<void> {
    const fields = changeFields;
    if (!fields || busy) return;
    changeError.textContent = "";
    await fields.settled();
    const result = validateMapSettings(fields.values(), fields.templates());
    fields.showErrors(result.ok ? {} : result.errors);
    if (!result.ok || changeFields !== fields) return;
    busy = true;
    render();
    try {
      await actions.changeSettings(changeSettingsBody(result));
      if (changeFields === fields) closeChange();
    } catch (cause) {
      changeError.textContent = handleProblem(cause, "settings").message;
    } finally {
      busy = false;
      if (!disposed) render();
    }
  }
  changeButton.addEventListener("click", openChange);
  cancelChange.addEventListener("click", () => {
    closeChange();
    render();
  });

  // A saved bot comes back in the snapshot, which the session shows.
  const picker = mountBotPicker({
    actions,
    onError(message) {
      problems.bot = message;
      renderProblems();
    },
    describeError(cause) {
      return handleProblem(cause, "bot").message;
    },
    playerId: () => realtime.session.get()?.playerId,
    takenPorts() {
      const snapshot = realtime.lobby.get();
      if (!snapshot) return [];
      return yourSeats(room(snapshot)).map((seat) => seat.port);
    },
    onPackages(items) {
      const key = (list: BotPackage[]) =>
        list.map((item) => item.package_id).join();
      if (key(items) === key(lobbyPackages)) return;
      lobbyPackages = items;
      render();
    },
    onCompute(spent) {
      computeSpent = spent;
      render();
    },
    onManageBotNames() {
      if (!options.openAccount) return;
      // Names and URLs changed on the account page reach the choices once it closes.
      void options.openAccount().then(() => {
        if (!disposed) void readAccount();
      });
    },
  });

  /** Reads your bot names for a rated lobby's choices; a failed read leaves them unknown, and the server decides. */
  async function readAccount(): Promise<void> {
    if (!options.readAccount) return;
    accountAsked = true;
    try {
      const read = await options.readAccount();
      if (disposed) return;
      account = read;
      picker.setAccount({ botNames: read.botNames });
      render();
    } catch {
      // Asked again in a while; until then the picker says they are loading.
      setTimeout(() => {
        accountAsked = false;
        if (!disposed) render();
      }, 5000);
    }
  }

  // A guest has no bot names; a signed-in member's are read once a rated lobby needs them.
  picker.setAccount(
    realtime.session.get()?.account !== undefined ? { botNames: null } : null,
  );

  // The people who watch with the join code, in a section of their own.
  const watchersPanel = mountWatchersPanel({ realtime, actions });

  function flash(button: HTMLButtonElement, text: string, original: string) {
    button.textContent = text;
    setTimeout(() => {
      button.textContent = original;
    }, 1500);
  }
  copyCode.addEventListener("click", async () => {
    const code = realtime.lobby.get()?.lobby.join_code ?? "";
    flash(
      copyCode,
      (await copyText(code)) ? "Copied" : "Copy failed",
      "Copy code",
    );
  });
  copyLink.addEventListener("click", async () => {
    const code = realtime.lobby.get()?.lobby.join_code ?? "";
    const link = `${webClientUrl()}?join=${encodeURIComponent(code)}`;
    flash(
      copyLink,
      (await copyText(link)) ? "Copied" : "Copy failed",
      "Copy invite link",
    );
  });

  /** Reads the effects of a failed call: leaving, signing out, or a sentence to show. */
  function handleProblem(cause: unknown, context: ProblemContext) {
    const shown = describeProblem(cause, context);
    if (shown.signedOut) {
      options.onSignedOut();
    } else if (shown.lostMembership) {
      realtime.leaveLobby();
      options.onLeft(shown.message);
    }
    return shown;
  }

  async function run(
    context: ProblemContext,
    task: () => Promise<void>,
    place: (shown: ReturnType<typeof describeProblem>) => void,
  ): Promise<void> {
    if (busy) return;
    busy = true;
    problems = { ...problems, highlight: [] };
    render();
    try {
      await task();
    } catch (cause) {
      place(handleProblem(cause, context));
    } finally {
      busy = false;
      if (!disposed) render();
    }
  }

  const joinTeam = (teamId: string | null) =>
    run(
      "team",
      () => actions.selectTeam(teamId),
      (shown) => {
        problems.team = teamId ? { teamId, message: shown.message } : undefined;
        if (!teamId) problems.ready = shown.message;
      },
    );
  const toggleReady = () => {
    const snapshot = realtime.lobby.get();
    const you = snapshot && room(snapshot).you;
    if (!you) return Promise.resolve();
    problems.ready = "";
    return run(
      "ready",
      () => actions.setReady(!you.ready),
      (shown) => {
        problems.ready = shown.message;
      },
    );
  };
  const start = () => {
    problems.start = "";
    return run(
      "start",
      () => actions.start(),
      (shown) => {
        problems.start = shown.message;
        problems.highlight = shown.teamIds;
      },
    );
  };
  /**
   * A fresh request ID per click; a transport retry resends the same body, so the seat is added
   * once. `wanted` adds that choice, such as a bot the search found, instead of the select's.
   */
  const addBot = (teamId: string, wanted?: string) => {
    const choice = wanted ?? seatChoice(teamId);
    const fromLeaderboard = choice.startsWith("competitor-");
    const snapshot = realtime.lobby.get();
    const taken = snapshot ? yourPorts(room(snapshot)) : [];
    const port = seatPorts.get(teamId) ?? String(nextFreePort(taken));
    const portRefused =
      choice === "localhost" ? portProblem(port, taken) : null;
    const settings = portRefused
      ? null
      : seatSettingsFromValue(
          choice,
          seatUrls.get(teamId) ?? "",
          port,
          realtime.session.get()?.playerId ?? "",
        );
    if (!settings) {
      problems.seat = {
        teamId,
        message:
          portRefused ??
          "Type the bot's URL first, such as https://bot.example.com/turn.",
      };
      render();
      return Promise.resolve();
    }
    const body = seatBody(teamId, settings, crypto.randomUUID());
    problems.seat = undefined;
    return run(
      // A refused bot from the leaderboard (paused, or no longer listed) is the game's own sentence.
      fromLeaderboard ? "listed-seat" : "seat",
      () => retryTransport(() => actions.addSeat(body)),
      (shown) => {
        problems.seat = { teamId, message: shown.message };
      },
    );
  };
  const removeBot = (member: MemberRow, teamId: string) => {
    problems.seat = undefined;
    return run(
      "seat",
      () => actions.removeSeat(member.memberId),
      (shown) => {
        problems.seat = { teamId, message: shown.message };
      },
    );
  };
  readyButton.addEventListener("click", () => void toggleReady());
  startButton.addEventListener("click", () => void start());
  leaveButton.addEventListener("click", () => {
    if (busy) return;
    busy = true;
    render();
    void actions
      .leave()
      .catch((cause) => {
        // Already out of the lobby counts as left.
        const shown = describeProblem(cause, "ready");
        if (!shown.lostMembership && !shown.signedOut) throw cause;
      })
      .then(() => {
        realtime.leaveLobby();
        options.onLeft("");
      })
      .catch((cause) => {
        busy = false;
        problems.ready = describeProblem(cause, "ready").message;
        render();
      });
  });

  /** The Add bot select's choices: uploaded bots are greyed out once their month's compute is spent. */
  const seatChoices = () => {
    const snapshot = realtime.lobby.get();
    const model = snapshot ? room(snapshot) : undefined;
    return seatOptions({
      levels: levels.map((bot) => bot.level),
      serverBots,
      packages: lobbyPackages,
      spent: computeSpent,
      // A rated lobby seats only default bots, each level once, and bots from the leaderboard, each once.
      rated: model?.rated === true,
      takenLevels: model ? seatLevels(model) : [],
      listed,
      takenCompetitors: model ? seatCompetitors(model) : [],
    });
  };

  /** The Add bot select's value for a team: its last choice while still offered, or the first level. */
  function seatChoice(teamId: string): string {
    const choices = seatChoices();
    return offeredChoice(choices, chosenSeat.get(teamId) ?? "");
  }

  const room = (snapshot: Snapshot): RoomModel =>
    buildRoom(snapshot, realtime.session.get()?.playerId ?? "");

  // --- Rendering ---

  /**
   * A seat is a bot: always ready, with no lease, and the host may remove it while waiting. The
   * player who added a localhost bot seat, whose tab relays it, sees whether its bot answers on
   * its port; everyone sees a localhost bot seat that nobody relays.
   */
  function memberItem(
    member: MemberRow,
    remove?: () => void,
    guest = false,
  ): HTMLElement {
    const seat = member.kind === "seat";
    const ready = seat || member.ready;
    const localPort =
      seat && member.relay?.isYou && member.bot?.kind === "localhost"
        ? member.bot.port
        : undefined;
    const up = localPort === undefined ? undefined : seatHealth.get(localPort);
    return h(
      "li",
      {
        class: `member${seat ? " seat" : ""}${member.isYou ? " you" : ""}${member.connected ? "" : " offline"}`,
        attrs: { "data-player-id": member.playerId },
      },
      h(
        "span",
        { class: "member-name" },
        member.name,
        seat ? h("span", { class: "tag bot" }, "bot") : null,
        member.isYou ? h("span", { class: "tag" }, "you") : null,
        member.isHost ? h("span", { class: "tag host" }, "host") : null,
        guest ? h("span", { class: "tag guest" }, guestTag) : null,
        member.connected
          ? null
          : h("span", { class: "tag offline" }, "disconnected"),
      ),
      h(
        "span",
        { class: `bot-label${member.bot ? "" : " none"}` },
        describeBot(member.bot, member.relay, listed),
        localPort === undefined
          ? null
          : h(
              "span",
              {
                class: "pill",
                attrs: {
                  "data-status":
                    up === undefined ? "checking" : up ? "up" : "down",
                },
              },
              up === undefined ? "Checking…" : up ? "Connected" : "Not running",
            ),
      ),
      h(
        "span",
        {
          class: `ready-mark${ready ? " is-ready" : ""}`,
          attrs: { "aria-label": ready ? "Ready" : "Not ready" },
        },
        ready ? "✓ Ready" : "Not ready",
      ),
      remove
        ? h(
            "div",
            { class: "seat-actions" },
            h(
              "button",
              {
                type: "button",
                class: "secondary small",
                disabled: busy,
                onclick: remove,
              },
              "Remove",
            ),
          )
        : null,
    );
  }

  function teamCard(
    team: TeamRow,
    model: RoomModel,
    guests: readonly string[],
  ): HTMLElement {
    const mine = model.yourTeamId === team.id;
    const taken = teamTaken(team);
    const needsPlayer = problems.highlight.includes(team.id);
    const colour = `var(--team-${((team.colour - 1) % 6) + 1})`;
    const error =
      problems.team?.teamId === team.id ? problems.team.message : "";
    const seatError =
      problems.seat?.teamId === team.id ? problems.seat.message : "";
    // Bots are added and removed by the host, while the lobby waits.
    const editSeats = model.isHost && model.phase === "waiting";
    // A team holds one member, so a bot goes only on an empty team.
    const choice = seatChoice(team.id);
    const option = (value: string, text: string, disabled: boolean) =>
      h(
        "option",
        {
          attrs: {
            value,
            ...(disabled && computeSpent ? { title: computeSpent } : {}),
          },
          selected: value === choice,
          disabled,
        },
        text,
      );
    const portInput =
      choice === "localhost"
        ? h("input", {
            type: "text",
            class: "bot-port",
            value:
              seatPorts.get(team.id) ?? String(nextFreePort(yourPorts(model))),
            attrs: {
              "aria-label": `Localhost bot port for ${team.name}`,
              inputmode: "numeric",
              maxlength: "5",
            },
            oninput: (event: Event) =>
              seatPorts.set(team.id, (event.target as HTMLInputElement).value),
          })
        : null;
    const urlInput =
      choice === "hosted"
        ? h("input", {
            type: "text",
            inputMode: "url",
            autocapitalize: "off",
            spellcheck: false,
            class: "bot-url",
            value: seatUrls.get(team.id) ?? "",
            placeholder: "https://bot.example.com/turn",
            attrs: {
              "aria-label": `Hosted bot URL for ${team.name}`,
              maxlength: "2048",
            },
            oninput: (event: Event) =>
              seatUrls.set(team.id, (event.target as HTMLInputElement).value),
          })
        : null;
    const offered = seatChoices();
    const addSeat =
      editSeats && !taken && offered.length === 0
        ? h(
            "p",
            { class: "hint seat-add" },
            "Each bot level and registered bot already has a seat: a rated game holds each once.",
          )
        : editSeats && !taken
          ? h(
              "div",
              { class: "seat-add" },
              h(
                "select",
                {
                  attrs: { "aria-label": `Bot for ${team.name}` },
                  onchange: (event: Event) => {
                    chosenSeat.set(
                      team.id,
                      (event.target as HTMLSelectElement).value,
                    );
                    // The URL field shows only for a hosted bot, the search only for Search for a bot….
                    if (chosenSeat.get(team.id) === searchSeatValue)
                      openSearch(team.id);
                    render();
                  },
                },
                ...offered.map((item) =>
                  option(item.value, item.label, item.disabled),
                ),
              ),
              urlInput,
              portInput,
              choice === searchSeatValue ? seatSearch(team.id, model) : null,
              portInput
                ? h(
                    "span",
                    { class: "hint" },
                    "Your tab relays this bot's turns to 127.0.0.1 on its port; serve it there with island-battles-bot serve --port N.",
                  )
                : null,
              computeSpent && serverBots && lobbyPackages.length > 0
                ? h("span", { class: "hint" }, computeSpent)
                : null,
              choice === searchSeatValue
                ? null
                : h(
                    "button",
                    {
                      type: "button",
                      class: "secondary small",
                      disabled: busy,
                      onclick: () => void addBot(team.id),
                    },
                    "Add bot",
                  ),
            )
          : null;
    const action = mine
      ? h(
          "button",
          {
            type: "button",
            class: "secondary small",
            disabled: busy || model.phase !== "waiting",
            onclick: () => void joinTeam(null),
          },
          "Leave team",
        )
      : h(
          "button",
          {
            type: "button",
            class: "secondary small",
            disabled: busy || !model.you || taken || model.phase !== "waiting",
            onclick: () => void joinTeam(team.id),
          },
          taken ? "Taken" : "Join team",
        );
    return h(
      "article",
      {
        class: `card team${mine ? " mine" : ""}${needsPlayer ? " needs-player" : ""}`,
        attrs: { "data-team-id": team.id, style: `--team:${colour}` },
      },
      h("header", {}, h("span", { class: "swatch" }), h("h3", {}, team.name)),
      team.members.length > 0
        ? h(
            "ul",
            { class: "members" },
            ...team.members.map((member) =>
              memberItem(
                member,
                editSeats && member.kind === "seat"
                  ? () => void removeBot(member, team.id)
                  : undefined,
                guests.includes(member.memberId),
              ),
            ),
          )
        : h(
            "p",
            { class: "hint empty" },
            needsPlayer ? "This team needs a player." : "Empty",
          ),
      needsPlayer && team.members.length > 0
        ? h("p", { class: "field-error" }, "This team needs a player.")
        : null,
      addSeat,
      h("p", { class: "field-error", attrs: { role: "alert" } }, seatError),
      action,
      h("p", { class: "field-error", attrs: { role: "alert" } }, error),
    );
  }

  function renderProblems(): void {
    botError.textContent = problems.bot;
    readyError.textContent = problems.ready;
    startError.textContent = problems.start;
  }

  /** Opens the search on a team's Add bot, reading the first registered bots unless it has read. */
  function openSearch(teamId: string): void {
    const first = searchTeamId === null;
    searchTeamId = teamId;
    if (first) scheduleSearch(0);
    queueMicrotask(() => searchInput.focus());
  }

  /**
   * A team's search of every registered bot, rated or not: the search box and what it
   * found, each with its owner, rating and URL and an Add button; another team's search, open
   * elsewhere, moves here with its button. A rated lobby leaves out the bots already seated.
   */
  function seatSearch(teamId: string, model: RoomModel): HTMLElement {
    if (searchTeamId !== teamId) {
      return h(
        "button",
        {
          type: "button",
          class: "secondary small",
          onclick: () => {
            openSearch(teamId);
            render();
          },
        },
        "Search here",
      );
    }
    const taken = model.rated ? seatCompetitors(model) : [];
    const found = (searchFound ?? []).filter(
      (bot) => !taken.includes(bot.competitorId),
    );
    const note =
      searchFound === null
        ? "Searching…"
        : searchFailed
          ? "The search failed. Type again to retry."
          : found.length === 0
            ? "No registered bot matches."
            : "";
    return h(
      "div",
      { class: "seat-search" },
      searchInput,
      note ? h("span", { class: "hint" }, note) : null,
      h(
        "ul",
        { class: "seat-search-results" },
        ...found.map((bot) =>
          h(
            "li",
            {},
            h(
              "span",
              { class: "bot-text" },
              h("strong", {}, listedBotLabel(bot)),
              h(
                "span",
                { class: "hint" },
                `${listedBotRating(bot)} · ${bot.url}`,
              ),
            ),
            h(
              "button",
              {
                type: "button",
                class: "secondary small",
                disabled: busy,
                attrs: { "aria-label": `Add ${listedBotLabel(bot)}` },
                onclick: () => {
                  const value = listedSeatValue(bot.competitorId);
                  chosenSeat.set(teamId, value);
                  void addBot(teamId, value);
                },
              },
              "Add",
            ),
          ),
        ),
      ),
    );
  }

  function render(): void {
    if (disposed) return;
    const snapshot = realtime.lobby.get();
    if (!snapshot) return;
    // Redrawing the teams moves the search box, which keeps what was typed but not the focus.
    const searching = document.activeElement === searchInput;
    const model = room(snapshot);
    const started = model.phase !== "waiting";
    const signedIn = realtime.session.get()?.account !== undefined;
    // A rated lobby needs your bot names, for its choices and your blocker.
    if (model.rated && signedIn && !accountAsked) void readAccount();
    const rated = ratedBlockers(model, account);
    title.textContent = snapshot.lobby.name;
    settingsLine.textContent = describeSettings(
      snapshot.lobby.game_settings,
      templates,
      anyPlaysByHand(model),
    );
    const changing = canChangeSettings(model);
    if (!changing && changeFields) closeChange();
    changeButton.hidden = !changing || changeFields !== null;
    changeButton.disabled = busy;
    saveChange.disabled = busy;
    cancelChange.disabled = busy;
    codeText.textContent = snapshot.lobby.join_code;
    const state = realtime.connection.get();
    connection.textContent =
      state === "connected"
        ? "Live"
        : state === "connecting"
          ? "Connecting…"
          : "Reconnecting…";
    connection.dataset.state = state;

    teamsEl.replaceChildren(
      ...model.teams.map((team) => teamCard(team, model, rated.guests)),
    );
    if (searching && searchInput.isConnected) searchInput.focus();
    unassignedEl.hidden = model.unassigned.length === 0;
    unassignedEl.replaceChildren(
      h("h3", {}, "Choosing a team"),
      h(
        "ul",
        { class: "members" },
        ...model.unassigned.map((member) =>
          memberItem(member, undefined, rated.guests.includes(member.memberId)),
        ),
      ),
    );

    picker.update(
      model.you?.kind === "human" ? model.you.bot : undefined,
      started,
      snapshot.lobby.game_settings,
    );
    readyButton.textContent = model.you?.ready ? "Cancel ready" : "I'm ready";
    readyButton.classList.toggle("is-ready", model.you?.ready === true);
    // In a rated lobby, what you still miss keeps you from getting ready; the server decides all the same.
    const youBlocked = rated.you !== null && model.you?.ready !== true;
    ratedLine.textContent = rated.you ?? "";
    ratedLine.hidden = started || rated.you === null;
    readyButton.disabled = busy || started || !model.you || youBlocked;
    readyButton.hidden = started;
    returnButton.hidden = !started;
    startButton.hidden = !model.isHost || started;
    startError.hidden = !model.isHost;
    const blocker = rated.host ?? startBlocker(model, snapshot.start_rules);
    startButton.disabled = busy || started || blocker !== null;
    leaveButton.disabled = busy;
    const members = countPeople([
      ...model.teams.flatMap((team) => team.members),
      ...model.unassigned,
    ]);
    // The room shows a started lobby only to a player who went back from its game.
    hint.textContent = started
      ? "A game is being played in this lobby. Return to it whenever you like; once it ends, Back to lobby opens the lobby for the next game."
      : model.isHost && blocker !== null
        ? blocker
        : members < 2
          ? "Share the code above to invite the other players."
          : model.isHost
            ? "Once every player has a team, a bot and is ready, you can start the game."
            : "Choose a team and a bot, then mark ready. The host will start the game.";
    renderProblems();
  }

  // Every snapshot carries every member's bot, in their member settings.
  function onLobby(snapshot: Snapshot | null): void {
    if (!snapshot || disposed) return;
    if (snapshot.lobby.version !== lastVersion) {
      // A change elsewhere makes an old refusal stale.
      if (lastVersion !== -1) {
        problems.start = "";
        problems.highlight = [];
        notice.textContent = "";
      }
      lastVersion = snapshot.lobby.version;
    }
    render();
  }

  const unsubscribers = [
    realtime.lobby.subscribe(onLobby),
    realtime.connection.subscribe(() => render()),
  ];

  /**
   * Asks the ports of the localhost bot seats that you added whether their bots run, while the
   * room shows. Nobody else's seats: their ports are on their machines.
   */
  async function checkSeats(): Promise<void> {
    const snapshot = realtime.lobby.get();
    if (disposed || document.hidden || !snapshot) return;
    const ports = [
      ...new Set(yourSeats(room(snapshot)).map((seat) => seat.port)),
    ];
    if (ports.length === 0 && seatHealth.size === 0) return;
    const answers = await Promise.all(
      ports.map((port) => checkBotHealth({ port })),
    );
    if (disposed) return;
    const next = new Map(
      ports.map((port, index) => [port, answers[index] === true]),
    );
    // A render rebuilds the teams' fields, so only a change renders.
    const same =
      next.size === seatHealth.size &&
      [...next].every(([port, up]) => seatHealth.get(port) === up);
    seatHealth = next;
    if (!same) render();
  }
  void checkSeats();
  const seatTimer = setInterval(() => void checkSeats(), 3000);
  unsubscribers.push(() => clearInterval(seatTimer));

  void actions
    .mapTemplates()
    .then((maps) => {
      templates = maps;
      render();
    })
    .catch(() => undefined);
  void actions
    .botOptions()
    .then((offered) => {
      if (disposed) return;
      if (offered.bots.length > 0) levels = offered.bots;
      serverBots = offered.serverBots;
      render();
      if (serverBots) void readListed();
    })
    .catch(() => undefined);

  /**
   * Reads the first registered bots, which the server plays only when it offers hosted bots;
   * a failed read is tried again in a while, and offers none until then.
   */
  async function readListed(): Promise<void> {
    try {
      const read = await actions.listedBots();
      if (disposed) return;
      listed = mergeListedBots(read, listed);
      picker.setListedBots(listed);
      render();
    } catch {
      setTimeout(() => {
        if (!disposed) void readListed();
      }, 30000);
    }
  }

  const el = h(
    "div",
    { class: "room" },
    h(
      "header",
      { class: "room-head panel" },
      h(
        "div",
        { class: "room-title" },
        title,
        settingsLine,
        h("div", { class: "actions" }, changeButton, rulesButton()),
      ),
      h(
        "div",
        { class: "invite" },
        h("span", { class: "hint" }, "Join code"),
        codeText,
        h("div", { class: "actions" }, copyCode, copyLink),
        connection,
      ),
    ),
    h(
      "div",
      { class: "room-body" },
      h("div", { class: "room-main" }, changePanel, teamsEl, unassignedEl),
      h(
        "div",
        { class: "room-side" },
        picker.el,
        botError,
        h(
          "section",
          { class: "card controls" },
          hint,
          ratedLine,
          h(
            "div",
            { class: "actions" },
            returnButton,
            readyButton,
            startButton,
            leaveButton,
          ),
          notice,
          readyError,
          startError,
        ),
        watchersPanel.el,
      ),
    ),
  );
  onLobby(realtime.lobby.get());
  return {
    el,
    showNotice(message) {
      notice.textContent = message;
    },
    dispose() {
      disposed = true;
      clearTimeout(searchTimer);
      for (const unsubscribe of unsubscribers) unsubscribe();
      picker.dispose();
      watchersPanel.dispose();
      closeChange();
    },
  };
}
