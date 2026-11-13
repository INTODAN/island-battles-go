// The lobby's bot packages in the bot choice, against a minimal stand-in for
// the DOM and the page's room requests.
import assert from "node:assert/strict";
import { setTimeout as delay } from "node:timers/promises";
import test from "node:test";

class FakeElement {
  constructor(tag) {
    this.tag = tag;
    this.children = [];
    this.listeners = {};
    this.attributes = {};
    this.hidden = false;
    this.disabled = false;
    this.parent = null;
    this.text = "";
    this.value = "";
  }
  append(...nodes) {
    for (const node of nodes) {
      if (node instanceof FakeElement) node.parent = this;
      this.children.push(node);
    }
  }
  replaceChildren(...nodes) {
    this.children = [];
    this.append(...nodes);
  }
  setAttribute(name, value) { this.attributes[name] = value; }
  addEventListener(type, listener) { this.listeners[type] = listener; }
  click() { this.listeners.click?.(); }
  set textContent(value) { this.text = value; }
  get textContent() {
    return this.text + this.children.map((child) => (child instanceof FakeElement ? child.textContent : String(child))).join("");
  }
  closest() {
    for (let element = this; element; element = element.parent) if (element.hidden) return element;
    return null;
  }
  findAll(predicate) {
    const found = predicate(this) ? [this] : [];
    for (const child of this.children) if (child instanceof FakeElement) found.push(...child.findAll(predicate));
    return found;
  }
}

globalThis.document = { createElement: (tag) => new FakeElement(tag) };

const { PackagesPanel, describePackage, maxZipBytes, uploadCommands, uploadPackage, storageProblem } = await import("./bot-packages.mjs");

const lobbyID = "6f1c2c9e-7d0a-4a43-9c55-6e3f0a1b2c3d";
const base = `/game/lobbies/${lobbyID}/bot-packages`;
const alexPackage = { package_id: "p1", owner_player_id: "alex", owner_name: "Alex", language: "python", entry_or_command: "bot:play", size_bytes: 1572864, uploaded_at: "2026-11-12T12:00:00Z" };
const samPackage = { package_id: "p2", owner_player_id: "sam", owner_name: "Sam", language: "native", entry_or_command: "./bot worker", size_bytes: 3145728, uploaded_at: "2026-11-12T11:00:00Z" };
const zip = { name: "island-battles-bot.zip", size: 1572864 };

// A game that answers the room requests, and records them.
function game({ complete = { status: 200, body: alexPackage }, packages = [alexPackage, samPackage] } = {}) {
  const calls = [];
  const request = async (method, path, body) => {
    calls.push([method, path, body]);
    if (method === "GET" && path === base) return { status: 200, body: { packages } };
    if (method === "POST" && path === base) return { status: 201, body: { package_id: "p1", upload: { url: "https://storage.example/bucket", fields: { key: "k", policy: "p" } } } };
    if (method === "POST" && path === `${base}/p1/complete`) return complete;
    if (method === "POST" && path === `${base}/upload-token`) return { status: 200, body: { token: "ibut1.abc.def", expires_at: "2026-11-12T12:15:00Z" } };
    if (method === "DELETE") return { status: 204, body: null };
    return { status: 404, body: { detail: "not found" } };
  };
  return { calls, request };
}

test("a package is described by its language, entry or command, and size", () => {
  assert.equal(describePackage(alexPackage), "python · bot:play · 1.5 MiB");
  assert.equal(describePackage(samPackage), "native · ./bot worker · 3.0 MiB");
});

test("the commands upload with the token in both SDKs", () => {
  const commands = uploadCommands("https://game.example", "ibut1.a.b");
  assert.equal(commands.python, "uv run island-battles-bot upload --game-url https://game.example --token ibut1.a.b dist/island-battles-bot.zip");
  assert.match(commands.go, /^go run github\.com\/INTODAN\/island-battles-go\/clients\/island-battles-go\/cmd\/island-battles-package upload --game-url https:\/\/game\.example --token ibut1\.a\.b dist\/island-battles-bot\.zip$/);
});

test("an upload starts, posts the zip to storage and completes", async () => {
  const { calls, request } = game();
  const posted = [];
  const progress = [];
  const uploaded = await uploadPackage({
    lobbyID, file: zip, request,
    post: async (url, fields, file, onProgress) => { posted.push([url, fields, file]); onProgress(0.5); onProgress(1); },
    onProgress: (fraction) => progress.push(fraction),
  });
  assert.deepEqual(uploaded, alexPackage);
  assert.deepEqual(calls, [["POST", base, { file_name: "island-battles-bot.zip" }], ["POST", `${base}/p1/complete`, undefined]]);
  assert.deepEqual(posted, [["https://storage.example/bucket", { key: "k", policy: "p" }, zip]]);
  assert.deepEqual(progress, [0, 0.5, 1]);
});

test("the game's reason for a bad package is the error", async () => {
  const { request } = game({ complete: { status: 422, body: { detail: "the package was refused: the zip has no island-battles-bot.json at its root." } } });
  await assert.rejects(uploadPackage({ lobbyID, file: zip, request, post: async () => {} }), /has no island-battles-bot\.json at its root/);
});

test("a file that can't be a package is refused before anything is sent", async () => {
  const { calls, request } = game();
  await assert.rejects(uploadPackage({ lobbyID, file: { name: "bot.tar.gz", size: 10 }, request }), /Choose the \.zip/);
  await assert.rejects(uploadPackage({ lobbyID, file: { name: "bot.zip", size: 0 }, request }), /is empty/);
  await assert.rejects(uploadPackage({ lobbyID, file: { name: "bot.zip", size: maxZipBytes + 1 }, request }), /the limit is 250\.0 MiB/);
  assert.equal(calls.length, 0);
});

test("storage's own refusal is read from its XML", () => {
  assert.equal(storageProblem("<Error><Code>EntityTooLarge</Code><Message>Too big</Message></Error>", 400), "EntityTooLarge: Too big");
  assert.equal(storageProblem("", 403), "HTTP 403");
});

function panel(options = {}) {
  const { calls, request } = game(options);
  const container = new FakeElement("div");
  const copied = [];
  const posted = [];
  const instance = new PackagesPanel({
    container, lobbyID, playerID: "alex", request, gameURL: "https://game.example",
    post: async (url, fields, file, onProgress) => { posted.push(file); onProgress(1); },
    copy: async (text) => { copied.push(text); },
  });
  return { instance, container, calls, copied, posted };
}

const buttons = (container, text) => container.findAll((element) => element.tag === "button" && element.textContent === text);

test("the list shows every package, with Delete on your own only", async () => {
  const { instance, container } = panel();
  await delay(5);
  const items = instance.list.children.map((item) => item.textContent);
  assert.deepEqual(items, ["Alex (you): python · bot:play · 1.5 MiB Delete", "Sam: native · ./bot worker · 3.0 MiB"]);
  assert.equal(buttons(container, "Delete").length, 1);
  assert.equal(buttons(container, "Upload bot (.zip)").length, 1);
});

test("an empty lobby says it has no packages", async () => {
  const { instance } = panel({ packages: [] });
  await delay(5);
  assert.deepEqual(instance.list.children.map((item) => item.textContent), ["No packages yet."]);
});

test("uploading shows the result and reads the list again", async () => {
  const { instance, calls, posted } = panel();
  await instance.upload(zip);
  assert.equal(instance.status.textContent, "Uploaded island-battles-bot.zip: python · bot:play · 1.5 MiB.");
  assert.deepEqual(posted, [zip]);
  assert.equal(calls.filter(([method, path]) => method === "GET" && path === base).length, 2);
  assert.equal(instance.progress.hidden, true);
  assert.equal(instance.uploadButton.disabled, false);
});

test("a refused upload shows the game's reason", async () => {
  const { instance } = panel({ complete: { status: 422, body: { detail: "the package was refused: the entry \"bot\" must be module:function." } } });
  await instance.upload(zip);
  assert.equal(instance.status.textContent, "the package was refused: the entry \"bot\" must be module:function.");
});

test("Delete deletes your package", async () => {
  const { container, calls } = panel();
  await delay(5);
  buttons(container, "Delete")[0].click();
  await delay(5);
  assert.deepEqual(calls.find(([method]) => method === "DELETE"), ["DELETE", `${base}/p1`, undefined]);
});

test("Upload from the command line shows both commands with a fresh token", async () => {
  const { container, instance, calls, copied } = panel();
  await instance.showCommand();
  assert.ok(calls.some(([method, path]) => method === "POST" && path === `${base}/upload-token`));
  assert.equal(instance.command.hidden, false);
  const text = instance.command.textContent;
  assert.match(text, /Python, in your bot's folder: uv run island-battles-bot upload --game-url https:\/\/game\.example --token ibut1\.abc\.def dist\/island-battles-bot\.zip/);
  assert.match(text, /Go, in your bot's folder: go run .*island-battles-package upload --game-url https:\/\/game\.example --token ibut1\.abc\.def/);
  await buttons(container, "Copy")[0].listeners.click();
  assert.deepEqual(copied, [uploadCommands("https://game.example", "ibut1.abc.def").python]);
});
