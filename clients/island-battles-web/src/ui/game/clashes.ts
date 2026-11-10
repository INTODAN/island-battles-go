// The one-line summary of a turn's clashes, as the 2D replay shows it under the outcome
// (`#clashes` in backend/services/replay.html). Pure, so `bun test` covers it.

import { teamName } from "../lobby/form";
import type { Generation } from "./history";

/**
 * "Clash: Blue 5 → 3 · Red 4 → 0" for each clash in the turn, joined with ". ", where each side
 * shows its soldiers before and after the contact (0 after: the group was stopped). An empty
 * string when nothing clashed. A clash without participants is skipped, as the replay does.
 */
export function clashSummary(frame: Pick<Generation, "clashes">): string {
  return frame.clashes
    .filter((clash) => clash.participants.length > 0)
    .map(
      (clash) =>
        `Clash: ${clash.participants
          .map(
            (side) =>
              `${teamName(side.team_color)} ${side.soldiers_before} → ${side.soldiers_after}`,
          )
          .join(" · ")}`,
    )
    .join(". ");
}
