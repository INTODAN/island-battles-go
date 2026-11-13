// The lobby's bot packages in the waiting room's bot choice: any member
// uploads the zip that the SDKs' package command builds, from this page or
// from the command line, and everyone sees the lobby's packages. The page
// calls the game's bot package endpoints through the lobby page's room
// requests, which add the player's token for the game, and posts the zip itself
// straight to storage with the presigned POST, which needs none. Choosing a
// package as a bot comes later; here it is only uploaded, listed and deleted.

// The zip's limit, in binary units as the SDKs' package commands check it.
export const maxZipBytes = 250 * 1024 * 1024;

// How often the list is read again while the room is shown, so packages that
// other members upload appear.
export const refreshMilliseconds = 10000;

// The game's bot package endpoints for a lobby.
export function packagesPath(lobbyID) {
  return `/game/lobbies/${encodeURIComponent(lobbyID)}/bot-packages`;
}

// A size in MiB, as the SDKs print it.
export function mebibytes(size) {
  return `${(size / (1024 * 1024)).toFixed(1)} MiB`;
}

// A few words about a package, such as "python · bot:play · 1.2 MiB".
export function describePackage(item) {
  return `${item.language} · ${item.entry_or_command} · ${mebibytes(item.size_bytes)}`;
}

// The commands that upload a package with an upload token, for the Python
// and the Go SDK.
export function uploadCommands(gameURL, token) {
  return {
    python: `uv run island-battles-bot upload --game-url ${gameURL} --token=${token} dist/island-battles-bot.zip`,
    go: `go run github.com/INTODAN/island-battles-go/clients/island-battles-go/cmd/island-battles-package upload --game-url ${gameURL} --token=${token} dist/island-battles-bot.zip`,
  };
}

// The message of a refused room request: the game's detail, or the status.
function refusal(answer, fallback) {
  const detail = answer?.body?.detail;
  return new Error(typeof detail === "string" && detail ? detail : `${fallback} (HTTP ${answer?.status ?? "?"}).`);
}

// postToStorage posts the presigned form, fields first and the file last,
// and reports the upload's progress as a fraction from 0 to 1. Storage
// answers with no JSON, so only its status matters.
export function postToStorage(url, fields, file, onProgress) {
  return new Promise((resolve, reject) => {
    const form = new FormData();
    for (const [name, value] of Object.entries(fields)) form.append(name, value);
    form.append("file", file);
    const request = new XMLHttpRequest();
    request.open("POST", url);
    request.upload.addEventListener("progress", (event) => {
      if (event.lengthComputable) onProgress(event.loaded / event.total);
    });
    request.addEventListener("load", () => {
      if (request.status >= 200 && request.status < 300) resolve();
      else reject(new Error(`Storage refused the zip: ${storageProblem(request.responseText, request.status)}`));
    });
    request.addEventListener("error", () => reject(new Error("Could not send the zip to storage. Check your connection and try again.")));
    request.send(form);
  });
}

// storageProblem is S3's own code and message, from its XML.
export function storageProblem(text, status) {
  const found = [...String(text || "").matchAll(/<(Code|Message)>([^<]*)<\/(?:Code|Message)>/g)].slice(0, 2).map((match) => match[2]);
  return found.length ? found.join(": ") : `HTTP ${status}`;
}

// uploadPackage uploads a zip to the lobby and resolves to the package, or
// rejects with an Error that says what is wrong: the game's own reason for a
// package it refuses. request is the room's request(method, path, body).
export async function uploadPackage({ lobbyID, file, request, post = postToStorage, onProgress = () => {} }) {
  if (!/\.zip$/i.test(file.name)) throw new Error("Choose the .zip that the SDK's package command writes, such as dist/island-battles-bot.zip.");
  if (file.size === 0) throw new Error(`${file.name} is empty.`);
  if (file.size > maxZipBytes) throw new Error(`${file.name} is ${mebibytes(file.size)}; the limit is ${mebibytes(maxZipBytes)}.`);
  const started = await request("POST", packagesPath(lobbyID), { file_name: file.name });
  if (started.status !== 201) throw refusal(started, "Could not start the upload");
  onProgress(0);
  await post(started.body.upload.url, started.body.upload.fields, file, onProgress);
  const completed = await request("POST", `${packagesPath(lobbyID)}/${encodeURIComponent(started.body.package_id)}/complete`);
  if (completed.status !== 200) throw refusal(completed, "Could not check the package");
  return completed.body;
}

// The list of the lobby's packages, with the upload controls above it.
export class PackagesPanel {
  constructor({ container, lobbyID, playerID, request, gameURL = globalThis.location?.origin ?? "", post = postToStorage, copy = (text) => navigator.clipboard.writeText(text) }) {
    this.container = container;
    this.lobbyID = lobbyID;
    this.playerID = playerID;
    this.request = request;
    this.gameURL = gameURL;
    this.post = post;
    this.copy = copy;
    this.packages = [];
    this.busy = false;
    this.timer = null;

    const box = document.createElement("div");
    box.className = "local-bot bot-packages";
    const title = document.createElement("p");
    title.className = "hint";
    title.textContent = "Bot packages of this lobby: the zip that island-battles-bot package or island-battles-package builds. Choosing one as your bot comes later.";
    this.file = document.createElement("input");
    this.file.type = "file";
    this.file.accept = ".zip,application/zip";
    this.file.hidden = true;
    this.file.addEventListener("change", () => {
      const chosen = this.file.files?.[0];
      this.file.value = "";
      if (chosen) void this.upload(chosen);
    });
    this.uploadButton = document.createElement("button");
    this.uploadButton.type = "button";
    this.uploadButton.textContent = "Upload bot (.zip)";
    this.uploadButton.addEventListener("click", () => this.file.click());
    this.commandButton = document.createElement("button");
    this.commandButton.type = "button";
    this.commandButton.className = "secondary";
    this.commandButton.textContent = "Upload from the command line";
    this.commandButton.addEventListener("click", () => void this.showCommand());
    const actions = document.createElement("p");
    actions.append(this.uploadButton, " ", this.commandButton, this.file);
    this.progress = document.createElement("progress");
    this.progress.max = 1;
    this.progress.hidden = true;
    this.status = document.createElement("p");
    this.status.className = "hint";
    this.status.setAttribute("role", "status");
    this.command = document.createElement("div");
    this.command.hidden = true;
    this.list = document.createElement("ul");
    this.list.className = "bot-package-list";
    box.append(title, actions, this.progress, this.status, this.command, this.list);
    container.append(box);
    void this.refresh();
  }

  setStatus(text) {
    this.status.textContent = text;
  }

  // Reads the lobby's packages again and shows them, newest first.
  async refresh() {
    try {
      const answer = await this.request("GET", packagesPath(this.lobbyID));
      if (answer.status !== 200) throw refusal(answer, "Could not read the lobby's packages");
      this.show(answer.body.packages);
    } catch (error) {
      this.list.replaceChildren?.();
      this.setStatus(error.message);
    }
  }

  show(packages) {
    this.packages = packages;
    const items = packages.map((item) => {
      const entry = document.createElement("li");
      const mine = item.owner_player_id === this.playerID;
      entry.append(`${item.owner_name}${mine ? " (you)" : ""}: ${describePackage(item)}`);
      if (mine) {
        const remove = document.createElement("button");
        remove.type = "button";
        remove.className = "secondary";
        remove.textContent = "Delete";
        remove.addEventListener("click", () => void this.remove(item));
        entry.append(" ", remove);
      }
      return entry;
    });
    if (items.length === 0) {
      const none = document.createElement("li");
      none.className = "hint";
      none.textContent = "No packages yet.";
      items.push(none);
    }
    this.list.replaceChildren(...items);
  }

  setBusy(busy) {
    this.busy = busy;
    this.uploadButton.disabled = busy;
    this.commandButton.disabled = busy;
  }

  async upload(file) {
    if (this.busy) return;
    this.setBusy(true);
    this.setStatus(`Uploading ${file.name}…`);
    this.progress.hidden = false;
    this.progress.value = 0;
    try {
      const uploaded = await uploadPackage({
        lobbyID: this.lobbyID, file, request: this.request, post: this.post,
        onProgress: (fraction) => {
          this.progress.value = fraction;
          if (fraction >= 1) this.setStatus("Checking the package…");
        },
      });
      this.setStatus(`Uploaded ${file.name}: ${describePackage(uploaded)}.`);
      await this.refresh();
    } catch (error) {
      this.setStatus(error.message || "The upload failed. Try again.");
    } finally {
      this.progress.hidden = true;
      this.setBusy(false);
    }
  }

  async remove(item) {
    if (this.busy) return;
    this.setBusy(true);
    try {
      const answer = await this.request("DELETE", `${packagesPath(this.lobbyID)}/${encodeURIComponent(item.package_id)}`);
      if (answer.status !== 204) throw refusal(answer, "Could not delete the package");
      this.setStatus("Deleted your package.");
      await this.refresh();
    } catch (error) {
      this.setStatus(error.message);
    } finally {
      this.setBusy(false);
    }
  }

  // Asks for an upload token and shows the commands that use it.
  async showCommand() {
    if (this.busy) return;
    this.setBusy(true);
    try {
      const answer = await this.request("POST", `${packagesPath(this.lobbyID)}/upload-token`);
      if (answer.status !== 200) throw refusal(answer, "Could not get an upload token");
      const commands = uploadCommands(this.gameURL, answer.body.token);
      const expires = new Date(answer.body.expires_at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
      const lines = [["Python", commands.python], ["Go", commands.go]].map(([label, text]) => {
        const line = document.createElement("p");
        line.className = "hint";
        const code = document.createElement("code");
        code.textContent = text;
        const copy = document.createElement("button");
        copy.type = "button";
        copy.className = "secondary";
        copy.textContent = "Copy";
        copy.addEventListener("click", async () => {
          try {
            await this.copy(text);
            copy.textContent = "Copied";
          } catch {
            copy.textContent = "Select the command to copy it";
          }
        });
        line.append(`${label}, in your bot's folder: `, code, " ", copy);
        return line;
      });
      const note = document.createElement("p");
      note.className = "hint";
      note.textContent = `The token in these commands uploads to this lobby as you until ${expires}.`;
      this.command.replaceChildren(...lines, note);
      this.command.hidden = false;
    } catch (error) {
      this.setStatus(error.message);
    } finally {
      this.setBusy(false);
    }
  }

  // Reads the list again every refreshMilliseconds while the room is shown.
  follow() {
    if (this.timer !== null) return;
    this.timer = setInterval(() => {
      if (this.container.closest("[hidden]")) return this.stop();
      if (!this.busy) void this.refresh();
    }, refreshMilliseconds);
  }

  stop() {
    clearInterval(this.timer);
    this.timer = null;
  }
}
