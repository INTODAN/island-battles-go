import { createApiClients } from "../../api/client";
import { ApiError } from "../../api/errors";
import { type BotRelay, startBotRelay } from "../../bots";
import { tabStorage } from "../../platform";
import type { Realtime } from "../../realtime";
import { h } from "../dom";
import { mountDemoView, mountGameView, mountWatchView } from "../game/index";
import { createLobbyActions } from "./actions";
import { releaseLease, startHeartbeat } from "./lease";
import { mountMenuScreen } from "./menuScreen";
import { mountNameScreen, type Screen } from "./nameScreen";
import { type Ocean, startOcean } from "./ocean";
import { describeProblem } from "./problems";
import { mountRelayBanner } from "./relayBanner";
import { mountRoomScreen } from "./roomScreen";
import { backToLobby, routeRound } from "./rounds";
import { watchForGame } from "./startRetry";
import { mountWatchersPanel } from "./watchersPanel";

const roomKey = "island-battles-web.room";
const watchKey = "island-battles-web.watch";

interface SavedWatch {
  lobbyId: string;
  channel: string;
  joinCode: string;
}
const nameKey = "island-battles-web.name";

interface SavedRoom {
  gameKey: string;
  lobbyId: string;
  joinCode: string;
}

function read(storage: () => Storage, key: string): string | null {
  try {
    return storage().getItem(key);
  } catch {
    return null;
  }
}

function write(
  storage: () => Storage,
  key: string,
  value: string | null,
): void {
  try {
    if (value === null) storage().removeItem(key);
    else storage().setItem(key, value);
  } catch {
    // Storage may be blocked; the app then forgets this between reloads.
  }
}

function savedRoom(): SavedRoom | undefined {
  try {
    const value = JSON.parse(read(tabStorage, roomKey) ?? "null");
    if (
      value &&
      typeof value.gameKey === "string" &&
      typeof value.lobbyId === "string" &&
      typeof value.joinCode === "string"
    ) {
      return value as SavedRoom;
    }
  } catch {
    // Treated as no saved room.
  }
  return undefined;
}

function savedWatch(): SavedWatch | undefined {
  try {
    const value = JSON.parse(read(tabStorage, watchKey) ?? "null");
    if (
      value &&
      typeof value.lobbyId === "string" &&
      typeof value.channel === "string" &&
      typeof value.joinCode === "string"
    ) {
      return value as SavedWatch;
    }
  } catch {
    // Treated as not watching.
  }
  return undefined;
}

export interface LobbyApp {
  dispose(): void;
}

/**
 * The web client's own flow: name, create or join, lobby room, then the game
 * view, and back to the room for the lobby's next game. Which screen shows is
 * decided by the realtime stores and the round whose game view the player
 * closed (`routeRound`), never by what a button press hopes will happen.
 */
export function mountLobbyApp(options: {
  root: HTMLElement;
  canvas: HTMLCanvasElement;
  realtime: Realtime;
}): LobbyApp {
  const { root, canvas, realtime } = options;
  // One id per tab and page load. A lease the page released on its way out cannot be
  // reused, so a reloaded tab joins again under a new id.
  const clientInstanceId = crypto.randomUUID();
  const actions = createLobbyActions({ realtime, clientInstanceId });

  const params = new URLSearchParams(location.search);
  let pendingJoin = params.get("join")?.trim() || undefined;
  let pendingWatch = params.get("watch")?.trim() || undefined;
  if (params.has("join") || params.has("watch")) {
    params.delete("join");
    params.delete("watch");
    const query = params.toString();
    history.replaceState(
      null,
      "",
      `${location.pathname}${query ? `?${query}` : ""}${location.hash}`,
    );
  }

  let notice = "";
  let resuming = false;
  let disposed = false;
  let ocean: Ocean | undefined;
  let screen: { key: string; screen: Screen } | undefined;
  let game:
    | {
        lobbyId: string;
        round: number;
        /** Shows or hides the note that the lobby is open again. */
        setReopened(reopened: boolean): void;
        /**
         * Hides the game behind the read-only room, or shows it again. The game,
         * the relay of a localhost bot and the view go on while it is hidden.
         */
        setHidden(hidden: boolean): void;
        stop(): void;
      }
    | undefined;
  /** The last round whose game view the player closed, in which lobby. */
  let closed: { lobbyId: string; round: number } | undefined;
  /** Shown in the room: why the game view closed, or why reopening failed. */
  let roomNotice = "";
  let room: ReturnType<typeof mountRoomScreen> | undefined;
  let watching: { lobbyId: string; stop(): void } | undefined;
  let heartbeat: { lobbyId: string; stop(): void } | undefined;

  const stage = h("div", { class: "overlay" });
  const gameHost = h("div", { class: "game-host", hidden: true });
  root.replaceChildren(stage, gameHost);

  // A demo file opened from the name or create-or-join screen, shown over the screens.
  const demoInput = h("input", {
    type: "file",
    accept: ".json,application/json",
    hidden: true,
  });
  demoInput.setAttribute("aria-label", "Demo file");
  const demoLink = h(
    "button",
    { type: "button", class: "demo-link", hidden: true },
    "Open demo file",
  );
  demoLink.addEventListener("click", () => demoInput.click());
  let demo: { stop(): void } | undefined;

  function closeDemo(): void {
    if (!demo) return;
    demo.stop();
    demo = undefined;
    demoLink.hidden = !(screen?.key === "name" || screen?.key === "menu");
    route();
  }

  demoInput.addEventListener("change", () => {
    const file = demoInput.files?.[0];
    demoInput.value = "";
    if (!file || game || demo) return;
    setOcean(false);
    stage.hidden = true;
    demoLink.hidden = true;
    gameHost.hidden = false;
    const viewHost = h("div", { class: "game-view" });
    gameHost.replaceChildren(viewHost);
    const view = mountDemoView(viewHost, { file, onBack: closeDemo });
    demo = {
      stop() {
        view.dispose();
        gameHost.replaceChildren();
        gameHost.hidden = true;
        stage.hidden = false;
      },
    };
  });
  root.append(demoLink, demoInput);

  function setOcean(on: boolean): void {
    if (on && !ocean) {
      canvas.hidden = false;
      ocean = startOcean(canvas);
    } else if (!on && ocean) {
      ocean.dispose();
      ocean = undefined;
      canvas.hidden = true;
    }
  }

  function forgetRoom(message: string): void {
    write(tabStorage, roomKey, null);
    notice = message;
  }

  // --- Lease ---

  function syncHeartbeat(): void {
    const lobbyId = realtime.lobby.get()?.lobby.id;
    if (heartbeat && heartbeat.lobbyId !== lobbyId) {
      heartbeat.stop();
      heartbeat = undefined;
    }
    if (!heartbeat && lobbyId) {
      const beat = startHeartbeat({
        heartbeat: () => actions.heartbeat(),
        onLost(error) {
          const shown = describeProblem(error, "ready");
          if (shown.signedOut) return;
          realtime.leaveLobby();
          forgetRoom(shown.message);
          route();
        },
        schedule(run, ms) {
          const timer = setTimeout(run, ms);
          return () => clearTimeout(timer);
        },
      });
      heartbeat = { lobbyId, stop: beat.stop };
    }
  }

  function onPageHide(): void {
    const snapshot = realtime.lobby.get();
    const session = realtime.session.get();
    if (!snapshot || !session) return;
    releaseLease({
      gameKey: snapshot.lobby.game_key,
      lobbyId: snapshot.lobby.id,
      clientInstanceId,
      membershipGeneration: snapshot.membership_generation,
      accessToken: session.accessToken,
    });
  }
  window.addEventListener("pagehide", onPageHide);

  // --- Game ---

  /**
   * Shows the game of a lobby round. The view mounts once the round's game is
   * there: until the host's start has made it, the lobby's newest game is the
   * previous round's, which this view must not show.
   */
  function enterGame(lobbyId: string, round: number): void {
    demo?.stop();
    demo = undefined;
    setOcean(false);
    stage.hidden = true;
    gameHost.hidden = false;
    realtime.followGame(lobbyId, round);
    const relay: BotRelay = startBotRelay({ realtime, lobbyId });
    const banner = mountRelayBanner(relay);
    const { gameApi } = createApiClients({
      getAccessToken: () => realtime.session.get()?.accessToken,
    });
    const placeholderBack = h(
      "button",
      { type: "button", class: "secondary" },
      "Back to lobby",
    );
    placeholderBack.addEventListener("click", () => closeGame(lobbyId, round));
    const viewHost = h(
      "div",
      { class: "game-view" },
      h(
        "div",
        { class: "game-placeholder", attrs: { role: "status" } },
        h("p", {}, "Starting the game…"),
        placeholderBack,
      ),
    );
    let view: { dispose(): void } | undefined;
    const showView = () => {
      if (view) return;
      viewHost.replaceChildren();
      // The shell follows the game for the relay and ends it itself, so the view's own
      // `leaveGame()` (on dispose, or when it swaps to a demo file) must not stop the relay.
      view = mountGameView(viewHost, {
        lobbyId,
        round,
        realtime: { ...realtime, leaveGame() {} },
        onBack: () => closeGame(lobbyId, round),
      });
    };
    const watcher = watchForGame({
      async probe() {
        if (realtime.game.get()) return;
        const { data } = await gameApi.GET(
          "/game/from-lobby/{lobby_id}/generation",
          { params: { path: { lobby_id: lobbyId } } },
        );
        // The newest game is still the previous round's: wait for this round's.
        if (data?.lobby_round !== round) {
          throw new ApiError({
            kind: "problem",
            status: 404,
            detail: "This round's game isn't there yet.",
          });
        }
      },
      // A start whose game failed (502 game_unavailable) leaves the lobby
      // started without a game; the host's screen starts it again.
      async restart() {
        const snapshot = realtime.lobby.get();
        if (
          snapshot?.lobby.id !== lobbyId ||
          snapshot.lobby.round !== round ||
          snapshot.lobby.phase !== "started" ||
          snapshot.lobby.host_player_id !== realtime.session.get()?.playerId
        ) {
          return;
        }
        await actions.start();
      },
      onReady() {
        if (!realtime.game.get()) realtime.refreshGame();
        showView();
      },
      onFatal(error) {
        gameFailed(lobbyId, round, describeProblem(error, "start").message);
      },
      schedule(run, ms) {
        const timer = setTimeout(run, ms);
        return () => clearTimeout(timer);
      },
    });
    // Over the result, once another player reopened the lobby.
    const reopenedNote = h(
      "p",
      { class: "lobby-open-note", hidden: true, attrs: { role: "status" } },
      "The lobby is open again. Back to lobby takes you to it.",
    );
    // The people watching with the join code; the host can kick one.
    const watchers = mountWatchersPanel({ realtime, actions, overlay: true });
    gameHost.replaceChildren(viewHost, reopenedNote, banner.el, watchers.el);
    const stopOnGame = realtime.game.subscribe((state) => {
      if (!state) return;
      watcher.stop();
      showView();
    });
    const stopOnProblem = realtime.problem.subscribe((problem) => {
      if (problem?.scope === "game") {
        gameFailed(
          lobbyId,
          round,
          describeProblem(problem.error, "start").message,
        );
      }
    });
    game = {
      lobbyId,
      round,
      setReopened(reopened) {
        reopenedNote.hidden = !reopened;
      },
      setHidden(hidden) {
        gameHost.hidden = hidden;
        stage.hidden = !hidden;
        if (!hidden) setOcean(false);
      },
      stop() {
        stopOnGame();
        stopOnProblem();
        watcher.stop();
        relay.dispose();
        banner.dispose();
        watchers.dispose();
        view?.dispose();
        realtime.leaveGame();
        gameHost.replaceChildren();
        gameHost.hidden = true;
        stage.hidden = false;
      },
    };
  }

  /**
   * "Back to lobby" in the game view. The player stays in the lobby: once the
   * game has ended, the lobby is reopened for the next game; while it runs, the
   * room shows read-only with "Return to game", and the game goes on.
   */
  function closeGame(lobbyId: string, round: number): void {
    const state = realtime.game.get();
    closed = { lobbyId, round };
    roomNotice = "";
    if (
      backToLobby(state ? (state.outcome ? "ended" : "running") : "unknown") ===
      "reopen"
    ) {
      void actions.reopen().catch((cause) => {
        const shown = describeProblem(cause, "reopen");
        // Another player started the next game meanwhile: routing shows it.
        if (cause instanceof ApiError && cause.code === "game_running") return;
        if (shown.signedOut) {
          realtime.signOut();
          return;
        }
        if (shown.lostMembership) {
          realtime.leaveLobby();
          forgetRoom(shown.message);
        } else {
          roomNotice = shown.message;
          room?.showNotice(shown.message);
        }
        route();
      });
    }
    route();
  }

  /** "Return to game" in the read-only room. */
  function returnToGame(): void {
    closed = undefined;
    roomNotice = "";
    route();
  }

  /** The game of a round can't be shown: back to the room, with the reason. */
  function gameFailed(lobbyId: string, round: number, message: string): void {
    closed = { lobbyId, round };
    roomNotice = message;
    room?.showNotice(message);
    route();
  }

  // --- Watching a game from outside the roster ---

  /**
   * Watches the game of a join code: the server adds a watcher row and returns the watch
   * channel. A member of that lobby gets their own lobby instead.
   */
  async function startWatch(joinCode: string): Promise<void> {
    const access = await actions.watch(joinCode);
    if (access.member) {
      realtime.followLobby({
        gameKey: access.game_key,
        lobbyId: access.lobby_id,
      });
      return;
    }
    enterWatch(access.lobby_id, access.channel, joinCode);
  }

  function enterWatch(lobbyId: string, channel: string, joinCode: string) {
    demo?.stop();
    demo = undefined;
    screen?.screen.dispose();
    screen = undefined;
    stage.replaceChildren();
    setOcean(false);
    stage.hidden = true;
    demoLink.hidden = true;
    gameHost.hidden = false;
    write(
      tabStorage,
      watchKey,
      JSON.stringify({ lobbyId, channel, joinCode } satisfies SavedWatch),
    );
    const viewHost = h("div", { class: "game-view" });
    gameHost.replaceChildren(viewHost);
    // No lease, heartbeat, relay or deadline call: a watcher only reads.
    const mount = () =>
      mountWatchView(viewHost, {
        lobbyId,
        channel,
        realtime,
        onBack: () => stopWatch(),
      });
    let view = mount();
    // The lobby plays one game per round. A watcher follows the lobby, so once
    // its next game starts, the view shows that game from its beginning.
    let shownRound: number | undefined;
    const stopOnRound = realtime.watch.subscribe((state) => {
      if (!state) return;
      if (shownRound === undefined || state.lobby_round <= shownRound) {
        shownRound ??= state.lobby_round;
        return;
      }
      shownRound = state.lobby_round;
      queueMicrotask(() => {
        if (watching?.lobbyId !== lobbyId) return;
        view.dispose();
        view = mount();
      });
    });
    watching = {
      lobbyId,
      stop() {
        stopOnRound();
        view.dispose();
        gameHost.replaceChildren();
        gameHost.hidden = true;
        stage.hidden = false;
      },
    };
  }

  /** Stops watching and goes back to the menu. */
  function stopWatch(): void {
    const current = watching;
    if (!current) return;
    watching = undefined;
    current.stop();
    realtime.leaveWatch();
    void actions.unwatch(current.lobbyId).catch(() => undefined);
    write(tabStorage, watchKey, null);
    route();
  }

  // --- Routing from the stores ---

  function show(key: string, make: () => Screen): void {
    if (screen?.key === key) return;
    screen?.screen.dispose();
    room = undefined;
    const next = make();
    screen = { key, screen: next };
    demoLink.hidden = !(key === "name" || key === "menu");
    stage.replaceChildren(next.el);
    stage.scrollTop = 0;
  }

  function route(): void {
    if (disposed) return;
    syncHeartbeat();
    const session = realtime.session.get();
    const lobby = realtime.lobby.get();
    const problem = realtime.problem.get();
    if (!session) {
      game?.stop();
      game = undefined;
      if (watching) {
        watching.stop();
        watching = undefined;
        realtime.leaveWatch();
      }
      setOcean(true);
      if (problem?.scope === "session")
        notice = describeProblem(problem.error, "ready").message;
      show("name", () => {
        const message = notice;
        notice = "";
        return mountNameScreen({
          joinCode: pendingJoin,
          watchCode: pendingWatch,
          initialName: read(() => localStorage, nameKey) ?? "",
          notice: message,
          async onSubmit(name) {
            try {
              await realtime.signIn(name);
            } catch (cause) {
              throw new Error(describeProblem(cause, "create").message);
            }
            write(() => localStorage, nameKey, name);
          },
        });
      });
      return;
    }
    // A watcher stays on the watched game until they stop watching.
    if (watching) return;
    const lobbyId = lobby?.lobby.id;
    const decision = lobby
      ? routeRound({
          phase: lobby.lobby.phase,
          round: lobby.lobby.round,
          closedRound:
            closed && closed.lobbyId === lobbyId ? closed.round : null,
          viewingRound: game && game.lobbyId === lobbyId ? game.round : null,
        })
      : undefined;
    if (demo && decision?.view !== "game") return;
    if (lobbyId && decision?.view === "game") {
      if (game?.lobbyId !== lobbyId || game.round !== decision.round) {
        game?.stop();
        game = undefined;
        screen?.screen.dispose();
        screen = undefined;
        room = undefined;
        stage.replaceChildren();
        enterGame(lobbyId, decision.round);
      }
      game?.setHidden(false);
      game?.setReopened(decision.reopened);
      return;
    }
    // The read-only room of a running game keeps the game going behind it.
    if (
      game &&
      decision?.view === "room" &&
      decision.readOnly &&
      game.lobbyId === lobbyId &&
      game.round === lobby?.lobby.round
    ) {
      game.setHidden(true);
    } else if (game) {
      game.stop();
      game = undefined;
    }
    setOcean(true);
    if (resuming) {
      show("resuming", () => ({
        el: h(
          "div",
          { class: "panel narrow" },
          h("h1", {}, "Island Battles"),
          h("p", { class: "lead" }, "Rejoining your lobby…"),
        ),
        dispose() {},
      }));
      return;
    }
    if (lobby) {
      pendingJoin = undefined;
      write(
        tabStorage,
        roomKey,
        JSON.stringify({
          gameKey: lobby.lobby.game_key,
          lobbyId: lobby.lobby.id,
          joinCode: lobby.lobby.join_code,
        } satisfies SavedRoom),
      );
      show(`room:${lobby.lobby.id}`, () => {
        const message = roomNotice;
        roomNotice = "";
        room = mountRoomScreen({
          realtime,
          actions,
          notice: message,
          onReturnToGame: returnToGame,
          onSignedOut: () => realtime.signOut(),
          onLeft(left) {
            forgetRoom(left);
            route();
          },
        });
        return room;
      });
      return;
    }
    if (problem?.scope === "lobby") {
      notice = describeProblem(problem.error, "ready").message;
      write(tabStorage, roomKey, null);
    }
    show("menu", () => {
      const message = notice;
      notice = "";
      const joinCode = pendingJoin;
      pendingJoin = undefined;
      const watchCode = pendingWatch;
      pendingWatch = undefined;
      return mountMenuScreen({
        playerName: session.displayName,
        actions,
        joinCode,
        watchCode,
        onWatch: startWatch,
        notice: message,
        onSignedOut: () => realtime.signOut(),
        onChangeName: () => realtime.signOut(),
        newId: () => crypto.randomUUID(),
      });
    });
  }

  const unsubscribers = [
    realtime.session.subscribe(route),
    realtime.lobby.subscribe(route),
    realtime.problem.subscribe(route),
  ];

  // A tab that reloaded keeps its session and lobby: join again with the same
  // instance id to take the lease back (the page released it on the way out).
  const saved = savedRoom();
  if (realtime.session.get() && saved && !pendingJoin) {
    resuming = true;
    void actions
      .join(saved.joinCode)
      .catch(() => {
        realtime.followLobby({
          gameKey: saved.gameKey,
          lobbyId: saved.lobbyId,
        });
      })
      .finally(() => {
        resuming = false;
        route();
      });
  }
  // A tab that reloaded while watching watches again: the code finds its watcher row.
  const watched = savedWatch();
  if (
    realtime.session.get() &&
    watched &&
    !saved &&
    !pendingJoin &&
    !pendingWatch
  ) {
    pendingWatch = watched.joinCode;
  }
  setOcean(true);
  route();

  return {
    dispose() {
      disposed = true;
      for (const unsubscribe of unsubscribers) unsubscribe();
      window.removeEventListener("pagehide", onPageHide);
      heartbeat?.stop();
      game?.stop();
      demo?.stop();
      watching?.stop();
      screen?.screen.dispose();
      setOcean(false);
    },
  };
}
