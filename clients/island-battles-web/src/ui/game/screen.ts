// The game screen: the 3D board with a HUD, the timeline, the outcome banner and the bot logs
// panel. It shows whatever its owner feeds the `GameController`, so the live view and the demo
// view share it. Every value from the server or a file is written with `textContent`.

import { Engine } from "@babylonjs/core/Engines/engine";
import { Scene } from "@babylonjs/core/scene";
import { gameBaseUrl, lobbyBaseUrl } from "../../platform";
import type { HandPlay } from "../../play";
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
import { fitBoard, installBoardControls } from "../../scene/picking";
import { showBoardLoading } from "../boardLoading";
import { rulesButton } from "../rules/rulesDialog";
import { type AgentGame, agentPrompt, copyText } from "./agentPrompt";
import { clashSummary } from "./clashes";
import { GameController, showsNewestTurn, type ViewState } from "./controller";
import "./game.css";
import {
  type Departure,
  departureText,
  type GameOutcome,
  type GameTeam,
  type Generation,
  mergeDepartures,
  sortTeams,
  teamStats,
  teamsDepartedAt,
  turnLabel,
} from "./history";
import {
  chooseLogs,
  hiddenNotice,
  type LogBook,
  LogFetchScheduler,
  openGenerationId,
} from "./logs";
import {
  confirmationKey,
  type ResignResult,
  resignControl,
  resignQuestion,
} from "./resign";

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
  /**
   * The game that "Ask your AI agent" names: a lobby code, or a demo file. Read when the button
   * is pressed; null when it isn't known yet.
   */
  agentGame?: () => AgentGame | null;
  /** Saves the game so far as a demo file; rejects with the reason. Absent for an opened demo. */
  onSaveDemo?: () => Promise<void>;
  /** Called when the shown turn's logs are not held or hide teams that have since been revealed. */
  requestLogs?: (generationId: number, ended: boolean) => void;
  /**
   * A roster player's hand play (`src/play/`), which the app shell owns: the screen never
   * disposes it. Null or absent for a watcher and a demo. A player whose bot plays has one whose
   * status isn't `playing`, so its controls stay off.
   */
  handPlay?: HandPlay | null;
  /**
   * Resigns the player's team (`POST .../resign`, retried, its answer installed). Absent for a
   * watcher and a demo, which show no Resign button.
   */
  onResign?: () => Promise<ResignResult>;
}

/**
 * What the hand player's controls on the board build on: drag to send (#343), the castle menu
 * (#344) and the hand player's view (#345). `GameScreen.hand` holds it while a game is started
 * with a hand play.
 */
export interface HandSurface {
  readonly handPlay: HandPlay;
  /** Whether the view shows the newest turn of the running game (`showsNewestTurn`): orders are given only then. */
  showsNewestTurn(): boolean;
  /** Hears every change of the view (the turn shown, Follow live, the outcome); returns the unsubscribe. */
  subscribeView(listener: (state: ViewState) => void): () => void;
  /** The overlay's bottom stack: the logs panel, then the timeline. */
  readonly bottomStack: HTMLElement;
  /** The notice line under the top bar (`GameScreen.setNotice`); null clears it. */
  setNotice(text: string | null): void;
  /**
   * Shows a sentence about one of the player's orders for 5 s, on its own line under the notice.
   * `orderId` is the PlayOrder's id: one order is shown once. Callers never clear the line.
   */
  showOrderFeedback(sentence: string, orderId?: number): void;
}

/** How long `showOrderFeedback` shows a sentence. */
const ORDER_FEEDBACK_MS = 5000;

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
  /** Feedback on the hand player's orders (`HandSurface.showOrderFeedback`). */
  private readonly orderNotice = element("p", "gv-notice gv-order-notice");
  private orderNoticeTimer: ReturnType<typeof setTimeout> | undefined;
  /** A hand player's status row above the timeline, such as "Return to live to give orders". */
  private readonly handStatus = element("div", "gv-hand-status");
  private readonly handStatusText = element(
    "span",
    "gv-hand-status-text",
    "Return to live to give orders",
  );
  private readonly summary = element("p", "gv-summary");
  /** The Resign button, or "You resigned" in its place (`resign.ts` says which). */
  private readonly resignButton = button(
    "Resign",
    "Resign",
    "gv-button gv-resign",
  );
  private readonly resigned = element("span", "gv-resigned", "You resigned");
  private readonly confirmBackdrop = element("div", "gv-confirm-backdrop");
  private readonly confirmBox = element("div", "gv-confirm");
  private readonly confirmResign = button(
    "Resign",
    "Resign",
    "gv-button gv-danger",
  );
  private readonly confirmKeep = button("Keep playing", "Keep playing");
  /** The top bar's first button, where the focus goes when the Resign button can't have it. */
  private backToLobby: HTMLButtonElement | undefined;
  private resignSending = false;
  private resignDone = false;
  /** What had the focus when the confirmation opened, to give it back when it closes. */
  private confirmReturnFocus: HTMLElement | null = null;
  private confirmKeyListener: ((event: KeyboardEvent) => void) | undefined;
  /** The teams out so far, from the live state (the outcome lists them all at the end). */
  private departures: readonly Departure[] = [];
  private readonly saveError = element("p", "gv-notice");
  private readonly askStatus = element("p", "gv-summary gv-ask-status");
  private readonly askText = element("textarea", "gv-ask-text");
  private askTimer: ReturnType<typeof setTimeout> | undefined;
  private readonly clashes = element("p", "gv-summary gv-clashes");
  private readonly overlay = element("div", "gv-overlay");
  private readonly overlayText = element("p");
  private readonly overlayAction = button("Try again", "Try again");
  private readonly fitButton = element("button", "gv-fit");
  private readonly teamsBox = element("div", "gv-teams");
  /** Your team's colour, from your seat's turn; a watcher has none. */
  private yourTeamColor: number | undefined;
  /** The line under each team whose bot you play or relay, by team colour. */
  private yourBots = new Map<number, string>();
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
    { soldiers: HTMLElement; castles: HTMLElement; status: HTMLElement }
  >();
  private readonly bottomStack = element("div", "gv-stack gv-stack-bottom");
  private running: Running | undefined;
  private scene: Scene | undefined;
  /** What `hand` returns. */
  private handSurface: HandSurface | null = null;
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
    this.backToLobby = back;
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
    const ask = button("Ask your AI agent", "Ask your AI agent");
    ask.title =
      "Copies a prompt for your AI agent: how to start the island-battles MCP server, this game and the turn shown";
    ask.addEventListener("click", () => void this.askAgent());
    this.connection.append(this.connectionDot, this.connectionLabel);
    this.connection.setAttribute("role", "status");
    this.turn.setAttribute("aria-live", "polite");
    top.append(
      back,
      this.title,
      this.turn,
      this.connection,
      open,
      save,
      ask,
      rulesButton("gv-button"),
      this.resignButton,
      this.resigned,
      file,
    );

    this.notice.setAttribute("role", "alert");
    this.notice.hidden = true;
    this.resignButton.hidden = true;
    this.resigned.hidden = true;
    this.resigned.setAttribute("role", "status");
    this.resignButton.addEventListener("click", () => this.openConfirmation());
    this.confirmBackdrop.hidden = true;
    this.confirmBox.setAttribute("role", "alertdialog");
    this.confirmBox.setAttribute("aria-modal", "true");
    this.confirmBox.setAttribute("aria-labelledby", "gv-confirm-question");
    const question = element("p", "gv-confirm-question", resignQuestion);
    question.id = "gv-confirm-question";
    const actions = element("div", "gv-confirm-actions");
    actions.append(this.confirmResign, this.confirmKeep);
    this.confirmBox.append(question, actions);
    this.confirmBackdrop.append(this.confirmBox);
    this.confirmResign.addEventListener("click", () => void this.resign());
    this.confirmKeep.addEventListener("click", () => this.closeConfirmation());
    // A click outside the box keeps playing too.
    this.confirmBackdrop.addEventListener("click", (event) => {
      if (event.target === this.confirmBackdrop) this.closeConfirmation();
    });
    this.confirmBox.addEventListener("keydown", (event) => {
      // Tab stays inside the dialog.
      if (event.key !== "Tab") return;
      const first = this.confirmResign;
      const last = this.confirmKeep;
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    });
    this.orderNotice.setAttribute("role", "alert");
    this.orderNotice.hidden = true;
    this.handStatusText.setAttribute("role", "status");
    this.handStatus.append(this.handStatusText);
    this.handStatus.hidden = true;
    this.summary.hidden = true;
    this.saveError.setAttribute("role", "alert");
    this.saveError.hidden = true;
    this.askStatus.setAttribute("role", "status");
    this.askStatus.hidden = true;
    this.askText.readOnly = true;
    this.askText.rows = 8;
    this.askText.setAttribute("aria-label", "Prompt for your AI agent");
    this.askText.hidden = true;
    // The prompt copied by hand goes away once the player leaves it.
    this.askText.addEventListener("blur", () => {
      this.askText.hidden = true;
      this.askStatus.hidden = true;
    });
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
      this.orderNotice,
      this.saveError,
      this.askStatus,
      this.askText,
      this.summary,
      this.teamsBox,
      this.banner,
      this.clashes,
    );
    // Touch screens have no F key: this button refits the board, as F does.
    this.fitButton.type = "button";
    this.fitButton.setAttribute("aria-label", "Fit the board (F)");
    this.fitButton.title = "Fit the board (F)";
    this.fitButton.innerHTML =
      '<svg viewBox="0 0 24 24" width="22" height="22" aria-hidden="true" focusable="false"><path d="M4 9V4h5M15 4h5v5M20 15v5h-5M9 20H4v-5" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>';
    this.fitButton.addEventListener("click", () => {
      if (this.scene) fitBoard(this.scene);
    });
    topStack.append(this.fitButton);
    this.bottomStack.append(this.logsBox, this.handStatus, timeline);
    root.append(
      this.canvas,
      topStack,
      this.bottomStack,
      this.overlay,
      this.confirmBackdrop,
    );
    this.container.replaceChildren(root);
    this.setConnection(this.options.live ? "connecting" : "saved");
  }

  /**
   * The hand player's surface while a game is started with a hand play: null before `start`,
   * after `stop`, and for a watcher or a demo.
   */
  get hand(): HandSurface | null {
    return this.handSurface;
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

  /** The teams that are out of the game so far, from the live state; the outcome adds the rest. */
  setDepartures(departures: readonly Departure[]): void {
    this.departures = departures;
    const running = this.running;
    if (running) this.render(running.controller.state);
  }

  /** The Resign button, "You resigned" or nothing, from the hand play's status. */
  private renderResign(): void {
    const control = resignControl({
      status: this.options.onResign
        ? (this.options.handPlay?.state.get().status ?? "idle")
        : "idle",
      sending: this.resignSending,
      resigned: this.resignDone,
    });
    this.resignButton.hidden = control.kind !== "button";
    this.resigned.hidden = control.kind !== "resigned";
    if (control.kind === "button") {
      this.resignButton.textContent = control.label;
      this.resignButton.setAttribute("aria-label", control.label);
      this.resignButton.disabled = control.disabled;
    }
    if (control.kind === "resigned") this.resigned.textContent = control.text;
    // The team is out or the game over while the question was open: nothing is left to ask.
    if (control.kind !== "button" && !this.confirmBackdrop.hidden) {
      this.closeConfirmation("top-bar");
    }
  }

  private openConfirmation(): void {
    if (this.resignSending || !this.confirmBackdrop.hidden) return;
    this.confirmReturnFocus =
      document.activeElement instanceof HTMLElement
        ? document.activeElement
        : this.resignButton;
    this.confirmBackdrop.hidden = false;
    // Keep playing is the safe default.
    this.confirmKeep.focus();
    this.confirmKeyListener = (event) => {
      if (confirmationKey(!this.confirmBackdrop.hidden, event) === null) return;
      event.preventDefault();
      this.closeConfirmation();
    };
    document.addEventListener("keydown", this.confirmKeyListener);
  }

  /**
   * Closes the question. The focus goes back to what opened it (`opener`), to the top bar's first
   * button when that can't have it (`top-bar`: the button is disabled or gone), or stays.
   */
  private closeConfirmation(
    focus: "opener" | "top-bar" | "none" = "opener",
  ): void {
    if (this.confirmKeyListener) {
      document.removeEventListener("keydown", this.confirmKeyListener);
      this.confirmKeyListener = undefined;
    }
    this.confirmBackdrop.hidden = true;
    const opener = this.confirmReturnFocus;
    this.confirmReturnFocus = null;
    if (focus === "none") return;
    const usable =
      focus === "opener" &&
      opener?.isConnected === true &&
      !(opener instanceof HTMLButtonElement && opener.disabled) &&
      !opener.hidden;
    (usable ? opener : this.backToLobby)?.focus();
  }

  private async resign(): Promise<void> {
    const onResign = this.options.onResign;
    if (!onResign || this.resignSending) return;
    // The button is disabled while it runs: the focus goes to the top bar's first button.
    this.closeConfirmation("top-bar");
    this.resignSending = true;
    this.renderResign();
    let result: ResignResult;
    try {
      result = await onResign();
    } catch {
      result = {
        kind: "unreachable",
        sentence: "The resignation could not be sent. Try again.",
      };
    }
    this.resignSending = false;
    if (this.disposed) return;
    if (result.kind === "resigned" || result.kind === "already") {
      this.resignDone = true;
    } else {
      // A refused resignation is feedback on an order of the player's (the notice contract).
      this.hand?.showOrderFeedback(result.sentence);
    }
    this.renderResign();
  }

  private showOrderFeedback(sentence: string): void {
    clearTimeout(this.orderNoticeTimer);
    this.orderNotice.textContent = sentence;
    this.orderNotice.hidden = false;
    this.orderNoticeTimer = setTimeout(() => {
      this.orderNotice.hidden = true;
    }, ORDER_FEEDBACK_MS);
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

  /** The prompt "Ask your AI agent" copies for the turn shown, or null before the board is drawn. */
  agentPromptText(): string | null {
    const running = this.running;
    if (!running) return null;
    const state = running.controller.state;
    const game = this.options.agentGame?.() ?? null;
    if (!game) return null;
    const yourTeam = running.teams.find(
      (team) => team.color === this.yourTeamColor,
    );
    return agentPrompt({
      game,
      turn: state.index + 1,
      turns: state.frames.length,
      team: yourTeam ? { name: yourTeam.name, color: yourTeam.color } : null,
      gameUrl: gameBaseUrl(),
      lobbyUrl: lobbyBaseUrl(),
    });
  }

  private async askAgent(): Promise<void> {
    clearTimeout(this.askTimer);
    const prompt = this.agentPromptText();
    this.askText.hidden = true;
    if (!prompt) {
      this.askStatus.textContent =
        "The game isn't loaded yet: try again in a moment.";
    } else if (await copyText(prompt)) {
      this.askStatus.textContent =
        "Copied a prompt for your AI agent: paste it into Claude Code or another agent with the island-battles MCP server.";
      this.askTimer = setTimeout(() => {
        this.askStatus.hidden = true;
      }, 8000);
    } else {
      // The clipboard refused, as some web views do: show the prompt to copy by hand.
      this.askStatus.textContent = "Copy this prompt into your AI agent:";
      this.askText.value = prompt;
      this.askText.hidden = false;
      this.askText.select();
    }
    this.askStatus.hidden = false;
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
    this.scene = scene;
    const handPlay = this.options.handPlay ?? null;
    const removeControls = installBoardControls(scene, { handPlay });
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
    let controllerOf: GameController | undefined;
    const { player } = createScenePlayer(scene, sorted, {
      onIdle: () => onIdle(),
      // The turn that `next` is: its index in the history, from 1.
      departed: (_previous, next) => {
        const index = controllerOf?.state.frames.findIndex(
          (frame) => frame.id === next.id,
        );
        const outcome = controllerOf?.state.outcome;
        return index === undefined || index < 0
          ? undefined
          : teamsDepartedAt(
              mergeDepartures(this.departures, outcome?.departures),
              index + 1,
            );
      },
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
    controllerOf = controller;
    onIdle = () => controller.tick();
    scene.onBeforeRenderObservable.add(() => controller.tick());
    this.handSurface = handPlay && {
      handPlay,
      showsNewestTurn: () => showsNewestTurn(controller.state),
      subscribeView: (listener) => controller.subscribe(listener),
      bottomStack: this.bottomStack,
      setNotice: (text) => this.setNotice(text),
      showOrderFeedback: (sentence) => this.showOrderFeedback(sentence),
    };
    // Scrubbed back, a hand player is told that orders are given from the newest turn.
    const showHandStatus = () => {
      this.handStatus.hidden = !(
        handPlay?.state.get().status === "playing" &&
        !showsNewestTurn(controller.state)
      );
    };
    const handStatusStops = handPlay
      ? [
          handPlay.state.subscribe(showHandStatus),
          controller.subscribe(showHandStatus),
          handPlay.state.subscribe(() => this.renderResign()),
        ]
      : [];
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
        for (const stop of handStatusStops) stop();
        this.closeConfirmation("none");
        this.resignButton.hidden = true;
        this.resigned.hidden = true;
        this.handStatus.hidden = true;
        clearTimeout(this.orderNoticeTimer);
        this.orderNotice.hidden = true;
        removeControls();
        resize.disconnect();
        engine.stopRenderLoop();
        scene.dispose();
        engine.dispose();
      },
    };
    this.render(controller.state);
    showHandStatus();
    this.renderResign();
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
    clearTimeout(this.askTimer);
    this.stop();
    this.container.replaceChildren();
  }

  private stop(): void {
    this.running?.dispose();
    this.running = undefined;
    this.scene = undefined;
    // The hand play itself belongs to the app shell: the screen only lets go of it.
    this.handSurface = null;
  }

  /**
   * Names the bot that plays your seat, as the game copied it at the start, under your team in
   * the player list: who plays it, and for a hosted or uploaded bot that the server does.
   */
  setYourBot(teamColor: number, text: string): void {
    this.setYourBots(teamColor, new Map([[teamColor, text]]));
  }

  /**
   * Names the bots under their teams: yours, on your team `yourTeam`, and the localhost bot seats
   * that this tab relays.
   */
  setYourBots(yourTeam: number, lines: ReadonlyMap<number, string>): void {
    this.yourTeamColor = yourTeam;
    if (
      lines.size === this.yourBots.size &&
      [...lines].every(([team, text]) => this.yourBots.get(team) === text)
    ) {
      return;
    }
    this.yourBots = new Map(lines);
    this.showYourBot();
  }

  private showYourBot(): void {
    for (const card of this.teamsBox.querySelectorAll<HTMLElement>(
      ".gv-team",
    )) {
      const line = card.querySelector<HTMLElement>(".gv-team-bot");
      const text = this.yourBots.get(Number(card.dataset.team));
      if (line) line.hidden = text === undefined;
      if (line && text !== undefined) line.textContent = text;
    }
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
      const bot = element("div", "gv-team-bot");
      bot.hidden = true;
      const soldiers = element("span", "gv-count");
      const castles = element("span", "gv-count");
      const counts = element("div", "gv-team-counts");
      counts.append(soldiers, castles);
      // "Resigned" or "Left the game", once the team is out of a real-time game.
      const status = element("div", "gv-team-status");
      status.hidden = true;
      card.append(name, players, bot, status, counts);
      this.teamRows.set(team.color, { soldiers, castles, status });
      rows.push(card);
    }
    this.teamsBox.replaceChildren(...rows);
    this.showYourBot();
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
    this.renderStats(running, frame, state.outcome);
    this.renderOutcome(running, state.outcome);
    this.renderClashes(frame);
    this.renderLogs(state);
  }

  private renderClashes(frame: Generation | undefined): void {
    const text = frame ? clashSummary(frame) : "";
    this.clashes.textContent = text;
    this.clashes.hidden = text === "";
  }

  private renderStats(
    running: Running,
    frame: Generation | undefined,
    outcome: GameOutcome | null,
  ): void {
    if (!frame) return;
    const departures = mergeDepartures(this.departures, outcome?.departures);
    for (const stats of teamStats(frame, running.teams)) {
      const row = this.teamRows.get(stats.team.color);
      if (!row) continue;
      row.soldiers.textContent = `${stats.soldiers} soldiers`;
      row.castles.textContent = `${stats.castles} ${stats.castles === 1 ? "castle" : "castles"}`;
      const out = stats.castles === 0 && stats.soldiers === 0;
      row.soldiers.parentElement?.parentElement?.classList.toggle(
        "gv-out",
        out,
      );
      // Said while the team is out in the turn shown, so scrubbing back to before it left drops it.
      const departure = out
        ? departures.find((entry) => entry.team_color === stats.team.color)
        : undefined;
      row.status.hidden = departure === undefined;
      row.status.textContent = departure ? departureText(departure) : "";
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
    const ended = state.outcome !== null;
    const book = this.options.logs;
    const choice = chooseLogs(
      {
        frames: state.frames,
        index: state.index,
        ended,
        live: state.live,
        watching: this.options.watching === true,
      },
      book,
    );
    const logs = choice.logs;
    const target = state.frames[choice.target];
    this.logsSummary.textContent = `Bot logs · ${turnLabel(choice.shown < 0 ? state.index : choice.shown, state.frames.length)}`;
    this.logScheduler.update({
      open: this.logsBox.open,
      generationId: target?.id ?? frame.id,
      ended,
      needsFetch:
        target !== undefined &&
        book.needsFetch(target.id, {
          ended,
          open: target.id === openGenerationId(state.frames, ended),
        }),
    });
    const parts: HTMLElement[] = [];
    if (choice.openNote) {
      parts.push(element("p", "gv-logs-note", choice.openNote));
    }
    const notice = hiddenNotice(logs, {
      ended,
      live: state.live,
      watching: this.options.watching === true,
    });
    if (notice) parts.push(element("p", "gv-logs-note", notice));
    if (!logs) {
      if (choice.shown >= 0) {
        parts.push(element("p", "gv-logs-note", "Loading the logs…"));
      }
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
