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
import { choiceFromValue, choiceValue } from "./roomModel";

export interface BotPicker {
  el: HTMLElement;
  /** Shows the saved choice, from the server, and whether choosing is still possible. */
  update(
    chosen: MemberBot | undefined,
    locked: boolean,
    /**
     * The lobby's `game_settings`: the Play by hand line names its turn pace, and a rated
     * lobby (`rated`) shows **Plays as** and disables the default bots.
     */
    gameSettings?: Record<string, unknown>,
  ): void;
  /**
   * Your account, for **Plays as**: your bot names, or null while they load or can't be read.
   * Null for a guest, who sees no **Plays as**.
   */
  setAccount(account: PickerAccount | null): void;
  dispose(): void;
}

/** A signed-in member's bot names, from the game's `GET /account`; null until they are read. */
export interface PickerAccount {
  botNames: readonly string[] | null;
}

/** Under **Plays as**, for a signed-in member who has no bot names yet. */
export const noBotNamesHint =
  "Register a bot name in your account to play rated games with a bot.";
/** Beside the default bots in a rated lobby, whose radios are disabled. */
export const ratedDefaultsHint =
  "Default bots play only in bot seats in a rated lobby.";

/**
 * The bot names a **Plays as** select offers, "" (No name) first: your names, and the one chosen
 * when it is no longer among them, so the select still shows what is saved.
 */
export function playsAsOptions(
  names: readonly string[],
  chosen: string,
): { value: string; label: string }[] {
  const options = [
    { value: "", label: "No name" },
    ...names.map((name) => ({ value: name, label: name })),
  ];
  if (
    chosen &&
    !names.some((name) => name.toLowerCase() === chosen.toLowerCase())
  ) {
    options.push({ value: chosen, label: `${chosen} (deleted)` });
  }
  return options;
}

/** Whether a radio value is a bot that plays under a bot name: a localhost, hosted or uploaded bot. */
export const takesBotName = (value: string): boolean =>
  value === "localhost" || value === "hosted" || value.startsWith("package-");

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
  /** Your own packages in the lobby, which you may choose as your bot. */
  let ownPackages: BotPackage[] = [];
  /** Why uploaded bots are greyed out this month (`GET /game/bot-compute`); null while they aren't. */
  let computeSpent: string | null = null;
  /** The lobby is rated: **Plays as** shows, and default bots can't be chosen. */
  let rated = false;
  /** Your bot names, for **Plays as**; null for a guest. */
  let account: PickerAccount | null = null;
  /** The name each choice's **Plays as** shows until it is saved, by radio value. */
  const pendingNames = new Map<string, string>();
  /** Each choice's **Plays as** row, by radio value. */
  const nameRows = new Map<
    string,
    {
      row: HTMLElement;
      select: HTMLSelectElement;
      hint: HTMLElement;
      key: string;
    }
  >();
  const defaultsNote = h(
    "p",
    { class: "hint", hidden: true },
    ratedDefaultsHint,
  );

  /**
   * The bot name a choice plays as: its **Plays as** while it is changed and not saved yet, else
   * the saved choice's name when it is this choice. A guest's choice has none.
   */
  function nameFor(value: string): string {
    const saved = choiceValue(chosen) === value ? (chosen?.name ?? "") : "";
    if (!rated) return saved;
    if (!account) return "";
    return pendingNames.get(value) ?? saved;
  }

  /** The **Plays as** row of a choice: made once, and drawn by `showNames`. */
  function nameRow(value: string, label: string): HTMLElement {
    const known = nameRows.get(value);
    if (known) return known.row;
    const id = `bot-name-${value}`;
    const select = h("select", { id, class: "bot-name" });
    select.setAttribute("aria-label", `Plays as, for ${label}`);
    select.addEventListener("change", () => {
      pendingNames.set(value, select.value);
      void choose(value);
    });
    const hint = h("span", { class: "hint" });
    const manage = h(
      "button",
      { type: "button", class: "link small" },
      "Manage bot names",
    );
    manage.addEventListener("click", () => options.onManageBotNames?.());
    const row = h(
      "div",
      { class: "bot-help bot-plays-as", hidden: true },
      h("label", { htmlFor: id }, "Plays as"),
      select,
      hint,
      manage,
    );
    nameRows.set(value, { row, select, hint, key: "" });
    return row;
  }

  /** Draws each **Plays as**: shown to a signed-in member in a rated lobby. */
  function showNames(): void {
    for (const [value, entry] of nameRows) {
      const shown = rated && account !== null;
      entry.row.hidden = !shown;
      if (!shown || !account) continue;
      const names = account.botNames;
      const current = nameFor(value);
      entry.select.hidden = names === null || names.length === 0;
      entry.hint.textContent =
        names === null
          ? "Loading your bot names…"
          : names.length === 0
            ? noBotNamesHint
            : "";
      entry.hint.hidden = entry.hint.textContent === "";
      const offered = playsAsOptions(names ?? [], current);
      const key = JSON.stringify(offered);
      // Replacing the options closes an open select, so only a change does.
      if (key !== entry.key) {
        entry.key = key;
        entry.select.replaceChildren(
          ...offered.map((option) => new Option(option.label, option.value)),
        );
      }
      if (document.activeElement !== entry.select) {
        entry.select.value =
          offered.find(
            (option) => option.value.toLowerCase() === current.toLowerCase(),
          )?.value ?? "";
      }
      entry.select.disabled = locked || saving;
    }
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
  const hostedName = nameRow("hosted", "My hosted bot");
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
    hostedName,
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
  const el = h(
    "section",
    { class: "card bot-picker" },
    h("h2", {}, "Your bot"),
    manualLabel,
    localLabel,
    localHelp,
    nameRow("localhost", "My localhost bot"),
    h(
      "p",
      { class: "hint" },
      "Or let the server play for you with a default bot:",
    ),
    defaultsNote,
    defaults,
    serverOptions,
    packages.el,
  );

  function radios(): HTMLInputElement[] {
    return Array.from(el.querySelectorAll<HTMLInputElement>("input[name=bot]"));
  }

  function sync(): void {
    const value = choiceValue(chosen);
    for (const radio of radios()) {
      radio.checked = radio.value === value;
      radio.disabled =
        locked ||
        saving ||
        uploadedGreyedOut(radio.value, computeSpent) ||
        // A rated lobby's default bots play only in the bot seats the host adds.
        (rated && radio.value.startsWith("level-"));
    }
    defaultsNote.hidden = !rated;
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
    showNames();
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
      return [
        label,
        nameRow(
          `package-${item.package_id}`,
          `Uploaded: ${describePackage(item)}`,
        ),
      ];
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
      takesBotName(value) ? nameFor(value) : "",
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
    saving = true;
    options.onError("");
    sync();
    try {
      await actions.chooseBot(choice);
    } catch (cause) {
      options.onError(options.describeError(cause));
    } finally {
      // Saved or refused, the select shows the saved choice again.
      pendingNames.delete(value);
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
