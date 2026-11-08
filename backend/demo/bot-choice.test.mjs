import { test } from "node:test";
import assert from "node:assert/strict";
import { choiceFromValue, choiceValue, describeBot, describeChoices, prepareDemo, seatOptions } from "./bot-choice.mjs";

test("select values stand for choices and back", () => {
  for (const choice of [{ kind: "localhost" }, { kind: "default", level: 1 }, { kind: "default", level: 3 }]) {
    assert.deepEqual(choiceFromValue(choiceValue(choice)), choice);
  }
  assert.equal(choiceValue(null), "");
  assert.equal(choiceFromValue(""), null);
  assert.equal(choiceFromValue("level-0"), null);
  assert.equal(choiceFromValue("level-x"), null);
});

test("every member's choice is named", () => {
  assert.equal(describeBot(undefined), "no bot yet");
  const members = [
    { player_id: "a", display_name: "Alex", bot: { kind: "localhost" } },
    { player_id: "s", display_name: "Sam", bot: { kind: "default", level: 2 } },
    { player_id: "k", display_name: "Kim" },
  ];
  assert.equal(describeChoices(members, "s"), "Alex: own bot (localhost) · Sam (you): level 2 bot · Kim: no bot yet");
});

test("a demo player chooses the localhost bot, which leaves its turns to the page", async () => {
  const calls = [];
  await prepareDemo({ lobbyID: "lobby-1", request: async (...call) => { calls.push(call); return { status: 200, body: {} }; } });
  assert.deepEqual(calls, [["PUT", "/game/lobbies/lobby-1/bot", { kind: "localhost" }]]);
  await assert.rejects(
    prepareDemo({ lobbyID: "lobby-1", request: async () => ({ status: 409, body: { detail: "the lobby has started" } }) }),
    /the lobby has started/,
  );
});

test("the host can seat a default bot of each level", async () => {
  assert.deepEqual(await seatOptions({ levels: async () => [1, 2] }), {
    label: "Add bot",
    choices: [
      { label: "Default bot, level 1", settings: { bot_level: 1 } },
      { label: "Default bot, level 2", settings: { bot_level: 2 } },
    ],
  });
});
