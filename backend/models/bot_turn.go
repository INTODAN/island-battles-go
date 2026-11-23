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
	// TurnNumber and MaxGenerations are GenerationState.TurnNumber and
	// GenerationState.MaxGenerations.
	TurnNumber     int           `json:"turn_number" minimum:"1" doc:"This turn's number in the game, from 1: the game's first generation is turn 1, and every turn adds one, the n of the replay's Turn n / m. generation.id is not the turn number: it is the server's ID of the generation, which increases with every turn but doesn't start at 1."`
	MaxGenerations int           `json:"max_generations" minimum:"2" doc:"The game's last generation, the same for the whole game. The turn that creates generation max_generations ends the game, if nothing has ended it before, so turn max_generations - 1 is the last one a bot plays: max_generations - turn_number turns are left, this one included."`
	TeamColor      uint          `json:"team_color" minimum:"1"`
	Upgrades       []UpgradeView `json:"upgrades" nullable:"false" doc:"The upgrade cost table, the same list GET /game/upgrades returns as its upgrades. It is the same for the whole game. Reaching a level costs its soldier_cost."`
	Memory         BotMemory     `json:"memory" doc:"The memory the bot returned with its previous turn in this game, or null on its first turn and after a turn that returned null."`
	// TurnDeadlineAt is GenerationState.TurnDeadlineAt.
	TurnDeadlineAt *time.Time `json:"turn_deadline_at" nullable:"true" doc:"When the turn ends, GenerationState.turn_deadline_at, or null when turns have no time limit. In a game that waits for every player, a bot that hasn't answered by then makes its whole team lose. In a real-time game, where a player plays by hand, the server plays the turn then without the bot's answer: a later answer joins the open turn, and completes it, while it names one of the two turns before the open one, and is refused as a stale generation after that, when the bot has passed."`
}

// BotTurnResult is a bot's answer to a BotTurn: the commands and game log
// lines that POST /generation/commands takes, and the memory for its next
// turn.
type BotTurnResult struct {
	Commands []PublicGenerationCommand `json:"commands" maxItems:"1000" nullable:"false"`
	Memory   BotMemory                 `json:"memory" doc:"Any JSON object of at most 64 KiB as compact JSON, given back as BotTurn.memory next turn, or null for none."`
	Logs     []GenerationLogLine       `json:"logs" maxItems:"100" nullable:"false" doc:"Game log lines for this turn, of at most 500 characters each."`
}

// TurnDelivery is one turn as it is delivered to a bot that runs outside the
// backend: the BotTurn to play, and how to submit the answer.
type TurnDelivery struct {
	Turn   BotTurn    `json:"turn"`
	Submit TurnSubmit `json:"submit"`
}

// TurnSubmit says where and with what a bot submits its answer to a
// TurnDelivery.
type TurnSubmit struct {
	URL        string     `json:"url" format:"uri" doc:"Where to submit the answer: POST /generation/commands on the game's server, as an absolute URL."`
	Token      string     `json:"token" doc:"The submit token, sent as Authorization: Bearer <token>. It lets the bearer submit this player's turn of this generation and nothing else, and expires at the turn's deadline plus 30 seconds, or 24 hours after the generation when turns have no time limit. Keep it secret until the turn is over."`
	DeadlineAt *time.Time `json:"deadline_at" nullable:"true" doc:"When the turn ends, the BotTurn's turn_deadline_at, or null when turns have no time limit. An answer should be submitted before it: in a real-time game a later one joins the open turn while it names one of the two turns before it, and is refused as a stale generation after that, when the bot has passed the turn."`
}

// GameEndedDelivery tells a bot that runs outside the backend that its game
// has ended: how it ended, the final generation as its team sees it, and a
// link that downloads the game's demo (#413). The server sends one to each
// hosted and uploaded bot on the roster once the outcome is stored, however
// the game ended, and a localhost bot's client reads it from
// GET /game/from-lobby/{lobby_id}/game-ended-delivery. It carries no submit
// token: a finished game takes no commands.
type GameEndedDelivery struct {
	GameID     uint            `json:"game_id" minimum:"1"`
	TeamColor  uint            `json:"team_color" minimum:"1" doc:"The bot's team, also when it was eliminated or left before the end."`
	TurnNumber int             `json:"turn_number" minimum:"1" doc:"The final generation's turn number, as BotTurn.turn_number counts it."`
	Outcome    GameOutcomeView `json:"outcome" doc:"How the game ended, as GenerationState.outcome says it."`
	Generation GenerationView  `json:"generation" doc:"The final generation, the board the outcome was decided on, as a BotTurn's generation shows it to the bot's team."`
	Memory     BotMemory       `json:"memory" doc:"The memory the bot sent with its latest completed turn, as the next BotTurn.memory would have been, or null."`
	Demo       DemoLink        `json:"demo" doc:"Where to download the game's demo."`
}

// DemoLink is a self-contained link to one game's demo, as one roster player
// sees it: once the game has ended, every team's game logs and the outcome.
type DemoLink struct {
	URL       string    `json:"url" format:"uri" doc:"GET /game/{id}/demo on the game's public URL, with a read-only demo token in its token query parameter, so a plain GET downloads the Demo file. The token lets its bearer download this game's demo as this player sees it, and nothing else; keep the link private."`
	ExpiresAt time.Time `json:"expires_at" doc:"When the link stops working: when game retention deletes the game, 7 days after its final generation by default. Download the demo before then."`
}

// GamePlayerBotMemory is the memory a player sent with their latest completed
// turn of a game. It is game data, and is deleted with the game.
type GamePlayerBotMemory struct {
	GameID   uint   `gorm:"primaryKey;autoIncrement:false"`
	PlayerID string `gorm:"type:uuid;primaryKey"`
	// GenerationID is the generation whose completed turn sent the memory.
	GenerationID uint `gorm:"not null"`
	// Memory is the compact JSON as validated, stored as bytea: jsonb would
	// reformat it, expand exponent numbers and reject \u0000 and lone surrogates.
	Memory    []byte `gorm:"type:bytea;not null"`
	UpdatedAt time.Time
}

func (GamePlayerBotMemory) TableName() string { return "game_player_bot_memory" }

// DefaultBotView is one of the default bots that the backend plays itself,
// as GET /bots/defaults lists it.
type DefaultBotView struct {
	Level       uint   `json:"level" minimum:"1" doc:"The bot's level. Higher levels play better."`
	Name        string `json:"name" doc:"A short name to show in a bot picker."`
	Description string `json:"description" doc:"How the bot plays, in a sentence or two."`
}
