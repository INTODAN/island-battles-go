import { test } from "node:test";
import assert from "node:assert/strict";
import { choiceFromValue, choiceValue, computeSpentMessage, describeBot, describeChoices, memberBot, prepareDemo, seatOptions } from "./bot-choice.mjs";

test("select values stand for choices and back", () => {
  for (const choice of [{ kind: "localhost" }, { kind: "default", level: 1 }, { kind: "default", level: 3 }, { kind: "uploaded", package_id: "p-1" }]) {
    assert.deepEqual(choiceFromValue(choiceValue(choice)), choice);
  }
  assert.equal(choiceValue(null), "");
  assert.equal(choiceFromValue(""), null);
  assert.equal(choiceFromValue("level-0"), null);
  assert.equal(choiceFromValue("level-x"), null);
  assert.equal(choiceFromValue("package-"), null);
  // A hosted bot takes the URL typed next to the select.
  assert.equal(choiceValue({ kind: "hosted", url: "https://bot.example.com/turn" }), "hosted");
  assert.deepEqual(choiceFromValue("hosted", " https://bot.example.com/turn "), { kind: "hosted", url: "https://bot.example.com/turn" });
  assert.equal(choiceFromValue("hosted", " "), null);
});

test("every member's choice is named, from the snapshot's members", () => {
  assert.equal(describeBot(undefined), "no bot yet");
  const members = [
    { player_id: "a", display_name: "Alex", kind: "human", seat_settings: null, member_settings: { bot: { kind: "localhost" } } },
    { player_id: "s", display_name: "Sam", kind: "human", seat_settings: null, member_settings: { bot: { kind: "default", level: 2 } } },
    { player_id: "k", display_name: "Kim", kind: "human", seat_settings: null, member_settings: null },
    { player_id: "b", display_name: "Bot (level 3)", kind: "seat", seat_settings: { bot_level: 3 }, member_settings: null },
    { player_id: "h", display_name: "Hana", kind: "human", seat_settings: null, member_settings: { bot: { kind: "hosted", url: "https://bot.example.com/turn" } } },
    { player_id: "u", display_name: "Uma", kind: "human", seat_settings: null, member_settings: { bot: { kind: "uploaded", package_id: "p-1" } } },
    { player_id: "hs", display_name: "Hosted bot", kind: "seat", seat_settings: { bot_url: "https://seat.example.com/turn" }, member_settings: null },
    { player_id: "us", display_name: "Uma's bot", kind: "seat", seat_settings: { bot_package_id: "p-1" }, member_settings: null },
  ];
  assert.equal(describeChoices(members, "s"), "Alex: own bot (localhost) · Sam (you): level 2 bot · Kim: no bot yet · Bot (level 3): level 3 bot · " +
    "Hana: hosted bot (bot.example.com) · Uma: uploaded bot · Hosted bot: hosted bot (seat.example.com) · Uma's bot: uploaded bot");
  assert.deepEqual(members.map(memberBot), [{ kind: "localhost" }, { kind: "default", level: 2 }, null, { kind: "default", level: 3 },
    { kind: "hosted", url: "https://bot.example.com/turn" }, { kind: "uploaded", package_id: "p-1" },
    { kind: "hosted", url: "https://seat.example.com/turn" }, { kind: "uploaded", package_id: "p-1" }]);
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
  assert.deepEqual(await seatOptions({ options: async () => ({ levels: [1, 2], serverBots: false }), packages: async () => { throw new Error("not read"); } }), {
    label: "Add bot",
    choices: [
      { label: "Default bot, level 1", settings: { bot_level: 1 } },
      { label: "Default bot, level 2", settings: { bot_level: 2 } },
    ],
  });
});

test("with server bots the host can also seat any package of the lobby and a hosted bot", async () => {
  const asked = [];
  const options = await seatOptions({
    lobbyID: "lobby-1", request: "the room's request",
    options: async () => ({ levels: [1], serverBots: true }),
    packages: async (lobbyID, request) => {
      asked.push([lobbyID, request]);
      return [{ package_id: "p-1", owner_name: "Uma", language: "python" }, { package_id: "p-2", owner_name: "Sam", language: "native" }];
    },
    compute: async () => ({ month: "2026-11", available: true }),
  });
  assert.deepEqual(asked, [["lobby-1", "the room's request"]]);
  assert.deepEqual(options, {
    label: "Add bot",
    choices: [
      { label: "Default bot, level 1", settings: { bot_level: 1 } },
      { label: "Uploaded: Uma's bot (python)", settings: { bot_package_id: "p-1" } },
      { label: "Uploaded: Sam's bot (native)", settings: { bot_package_id: "p-2" } },
      { label: "Hosted bot (URL)", settings: {}, input: { name: "bot_url", label: "Bot URL", placeholder: "https://bot.example.com/turn" } },
    ],
  });
  // Packages that can't be read leave the other choices.
  const withoutPackages = await seatOptions({ options: async () => ({ levels: [1], serverBots: true }), packages: async () => { throw new Error("offline"); }, compute: async () => null });
  assert.deepEqual(withoutPackages.choices.map((choice) => choice.label), ["Default bot, level 1", "Hosted bot (URL)"]);
});

test("once the month's compute is spent, the host's packages are greyed out and nothing else", async () => {
  const seat = (compute) => seatOptions({
    options: async () => ({ levels: [1], serverBots: true }),
    packages: async () => [{ package_id: "p-1", owner_name: "Uma", language: "python" }],
    compute,
  });
  const spent = await seat(async () => ({ month: "2026-11", used_gb_seconds: 300000, budget_gb_seconds: 300000, available: false }));
  assert.deepEqual(spent.choices.map((choice) => [choice.label, choice.disabled === true]), [
    ["Default bot, level 1", false], ["Uploaded: Uma's bot (python)", true], ["Hosted bot (URL)", false],
  ]);
  // Available, or unknown, greys out nothing.
  for (const compute of [async () => ({ month: "2026-11", available: true }), async () => null, async () => { throw new Error("offline"); }]) {
    assert.ok((await seat(compute)).choices.every((choice) => choice.disabled === undefined));
  }
});

test("the spent sentence names the first of the next month, as the server's refused start does", () => {
  assert.equal(computeSpentMessage({ month: "2026-11", available: false }), "Uploaded bots have used this month's free compute. They're back on 1 December.");
  assert.equal(computeSpentMessage({ month: "2026-12", available: false }), "Uploaded bots have used this month's free compute. They're back on 1 January.");
  assert.equal(computeSpentMessage({ month: "2026-12", available: true }), null);
  assert.equal(computeSpentMessage(null), null);
});
