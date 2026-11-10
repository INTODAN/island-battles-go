// The game screen: the 3D board with a HUD, the timeline, the outcome banner and the bot logs
// panel. It shows whatever its owner feeds the `GameController`, so the live view and the demo
// view share it. Every value from the server or a file is written with `textContent`.

import { Engine } from "@babylonjs/core/Engines/engine";
import { Scene } from "@babylonjs/core/scene";
import type { ConnectionState } from "../../realtime";
import { createScenePlayer } from "../../scene/animation";
import { battlefieldOf } from "../../scene/battlefield";
import { loadBoardAssets } from "../../scene/boardAssets";
import { fitCamera } from "../../scene/camera";
import {
  applyGraphics,
  type Graphics,
  savedGraphics,
  saveGraphics,
} from "../../scene/graphics";
import { teamColorHex } from "../../scene/palette";
import { installBoardControls } from "../../scene/picking";
import { showBoardLoading } from "../boardLoading";
import { clashSummary } from "./clashes";
import { GameController, type ViewState } from "./controller";
import "./game.css";
import {
  type GameOutcome,
  type GameTeam,
  type Generation,
  sortTeams,
  teamStats,
  turnLabel,
} from "./history";
import { hiddenNotice, type LogBook, LogFetchScheduler } from "./logs";

export interface ScreenOptions {
  /** True when new turns can arrive: the live view. */
  live: boolean;
  /** True for a watcher outside the roster, who sees no team's logs until the end. */
  watching?: boolean;
  logs: LogBook;
  onBack: () => void;
  /** The label of the button that leaves the screen. Default "Back to lobby". */
  backLabel?: string;
  /** Called with a file the player chose with Open demo file. */
  onOpenFile: (file: File) => void;
  /** Saves the game so far as a demo file; rejects with the reason. Absent for an opened demo. */
  onSaveDemo?: () => Promise<void>;
  /** Called when the shown turn's logs are not held or hide teams that have since been revealed. */
  requestLogs?: (generationId: number, ended: boolean) => void;
}

/** `stopped`: a watcher the host removed, who receives no more turns. */
export type ConnectionLabel = ConnectionState | "saved" | "stopped";

const connectionText: Record<ConnectionLabel, string> = {
  connecting: "Connecting…",
  connected: "Live",
  disconnected: "Disconnected, reconnecting…",
  saved: "Saved demo",
  stopped: "Not receiving turns",
};

function element<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  className?: string,
  text?: string,
): HTMLElementTagNameMap[K] {
  const created = document.createElement(tag);
  if (className) created.className = className;
  if (text !== undefined) created.textContent = text;
  return created;
}

function button(label: string, text: string, className = "gv-button") {
  const created = element("button", className, text);
  created.type = "button";
  created.setAttribute("aria-label", label);
  return created;
}

function swatch(color: number): HTMLElement {
  const created = element("span", "gv-swatch");
  created.style.background = teamColorHex(color);
  created.setAttribute("aria-hidden", "true");
  return created;
}

/** The running board, created once the history is known. */
interface Running {
  controller: GameController;
  teams: GameTeam[];
  dispose: () => void;
}

export class GameScreen {
  private readonly root = element("div", "gv");
  private readonly canvas = element("canvas", "gv-canvas");
  private readonly title = element("span", "gv-title");
  private readonly connection = element("span", "gv-connection");
  private readonly connectionDot = element("span", "gv-dot");
  private readonly connectionLabel = element("span");
  private readonly turn = element("span", "gv-turn");
  private readonly notice = element("p", "gv-notice");
  private readonly summary = element("p", "gv-summary");
  private readonly saveError = element("p", "gv-notice");
  private readonly clashes = element("p", "gv-summary gv-clashes");
  private readonly overlay = element("div", "gv-overlay");
  private readonly overlayText = element("p");
  private readonly overlayAction = button("Try again", "Try again");
  private readonly teamsBox = element("div", "gv-teams");
  private readonly banner = element("div", "gv-outcome");
  private readonly bannerText = element("span");
  private readonly slider = element("input", "gv-slider");
  private readonly playButton = button("Play", "▶");
  private readonly backButton = button("Previous turn", "◀");
  private readonly forwardButton = button("Next turn", "▶|");
  private readonly follow = element("input");
  private readonly followLabel = element("label", "gv-follow");
  private readonly speed = element("select", "gv-speed");
  private readonly effects = element("input");
  private readonly effectsLabel = element("label", "gv-follow gv-effects");
  private readonly logsBox = element("details", "gv-logs");
  private readonly logsSummary = element("summary");
  private readonly logsBody = element("div", "gv-logs-body");
  private readonly logScheduler = new LogFetchScheduler((generationId, ended) =>
    this.options.requestLogs?.(generationId, ended),
  );
  private teamRows = new Map<
    number,
    { soldiers: HTMLElement; castles: HTMLElement }
  >();
  private running: Running | undefined;
  private disposed = false;
  private retry: (() => void) | undefined;

  constructor(
    private readonly container: HTMLElement,
    private readonly options: ScreenOptions,
  ) {
    const root = this.root;
    root.setAttribute("role", "region");
    root.setAttribute("aria-label", "Game");
    this.canvas.setAttribute("aria-label", "Island Battles battlefield");

    const top = element("header", "gv-top");
    const backLabel = this.options.backLabel ?? "Back to lobby";
    const back = button(backLabel, backLabel);
    back.addEventListener("click", () => this.options.onBack());
    const open = button("Open demo file", "Open demo file");
    const file = element("input");
    file.type = "file";
    file.accept = ".json,application/json";
    file.hidden = true;
    file.setAttribute("aria-label", "Demo file");
    open.addEventListener("click", () => file.click());
    file.addEventListener("change", () => {
      const chosen = file.files?.[0];
      // Choosing the same file again opens it again.
      file.value = "";
      if (chosen) this.options.onOpenFile(chosen);
    });
    const save = button("Save demo", "Save demo");
    save.hidden = !this.options.onSaveDemo;
    save.addEventListener("click", () => void this.saveDemo(save));
    this.connection.append(this.connectionDot, this.connectionLabel);
    this.connection.setAttribute("role", "status");
    this.turn.setAttribute("aria-live", "polite");
    top.append(back, this.title, this.turn, this.connection, open, save, file);

    this.notice.setAttribute("role", "alert");
    this.notice.hidden = true;
    this.summary.hidden = true;
    this.saveError.setAttribute("role", "alert");
    this.saveError.hidden = true;
    this.clashes.setAttribute("role", "status");
    this.clashes.hidden = true;

    this.banner.setAttribute("role", "status");
    this.banner.hidden = true;
    this.banner.append(this.bannerText);

    this.overlay.setAttribute("role", "status");
    this.overlayAction.addEventListener("click", () => this.retry?.());
    this.overlay.append(this.overlayText, this.overlayAction);
    this.overlay.hidden = true;

    const timeline = element("div", "gv-timeline");
    this.slider.type = "range";
    this.slider.min = "0";
    this.slider.max = "0";
    this.slider.value = "0";
    this.slider.setAttribute("aria-label", "Turn");
    this.follow.type = "checkbox";
    this.follow.setAttribute("aria-label", "Follow live");
    this.followLabel.append(this.follow, " Follow live");
    this.followLabel.hidden = !this.options.live;
    this.speed.setAttribute("aria-label", "Speed");
    for (const value of [0.5, 1, 2, 4]) {
      const choice = element("option", undefined, `${value}×`);
      choice.value = String(value);
      choice.selected = value === 1;
      this.speed.append(choice);
    }
    const buttons = element("div", "gv-buttons");
    buttons.append(this.backButton, this.playButton, this.forwardButton);
    this.effects.type = "checkbox";
    this.effects.setAttribute("aria-label", "Effects");
    this.effects.checked = savedGraphics() === "high";
    this.effectsLabel.title =
      "Post-processing, shadows and moving flags. Turn off on a slow device.";
    this.effectsLabel.append(this.effects, " Effects");
    timeline.append(
      buttons,
      this.slider,
      this.followLabel,
      this.speed,
      this.effectsLabel,
    );

    this.logsSummary.textContent = "Bot logs";
    this.logsBox.append(this.logsSummary, this.logsBody);
    // Logs are fetched only while the panel is open; opening it asks for the turn on screen.
    this.logsBox.addEventListener("toggle", () => this.refreshLogs());

    const topStack = element("div", "gv-stack gv-stack-top");
    topStack.append(
      top,
      this.notice,
      this.saveError,
      this.summary,
      this.teamsBox,
      this.banner,
      this.clashes,
    );
    const bottomStack = element("div", "gv-stack gv-stack-bottom");
    bottomStack.append(this.logsBox, timeline);
    root.append(this.canvas, topStack, bottomStack, this.overlay);
    this.container.replaceChildren(root);
    this.setConnection(this.options.live ? "connecting" : "saved");
  }

  setTitle(text: string): void {
    this.title.textContent = text;
  }

  setConnection(state: ConnectionLabel): void {
    this.connection.dataset.state = state;
    this.connectionLabel.textContent = connectionText[state];
  }

  /** A line under the top bar, such as what an opened demo is. */
  setSummary(text: string | null): void {
    this.summary.textContent = text ?? "";
    this.summary.hidden = !text;
  }

  /** A problem line that leaves the board as it is, such as a file that could not be opened. */
  setNotice(text: string | null): void {
    this.notice.textContent = text ?? "";
    this.notice.hidden = !text;
  }

  private async saveDemo(save: HTMLButtonElement): Promise<void> {
    save.disabled = true;
    this.saveError.hidden = true;
    try {
      await this.options.onSaveDemo?.();
    } catch (error) {
      this.saveError.textContent = `The demo could not be saved: ${error instanceof Error ? error.message : "the request failed."}`;
      this.saveError.hidden = false;
    } finally {
      save.disabled = false;
    }
  }

  showLoading(text: string): void {
    this.retry = undefined;
    this.overlayText.textContent = text;
    this.overlayAction.hidden = true;
    this.overlay.dataset.kind = "loading";
    this.overlay.hidden = false;
  }

  showFailure(text: string, retry?: () => void): void {
    this.retry = retry;
    this.overlayText.textContent = text;
    this.overlayAction.hidden = !retry;
    this.overlay.dataset.kind = "failure";
    this.overlay.hidden = false;
  }

  hideOverlay(): void {
    this.overlay.hidden = true;
  }

  /** Draws the board for a loaded history and returns the controller that drives it. */
  start(
    teams: readonly GameTeam[],
    frames: readonly Generation[],
    outcome: GameOutcome | null,
  ): GameController {
    this.stop();
    const sorted = sortTeams(teams);
    const engine = new Engine(this.canvas, true, {
      preserveDrawingBuffer: true,
    });
    const scene = new Scene(engine);
    installBoardControls(scene);
    // The board's art loads behind a loading screen; the board draws placeholders meanwhile.
    const loading = showBoardLoading(this.root);
    loadBoardAssets(scene, (progress) => loading.update(progress)).then(
      () => loading.done(),
      (error) => {
        console.error("the board's art failed to load", error);
        loading.failed();
      },
    );
    // A turn that ends asks for the next one at once, so playing on loses no time between turns.
    let onIdle = () => {};
    const { player } = createScenePlayer(scene, sorted, {
      onIdle: () => onIdle(),
    });
    const controller = new GameController(
      {
        play: (previous, next) => player.play(previous, next),
        seek: (generation) => player.seek(generation),
        get busy() {
          return player.busy;
        },
        setFollowing: (following) => player.setLive(following),
      },
      { live: this.options.live },
    );
    onIdle = () => controller.tick();
    scene.onBeforeRenderObservable.add(() => controller.tick());
    let aspect =
      engine.getRenderWidth() / Math.max(1, engine.getRenderHeight());
    const resize = new ResizeObserver(() => {
      engine.resize();
      // A very different shape (a phone turned around) needs the board fitted again.
      const field = battlefieldOf(scene);
      const now =
        engine.getRenderWidth() / Math.max(1, engine.getRenderHeight());
      if (field && Math.abs(now / aspect - 1) > 0.15) {
        aspect = now;
        fitCamera(scene, field.camera, field.bounds);
      }
    });
    resize.observe(this.container);

    this.buildTeams(sorted);
    const unsubscribe = controller.subscribe((state) => this.render(state));
    this.slider.oninput = () => controller.scrubTo(Number(this.slider.value));
    this.backButton.onclick = () => controller.step(-1);
    this.forwardButton.onclick = () => controller.step(1);
    this.playButton.onclick = () => controller.togglePlay();
    this.follow.onchange = () => controller.setFollow(this.follow.checked);
    this.speed.onchange = () => player.setSpeed(Number(this.speed.value));
    player.setSpeed(Number(this.speed.value));
    const setGraphics = (value: Graphics) => {
      const camera = scene.activeCamera;
      if (camera) void applyGraphics(scene, camera, value);
    };
    this.effects.onchange = () => {
      const value: Graphics = this.effects.checked ? "high" : "low";
      saveGraphics(value);
      setGraphics(value);
    };

    controller.load(frames, outcome);
    // The board's camera exists once the first turn is drawn.
    setGraphics(this.effects.checked ? "high" : "low");
    engine.runRenderLoop(() => scene.render());
    engine.resize();
    this.running = {
      controller,
      teams: sorted,
      dispose: () => {
        unsubscribe();
        resize.disconnect();
        engine.stopRenderLoop();
        scene.dispose();
        engine.dispose();
      },
    };
    this.render(controller.state);
    return controller;
  }

  /** Redraws the logs panel, for when logs arrive after the turn was shown. */
  refreshLogs(): void {
    const running = this.running;
    if (running) this.renderLogs(running.controller.state);
  }

  dispose(): void {
    if (this.disposed) return;
    this.disposed = true;
    this.logScheduler.cancel();
    this.stop();
    this.container.replaceChildren();
  }

  private stop(): void {
    this.running?.dispose();
    this.running = undefined;
  }

  private buildTeams(teams: readonly GameTeam[]): void {
    this.teamRows = new Map();
    const rows: HTMLElement[] = [];
    for (const team of teams) {
      const card = element("div", "gv-team");
      card.dataset.team = String(team.color);
      const name = element("div", "gv-team-name");
      name.append(swatch(team.color), element("strong", undefined, team.name));
      const players = element(
        "div",
        "gv-team-players",
        team.players.join(", "),
      );
      const soldiers = element("span", "gv-count");
      const castles = element("span", "gv-count");
      const counts = element("div", "gv-team-counts");
      counts.append(soldiers, castles);
      card.append(name, players, counts);
      this.teamRows.set(team.color, { soldiers, castles });
      rows.push(card);
    }
    this.teamsBox.replaceChildren(...rows);
  }

  private render(state: ViewState): void {
    const running = this.running;
    if (!running) return;
    const count = state.frames.length;
    const frame = state.frames[state.index];
    this.turn.textContent = frame ? turnLabel(state.index, count) : "";
    this.slider.max = String(Math.max(0, count - 1));
    this.slider.value = String(Math.max(0, state.index));
    this.slider.setAttribute("aria-valuetext", this.turn.textContent ?? "");
    this.backButton.disabled = state.index <= 0;
    this.forwardButton.disabled = state.index >= count - 1;
    this.playButton.textContent = state.playing || state.follow ? "⏸" : "▶";
    this.playButton.setAttribute(
      "aria-label",
      state.playing || state.follow ? "Pause" : "Play",
    );
    this.follow.checked = state.follow;
    this.follow.disabled = !state.live || state.outcome !== null;
    this.renderStats(running, frame);
    this.renderOutcome(running, state.outcome);
    this.renderClashes(frame);
    this.renderLogs(state);
  }

  private renderClashes(frame: Generation | undefined): void {
    const text = frame ? clashSummary(frame) : "";
    this.clashes.textContent = text;
    this.clashes.hidden = text === "";
  }

  private renderStats(running: Running, frame: Generation | undefined): void {
    if (!frame) return;
    for (const stats of teamStats(frame, running.teams)) {
      const row = this.teamRows.get(stats.team.color);
      if (!row) continue;
      row.soldiers.textContent = `${stats.soldiers} soldiers`;
      row.castles.textContent = `${stats.castles} ${stats.castles === 1 ? "castle" : "castles"}`;
      row.soldiers.parentElement?.parentElement?.classList.toggle(
        "gv-out",
        stats.castles === 0 && stats.soldiers === 0,
      );
    }
  }

  private renderOutcome(running: Running, outcome: GameOutcome | null): void {
    this.banner.hidden = outcome === null;
    if (!outcome) return;
    const winner = running.teams.find(
      (team) => team.color === outcome.winner_team_color,
    );
    const color = winner ? teamColorHex(winner.color) : "#8290a4";
    this.banner.style.setProperty("--winner", color);
    this.banner.dataset.result = winner ? "win" : "draw";
    this.bannerText.textContent = outcome.summary;
  }

  private renderLogs(state: ViewState): void {
    const running = this.running;
    const frame = state.frames[state.index];
    if (!running || !frame) return;
    const logs = this.options.logs.get(frame.id);
    const ended = state.outcome !== null;
    this.logsSummary.textContent = `Bot logs · ${turnLabel(state.index, state.frames.length)}`;
    this.logScheduler.update({
      open: this.logsBox.open,
      generationId: frame.id,
      ended,
      needsFetch: this.options.logs.needsFetch(frame.id, ended),
    });
    const parts: HTMLElement[] = [];
    const notice = hiddenNotice(logs, {
      ended,
      live: state.live,
      watching: this.options.watching === true,
    });
    if (notice) parts.push(element("p", "gv-logs-note", notice));
    if (!logs) {
      parts.push(element("p", "gv-logs-note", "Loading the logs…"));
    } else {
      for (const team of running.teams) {
        const players = logs.players.filter(
          (player) => player.team_color === team.color,
        );
        if (players.length === 0) continue;
        const section = element("section", "gv-logs-team");
        const heading = element("h3");
        heading.append(swatch(team.color), ` ${team.name}`);
        section.append(heading);
        for (const player of players) {
          const block = element("div", "gv-logs-player");
          block.append(element("h4", undefined, player.display_name));
          if (player.lines.length === 0) {
            block.append(element("p", "gv-logs-empty", "No lines this turn."));
          }
          for (const line of player.lines) {
            const row = element("p", "gv-logs-line");
            row.dataset.level = line.level;
            row.append(
              element("span", "gv-logs-level", line.level),
              element("span", "gv-logs-logger", line.logger),
              element("span", "gv-logs-message", line.message),
            );
            block.append(row);
          }
          section.append(block);
        }
        parts.push(section);
      }
      if (parts.length === 0) {
        parts.push(element("p", "gv-logs-note", "No logs for this turn."));
      }
    }
    this.logsBody.replaceChildren(...parts);
  }
}
