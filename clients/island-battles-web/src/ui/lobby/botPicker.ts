import { checkBotHealth, serveCommand } from "../../bots";
import {
  type BotPackage,
  describePackage,
  type StoragePost,
  uploadCommands,
  uploadPackage,
} from "../../bots/packages";
import { apiBaseUrl } from "../../platform";
import { copyText, h } from "../dom";
import type { BotChoice, DefaultBot, LobbyActions } from "./actions";
import { choiceFromValue, choiceValue } from "./roomModel";

export interface BotPicker {
  el: HTMLElement;
  /** Shows the saved choice, from the server, and whether choosing is still possible. */
  update(chosen: BotChoice | undefined, locked: boolean): void;
  dispose(): void;
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
}): BotPicker {
  const { actions } = options;
  let chosen: BotChoice | undefined;
  let locked = false;
  let saving = false;
  let disposed = false;

  const status = h(
    "span",
    { class: "pill", attrs: { "data-status": "checking" } },
    "Checking…",
  );
  const command = h("code", { class: "command" }, serveCommand);
  const copy = h(
    "button",
    { type: "button", class: "secondary small" },
    "Copy command",
  );
  copy.addEventListener("click", async () => {
    copy.textContent = (await copyText(serveCommand))
      ? "Copied"
      : "Copy failed";
    setTimeout(() => {
      copy.textContent = "Copy command";
    }, 1500);
  });
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
  const localHelp = h(
    "div",
    { class: "bot-help" },
    h("span", { class: "hint" }, "Run this in your bot's folder:"),
    command,
    copy,
    h("span", { class: "hint" }, safariNote),
  );
  const defaults = h("div", { class: "bot-defaults" });
  const packages = mountPackages({
    actions,
    playerId: options.playerId ?? (() => undefined),
    gameUrl: options.gameUrl ?? apiBaseUrl(),
    post: options.post,
    refreshIntervalMs: options.packagesIntervalMs ?? 10000,
  });
  const el = h(
    "section",
    { class: "card bot-picker" },
    h("h2", {}, "Your bot"),
    localLabel,
    localHelp,
    h(
      "p",
      { class: "hint" },
      "Or let the server play for you with a default bot:",
    ),
    defaults,
    packages.el,
  );

  function radios(): HTMLInputElement[] {
    return Array.from(el.querySelectorAll<HTMLInputElement>("input[name=bot]"));
  }

  function sync(): void {
    const value = choiceValue(chosen);
    for (const radio of radios()) {
      radio.checked = radio.value === value;
      radio.disabled = locked || saving;
    }
    el.classList.toggle("saving", saving);
  }

  async function choose(value: string): Promise<void> {
    const choice = choiceFromValue(value);
    if (!choice || saving || locked || value === choiceValue(chosen)) {
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
    .defaultBots()
    .then((bots) => {
      if (!disposed && bots.length > 0) showDefaults(bots);
    })
    .catch(() => undefined);

  async function checkHealth(): Promise<void> {
    if (document.hidden) return;
    const up = await checkBotHealth();
    if (disposed) return;
    status.dataset.status = up ? "up" : "down";
    status.textContent = up ? "Connected" : "Not running";
  }
  void checkHealth();
  const timer = setInterval(
    () => void checkHealth(),
    options.healthIntervalMs ?? 3000,
  );

  return {
    el,
    update(next, isLocked) {
      chosen = next;
      locked = isLocked;
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
  const el = h(
    "div",
    { class: "bot-packages-panel" },
    h(
      "p",
      { class: "hint" },
      "Bot packages of this lobby: the zip that island-battles-bot package or island-battles-package builds. Choosing one as your bot comes later.",
    ),
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
      if (!disposed) show(items);
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
    refresh,
    upload,
    dispose() {
      disposed = true;
      clearInterval(timer);
    },
  };
}
