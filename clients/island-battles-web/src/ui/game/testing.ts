// Builders for the game view's tests: small demos and generations.

import type { components } from "../../api/generated/game";
import type { Demo } from "./demoFile";

type Generation = components["schemas"]["Generation"];

/** A generation with two castles, Blue's holding `blue` soldiers and Red's `red`. */
export function generation(id: number, blue = 30, red = 30): Generation {
  const upgrades = { speed: 1, clash_stop_generations: 1 };
  return {
    id,
    castles: [
      { id: 0, team_color: 1, soldiers: blue, x: -2, y: 0, upgrades },
      { id: 1, team_color: 2, soldiers: red, x: 2, y: 0, upgrades },
    ],
    soldier_groups: [],
    clashes: [],
  };
}

/** A two-generation demo saved by Alex on Blue after the game ended. */
export function sampleDemo(): Demo {
  const upgrades = { speed: 1, clash_stop_generations: 1 };
  const second = generation(42, 29, 30);
  second.soldier_groups = [
    {
      id: 0,
      team_color: 1,
      soldiers: 1,
      x: -1.5,
      y: 0,
      origin_castle_id: 0,
      destination_castle_id: 1,
      clash_radius: 0.25,
      upgrades,
    },
  ];
  second.clashes = [
    {
      id: 0,
      time: 0.5,
      participants: [
        {
          group_id: 0,
          team_color: 1,
          x: 0,
          y: 0,
          clash_radius: 0.25,
          soldiers_before: 2,
          soldiers_after: 1,
        },
      ],
    },
  ];
  return {
    format: "island-battles-demo",
    schema_version: 1,
    saved_at: "2026-10-31T12:00:00Z",
    game_id: 12,
    teams: [
      { color: 1, name: "Blue", players: ["Alex"] },
      { color: 2, name: "Red", players: ["Sam"] },
    ],
    outcome: {
      reason: "last_team_standing",
      winner_team_color: 1,
      summary: "Blue wins as the last team standing",
    },
    frames: [generation(41), second],
    logs: [
      {
        generation_id: 41,
        players: [
          {
            display_name: "Alex",
            team_color: 1,
            lines: [
              {
                level: "info",
                logger: "island_battles.player",
                message: "attacking castle 1",
              },
            ],
          },
          { display_name: "Sam", team_color: 2, lines: [] },
        ],
        hidden_team_colors: [],
      },
      {
        generation_id: 42,
        players: [
          { display_name: "Alex", team_color: 1, lines: [] },
          { display_name: "Sam", team_color: 2, lines: [] },
        ],
        hidden_team_colors: [],
      },
    ],
  };
}
