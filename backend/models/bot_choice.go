package models

import "net/url"

// BotKind says who plays a player's turns.
type BotKind string

const (
	// LocalhostBot is the player's own bot, which island-battles-bot serve
	// runs on their machine. Their game client relays each turn to it.
	LocalhostBot BotKind = "localhost"
	// DefaultBot is one of the default bots of GET /bots/defaults, which the
	// backend plays itself.
	DefaultBot BotKind = "default"
	// HostedBot is the player's own bot server at a public URL, which speaks
	// the turn delivery protocol. The server sends it each turn, so no tab
	// relays it.
	HostedBot BotKind = "hosted"
	// UploadedBot is a bot package uploaded to the lobby, which the server's
	// runners play, so no tab relays it.
	UploadedBot BotKind = "uploaded"
)

// PlayedByServer reports whether the server, not the player's game client,
// delivers the bot's turns.
func (kind BotKind) PlayedByServer() bool { return kind == HostedBot || kind == UploadedBot }

// BotChoiceView is a player's bot in a game, copied from their lobby choice
// when the game started.
type BotChoiceView struct {
	Kind  BotKind `json:"kind" enum:"localhost,default,hosted,uploaded" doc:"localhost: the player's own bot, which island-battles-bot serve runs on their machine; their game client relays each turn to it. default: a default bot of GET /bots/defaults, which the backend plays itself, so the player's client submits nothing. hosted: the player's own bot server at a public URL, which the server sends each turn to; no client relays it. uploaded: a bot package uploaded to the lobby, which the server runs; no client relays it."`
	Level *uint   `json:"level,omitempty" minimum:"1" doc:"For kind default, the level of the default bot, one that GET /bots/defaults lists. Absent for the other kinds."`
	Host  string  `json:"host,omitempty" doc:"For kind hosted, the host name of the bot's URL, such as bot.example.com; the rest of the URL is not shown. Absent for the other kinds."`
}

// MemberBotView is the bot that a person chooses for themselves, in their
// lobby member settings, {"bot": {...}}.
type MemberBotView struct {
	Kind      BotKind `json:"kind" enum:"localhost,default,hosted,uploaded" doc:"localhost: your own bot, which island-battles-bot serve runs on your machine and your game client relays each turn to. default: a default bot of GET /bots/defaults, which the backend plays. hosted: your own bot server at url, which the server sends each turn to. uploaded: your own bot package in this lobby, package_id, which the server runs. hosted and uploaded are offered only when GET /bots/defaults says server_bots."`
	Level     *uint   `json:"level,omitempty" minimum:"1" doc:"For kind default only: a level that GET /bots/defaults lists."`
	URL       string  `json:"url,omitempty" maxLength:"2048" doc:"For kind hosted only: where the server POSTs each TurnDelivery. https:// on port 443, without credentials, on a host name (not an IP address) whose addresses are all public."`
	PackageID string  `json:"package_id,omitempty" format:"uuid" doc:"For kind uploaded only: a package of yours in this lobby, from GET /game/lobbies/{lobby_id}/bot-packages."`
}

// MemberSettingsView are Island Battles' member settings in a lobby: the bot
// that plays for the person. Set them with the lobby's
// setLobbyMemberSettings.
type MemberSettingsView struct {
	Bot MemberBotView `json:"bot"`
}

// BotSeatSettingsView are the seat_settings of a bot seat that the host adds.
// Exactly one field is set.
type BotSeatSettingsView struct {
	BotLevel     *uint  `json:"bot_level,omitempty" minimum:"1" doc:"A default bot of this level, one that GET /bots/defaults lists, which the backend plays. The seat is named \"Bot (level N)\"."`
	BotURL       string `json:"bot_url,omitempty" maxLength:"2048" doc:"A hosted bot at this URL, under the rules of a member's hosted bot; the server sends it each turn. The seat is named \"Hosted bot\". Only when GET /bots/defaults says server_bots."`
	BotPackageID string `json:"bot_package_id,omitempty" format:"uuid" doc:"Any member's bot package in this lobby, which the server runs. The seat is named after its owner, such as \"Alex's bot\". Only when GET /bots/defaults says server_bots."`
}

// URLHost is the host name of a hosted bot's URL, the only part of it that
// GenerationState.bot shows.
func URLHost(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

// FrozenBot is the bot a frozen roster player plays with, as
// GenerationState.bot reports it.
func FrozenBot(player FrozenGamePlayer) BotChoiceView {
	switch player.BotKind {
	case DefaultBot:
		view := BotChoiceView{Kind: DefaultBot}
		if player.BotLevel != nil {
			level := *player.BotLevel
			view.Level = &level
		}
		return view
	case HostedBot:
		view := BotChoiceView{Kind: HostedBot}
		if player.BotURL != nil {
			view.Host = URLHost(*player.BotURL)
		}
		return view
	case UploadedBot:
		return BotChoiceView{Kind: UploadedBot}
	default:
		return BotChoiceView{Kind: LocalhostBot}
	}
}
