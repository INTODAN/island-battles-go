import { IslandBattlesClient } from "../dist/index.js";

// A separate lobby client must keep the guest joined and renew its lease.
const client = new IslandBattlesClient({
  baseUrl: process.env.ISLAND_BASE_URL ?? "http://localhost:8080",
  credential: process.env.ISLAND_GUEST_CREDENTIAL ?? "",
  lobbyId: process.env.ISLAND_LOBBY_ID ?? "",
  playerFactory: (teamColor) => ({
    teamColor,
    async onGeneration(generation, commands, signal) {
      const origin = generation.castles.find(
        (castle) => castle.teamColor === teamColor && castle.soldiers > 10,
      );
      const target = generation.castles.find(
        (castle) => castle.teamColor !== teamColor,
      );
      if (origin && target && !signal.aborted)
        await commands.sendSoldierGroup(origin.id, target.id, 5);
    },
  }),
});
process.once("SIGINT", () => {
  void client.close();
});
process.once("SIGTERM", () => {
  void client.close();
});
await client.run();
