package models

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

// MaxBotMemoryBytes caps a bot's memory, serialized as compact JSON.
const MaxBotMemoryBytes = 64 << 10

// BotMemory is the JSON object a bot returns with its turn and receives back
// with its next one. Empty means none, which is null on the wire.
type BotMemory []byte

// MarshalJSON writes the memory as stored, or null when there is none.
func (memory BotMemory) MarshalJSON() ([]byte, error) {
	if len(memory) == 0 {
		return []byte("null"), nil
	}
	return memory, nil
}

// UnmarshalJSON keeps the raw JSON, so numbers and keys reach the next turn
// unchanged. A JSON null leaves no memory.
func (memory *BotMemory) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		*memory = nil
		return nil
	}
	*memory = append((*memory)[:0], data...)
	return nil
}

// Schema publishes memory as any JSON object, or null.
func (BotMemory) Schema(huma.Registry) *huma.Schema {
	return &huma.Schema{Type: huma.TypeObject, Nullable: true, AdditionalProperties: true}
}

// Compact returns the memory as compact JSON, the form its size is measured
// and stored in. No memory, or a JSON null, returns nil.
func (memory BotMemory) Compact() (BotMemory, error) {
	if len(memory) == 0 {
		return nil, nil
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, memory); err != nil {
		return nil, err
	}
	if bytes.Equal(compact.Bytes(), []byte("null")) {
		return nil, nil
	}
	return compact.Bytes(), nil
}

// BotTurn is everything a stateless bot is given to play one turn: the
// current generation, its team and the memory it returned last turn.
type BotTurn struct {
	GameID     uint           `json:"game_id" minimum:"1"`
	Generation GenerationView `json:"generation"`
	TeamColor  uint           `json:"team_color" minimum:"1"`
	Memory     BotMemory      `json:"memory" doc:"The memory the bot returned with its previous turn in this game, or null on its first turn and after a turn that returned null."`
	// TurnDeadlineAt stays nil until turns have a deadline.
	TurnDeadlineAt *time.Time `json:"turn_deadline_at" nullable:"true" doc:"When the turn times out, or null when turns have no time limit."`
}

// BotTurnResult is a bot's answer to a BotTurn: the commands and game log
// lines that POST /generation/commands takes, and the memory for its next
// turn.
type BotTurnResult struct {
	Commands []PublicGenerationCommand `json:"commands" maxItems:"1000" nullable:"false"`
	Memory   BotMemory                 `json:"memory" doc:"Any JSON object of at most 64 KiB as compact JSON, given back as BotTurn.memory next turn, or null for none."`
	Logs     []GenerationLogLine       `json:"logs" maxItems:"100" nullable:"false" doc:"Game log lines for this turn, of at most 500 characters each."`
}

// GamePlayerBotMemory is the memory a player sent with their latest completed
// turn of a game. It is game data, and is deleted with the game.
type GamePlayerBotMemory struct {
	GameID   uint   `gorm:"primaryKey;autoIncrement:false"`
	PlayerID string `gorm:"type:uuid;primaryKey"`
	// GenerationID is the generation whose completed turn sent the memory.
	GenerationID uint   `gorm:"not null"`
	Memory       []byte `gorm:"type:jsonb;not null"`
	UpdatedAt    time.Time
}

func (GamePlayerBotMemory) TableName() string { return "game_player_bot_memory" }
