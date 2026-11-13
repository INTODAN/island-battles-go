/**
 * The monthly guard on uploaded bots (`GET /game/bot-compute`): the server's bot runners have a
 * monthly compute budget, and once they have used it no game with an uploaded bot starts until
 * the first of the next month (UTC). The bot picker and the host's Add bot grey out uploaded
 * bots then, with the sentence that the server's refused start also gives.
 */

import type { components } from "../api/generated/game";

export type BotCompute = components["schemas"]["BotCompute"];

const monthNames = [
  "January",
  "February",
  "March",
  "April",
  "May",
  "June",
  "July",
  "August",
  "September",
  "October",
  "November",
  "December",
];

/**
 * Why uploaded bots can't be chosen, such as "Uploaded bots have used this month's free
 * compute. They're back on 1 December.", or null while they can (or before the server has
 * said).
 */
export function computeSpentMessage(
  compute: Pick<BotCompute, "month" | "available"> | null | undefined,
): string | null {
  if (!compute || compute.available) return null;
  const month = Number(/^\d{4}-(\d{2})$/.exec(compute.month)?.[1]);
  const back =
    month >= 1 && month <= 12 ? `on 1 ${monthNames[month % 12]}` : "next month";
  return `Uploaded bots have used this month's free compute. They're back ${back}.`;
}

/** Whether the bot picker greys out a choice, by its value: your packages, once their compute is spent. */
export function uploadedGreyedOut(
  value: string,
  spent: string | null,
): boolean {
  return spent !== null && value.startsWith("package-");
}

/** A choice of a bot select: its value, its label and whether it is greyed out. */
export interface BotOption {
  value: string;
  label: string;
  disabled: boolean;
}

/**
 * The host's Add bot choices for an empty team: a default bot of each level and, when the
 * server offers them, each of the lobby's packages, greyed out once their compute is spent,
 * and a hosted bot.
 */
export function seatOptions(options: {
  levels: number[];
  serverBots: boolean;
  packages: { package_id: string; owner_name: string; language: string }[];
  spent: string | null;
}): BotOption[] {
  const choices: BotOption[] = options.levels.map((level) => ({
    value: `level-${level}`,
    label: `Bot level ${level}`,
    disabled: false,
  }));
  if (!options.serverBots) return choices;
  for (const item of options.packages) {
    choices.push({
      value: `package-${item.package_id}`,
      label: `Uploaded: ${item.owner_name}'s bot (${item.language})`,
      disabled: options.spent !== null,
    });
  }
  choices.push({ value: "hosted", label: "Hosted bot (URL)", disabled: false });
  return choices;
}

/** The choice a select shows: `wanted` while it is offered and not greyed out, or else the first. */
export function offeredChoice(choices: BotOption[], wanted: string): string {
  const usable = choices.filter((choice) => !choice.disabled);
  return usable.some((choice) => choice.value === wanted)
    ? wanted
    : (usable[0]?.value ?? "");
}
