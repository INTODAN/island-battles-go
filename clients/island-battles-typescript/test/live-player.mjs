/** Real backend/Centrifugo smoke entry point, orchestrated by integration tests. */
import { IslandBattlesClient } from "../dist/index.js";

const [baseUrl, credential, lobbyId] = [
  "SDK_TEST_BASE_URL",
  "SDK_TEST_CREDENTIAL",
  "SDK_TEST_LOBBY_ID",
].map((name) => {
  if (!process.env[name]) throw new Error(`Missing ${name}`);
  return process.env[name];
});
let arrived;
let firstGenerationId;
const nextGeneration = new Promise((resolve) => {
  arrived = resolve;
});
const client = new IslandBattlesClient({
  baseUrl,
  credential,
  lobbyId,
  playerFactory: (teamColor) => ({
    teamColor,
    async onGeneration(generation, _commands, signal) {
      firstGenerationId ??= generation.id;
      if (generation.id === firstGenerationId) return; // Complete the first turn through the actual generated HTTP transport.
      console.log(
        JSON.stringify({
          first_generation_id: firstGenerationId,
          generation_id: generation.id,
          team_color: teamColor,
        }),
      );
      arrived();
      // Leave the next turn open; the outer test closes and cancels this callback.
      await new Promise((resolve) => {
        signal.addEventListener("abort", resolve, { once: true });
        if (signal.aborted) resolve();
      });
    },
  }),
});
const timeout = setTimeout(() => {
  console.error("Timed out waiting for generation 2");
  process.exitCode = 1;
  void client.close();
}, 20000);
try {
  await client.start();
  await Promise.race([nextGeneration, client.wait()]);
} finally {
  clearTimeout(timeout);
  await client.close();
}
