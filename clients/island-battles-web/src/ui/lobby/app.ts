import { createApiClients } from "../../api/client";
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
 * view. Which screen shows is decided by the realtime stores alone, never by
 * what a button press hopes will happen.
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
  let game: { lobbyId: string; stop(): void } | undefined;
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

  function enterGame(lobbyId: string): void {
    demo?.stop();
    demo = undefined;
    setOcean(false);
    stage.hidden = true;
    gameHost.hidden = false;
    realtime.followGame(lobbyId);
    const relay: BotRelay = startBotRelay({ realtime, lobbyId });
    const banner = mountRelayBanner(relay);
    const { gameApi } = createApiClients({
      getAccessToken: () => realtime.session.get()?.accessToken,
    });
    const watcher = watchForGame({
      async probe() {
        if (realtime.game.get()) return;
        await gameApi.GET("/game/from-lobby/{lobby_id}/generation", {
          params: { path: { lobby_id: lobbyId } },
        });
      },
      onReady() {
        if (!realtime.game.get()) realtime.refreshGame();
      },
      onFatal(error) {
        gameFailed(describeProblem(error, "start").message);
      },
      schedule(run, ms) {
        const timer = setTimeout(run, ms);
        return () => clearTimeout(timer);
      },
    });
    const viewHost = h("div", { class: "game-view" });
    // The people watching with the join code; the host can kick one.
    const watchers = mountWatchersPanel({ realtime, actions, overlay: true });
    gameHost.replaceChildren(viewHost, banner.el, watchers.el);
    // The shell follows the game for the relay and ends it itself, so the view's own
    // `leaveGame()` (on dispose, or when it swaps to a demo file) must not stop the relay.
    const view = mountGameView(viewHost, {
      lobbyId,
      realtime: { ...realtime, leaveGame() {} },
      onBack: () => leaveGame(""),
    });
    const stopOnGame = realtime.game.subscribe((state) => {
      if (state) watcher.stop();
    });
    const stopOnProblem = realtime.problem.subscribe((problem) => {
      if (problem?.scope === "game") {
        gameFailed(describeProblem(problem.error, "start").message);
      }
    });
    game = {
      lobbyId,
      stop() {
        stopOnGame();
        stopOnProblem();
        watcher.stop();
        relay.dispose();
        banner.dispose();
        watchers.dispose();
        view.dispose();
        gameHost.replaceChildren();
        gameHost.hidden = true;
        stage.hidden = false;
      },
    };
  }

  function gameFailed(message: string): void {
    leaveGame(message);
  }

  /** Leaves the finished (or failed) game and goes back to the menu. */
  function leaveGame(message: string): void {
    game?.stop();
    game = undefined;
    // Leave the membership before the lobby store is emptied: it names the lobby.
    void actions.leave().catch(() => undefined);
    realtime.leaveGame();
    realtime.leaveLobby();
    forgetRoom(message);
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
    const view = mountWatchView(viewHost, {
      lobbyId,
      channel,
      realtime,
      onBack: () => stopWatch(),
    });
    watching = {
      lobbyId,
      stop() {
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
    if (demo && !(lobby && lobby.lobby.phase === "started")) return;
    if (lobby && lobby.lobby.phase === "started") {
      if (game?.lobbyId !== lobby.lobby.id) {
        screen?.screen.dispose();
        screen = undefined;
        stage.replaceChildren();
        enterGame(lobby.lobby.id);
      }
      return;
    }
    if (game) {
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
      show(`room:${lobby.lobby.id}`, () =>
        mountRoomScreen({
          realtime,
          actions,
          onSignedOut: () => realtime.signOut(),
          onLeft(message) {
            forgetRoom(message);
            route();
          },
        }),
      );
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
