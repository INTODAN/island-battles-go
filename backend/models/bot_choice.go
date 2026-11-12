package models

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

// BotChoiceView is a player's bot: the one they choose in a lobby's member
// settings before getting ready, and the one their game copied from it at the
// start.
type BotChoiceView struct {
	Kind  BotKind `json:"kind" enum:"localhost,default" doc:"localhost: the player's own bot, which island-battles-bot serve runs on their machine; their game client relays each turn to it and submits its answer. default: a default bot of GET /bots/defaults, which the backend plays itself, so the player's client submits nothing."`
	Level *uint   `json:"level,omitempty" minimum:"1" doc:"For kind default, the level of the default bot, one that GET /bots/defaults lists. Absent for kind localhost."`
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
