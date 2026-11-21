// The account page (#369, option A of the mockups): a dialog like the Game rules page, opened from
// the menu's Account link. It draws `accountModel.ts` and sends `accountService.ts`'s requests.

import type { Realtime } from "../../realtime";
import { h } from "../dom";
import {
  accountProblem,
  accountView,
  botNameProblem,
  type Competitor,
  type Confirmation,
  deleteAccountConfirmation,
  deleteBotNameConfirmation,
  displayNameProblem,
  endsSession,
  type IslandAccount,
  nameRule,
  partlyDeletedProblem,
  validateAccountName,
  validateBotName,
  withBotName,
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
  row: { name: string; rating: string; games: string },
  action?: HTMLElement,
): HTMLElement {
  return h(
    "li",
    { class: action ? "" : "hand" },
    h("span", { class: "account-name" }, row.name),
    h("span", { class: "account-rating" }, row.rating),
    h("span", { class: "account-games" }, row.games),
    action ?? null,
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
  const addButton = h("button", { type: "submit", class: "primary" }, "Add");
  const addHint = h("p", { class: "hint" });
  const addError = h("p", { class: "field-error", attrs: { role: "alert" } });
  const addForm = h(
    "form",
    { class: "account-add", noValidate: true },
    h("label", { htmlFor: "new-bot-name" }, "New bot name"),
    h("div", { class: "account-inline" }, botInput, addButton),
    addHint,
    addError,
  );
  const botSection = h(
    "section",
    { class: "account-section" },
    h("h3", {}, "Bot names ", count),
    h(
      "p",
      { class: "hint" },
      "Choose one of these where you choose your bot in a rated lobby. Each name has its own rating; the code behind it can change.",
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
        return competitorItem(row, remove);
      }),
    );
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

  function addInputs(disabled: boolean): void {
    botInput.disabled = disabled;
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
    addError.textContent = "";
    adding = true;
    draw();
    try {
      // The page has no URL field yet (#404), so the game refuses the name
      // with its 422, which addError shows.
      const competitor = await service.addBotName(botInput.value.trim(), "");
      if (account) account = withBotName(account, competitor);
      botInput.value = "";
    } catch (error) {
      failed(error, addError, botNameProblem(error));
    } finally {
      adding = false;
      draw();
      if (!botInput.disabled) botInput.focus();
    }
  });

  async function removeBot(competitor: Competitor): Promise<void> {
    if (!(await confirm(deleteBotNameConfirmation(competitor)))) return;
    addError.textContent = "";
    setBusy(true);
    try {
      await service.deleteBotName(competitor.id);
      if (account) account = withoutBotName(account, competitor.id);
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
