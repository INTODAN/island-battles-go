import { webClientUrl } from "../../platform";
import type { Realtime, Snapshot } from "../../realtime";
import { copyText, h } from "../dom";
import {
  type DefaultBot,
  type LobbyActions,
  type LobbyBots,
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
  buildRoom,
  canChangeSettings,
  countPeople,
  describeBot,
  describeSettings,
  type MemberRow,
  type RoomModel,
  seatBody,
  startBlocker,
  type TeamRow,
  teamTaken,
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
  /** The level each team's Add bot select shows, by team ID. */
  const chosenLevel = new Map<string, number>();
  let bots: LobbyBots | null = null;
  let botsKey = "";
  let botsReading = false;
  let botsDirty = false;
  let lastVersion = -1;

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

  // --- Change map: the host's map and turn limit, while the lobby waits ---
  const changeButton = h(
    "button",
    { type: "button", class: "secondary small", hidden: true },
    "Change map",
  );
  const changePanel = h("section", {
    class: "card change-map",
    hidden: true,
    attrs: { "aria-label": "Change map" },
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
      initial: lobbyMapSettings(
        snapshot.lobby.game_settings,
        snapshot.teams?.length ?? 2,
      ),
    });
    changeError.textContent = "";
    const form = h(
      "form",
      { noValidate: true },
      h("h2", {}, "Change map"),
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

  const picker = mountBotPicker({
    actions,
    onSaved(next) {
      bots = next;
      render();
    },
    onError(message) {
      problems.bot = message;
      renderProblems();
    },
    describeError(cause) {
      return handleProblem(cause, "bot").message;
    },
  });

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
  /** A fresh request ID per click; a transport retry resends the same body, so the seat is added once. */
  const addBot = (teamId: string) => {
    const level = chosenLevel.get(teamId) ?? levels[0]?.level ?? 1;
    const body = seatBody(teamId, level, crypto.randomUUID());
    problems.seat = undefined;
    return run(
      "seat",
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

  const room = (snapshot: Snapshot): RoomModel =>
    buildRoom(snapshot, bots, realtime.session.get()?.playerId ?? "");

  // --- Rendering ---

  /** A seat is a bot: always ready, with no lease, and the host may remove it while waiting. */
  function memberItem(member: MemberRow, remove?: () => void): HTMLElement {
    const seat = member.kind === "seat";
    const ready = seat || member.ready;
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
        member.connected
          ? null
          : h("span", { class: "tag offline" }, "disconnected"),
      ),
      h(
        "span",
        { class: `bot-label${member.bot ? "" : " none"}` },
        describeBot(member.bot),
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

  function teamCard(team: TeamRow, model: RoomModel): HTMLElement {
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
    const level = chosenLevel.get(team.id) ?? levels[0]?.level ?? 1;
    const addSeat =
      editSeats && !taken
        ? h(
            "div",
            { class: "seat-add" },
            h(
              "select",
              {
                attrs: { "aria-label": `Bot level for ${team.name}` },
                onchange: (event: Event) =>
                  chosenLevel.set(
                    team.id,
                    Number((event.target as HTMLSelectElement).value),
                  ),
              },
              ...levels.map((bot) =>
                h(
                  "option",
                  {
                    attrs: { value: String(bot.level) },
                    selected: bot.level === level,
                  },
                  `Bot level ${bot.level}`,
                ),
              ),
            ),
            h(
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

  function render(): void {
    if (disposed) return;
    const snapshot = realtime.lobby.get();
    if (!snapshot) return;
    const model = room(snapshot);
    const started = model.phase !== "waiting";
    title.textContent = snapshot.lobby.name;
    settingsLine.textContent = describeSettings(
      snapshot.lobby.game_settings,
      templates,
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
      ...model.teams.map((team) => teamCard(team, model)),
    );
    unassignedEl.hidden = model.unassigned.length === 0;
    unassignedEl.replaceChildren(
      h("h3", {}, "Choosing a team"),
      h(
        "ul",
        { class: "members" },
        ...model.unassigned.map((member) => memberItem(member)),
      ),
    );

    picker.update(
      model.you
        ? bots?.members.find((m) => m.player_id === model.you?.playerId)?.bot
        : undefined,
      started,
    );
    readyButton.textContent = model.you?.ready ? "Cancel ready" : "I'm ready";
    readyButton.classList.toggle("is-ready", model.you?.ready === true);
    readyButton.disabled = busy || started || !model.you;
    readyButton.hidden = started;
    returnButton.hidden = !started;
    startButton.hidden = !model.isHost || started;
    startError.hidden = !model.isHost;
    const blocker = startBlocker(model, snapshot.start_rules);
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

  // --- Bots of all members: read again whenever the lobby changes ---

  async function refreshBots(snapshot: Snapshot): Promise<void> {
    if (botsReading) {
      botsDirty = true;
      return;
    }
    botsReading = true;
    try {
      bots = await actions.listBots(snapshot.lobby.id);
    } catch (cause) {
      handleProblem(cause, "bot");
    } finally {
      botsReading = false;
    }
    if (disposed) return;
    render();
    const latest = realtime.lobby.get();
    if (botsDirty && latest) {
      botsDirty = false;
      void refreshBots(latest);
    }
  }

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
      const key = `${snapshot.lobby.version}:${(snapshot.members ?? []).map((m) => m.player_id).join(",")}`;
      if (key !== botsKey) {
        botsKey = key;
        void refreshBots(snapshot);
      }
    }
    render();
  }

  const unsubscribers = [
    realtime.lobby.subscribe(onLobby),
    realtime.connection.subscribe(() => render()),
  ];
  const onFocus = () => {
    const snapshot = realtime.lobby.get();
    if (snapshot) void refreshBots(snapshot);
  };
  window.addEventListener("focus", onFocus);

  void actions
    .mapTemplates()
    .then((maps) => {
      templates = maps;
      render();
    })
    .catch(() => undefined);
  void actions
    .defaultBots()
    .then((list) => {
      if (disposed || list.length === 0) return;
      levels = list;
      render();
    })
    .catch(() => undefined);

  const el = h(
    "div",
    { class: "room" },
    h(
      "header",
      { class: "room-head panel" },
      h("div", { class: "room-title" }, title, settingsLine, changeButton),
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
      for (const unsubscribe of unsubscribers) unsubscribe();
      window.removeEventListener("focus", onFocus);
      picker.dispose();
      watchersPanel.dispose();
      closeChange();
    },
  };
}
