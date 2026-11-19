// The live game view: the history of a game, then each new turn as it arrives. A roster
// player and a watcher outside the roster see the same view, from different sources
// (`sources.ts`).
//
// Order matters. The view subscribes to the game's channel first (`source.follow`), then loads
// the history, then merges the live generations into it by `id`, ignoring ones it already
// holds. A turn that arrives while the history loads is kept and merged after it.

import { ApiError } from "../../api/errors";
import { describeGameBot, describeRelayedBot } from "../../bots/kinds";
import type { HandPlay } from "../../play";
import type { Realtime } from "../../realtime";
import type { GameController } from "./controller";
import { type Demo, DemoFileError, readDemoFile } from "./demoFile";
import { mergeGenerations } from "./history";
import { LogBook, openGenerationId } from "./logs";
import { saveTextFile } from "./saveDemo";
import { GameScreen } from "./screen";
import type { LiveSource, LiveTurn } from "./sources";

export interface LiveOptions {
  realtime: Realtime;
  source: LiveSource;
  /** The roster player's hand play, for the screen's controls; null for a watcher. */
  handPlay: HandPlay | null;
  onBack: () => void;
  /** Shows a demo file the player opened, with its file name. */
  onDemo: (demo: Demo, fileName: string) => void;
}

const RESYNC_RETRY_MS = 3000;

/** Why the history could not be read, in words for the player. */
export function loadFailureText(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.status === 404) {
      return "This game is not available. Games are deleted a while after their last turn, and a game that has not started has no history yet.";
    }
    if (error.status === 403) {
      return "Your guest session has no place in this game.";
    }
    if (error.status === 401) return "Your session has ended. Sign in again.";
    return `The game could not be loaded: ${error.message}`;
  }
  return "The game could not be loaded.";
}

/** Why a watcher's view stopped or could not load, in words for them. */
export function watchFailureText(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.status === 403) {
      return "The host removed you from this game's watchers.";
    }
    if (error.status === 404) {
      return "This game has not started yet, or it is gone. It appears here when its first turn is played.";
    }
  }
  return loadFailureText(error);
}

export function mountLive(
  container: HTMLElement,
  options: LiveOptions,
): { dispose(): void } {
  const { realtime, source } = options;
  const { watching } = source;
  const failureText = watching ? watchFailureText : loadFailureText;
  const logs = new LogBook();
  // Only a roster player with a hand play can resign; the screen shows the button by its status.
  const resign = options.handPlay ? source.resign?.bind(source) : undefined;
  const screen = new GameScreen(container, {
    live: true,
    watching,
    handPlay: options.handPlay,
    ...(resign ? { onResign: resign } : {}),
    logs,
    onBack: options.onBack,
    ...(source.backLabel === undefined ? {} : { backLabel: source.backLabel }),
    onOpenFile: (file) => void openFile(file),
    agentGame: () => {
      const code = source.lobbyCode();
      if (code) return { kind: "lobby", code };
      // Nothing was saved: the prompt asks the player for the lobby's code.
      return gameId === undefined ? null : { kind: "unknown-lobby", gameId };
    },
    onSaveDemo: async () => saveTextFile(await source.saveDemo()),
    requestLogs: (generationId, ended) => void fetchLogs(generationId, ended),
  });
  screen.setTitle(source.title);

  let disposed = false;
  let controller: GameController | undefined;
  let gameId: number | undefined;
  let teamColors: number[] = [];
  /** Turns that arrived before the history was loaded. */
  let pending: LiveTurn[] = [];
  let loading = false;
  let wasConnected = false;
  let resyncTimer: ReturnType<typeof setTimeout> | undefined;
  /** The host removed this watcher: no more turns arrive. */
  let stopped = false;
  const requested = new Set<string>();

  async function openFile(file: File): Promise<void> {
    try {
      const demo = await readDemoFile(file);
      if (!disposed) options.onDemo(demo, file.name);
    } catch (error) {
      screen.setNotice(
        `${error instanceof DemoFileError ? error.message : "The file could not be read."} The game shown before stays open.`,
      );
    }
  }

  async function fetchLogs(generationId: number, ended: boolean) {
    if (watching && !ended) {
      // A watcher sees no logs while the game runs: every team is hidden, so nothing is read.
      // Once it has ended, `/watch/logs` has every team's.
      if (
        logs.add({
          generation_id: generationId,
          players: [],
          hidden_team_colors: teamColors,
        })
      ) {
        // The panel asked while drawing: draw again once it has finished.
        queueMicrotask(() => {
          if (!disposed) screen.refreshLogs();
        });
      }
      return;
    }
    // The panel of a player never asks for the open turn (it shows the latest ended turn
    // instead), so this is a safeguard: logs read while their turn is open would be
    // provisional, read again once it ends.
    const state = controller?.state;
    const open =
      state !== undefined &&
      generationId === openGenerationId(state.frames, state.outcome !== null);
    const key = `${generationId}:${ended}:${open}`;
    if (requested.has(key)) return;
    requested.add(key);
    try {
      const data = await source.readLogs(generationId);
      if (disposed) return;
      if (logs.add(data, { provisional: open })) screen.refreshLogs();
    } catch {
      // Try again later, not at once: the panel asks again whenever the view redraws.
      setTimeout(() => requested.delete(key), RESYNC_RETRY_MS);
    }
  }

  /**
   * Names the bot that plays your seat under your team, from a roster player's turn, and each
   * localhost bot seat that this tab relays under the seat's team.
   */
  function showYourBot(turn: LiveTurn | null): void {
    if (turn?.bot && turn.team_color !== undefined) {
      const lines = new Map([[turn.team_color, describeGameBot(turn.bot)]]);
      for (const seat of turn.relayed_bots ?? []) {
        lines.set(seat.team_color, describeRelayedBot(seat));
      }
      screen.setYourBots(turn.team_color, lines);
    }
  }

  function installTurn(turn: LiveTurn): void {
    if (gameId === undefined) {
      pending.push(turn);
      return;
    }
    if (turn.game_id !== gameId) return;
    screen.setGamePace(turn.turn_pace_milliseconds ?? null);
    screen.setDepartures(turn.departures ?? []);
    controller?.merge([turn.generation], turn.outcome ?? null);
  }

  async function load(): Promise<void> {
    if (loading || disposed) return;
    loading = true;
    screen.showLoading("Loading the game so far…");
    try {
      const demo = await source.readHistory();
      if (disposed) return;
      // The history's newest turn may still be open: its logs are read again once it ends.
      logs.addAll(
        demo.logs,
        openGenerationId(demo.frames, Boolean(demo.outcome)),
      );
      const turns = [...pending];
      const held = source.current();
      if (held) turns.push(held);
      const live = turns.filter((turn) => turn.game_id === demo.game_id);
      const frames = mergeGenerations(
        demo.frames,
        live.map((turn) => turn.generation),
      );
      const outcome =
        demo.outcome ?? live.find((turn) => turn.outcome)?.outcome ?? null;
      pending = [];
      gameId = demo.game_id;
      teamColors = demo.teams.map((team) => team.color);
      screen.setTitle(
        watching ? `Watching game ${demo.game_id}` : `Game ${demo.game_id}`,
      );
      screen.setGamePace(live.at(-1)?.turn_pace_milliseconds ?? null);
      controller = screen.start(demo.teams, frames, outcome);
      screen.setDepartures(held?.departures ?? []);
      showYourBot(held);
      screen.hideOverlay();
    } catch (error) {
      if (disposed) return;
      if (watching && error instanceof ApiError && error.status === 404) {
        // Not started yet: the first turn on the channel loads it.
        screen.showLoading(watchFailureText(error));
        return;
      }
      screen.showFailure(failureText(error), () => void load());
    } finally {
      loading = false;
    }
  }

  /** After a reconnect the channel may have skipped turns: read the history again and merge it. */
  async function resync(): Promise<void> {
    clearTimeout(resyncTimer);
    try {
      const demo = await source.readHistory();
      if (disposed || !controller || demo.game_id !== gameId) return;
      logs.addAll(
        demo.logs,
        openGenerationId(demo.frames, Boolean(demo.outcome)),
      );
      controller.merge(demo.frames, demo.outcome);
      screen.refreshLogs();
    } catch {
      if (!disposed && realtime.connection.get() === "connected") {
        resyncTimer = setTimeout(() => void resync(), RESYNC_RETRY_MS);
      }
    }
  }

  if (!realtime.session.get()) {
    screen.showFailure("Sign in to watch the game.");
    return {
      dispose() {
        disposed = true;
        screen.dispose();
      },
    };
  }
  // 1. Subscribe to the game's channel first.
  source.follow();
  const stops = [
    source.subscribe((turn) => {
      if (!turn) return;
      installTurn(turn);
      showYourBot(turn);
      // A watcher may arrive before the game starts: its first turn loads the view.
      if (gameId === undefined && !loading) void load();
    }),
    realtime.connection.subscribe((state) => {
      screen.setConnection(stopped ? "stopped" : state);
      if (state === "connected") {
        if (controller && !wasConnected) void resync();
        wasConnected = true;
      } else {
        wasConnected = false;
        clearTimeout(resyncTimer);
      }
    }),
    realtime.problem.subscribe((problem) => {
      if (
        problem &&
        (problem.scope === source.problemScope || problem.scope === "session")
      ) {
        screen.setNotice(failureText(problem.error));
        if (problem.scope === "watch") {
          stopped = true;
          screen.setConnection("stopped");
        }
      }
    }),
  ];
  screen.setConnection(realtime.connection.get());
  wasConnected = realtime.connection.get() === "connected";
  // 2. Then load the history.
  void load();

  return {
    dispose() {
      if (disposed) return;
      disposed = true;
      clearTimeout(resyncTimer);
      for (const stop of stops) stop();
      source.leave();
      screen.dispose();
    },
  };
}
