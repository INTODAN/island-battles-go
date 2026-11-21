import {
  checkBotHealth,
  defaultBotPort,
  nextFreePort,
  parseBotPort,
  portProblem,
  serveCommandFor,
} from "../../bots";
import { computeSpentMessage, uploadedGreyedOut } from "../../bots/compute";
import {
  type BotPackage,
  describePackage,
  type StoragePost,
  uploadCommands,
  uploadPackage,
} from "../../bots/packages";
import { gameBaseUrl } from "../../platform";
import { copyText, h } from "../dom";
import type { DefaultBot, LobbyActions, MemberBot } from "./actions";
import { describePace, turnPaceOf } from "./form";
import {
  type BotName,
  choiceFromValue,
  choiceValue,
  findBotName,
} from "./roomModel";

export interface BotPicker {
  el: HTMLElement;
  /** Shows the saved choice, from the server, and whether choosing is still possible. */
  update(
    chosen: MemberBot | undefined,
    locked: boolean,
    /**
     * The lobby's `game_settings`: the Play by hand line names its turn pace, and a rated
     * lobby (`rated`) offers only hand play and your hosted bot names.
     */
    gameSettings?: Record<string, unknown>,
  ): void;
  /**
   * Your account, for a rated lobby's choices: your bot names, or null while they load or can't
   * be read. Null for a guest, who has none.
   */
  setAccount(account: PickerAccount | null): void;
  dispose(): void;
}

/** A signed-in member's bot names with their URLs, from the game's `GET /account`; null until they are read. */
export interface PickerAccount {
  botNames: readonly BotName[] | null;
}

/** In a rated lobby, for a signed-in member who has no bot names yet. */
export const noBotNamesHint =
  "Register a bot name with its hosted bot's URL in your account to play rated games with a bot.";
/** In a rated lobby, for a guest: only a signed-in member has bot names. */
export const guestBotNamesHint =
  "Sign in with Google to play a rated game with one of your bot names.";
/** In a rated lobby: why your localhost, uploaded and default bots aren't offered (#400). */
export const ratedKindsHint =
  "Rated bots run at their bot name's URL, so localhost and uploaded bots aren't offered here, and default bots play only in the host's bot seats.";
/** In a rated lobby, when the server plays no hosted bots: no bot name can play. */
export const noServerBotsHint =
  "This server doesn't play hosted bots, so here a rated game is played by hand.";
/** Beside a bot name without a URL, whose radio is disabled. */
export const nameWithoutUrlHint = "No URL yet, so it can't play rated games.";

/** The radio value of one of your bot names in a rated lobby. */
export const botNameValue = (name: string): string => `botname:${name}`;

/** The bot name a radio value stands for, or undefined for any other choice. */
export function botNameOfValue(value: string): string | undefined {
  return value.startsWith("botname:") && value.length > "botname:".length
    ? value.slice("botname:".length)
    : undefined;
}

/**
 * The radio a saved choice checks in a rated lobby, "" for none: hand play, or a hosted bot under
 * one of your names (found without regard to case). Any other choice checks none, and the room
 * says what to do (`ratedBlockers`).
 */
export function ratedChoiceValue(
  chosen: MemberBot | undefined,
  names: readonly BotName[] | null,
): string {
  if (chosen?.kind === "manual") return "manual";
  if (chosen?.kind !== "hosted" || !chosen.name) return "";
  const found = findBotName(names ?? [], chosen.name);
  return found ? botNameValue(found.name) : "";
}

/** What a rated lobby's choice of a bot name saves: a hosted bot under the name, which plays at its URL. */
export const ratedChoice = (name: string): MemberBot => ({
  kind: "hosted",
  name,
});

/** Whether a bot name's radio is disabled: without a URL, while the lobby locks or a save runs, or when the server plays no hosted bots. */
export function botNameLocked(
  name: BotName,
  state: { locked: boolean; saving: boolean; serverBots: boolean | undefined },
): boolean {
  return (
    state.locked ||
    state.saving ||
    name.url === null ||
    state.serverBots === false
  );
}

/**
 * The name an unrated lobby's choice keeps: the saved choice's, when the same kind of bot is saved
 * again (a new port or URL), else none. Only a rated lobby picks a name.
 */
export function keptName(value: string, chosen: MemberBot | undefined): string {
  return choiceValue(chosen) === value ? (chosen?.name ?? "") : "";
}

/** The note for browsers that block a page from reaching localhost (the lobby page's `safariNote`). */
export const safariNote =
  "Safari does not let an https page reach a bot on localhost. Use Chrome, Edge or Firefox to play with your own bot, or open this page over http on localhost.";

/** Used when `GET /bots/defaults` cannot be read. */
const fallbackBots: DefaultBot[] = [1, 2, 3].map((level) => ({
  level,
  name: `Level ${level}`,
  description: "",
}));

export function mountBotPicker(options: {
  /** Saves a choice; the lobby snapshot it answers with reaches `update`. */
  actions: LobbyActions;
  /** Gets the sentence for a refused or failed save; empty clears it. */
  onError(message: string): void;
  describeError(cause: unknown): string;
  healthIntervalMs?: number;
  /** The player, whose own package gets Delete. */
  playerId?(): string | undefined;
  /** The game's address in the command-line upload; the API's by default. */
  gameUrl?: string;
  /** Replaces the upload to storage; tests use it. */
  post?: StoragePost;
  packagesIntervalMs?: number;
  /** Gets the lobby's packages each time they are read, for the host's Add bot. */
  onPackages?(items: BotPackage[]): void;
  /** Gets why uploaded bots can't be chosen this month, or null, for the host's Add bot. */
  onCompute?(spent: string | null): void;
  /** The ports of the localhost bot seats that you added and relay, which yours must differ from. */
  takenPorts?(): number[];
  /** Opens the account page, from **Manage bot names**. */
  onManageBotNames?(): void;
}): BotPicker {
  const { actions } = options;
  let chosen: MemberBot | undefined;
  let locked = false;
  let saving = false;
  let disposed = false;
  /** The server offers hosted and uploaded bots (`GET /bots/defaults`). */
  let serverBots = false;
  /** Whether `GET /bots/defaults` has answered, so a rated lobby knows whether names can play. */
  let serverBotsKnown = false;
  /** Your own packages in the lobby, which you may choose as your bot. */
  let ownPackages: BotPackage[] = [];
  /** Why uploaded bots are greyed out this month (`GET /game/bot-compute`); null while they aren't. */
  let computeSpent: string | null = null;
  /** The lobby is rated: only hand play and your hosted bot names are offered. */
  let rated = false;
  /** Your bot names, for a rated lobby; null for a guest. */
  let account: PickerAccount | null = null;

  // --- A rated lobby's choices: your bot names, each a hosted bot at its own URL (#400) ---
  const ratedNames = h("div", { class: "bot-names" });
  const ratedNote = h("p", { class: "hint" });
  const manageNames = h(
    "button",
    { type: "button", class: "link small" },
    "Manage bot names",
  );
  manageNames.addEventListener("click", () => options.onManageBotNames?.());
  const ratedOptions = h(
    "div",
    { class: "bot-rated", hidden: true },
    h(
      "p",
      { class: "hint" },
      "Or play one of your bot names. The server plays it at the name's URL; this tab relays nothing.",
    ),
    ratedNames,
    ratedNote,
    h("p", { class: "hint" }, ratedKindsHint),
  );
  /** The names drawn last, so a redraw only replaces them when they change. */
  let ratedKey = "";

  /** Draws your bot names as choices, each with its URL as text, or what to do without one. */
  function showRatedNames(): void {
    const names = account?.botNames ?? null;
    const key = JSON.stringify([account === null, names]);
    if (key !== ratedKey) {
      ratedKey = key;
      const rows: HTMLElement[] = (names ?? []).map((name, index) => {
        const id = `bot-name-${index}`;
        const radio = h("input", {
          type: "radio",
          name: "bot",
          value: botNameValue(name.name),
          id,
        });
        const help: (HTMLElement | null)[] = [];
        if (name.url === null) {
          const add = h(
            "button",
            { type: "button", class: "link small" },
            "Add its URL in your account",
          );
          add.addEventListener("click", (event) => {
            event.preventDefault();
            options.onManageBotNames?.();
          });
          help.push(h("span", { class: "hint" }, nameWithoutUrlHint), add);
        }
        return h(
          "div",
          { class: "bot-name-choice" },
          h(
            "label",
            {
              class: `bot-option${name.url === null ? " no-url" : ""}`,
              htmlFor: id,
            },
            radio,
            h(
              "span",
              { class: "bot-text" },
              h("strong", {}, name.name),
              name.url === null
                ? null
                : h("span", { class: "hint bot-name-url" }, name.url),
            ),
          ),
          help.length > 0 ? h("div", { class: "bot-help" }, ...help) : null,
        );
      });
      ratedNames.replaceChildren(...rows);
    }
    const sentence =
      account === null
        ? guestBotNamesHint
        : names === null
          ? "Loading your bot names…"
          : names.length === 0
            ? noBotNamesHint
            : serverBotsKnown && !serverBots
              ? noServerBotsHint
              : "";
    ratedNote.replaceChildren(
      ...(sentence ? [sentence, " "] : []),
      ...(account === null ? [] : [manageNames]),
    );
  }

  const status = h(
    "span",
    { class: "pill", attrs: { "data-status": "checking" } },
    "Checking…",
  );
  /** The saved localhost bot's port, the default one for any other bot. */
  const chosenPort = () =>
    chosen?.kind === "localhost" && chosen.port ? chosen.port : defaultBotPort;
  const command = h("code", { class: "command" }, serveCommandFor());
  const copy = h(
    "button",
    { type: "button", class: "secondary small" },
    "Copy command",
  );
  copy.addEventListener("click", async () => {
    copy.textContent = (await copyText(serveCommandFor(chosenPort())))
      ? "Copied"
      : "Copy failed";
    setTimeout(() => {
      copy.textContent = "Copy command";
    }, 1500);
  });
  const manualHint = h("span", { class: "hint" });
  const manualLabel = h(
    "label",
    { class: "bot-option", htmlFor: "bot-manual" },
    h("input", {
      type: "radio",
      name: "bot",
      value: "manual",
      id: "bot-manual",
    }),
    h(
      "span",
      { class: "bot-text" },
      h("strong", {}, "Play by hand"),
      manualHint,
    ),
  );
  /** Names the pace of the lobby's real-time game in the Play by hand line. */
  function showPace(settings: Record<string, unknown> | undefined): void {
    manualHint.textContent = `You command your castles yourself in this client. The game is then real-time: a turn every ${describePace(turnPaceOf(settings))}.`;
  }
  showPace(undefined);
  const local = h("input", {
    type: "radio",
    name: "bot",
    value: "localhost",
    id: "bot-localhost",
  });
  const localLabel = h(
    "label",
    { class: "bot-option", htmlFor: "bot-localhost" },
    local,
    h(
      "span",
      { class: "bot-text" },
      h("strong", {}, "My localhost bot"),
      h(
        "span",
        { class: "hint" },
        "Your own bot, running on this computer. This page relays each turn to it.",
      ),
    ),
    status,
  );
  // The port your bot serves on, 41414 unless you change it.
  const port = h("input", {
    type: "text",
    class: "bot-port",
    value: String(defaultBotPort),
    attrs: {
      "aria-label": "Your localhost bot's port",
      inputmode: "numeric",
      maxlength: "5",
    },
  });
  const usePort = h(
    "button",
    { type: "button", class: "secondary small" },
    "Use this port",
  );
  const localHelp = h(
    "div",
    { class: "bot-help" },
    h("span", { class: "hint" }, "Port on 127.0.0.1:"),
    port,
    usePort,
    h("span", { class: "hint" }, "Run this in your bot's folder:"),
    command,
    copy,
    h("span", { class: "hint" }, safariNote),
  );
  const defaults = h("div", { class: "bot-defaults" });
  // The bots that the server plays without this tab, shown when it offers them.
  const hostedUrl = h("input", {
    type: "url",
    class: "bot-url",
    placeholder: "https://bot.example.com/turn",
    attrs: { "aria-label": "Your hosted bot's URL", maxlength: "2048" },
  });
  const useUrl = h(
    "button",
    { type: "button", class: "secondary small" },
    "Use this URL",
  );
  const hostedLabel = h(
    "label",
    { class: "bot-option", htmlFor: "bot-hosted" },
    h("input", {
      type: "radio",
      name: "bot",
      value: "hosted",
      id: "bot-hosted",
    }),
    h(
      "span",
      { class: "bot-text" },
      h("strong", {}, "My hosted bot (URL)"),
      h(
        "span",
        { class: "hint" },
        "Your own bot server at a public https:// URL. The server sends it each turn; this tab relays nothing.",
      ),
    ),
  );
  const hostedHelp = h("div", { class: "bot-help" }, hostedUrl, useUrl);
  const uploaded = h("div", { class: "bot-uploaded" });
  const serverOptions = h(
    "div",
    { class: "bot-server", hidden: true },
    h(
      "p",
      { class: "hint" },
      "Or let the server play your own bot, without this tab:",
    ),
    hostedLabel,
    hostedHelp,
    uploaded,
  );
  const packages = mountPackages({
    actions,
    playerId: options.playerId ?? (() => undefined),
    gameUrl: options.gameUrl ?? gameBaseUrl(),
    post: options.post,
    refreshIntervalMs: options.packagesIntervalMs ?? 10000,
    onList(items) {
      const you = options.playerId?.();
      ownPackages = items.filter((item) => item.owner_player_id === you);
      showUploaded();
      options.onPackages?.(items);
    },
  });
  // Every choice of a lobby that isn't rated; a rated lobby offers its own (`ratedOptions`).
  const unratedOptions = h(
    "div",
    { class: "bot-unrated" },
    localLabel,
    localHelp,
    h(
      "p",
      { class: "hint" },
      "Or let the server play for you with a default bot:",
    ),
    defaults,
    serverOptions,
    packages.el,
  );
  const el = h(
    "section",
    { class: "card bot-picker" },
    h("h2", {}, "Your bot"),
    manualLabel,
    ratedOptions,
    unratedOptions,
  );

  function radios(): HTMLInputElement[] {
    return Array.from(el.querySelectorAll<HTMLInputElement>("input[name=bot]"));
  }

  function sync(): void {
    const names = account?.botNames ?? null;
    if (rated) showRatedNames();
    const value = rated ? ratedChoiceValue(chosen, names) : choiceValue(chosen);
    for (const radio of radios()) {
      radio.checked = radio.value === value;
      const name = botNameOfValue(radio.value);
      const botName = name ? findBotName(names ?? [], name) : undefined;
      radio.disabled = botName
        ? botNameLocked(botName, {
            locked,
            saving,
            serverBots: serverBotsKnown ? serverBots : undefined,
          })
        : locked || saving || uploadedGreyedOut(radio.value, computeSpent);
    }
    ratedOptions.hidden = !rated;
    unratedOptions.hidden = rated;
    hostedUrl.disabled = locked || saving;
    useUrl.disabled = locked || saving;
    port.disabled = locked || saving;
    usePort.disabled = locked || saving;
    if (document.activeElement !== port) port.value = String(chosenPort());
    command.textContent = serveCommandFor(chosenPort());
    if (chosen?.kind === "hosted" && document.activeElement !== hostedUrl) {
      hostedUrl.value = chosen.url ?? "";
    }
    el.classList.toggle("saving", saving);
  }

  /** Your packages as choices, "Uploaded: <package>", and the one you chose if it is gone. */
  function showUploaded(): void {
    const items: HTMLElement[] = ownPackages.flatMap((item) => {
      const id = `bot-package-${item.package_id}`;
      const label = h(
        "label",
        { class: "bot-option", htmlFor: id },
        h("input", {
          type: "radio",
          name: "bot",
          value: `package-${item.package_id}`,
          id,
        }),
        h(
          "span",
          { class: "bot-text" },
          h("strong", {}, `Uploaded: ${describePackage(item)}`),
          h(
            "span",
            { class: "hint" },
            "Your package in this lobby. The server runs it; this tab relays nothing.",
          ),
        ),
      );
      return [label];
    });
    if (
      chosen?.kind === "uploaded" &&
      !ownPackages.some((item) => item.package_id === chosen?.package_id)
    ) {
      items.push(
        h(
          "p",
          { class: "field-error" },
          "Your uploaded bot was replaced or deleted. Choose a package again.",
        ),
      );
    }
    if (computeSpent) {
      items.unshift(h("p", { class: "field-error" }, computeSpent));
    }
    uploaded.classList.toggle("spent", computeSpent !== null);
    uploaded.replaceChildren(
      ...(items.length > 0
        ? items
        : [
            h(
              "p",
              { class: "hint" },
              "Upload a package below to choose it as your bot.",
            ),
          ]),
    );
    sync();
  }

  async function choose(value: string): Promise<void> {
    // A package greyed out this month can't be chosen.
    if (uploadedGreyedOut(value, computeSpent)) {
      sync();
      return;
    }
    // A rated lobby's bot name: a hosted bot under it, which plays at the name's URL.
    const name = botNameOfValue(value);
    if (name !== undefined) {
      const same =
        chosen?.kind === "hosted" &&
        chosen.name?.toLowerCase() === name.toLowerCase() &&
        !chosen.url;
      if (!same && !saving && !locked) await save(ratedChoice(name));
      else sync();
      return;
    }
    // Your localhost bot keeps the port in its field, which the seats you relay mustn't use;
    // a first choice of it takes the first port they leave free.
    const taken = options.takenPorts?.() ?? [];
    let localPort = defaultBotPort;
    if (value === "localhost" && chosen?.kind === "localhost") {
      const problem = portProblem(port.value, taken);
      if (problem) {
        options.onError(problem);
        sync();
        return;
      }
      localPort = parseBotPort(port.value) ?? defaultBotPort;
    } else if (value === "localhost") {
      localPort = nextFreePort(taken);
    }
    const choice = choiceFromValue(
      value,
      hostedUrl.value,
      localPort,
      keptName(value, chosen),
    );
    const same =
      value === choiceValue(chosen) &&
      (choice?.name ?? "") === (chosen?.name ?? "") &&
      (choice?.kind !== "hosted" ||
        (chosen?.kind === "hosted" && chosen.url === choice.url)) &&
      (choice?.kind !== "localhost" ||
        (choice.port ?? defaultBotPort) === chosenPort());
    if (!choice || saving || locked || same) {
      if (value === "hosted" && !choice) {
        options.onError(
          "Type your bot's URL first, such as https://bot.example.com/turn.",
        );
        hostedUrl.focus();
      }
      sync();
      return;
    }
    await save(choice);
  }

  /** Saves a choice; saved or refused, the radios show the saved choice again. */
  async function save(choice: MemberBot): Promise<void> {
    saving = true;
    options.onError("");
    sync();
    try {
      await actions.chooseBot(choice);
    } catch (cause) {
      options.onError(options.describeError(cause));
    } finally {
      saving = false;
      if (!disposed) sync();
    }
  }

  el.addEventListener("change", (event) => {
    const target = event.target;
    if (target instanceof HTMLInputElement && target.name === "bot") {
      void choose(target.value);
    }
  });
  useUrl.addEventListener("click", () => void choose("hosted"));
  usePort.addEventListener("click", () => void choose("localhost"));
  port.addEventListener("keydown", (event) => {
    if (event.key === "Enter") void choose("localhost");
  });
  hostedUrl.addEventListener("keydown", (event) => {
    if (event.key === "Enter") void choose("hosted");
  });

  function showDefaults(bots: DefaultBot[]): void {
    defaults.replaceChildren(
      ...bots.map((bot) => {
        const id = `bot-level-${bot.level}`;
        return h(
          "label",
          { class: "bot-option", htmlFor: id },
          h("input", {
            type: "radio",
            name: "bot",
            value: `level-${bot.level}`,
            id,
          }),
          h(
            "span",
            { class: "bot-text" },
            h(
              "strong",
              {},
              `Level ${bot.level}${bot.name && bot.name !== `Level ${bot.level}` ? ` · ${bot.name}` : ""}`,
            ),
            bot.description
              ? h("span", { class: "hint" }, bot.description)
              : null,
          ),
        );
      }),
    );
    sync();
  }
  showDefaults(fallbackBots);
  void actions
    .botOptions()
    .then((offered) => {
      if (disposed) return;
      if (offered.bots.length > 0) showDefaults(offered.bots);
      serverBots = offered.serverBots;
      serverBotsKnown = true;
      serverOptions.hidden = !serverBots;
      packages.setChoosable(serverBots);
      sync();
      if (serverBots) void readCompute();
    })
    .catch(() => undefined);

  /** Greys out uploaded bots once their month's compute is spent; a failed read greys out nothing, and the start says why. */
  async function readCompute(): Promise<void> {
    try {
      const compute = await actions.botCompute();
      if (disposed) return;
      computeSpent = computeSpentMessage(compute);
      options.onCompute?.(computeSpent);
      showUploaded();
    } catch {
      // The start refuses uploaded bots all the same.
    }
  }

  async function checkHealth(): Promise<void> {
    if (document.hidden) return;
    const asked = chosenPort();
    const up = await checkBotHealth({ port: asked });
    if (disposed || asked !== chosenPort()) return;
    status.dataset.status = up ? "up" : "down";
    status.textContent = up ? `Connected on ${asked}` : "Not running";
  }
  void checkHealth();
  const timer = setInterval(
    () => void checkHealth(),
    options.healthIntervalMs ?? 3000,
  );

  return {
    el,
    update(next, isLocked, gameSettings) {
      showPace(gameSettings);
      rated = gameSettings?.rated === true;
      const before = chosenPort();
      chosen = next;
      locked = isLocked;
      if (chosenPort() !== before) void checkHealth();
      if (serverBots) showUploaded();
      else sync();
    },
    setAccount(next) {
      account = next;
      sync();
    },
    dispose() {
      disposed = true;
      clearInterval(timer);
      packages.dispose();
    },
  };
}

/** What the lobby's bot packages need from the lobby screens. */
interface PackagesOptions {
  actions: Pick<
    LobbyActions,
    | "listBotPackages"
    | "startBotPackageUpload"
    | "completeBotPackageUpload"
    | "deleteBotPackage"
    | "botUploadToken"
  >;
  playerId(): string | undefined;
  /** The game's address, for the command line. */
  gameUrl: string;
  post?: StoragePost | undefined;
  refreshIntervalMs: number;
  /** Gets the list each time it is read. */
  onList?(items: BotPackage[]): void;
}

const messageOf = (cause: unknown): string =>
  cause instanceof Error && cause.message
    ? cause.message
    : "Something went wrong. Try again.";

/**
 * The lobby's bot packages: "Upload bot (.zip)", "Upload from the command
 * line", the upload's progress and result, and every member's package, with
 * Delete on your own. The list is read again every `refreshIntervalMs`, so
 * packages that others upload appear.
 */
export function mountPackages(options: PackagesOptions): {
  el: HTMLElement;
  /** Says whether a package can be chosen as a bot, as the server offers. */
  setChoosable(on: boolean): void;
  /** Reads the list again; resolves once it is shown. */
  refresh(): Promise<void>;
  /** Uploads a file, as choosing it in the file picker does. */
  upload(file: File): Promise<void>;
  dispose(): void;
} {
  const { actions } = options;
  let busy = false;
  let disposed = false;

  const fileInput = h("input", {
    type: "file",
    accept: ".zip,application/zip",
    hidden: true,
    attrs: { "aria-label": "Bot package (.zip)" },
  });
  const uploadButton = h(
    "button",
    { type: "button", class: "small" },
    "Upload bot (.zip)",
  );
  const commandButton = h(
    "button",
    { type: "button", class: "secondary small" },
    "Upload from the command line",
  );
  const progress = h("progress", { max: 1, value: 0, hidden: true });
  const status = h("p", { class: "hint", attrs: { role: "status" } });
  const commands = h("div", {
    class: "bot-help package-commands",
    hidden: true,
  });
  const list = h("ul", { class: "bot-packages" });
  const about = h("p", { class: "hint" });
  const setChoosable = (on: boolean) => {
    about.textContent = `Bot packages of this lobby: the zip that island-battles-bot package or island-battles-package builds. ${
      on
        ? "Choose one of yours above as your bot; the host can seat anyone's as a bot."
        : "Choosing one as your bot comes later."
    }`;
  };
  setChoosable(false);
  const el = h(
    "div",
    { class: "bot-packages-panel" },
    about,
    h("div", { class: "bot-help" }, uploadButton, commandButton, fileInput),
    progress,
    status,
    commands,
    list,
  );

  function setBusy(next: boolean): void {
    busy = next;
    uploadButton.disabled = next;
    commandButton.disabled = next;
  }

  function show(items: BotPackage[]): void {
    const you = options.playerId();
    list.replaceChildren(
      ...(items.length === 0
        ? [h("li", { class: "hint" }, "No packages yet.")]
        : items.map((item) => {
            const mine = item.owner_player_id === you;
            const remove = mine
              ? h(
                  "button",
                  { type: "button", class: "secondary small" },
                  "Delete",
                )
              : null;
            remove?.addEventListener("click", () => void removePackage(item));
            return h(
              "li",
              { attrs: { "data-package-id": item.package_id } },
              h(
                "span",
                {},
                `${item.owner_name}${mine ? " (you)" : ""}: ${describePackage(item)}`,
              ),
              remove,
            );
          })),
    );
  }

  async function refresh(): Promise<void> {
    try {
      const items = await actions.listBotPackages();
      if (!disposed) {
        show(items);
        options.onList?.(items);
      }
    } catch (cause) {
      if (!disposed) status.textContent = messageOf(cause);
    }
  }

  async function upload(file: File): Promise<void> {
    if (busy) return;
    setBusy(true);
    status.textContent = `Uploading ${file.name}…`;
    progress.value = 0;
    progress.hidden = false;
    try {
      const uploaded = await uploadPackage({
        file,
        calls: actions,
        post: options.post,
        onProgress(fraction) {
          progress.value = fraction;
          if (fraction >= 1) status.textContent = "Checking the package…";
        },
      });
      status.textContent = `Uploaded ${file.name}: ${describePackage(uploaded)}.`;
      await refresh();
    } catch (cause) {
      status.textContent = messageOf(cause);
    } finally {
      progress.hidden = true;
      setBusy(false);
    }
  }

  async function removePackage(item: BotPackage): Promise<void> {
    if (busy) return;
    setBusy(true);
    try {
      await actions.deleteBotPackage(item.package_id);
      status.textContent = "Deleted your package.";
      await refresh();
    } catch (cause) {
      status.textContent = messageOf(cause);
    } finally {
      setBusy(false);
    }
  }

  async function showCommands(): Promise<void> {
    if (busy) return;
    setBusy(true);
    try {
      const issued = await actions.botUploadToken();
      const both = uploadCommands(options.gameUrl, issued.token);
      const until = new Date(issued.expires_at).toLocaleTimeString([], {
        hour: "2-digit",
        minute: "2-digit",
      });
      commands.replaceChildren(
        ...(
          [
            ["Python", both.python],
            ["Go", both.go],
          ] as const
        ).flatMap(([label, text]) => {
          const copy = h(
            "button",
            { type: "button", class: "secondary small" },
            "Copy",
          );
          copy.addEventListener("click", async () => {
            copy.textContent = (await copyText(text))
              ? "Copied"
              : "Copy failed";
          });
          return [
            h("span", { class: "hint" }, `${label}, in your bot's folder:`),
            h("code", { class: "command" }, text),
            copy,
          ];
        }),
        h(
          "span",
          { class: "hint" },
          `The token in these commands uploads to this lobby as you until ${until}.`,
        ),
      );
      commands.hidden = false;
    } catch (cause) {
      status.textContent = messageOf(cause);
    } finally {
      setBusy(false);
    }
  }

  uploadButton.addEventListener("click", () => fileInput.click());
  fileInput.addEventListener("change", () => {
    const chosen = fileInput.files?.[0];
    fileInput.value = "";
    if (chosen) void upload(chosen);
  });
  commandButton.addEventListener("click", () => void showCommands());
  void refresh();
  const timer = setInterval(() => {
    if (!busy && !document.hidden) void refresh();
  }, options.refreshIntervalMs);

  return {
    el,
    setChoosable,
    refresh,
    upload,
    dispose() {
      disposed = true;
      clearInterval(timer);
    },
  };
}
