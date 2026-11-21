package models

import (
	"cmp"
	"net/url"
	"slices"
)

// DefaultLocalhostPort is the Island Battles bot port, where
// island-battles-bot serve listens unless --port says otherwise, and where a
// game client delivers a localhost bot's turns unless the player chose
// another port.
const DefaultLocalhostPort uint = 41414

// MinLocalhostPort and MaxLocalhostPort bound a localhost bot's port: not a
// privileged port, which a bot server would need root for.
const (
	MinLocalhostPort uint = 1024
	MaxLocalhostPort uint = 65535
)

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
	// ManualBot is a person who plays by hand: nothing plays or relays the
	// player, and their game client sends their commands with POST
	// /generation/commands.
	ManualBot BotKind = "manual"
)

// PlayedByServer reports whether the server, not the player's game client,
// delivers the bot's turns.
func (kind BotKind) PlayedByServer() bool { return kind == HostedBot || kind == UploadedBot }

// BotChoiceView is a player's bot in a game, copied from their lobby choice
// when the game started.
type BotChoiceView struct {
	Kind  BotKind `json:"kind" enum:"localhost,default,hosted,uploaded,manual" doc:"localhost: the player's own bot, which island-battles-bot serve runs on their machine; their game client relays each turn to it. default: a default bot of GET /bots/defaults, which the backend plays itself, so the player's client submits nothing. hosted: the player's own bot server at a public URL, which the server sends each turn to; no client relays it. uploaded: a bot package uploaded to the lobby, which the server runs; no client relays it. manual: a person who plays by hand in the 3D client; nothing plays or relays the player, and their client sends their commands."`
	Level *uint   `json:"level,omitempty" minimum:"1" doc:"For kind default, the level of the default bot, one that GET /bots/defaults lists. Absent for the other kinds."`
	Host  string  `json:"host,omitempty" doc:"For kind hosted, the host name of the bot's URL, such as bot.example.com; the rest of the URL is not shown. Absent for the other kinds."`
	Port  *uint   `json:"port,omitempty" minimum:"1024" maximum:"65535" doc:"For kind localhost, the port on 127.0.0.1 where the player's client delivers each turn: the one they chose in the lobby, 41414 by default. Absent for the other kinds."`
	Name  string  `json:"name,omitempty" doc:"In a rated game, the bot name that a hosted bot plays as, one that its player registered in their account, such as Kraken. Absent for the other kinds and in a game that isn't rated."`
}

// MemberBotView is the bot that a person chooses for themselves, in their
// lobby member settings, {"bot": {...}}.
type MemberBotView struct {
	Kind      BotKind `json:"kind" enum:"localhost,default,hosted,uploaded,manual" doc:"localhost: your own bot, which island-battles-bot serve runs on your machine and your game client relays each turn to. default: a default bot of GET /bots/defaults, which the backend plays. hosted: your own bot server at url, which the server sends each turn to. uploaded: your own bot package in this lobby, package_id, which the server runs. manual: you play by hand in the 3D client; no bot plays for you. hosted and uploaded are offered only when GET /bots/defaults says server_bots."`
	Level     *uint   `json:"level,omitempty" minimum:"1" doc:"For kind default only: a level that GET /bots/defaults lists."`
	URL       string  `json:"url,omitempty" maxLength:"2048" doc:"For kind hosted only: where the server POSTs each TurnDelivery. https:// on port 443, without credentials, on a host name (not an IP address) whose addresses are all public. Required unless the bot has a name, which then plays at its own URL (GET /account). In a rated lobby a named hosted bot always plays at its name's URL, and url is ignored."`
	PackageID string  `json:"package_id,omitempty" format:"uuid" doc:"For kind uploaded only: a package of yours in this lobby, from GET /game/lobbies/{lobby_id}/bot-packages."`
	Port      *uint   `json:"port,omitempty" minimum:"1024" maximum:"65535" doc:"For kind localhost only: the port on 127.0.0.1 where your bot serves (island-battles-bot serve --port N), where your game client delivers each turn. 41414 when absent."`
	Name      string  `json:"name,omitempty" maxLength:"200" doc:"For kinds localhost, hosted and uploaded only: one of the bot names of your account (GET /account), compared without regard to case, that the bot plays as. Only a signed-in player has bot names. A rated lobby takes a bot only as a hosted bot under one of your bot names, which plays at the name's URL, and records the game under the name. Elsewhere the name changes nothing, except that a hosted bot without a url plays at its name's URL."`
}

// MemberSettingsView are Island Battles' member settings in a lobby: the bot
// that plays for the person. Set them with the lobby's
// setLobbyMemberSettings.
type MemberSettingsView struct {
	Bot MemberBotView `json:"bot"`
}

// BotSeatSettingsView are the seat_settings of a bot seat that the host adds.
// Exactly one bot field is set: bot_level, bot_localhost_port, bot_url or
// bot_package_id. A localhost bot seat also names its relay.
type BotSeatSettingsView struct {
	BotLocalhostPort *uint `json:"bot_localhost_port,omitempty" minimum:"1024" maximum:"65535" doc:"A localhost bot on the machine of the host who adds the seat, which island-battles-bot serve --port N runs on this port of 127.0.0.1; that host's game client delivers each turn to it, as to their own localhost bot. A string of the port's digits, which the lobby page's text field sends, is read the same. The port must differ from that host's own localhost bot's and from every other localhost bot seat's that they added. Needs bot_relay_player_id. The seat is named \"Localhost bot (port N)\"."`
	// BotRelayPlayerID records who added a localhost bot seat, since the
	// lobby doesn't: only their tab relays the seat, never a later host's.
	BotRelayPlayerID string `json:"bot_relay_player_id,omitempty" format:"uuid" doc:"With bot_localhost_port only, and required with it: the player ID of the host who adds the seat, whose game client alone relays it. It must be the lobby's host. When that player is no longer in the lobby, the start is refused until the seat is removed."`
	BotLevel         *uint  `json:"bot_level,omitempty" minimum:"1" doc:"A default bot of this level, one that GET /bots/defaults lists, which the backend plays. The seat is named \"Bot (level N)\"."`
	BotURL           string `json:"bot_url,omitempty" maxLength:"2048" doc:"A hosted bot at this URL, under the rules of a member's hosted bot; the server sends it each turn. The seat is named \"Hosted bot\". Only when GET /bots/defaults says server_bots."`
	BotPackageID     string `json:"bot_package_id,omitempty" format:"uuid" doc:"Any member's bot package in this lobby, which the server runs. The seat is named after its owner, such as \"Alex's bot\". Only when GET /bots/defaults says server_bots."`
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
// GenerationState.bot reports it, with the bot name it plays as in a rated
// game.
func FrozenBot(player FrozenGamePlayer) BotChoiceView {
	view := frozenBotKind(player)
	if player.BotName != nil && view.Kind != DefaultBot && view.Kind != ManualBot {
		view.Name = *player.BotName
	}
	return view
}

func frozenBotKind(player FrozenGamePlayer) BotChoiceView {
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
	case ManualBot:
		return BotChoiceView{Kind: ManualBot}
	default:
		port := LocalhostPort(player.BotPort)
		return BotChoiceView{Kind: LocalhostBot, Port: &port}
	}
}

// LocalhostPort is a localhost bot's port: port, or DefaultLocalhostPort
// when none was chosen.
func LocalhostPort(port *uint) uint {
	if port == nil {
		return DefaultLocalhostPort
	}
	return *port
}

// RelayedBotView is a localhost bot seat that a player's game client relays.
type RelayedBotView struct {
	PlayerID    string `json:"player_id" format:"uuid" doc:"The seat's player ID on the roster, which GET /game/from-lobby/{lobby_id}/turn-delivery?player_id= takes."`
	TeamColor   uint   `json:"team_color" minimum:"1"`
	DisplayName string `json:"display_name" doc:"The seat's name in the lobby, such as \"Localhost bot (port 41415)\"."`
	Port        uint   `json:"port" minimum:"1024" maximum:"65535" doc:"The port on 127.0.0.1 where your client delivers the seat's turns."`
}

// RelayedBots are the localhost bot seats that relayer relays, those that
// they added to the lobby, in team order.
func RelayedBots(players []FrozenGamePlayer, relayer string) []RelayedBotView {
	relayed := []RelayedBotView{}
	for _, player := range players {
		if player.BotKind == LocalhostBot && player.BotRelayPlayerID != nil && *player.BotRelayPlayerID == relayer {
			relayed = append(relayed, RelayedBotView{PlayerID: player.PlayerID, TeamColor: player.TeamColor, DisplayName: player.DisplayName, Port: LocalhostPort(player.BotPort)})
		}
	}
	slices.SortFunc(relayed, func(left, right RelayedBotView) int { return cmp.Compare(left.TeamColor, right.TeamColor) })
	return relayed
}
