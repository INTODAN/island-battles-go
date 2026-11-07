package models

import "time"

// BotKind says who plays a player's turns.
type BotKind string

const (
	// LocalhostBot is the player's own bot, which island-battles-bot serve
	// runs on their machine. Their game client relays each turn to it and
	// submits its answer.
	LocalhostBot BotKind = "localhost"
	// DefaultBot is one of the default bots of GET /bots/defaults, which the
	// backend plays itself.
	DefaultBot BotKind = "default"
)

// BotChoiceView is a player's bot: the one they choose in a lobby before
// getting ready, and the one their game copied from it at the start.
type BotChoiceView struct {
	Kind  BotKind `json:"kind" enum:"localhost,default" doc:"localhost: the player's own bot, which island-battles-bot serve runs on their machine; their game client relays each turn to it and submits its answer. default: a default bot of GET /bots/defaults, which the backend plays itself, so the player's client submits nothing."`
	Level *uint   `json:"level,omitempty" minimum:"1" doc:"For kind default, the level of the default bot, one that GET /bots/defaults lists. Absent for kind localhost."`
}

// LobbyBotsView lists the bot choices of a lobby's members.
type LobbyBotsView struct {
	LobbyID string               `json:"lobby_id" format:"uuid"`
	Members []LobbyMemberBotView `json:"members" nullable:"false" doc:"Every member of the lobby, in the lobby's member order."`
}

// LobbyMemberBotView is one lobby member's bot choice.
type LobbyMemberBotView struct {
	PlayerID    string         `json:"player_id" format:"uuid"`
	DisplayName string         `json:"display_name"`
	Bot         *BotChoiceView `json:"bot,omitempty" doc:"The member's bot. Absent until they choose one, and a member can't get ready without one."`
}

// LobbyBotChoice is a lobby member's bot choice, kept while the lobby waits.
// The game copies it to FrozenGamePlayer.BotLevel at the start, and never
// reads it again. A foreign key to the member's lobby membership deletes it
// with the lobby.
type LobbyBotChoice struct {
	LobbyID  string  `gorm:"type:uuid;primaryKey"`
	PlayerID string  `gorm:"type:uuid;primaryKey"`
	Kind     BotKind `gorm:"not null"`
	// Level is the default bot's level for DefaultBot, and nil for
	// LocalhostBot.
	Level     *uint
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (LobbyBotChoice) TableName() string { return "island_lobby_bot_choices" }

// View is the choice as the API shows it.
func (choice LobbyBotChoice) View() BotChoiceView {
	return BotChoiceView{Kind: choice.Kind, Level: choice.Level}
}

// FrozenBot is the bot a frozen roster player plays with: their default
// bot's level, or their localhost bot.
func FrozenBot(player FrozenGamePlayer) BotChoiceView {
	if player.BotLevel == nil {
		return BotChoiceView{Kind: LocalhostBot}
	}
	level := *player.BotLevel
	return BotChoiceView{Kind: DefaultBot, Level: &level}
}
