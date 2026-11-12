import { test } from "node:test";
import assert from "node:assert/strict";
import { choiceFromValue, choiceValue, describeBot, describeChoices, memberBot, prepareDemo, seatOptions } from "./bot-choice.mjs";

test("select values stand for choices and back", () => {
  for (const choice of [{ kind: "localhost" }, { kind: "default", level: 1 }, { kind: "default", level: 3 }]) {
    assert.deepEqual(choiceFromValue(choiceValue(choice)), choice);
  }
  assert.equal(choiceValue(null), "");
  assert.equal(choiceFromValue(""), null);
  assert.equal(choiceFromValue("level-0"), null);
  assert.equal(choiceFromValue("level-x"), null);
});

test("every member's choice is named, from the snapshot's members", () => {
  assert.equal(describeBot(undefined), "no bot yet");
  const members = [
    { player_id: "a", display_name: "Alex", kind: "human", seat_settings: null, member_settings: { bot: { kind: "localhost" } } },
    { player_id: "s", display_name: "Sam", kind: "human", seat_settings: null, member_settings: { bot: { kind: "default", level: 2 } } },
    { player_id: "k", display_name: "Kim", kind: "human", seat_settings: null, member_settings: null },
    { player_id: "b", display_name: "Bot (level 3)", kind: "seat", seat_settings: { bot_level: 3 }, member_settings: null },
  ];
  assert.equal(describeChoices(members, "s"), "Alex: own bot (localhost) · Sam (you): level 2 bot · Kim: no bot yet · Bot (level 3): level 3 bot");
  assert.deepEqual(members.map(memberBot), [{ kind: "localhost" }, { kind: "default", level: 2 }, null, { kind: "default", level: 3 }]);
  assert.equal(memberBot({ kind: "human", member_settings: { other: true } }), null);
});

test("a demo player chooses the localhost bot, which leaves its turns to the page", async () => {
  const saved = [];
  await prepareDemo({ lobbyID: "lobby-1", saveMemberSettings: async (settings) => { saved.push(settings); return {}; } });
  assert.deepEqual(saved, [{ bot: { kind: "localhost" } }]);
  await assert.rejects(
    prepareDemo({ lobbyID: "lobby-1", saveMemberSettings: async () => { throw new Error("the lobby has started"); } }),
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
