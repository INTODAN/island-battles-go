import { createApiClients } from "../../api/client";
import { type BotRelay, startBotRelay } from "../../bots";
import type { Realtime } from "../../realtime";
import { h } from "../dom";
import { mountDemoView, mountGameView } from "../game/index";
import { createLobbyActions } from "./actions";
import { releaseLease, startHeartbeat } from "./lease";
import { mountMenuScreen } from "./menuScreen";
import { mountNameScreen, type Screen } from "./nameScreen";
import { type Ocean, startOcean } from "./ocean";
import { describeProblem } from "./problems";
import { mountRelayBanner } from "./relayBanner";
import { mountRoomScreen } from "./roomScreen";
import { watchForGame } from "./startRetry";

const roomKey = "island-battles-web.room";
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
    const value = JSON.parse(read(() => sessionStorage, roomKey) ?? "null");
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
  if (params.has("join")) {
    params.delete("join");
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
    write(() => sessionStorage, roomKey, null);
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
    gameHost.replaceChildren(viewHost, banner.el);
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
      setOcean(true);
      if (problem?.scope === "session")
        notice = describeProblem(problem.error, "ready").message;
      show("name", () => {
        const message = notice;
        notice = "";
        return mountNameScreen({
          joinCode: pendingJoin,
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
        () => sessionStorage,
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
      write(() => sessionStorage, roomKey, null);
    }
    show("menu", () => {
      const message = notice;
      notice = "";
      const joinCode = pendingJoin;
      pendingJoin = undefined;
      return mountMenuScreen({
        playerName: session.displayName,
        actions,
        joinCode,
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
      screen?.screen.dispose();
      setOcean(false);
    },
  };
}
