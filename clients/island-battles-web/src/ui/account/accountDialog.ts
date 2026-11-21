// The account page (#369, option A of the mockups): a dialog like the Game rules page, opened from
// the menu's Account link. It draws `accountModel.ts` and sends `accountService.ts`'s requests.

import type { Realtime } from "../../realtime";
import { h } from "../dom";
import {
  accountProblem,
  accountView,
  botNameProblem,
  botUrlEditHint,
  botUrlPublicSentence,
  type Competitor,
  type CompetitorRow,
  type Confirmation,
  deleteAccountConfirmation,
  deleteBotNameConfirmation,
  displayNameProblem,
  endsSession,
  exampleBotUrl,
  type IslandAccount,
  nameRule,
  noBotUrlSentence,
  partlyDeletedProblem,
  validateAccountName,
  validateBotName,
  validateBotUrl,
  withBotName,
  withChangedBotName,
  withoutBotName,
} from "./accountModel";
import { type AccountService, createAccountService } from "./accountService";
import "../rules/rules.css";
import "./account.css";

export interface AccountDialogOptions {
  realtime: Realtime;
  /**
   * Signs out for good: Google's silent sign-in off, the session ended, back to the name screen.
   * Called by Sign out, and after the account is deleted.
   */
  onSignOut(): void;
  /**
   * Turns Google's silent sign-in off, without signing out. Called once the game's part of the
   * account is deleted, before the lobby's, so a refusal there can't sign the person in again.
   */
  onForgetGoogle(): void;
  /** Replaces the requests; tests and the browser check use it. */
  service?: AccountService;
}

/** Asks in a box over the page, as the Resign confirmation does; resolves true for the action. */
function confirm(question: Confirmation): Promise<boolean> {
  return new Promise((resolve) => {
    const yes = h(
      "button",
      { type: "button", class: "danger solid" },
      question.confirm,
    );
    const keep = h("button", { type: "button", class: "secondary" }, "Keep it");
    const box = h(
      "dialog",
      {
        class: "account-confirm",
        attrs: {
          role: "alertdialog",
          "aria-labelledby": "account-confirm-question",
          "aria-describedby": "account-confirm-text",
        },
      },
      h(
        "p",
        { class: "account-confirm-question", id: "account-confirm-question" },
        question.question,
      ),
      h("p", { id: "account-confirm-text" }, question.text),
      h("div", { class: "actions" }, yes, keep),
    );
    let answer = false;
    yes.addEventListener("click", () => {
      answer = true;
      box.close();
    });
    keep.addEventListener("click", () => box.close());
    box.addEventListener("click", (event) => {
      if (event.target === box) box.close();
    });
    box.addEventListener("close", () => {
      box.remove();
      resolve(answer);
    });
    document.body.append(box);
    box.showModal();
    keep.focus();
  });
}

function competitorItem(
  row: Pick<CompetitorRow, "name" | "rating" | "games">,
  action?: HTMLElement,
  ...below: (HTMLElement | null)[]
): HTMLElement {
  return h(
    "li",
    { class: action ? "" : "hand" },
    h("span", { class: "account-name" }, row.name),
    h("span", { class: "account-rating" }, row.rating),
    h("span", { class: "account-games" }, row.games),
    action ?? null,
    ...below,
  );
}

/** Opens the account page over the screen; it closes itself when the session ends. */
export function openAccount(options: AccountDialogOptions): HTMLDialogElement {
  const { realtime } = options;
  const service = options.service ?? createAccountService(realtime);
  let account: IslandAccount | null = null;
  /** The game's part of the account is deleted; the lobby's call failed. */
  let gameDeleted = false;
  let adding = false;
  let busy = false;

  // --- Display name ---
  const nameInput = h("input", {
    type: "text",
    maxLength: 24,
    autocomplete: "off",
    spellcheck: false,
    value: realtime.session.get()?.displayName ?? "",
    attrs: { "aria-label": "Display name" },
  });
  const saveButton = h(
    "button",
    { type: "submit", class: "secondary" },
    "Save",
  );
  const nameError = h("p", { class: "field-error", attrs: { role: "alert" } });
  const nameStatus = h("p", { class: "hint", attrs: { role: "status" } });
  const nameForm = h(
    "form",
    { class: "account-section", noValidate: true },
    h("h3", {}, "Display name"),
    h(
      "p",
      { class: "hint" },
      "The name others see in lobbies, and beside your bots on the leaderboard.",
    ),
    h("div", { class: "account-inline" }, nameInput, saveButton),
    h("p", { class: "hint" }, nameRule),
    nameError,
    nameStatus,
  );

  // --- Playing by hand ---
  const handList = h("ul", { class: "account-list" });
  const handSection = h(
    "section",
    { class: "account-section" },
    h("h3", {}, "Playing by hand"),
    handList,
  );

  // --- Bot names ---
  const count = h("span", { class: "account-count" });
  const botList = h("ul", { class: "account-list" });
  const botInput = h("input", {
    id: "new-bot-name",
    type: "text",
    maxLength: 24,
    autocomplete: "off",
    spellcheck: false,
    placeholder: "Leviathan",
  });
  const urlInput = h("input", {
    id: "new-bot-url",
    type: "url",
    maxLength: 2048,
    autocomplete: "off",
    spellcheck: false,
    placeholder: exampleBotUrl,
  });
  const addButton = h("button", { type: "submit", class: "primary" }, "Add");
  const addHint = h("p", { class: "hint" });
  const addError = h("p", { class: "field-error", attrs: { role: "alert" } });
  const addForm = h(
    "form",
    { class: "account-add", noValidate: true },
    h("label", { htmlFor: "new-bot-name" }, "New bot name"),
    botInput,
    addHint,
    h("label", { htmlFor: "new-bot-url" }, "Its hosted bot's URL"),
    h("div", { class: "account-inline" }, urlInput, addButton),
    h("p", { class: "hint account-public" }, botUrlPublicSentence),
    addError,
  );

  // --- Changing a name's URL: one form, in the row of the name being changed ---
  /** The bot name whose URL is being changed, by competitor ID. */
  let editing: string | null = null;
  let savingUrl = false;
  const editInput = h("input", {
    type: "url",
    maxLength: 2048,
    autocomplete: "off",
    spellcheck: false,
    placeholder: exampleBotUrl,
  });
  const editSave = h(
    "button",
    { type: "submit", class: "primary small" },
    "Save",
  );
  const editCancel = h(
    "button",
    { type: "button", class: "secondary small" },
    "Cancel",
  );
  const editHint = h("p", { class: "hint" });
  const editError = h("p", { class: "field-error", attrs: { role: "alert" } });
  const editForm = h(
    "form",
    { class: "account-url account-url-form", noValidate: true },
    h("div", { class: "account-inline" }, editInput, editSave, editCancel),
    editHint,
    editError,
  );
  const botSection = h(
    "section",
    { class: "account-section" },
    h("h3", {}, "Bot names ", count),
    h(
      "p",
      { class: "hint" },
      "Each name is a hosted bot at its own URL, which plays when you choose the name in a rated lobby. Each name has its own rating, which stays when its URL changes.",
    ),
    botList,
    addForm,
  );

  // --- Sign out, delete ---
  const signOutButton = h(
    "button",
    { type: "button", class: "secondary" },
    "Sign out",
  );
  const deleteButton = h(
    "button",
    { type: "button", class: "danger" },
    "Delete account…",
  );
  const accountError = h("p", {
    class: "field-error",
    attrs: { role: "alert" },
  });
  const actionsSection = h(
    "section",
    { class: "account-section" },
    h("div", { class: "account-danger-zone" }, signOutButton, deleteButton),
    accountError,
  );

  const close = h(
    "button",
    { type: "button", class: "secondary small" },
    "Close",
  );
  const dialog = h(
    "dialog",
    {
      class: "rules-dialog account-dialog",
      attrs: { "aria-labelledby": "account-title" },
    },
    h(
      "header",
      { class: "rules-header" },
      h("h2", { id: "account-title" }, "Account"),
      close,
    ),
    h(
      "div",
      { class: "account-body" },
      nameForm,
      handSection,
      botSection,
      actionsSection,
    ),
  );

  function loading(list: HTMLElement, text: string, retry = false): void {
    const item = h("li", { class: "account-empty" }, text);
    if (retry) {
      const again = h(
        "button",
        { type: "button", class: "secondary small" },
        "Try again",
      );
      again.addEventListener("click", () => void load());
      item.append(" ", again);
    }
    list.replaceChildren(item);
  }

  function draw(): void {
    if (gameDeleted) {
      count.textContent = "";
      const deleted = (text: string) =>
        h("li", { class: "account-empty" }, text);
      handList.replaceChildren(deleted("Deleted, with its rating."));
      botList.replaceChildren(deleted("Deleted, with their ratings."));
      addInputs(true);
      addHint.textContent = nameRule;
      return;
    }
    if (!account) {
      count.textContent = "";
      addInputs(true);
      addHint.textContent = nameRule;
      return;
    }
    const view = accountView(account, adding);
    count.textContent = view.count;
    handList.replaceChildren(competitorItem(view.hand));
    botList.replaceChildren(
      ...view.bots.map((row, index) => {
        const competitor = account?.bot_names[index];
        const remove = h(
          "button",
          { type: "button", class: "danger small", disabled: busy },
          "Delete",
        );
        remove.setAttribute("aria-label", `Delete ${row.name}`);
        if (competitor) {
          remove.addEventListener("click", () => void removeBot(competitor));
        }
        return competitorItem(
          row,
          remove,
          competitor && editing === competitor.id
            ? editForm
            : urlLine(row, competitor),
          pausedLine(row, competitor),
        );
      }),
    );
    editInput.disabled = savingUrl || busy;
    editSave.disabled = savingUrl || busy;
    editCancel.disabled = savingUrl;
    if (view.bots.length === 0) {
      botList.replaceChildren(
        h("li", { class: "account-empty" }, "No bot names yet."),
      );
    }
    addInputs(view.add.disabled || busy);
    addHint.replaceChildren(
      ...(view.add.limit ? [h("strong", {}, view.add.limit), " "] : []),
      view.add.hint,
    );
  }

  /**
   * A name's URL as text, never a link, with **Change URL**; a name registered before names had
   * URLs says what to do, with **Add URL**.
   */
  function urlLine(
    row: CompetitorRow,
    competitor: Competitor | undefined,
  ): HTMLElement {
    const change = h(
      "button",
      { type: "button", class: "link small", disabled: busy || savingUrl },
      row.url ? "Change URL" : "Add URL",
    );
    change.setAttribute(
      "aria-label",
      `${row.url ? "Change" : "Add"} the URL of ${row.name}`,
    );
    if (competitor) {
      change.addEventListener("click", () => startEditing(competitor));
    }
    return h(
      "div",
      { class: "account-url" },
      row.url
        ? h("span", { class: "account-url-text" }, row.url)
        : h("span", { class: "account-url-missing" }, noBotUrlSentence),
      change,
    );
  }

  /**
   * A paused bot name's reason, with **Resume** (#412): it missed a turn while it played away,
   * and plays away again once resumed. Null for a name that isn't paused.
   */
  function pausedLine(
    row: CompetitorRow,
    competitor: Competitor | undefined,
  ): HTMLElement | null {
    if (!row.paused) return null;
    const resume = h(
      "button",
      { type: "button", class: "secondary small", disabled: busy },
      "Resume",
    );
    resume.setAttribute("aria-label", `Resume ${row.name}`);
    if (competitor) {
      resume.addEventListener("click", () => void resumeBot(competitor));
    }
    return h(
      "div",
      { class: "account-paused" },
      h("span", { class: "account-paused-text" }, row.paused),
      resume,
    );
  }

  async function resumeBot(competitor: Competitor): Promise<void> {
    addError.textContent = "";
    setBusy(true);
    try {
      const resumed = await service.resumeBotName(competitor.id);
      if (account) account = withChangedBotName(account, resumed);
    } catch (error) {
      failed(error, addError, accountProblem(error));
    } finally {
      setBusy(false);
    }
  }

  function startEditing(competitor: Competitor): void {
    editing = competitor.id;
    editInput.value = competitor.url ?? "";
    editInput.setAttribute("aria-label", `The URL of ${competitor.name}`);
    editHint.textContent = botUrlEditHint(competitor);
    editError.textContent = "";
    draw();
    editInput.focus();
  }

  function stopEditing(): void {
    editing = null;
    editError.textContent = "";
    draw();
  }

  function addInputs(disabled: boolean): void {
    botInput.disabled = disabled;
    urlInput.disabled = disabled;
    addButton.disabled = disabled;
  }

  function setBusy(next: boolean): void {
    busy = next;
    // After the game's part is deleted, a rename would read the game's account, which makes it again.
    saveButton.disabled = next || gameDeleted;
    signOutButton.disabled = next;
    deleteButton.disabled = next;
    draw();
  }

  /**
   * A failure that ended the session closes the page; the service has ended the realtime
   * session, so the silent sign-in or the name screen follows.
   */
  function failed(error: unknown, shown: HTMLElement, sentence: string): void {
    if (endsSession(error)) {
      dialog.close();
      return;
    }
    shown.textContent = sentence;
  }

  async function load(): Promise<void> {
    loading(handList, "Loading your ratings…");
    loading(botList, "Loading your bot names…");
    try {
      account = await service.read();
      draw();
    } catch (error) {
      if (endsSession(error)) {
        dialog.close();
        return;
      }
      const sentence = accountProblem(error);
      loading(handList, sentence, true);
      botList.replaceChildren();
    }
  }

  nameForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    nameStatus.textContent = "";
    const problem = validateAccountName(nameInput.value);
    if (problem) {
      nameError.textContent = problem;
      nameInput.focus();
      return;
    }
    nameError.textContent = "";
    setBusy(true);
    try {
      account = await service.rename(nameInput.value.trim());
      nameInput.value = account.display_name;
      nameStatus.textContent = "Saved.";
    } catch (error) {
      failed(error, nameError, displayNameProblem(error));
    } finally {
      setBusy(false);
    }
  });

  addForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    if (!account) return;
    const problem = validateBotName(botInput.value);
    if (problem) {
      addError.textContent = problem;
      botInput.focus();
      return;
    }
    const urlProblem = validateBotUrl(urlInput.value);
    if (urlProblem) {
      addError.textContent = urlProblem;
      urlInput.focus();
      return;
    }
    addError.textContent = "";
    adding = true;
    draw();
    try {
      const competitor = await service.addBotName(
        botInput.value.trim(),
        urlInput.value.trim(),
      );
      if (account) account = withBotName(account, competitor);
      botInput.value = "";
      urlInput.value = "";
    } catch (error) {
      failed(error, addError, botNameProblem(error));
    } finally {
      adding = false;
      draw();
      if (!botInput.disabled) botInput.focus();
    }
  });

  editForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    const competitorId = editing;
    if (!account || !competitorId || savingUrl) return;
    const problem = validateBotUrl(editInput.value);
    if (problem) {
      editError.textContent = problem;
      editInput.focus();
      return;
    }
    editError.textContent = "";
    savingUrl = true;
    draw();
    try {
      const changed = await service.setBotNameUrl(
        competitorId,
        editInput.value.trim(),
      );
      if (account) account = withChangedBotName(account, changed);
      editing = null;
    } catch (error) {
      failed(error, editError, botNameProblem(error));
    } finally {
      savingUrl = false;
      draw();
      if (editing) editInput.focus();
    }
  });
  editCancel.addEventListener("click", stopEditing);
  editInput.addEventListener("keydown", (event) => {
    if (event.key === "Escape") {
      // Escape cancels the change, not the whole page.
      event.preventDefault();
      event.stopPropagation();
      stopEditing();
    }
  });

  async function removeBot(competitor: Competitor): Promise<void> {
    if (!(await confirm(deleteBotNameConfirmation(competitor)))) return;
    addError.textContent = "";
    setBusy(true);
    try {
      await service.deleteBotName(competitor.id);
      if (account) account = withoutBotName(account, competitor.id);
      if (editing === competitor.id) editing = null;
    } catch (error) {
      failed(error, addError, accountProblem(error));
    } finally {
      setBusy(false);
    }
  }

  signOutButton.addEventListener("click", () => options.onSignOut());

  deleteButton.addEventListener("click", async () => {
    if (!(await confirm(deleteAccountConfirmation(account)))) return;
    accountError.textContent = "";
    setBusy(true);
    try {
      await service.deleteAccount({
        gameDeleted() {
          options.onForgetGoogle();
          gameDeleted = true;
          account = null;
        },
        signOut: () => options.onSignOut(),
      });
    } catch (error) {
      failed(
        error,
        accountError,
        gameDeleted ? partlyDeletedProblem(error) : accountProblem(error),
      );
      setBusy(false);
    }
  });

  close.addEventListener("click", () => dialog.close());
  dialog.addEventListener("click", (event) => {
    // A click on the backdrop lands on the dialog itself.
    if (event.target === dialog) dialog.close();
  });
  const stopSession = realtime.session.subscribe((session) => {
    if (!session) dialog.close();
  });
  dialog.addEventListener("close", () => {
    stopSession();
    dialog.remove();
  });
  document.body.append(dialog);
  draw();
  dialog.showModal();
  close.focus();
  void load();
  return dialog;
}
