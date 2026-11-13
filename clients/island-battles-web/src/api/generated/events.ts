// Generated from api/lobby/events.json by tools/codegen/generate.ts. Do not edit; run `bun run generate`.

export interface LobbyInvalidation {
event_id: string
game_key: string
lobby_id: string
schema_version: 1
type: "lobby.changed"
version: number
}
