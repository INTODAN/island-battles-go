package islandbattles

import (
	"encoding/json"
	"time"
)

// The types in this file are the turn contract, BotTurn and BotTurnResult in
// the player API document (backend/api/openapi.json), written by hand. Their
// JSON names are the contract's; a test keeps them equal to the document.

// Turn is everything a bot is given to play one turn: the board, its team,
// the upgrade cost table and the memory it returned last turn. It is the
// contract's BotTurn.
type Turn struct {
	// GameID is the game the turn belongs to.
	GameID uint `json:"game_id"`
	// Generation is the board of this turn.
	Generation Generation `json:"generation"`
	// TeamColor is the bot's team.
	TeamColor uint `json:"team_color"`
	// Upgrades is the upgrade cost table, the list GET /game/upgrades
	// returns: every upgrade type with each level's value and cost. It is
	// the same for the whole game.
	Upgrades []Upgrade `json:"upgrades"`
	// Memory is what the bot returned as its memory last turn, or empty on
	// its first turn and after a turn that returned none.
	Memory Memory `json:"memory"`
	// TurnDeadlineAt is when the turn times out, or nil when turns have no
	// time limit. A bot that hasn't answered by then makes its team lose.
	TurnDeadlineAt *time.Time `json:"turn_deadline_at"`
}

// TurnDelivery is one turn as it is delivered to a bot server, such as serve:
// the turn to play, and where and with what to submit the answer. It is the
// contract's TurnDelivery.
type TurnDelivery struct {
	Turn   Turn       `json:"turn"`
	Submit TurnSubmit `json:"submit"`
}

// TurnSubmit says where a delivered turn's answer goes. It is the contract's
// TurnSubmit.
type TurnSubmit struct {
	// URL is POST /generation/commands on the game's server.
	URL string `json:"url"`
	// Token is the submit token, sent as Authorization: Bearer <token>. It
	// submits this one turn and nothing else.
	Token string `json:"token"`
	// DeadlineAt is when the turn times out, or nil when turns have no time
	// limit.
	DeadlineAt *time.Time `json:"deadline_at"`
}

// Generation is the board of one turn. It is the contract's Generation.
type Generation struct {
	// ID increases with every turn.
	ID            uint           `json:"id"`
	Castles       []Castle       `json:"castles"`
	SoldierGroups []SoldierGroup `json:"soldier_groups"`
	// Clashes are the fights of the turn that made this generation.
	Clashes []Clash `json:"clashes"`
}

// Castle is a castle on the board.
type Castle struct {
	// ID is stable for the whole game.
	ID uint `json:"id"`
	// TeamColor owns the castle; NeutralTeamColor is no team.
	TeamColor uint    `json:"team_color"`
	Soldiers  uint    `json:"soldiers"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	// Upgrades is the level of every upgrade type.
	Upgrades Upgrades `json:"upgrades"`
}

// SoldierGroup is a group of soldiers marching from one castle to another.
type SoldierGroup struct {
	// ID is never reassigned within a game.
	ID                  uint    `json:"id"`
	TeamColor           uint    `json:"team_color"`
	Soldiers            uint    `json:"soldiers"`
	X                   float64 `json:"x"`
	Y                   float64 `json:"y"`
	OriginCastleID      uint    `json:"origin_castle_id"`
	DestinationCastleID uint    `json:"destination_castle_id"`
	// ClashRadius: enemies clash when their centres are within the larger
	// of their radii.
	ClashRadius float64 `json:"clash_radius"`
	// Upgrades are the origin castle's levels when the group left.
	Upgrades Upgrades `json:"upgrades"`
}

// Clash is a fight between soldier groups during the last turn.
type Clash struct {
	// ID identifies the clash within its generation.
	ID uint `json:"id"`
	// Time is the fraction of the turn at which contact happened.
	Time         float64            `json:"time"`
	Participants []ClashParticipant `json:"participants"`
}

// ClashParticipant is one group in a clash.
type ClashParticipant struct {
	// GroupID is the soldier group, which may no longer exist.
	GroupID     uint    `json:"group_id"`
	TeamColor   uint    `json:"team_color"`
	X           float64 `json:"x"`
	Y           float64 `json:"y"`
	ClashRadius float64 `json:"clash_radius"`
	// SoldiersBefore and SoldiersAfter are the group's soldiers around the
	// contact; 0 after means the group was defeated.
	SoldiersBefore uint `json:"soldiers_before"`
	SoldiersAfter  uint `json:"soldiers_after"`
}

// UpgradeType is a kind of castle upgrade.
type UpgradeType string

// The upgrade types.
const (
	// SpeedUpgrade sets how far a group moves per turn.
	SpeedUpgrade UpgradeType = "speed"
	// ClashStopGenerationsUpgrade sets how many turns a group stays stopped
	// after a clash.
	ClashStopGenerationsUpgrade UpgradeType = "clash_stop_generations"
)

// UpgradeTypes lists every upgrade type, sorted by name.
func UpgradeTypes() []UpgradeType {
	return []UpgradeType{ClashStopGenerationsUpgrade, SpeedUpgrade}
}

// Upgrades maps upgrade types to levels. Every castle starts at level 1.
type Upgrades map[UpgradeType]int

// Level is the level of an upgrade type, 1 when the map doesn't list it.
func (upgrades Upgrades) Level(upgradeType UpgradeType) int {
	level, found := upgrades[upgradeType]
	if !found {
		return 1
	}
	return level
}

// Upgrade is one upgrade type of the turn's cost table, with every level a
// castle can reach.
type Upgrade struct {
	Type UpgradeType `json:"type"`
	// Description says what the levels' values measure.
	Description string `json:"description"`
	// MaxLevel is the highest level.
	MaxLevel int `json:"max_level"`
	// Levels are every level from 1 to MaxLevel, in order.
	Levels []UpgradeLevel `json:"levels"`
}

// UpgradeLevel is one level of an upgrade type.
type UpgradeLevel struct {
	Level int `json:"level"`
	// Value is the level's effect, in the unit the type's description names.
	Value float64 `json:"value"`
	// SoldierCost is what a castle pays to reach this level from the one
	// below. Level 1 is free.
	SoldierCost uint `json:"soldier_cost"`
}

// CommandType names a command.
type CommandType string

// The command types a bot may return.
const (
	SendSoldierGroupCommand CommandType = "send_soldier_group"
	UpgradeCastleCommand    CommandType = "upgrade_castle"
)

// Command is one order of a turn's result. Build it with SendSoldierGroup or
// UpgradeCastle. Parameters are SendSoldierGroupParameters or
// UpgradeCastleParameters as JSON, by CommandType.
type Command struct {
	CommandType CommandType     `json:"command_type"`
	Parameters  json.RawMessage `json:"parameters"`
}

// SendSoldierGroupParameters sends soldiers from a castle the bot's team
// owns to any other castle.
type SendSoldierGroupParameters struct {
	OriginCastleID      uint `json:"origin_castle_id"`
	DestinationCastleID uint `json:"destination_castle_id"`
	// SoldierAmount is at least 1, and counts against the soldiers the
	// team already reserved this turn.
	SoldierAmount uint `json:"soldier_amount"`
}

// UpgradeCastleParameters buys the next level of an upgrade type for a castle
// the bot's team owns. The castle pays the level's cost in soldiers.
type UpgradeCastleParameters struct {
	CastleID    uint        `json:"castle_id"`
	UpgradeType UpgradeType `json:"upgrade_type"`
}

// LogLevel is the severity of a game log line.
type LogLevel string

// The game log levels.
const (
	LogDebug   LogLevel = "debug"
	LogInfo    LogLevel = "info"
	LogWarning LogLevel = "warning"
	LogError   LogLevel = "error"
)

// LogLevels lists every game log level, from the least severe.
func LogLevels() []LogLevel {
	return []LogLevel{LogDebug, LogInfo, LogWarning, LogError}
}

// LogLine is one line of the game log, which the replay shows with its
// generation. Build it with Log, Logf or LogAt.
type LogLine struct {
	Level LogLevel `json:"level"`
	// Logger names who wrote the line, PlayerLogger by default.
	Logger string `json:"logger"`
	// Message is 1 to MaxLogMessageLength characters.
	Message string `json:"message"`
}

// Result is a bot's answer to a Turn: the commands, the game log lines and
// the memory for its next turn. Every field may be left empty. It is the
// contract's BotTurnResult.
type Result struct {
	// Commands run in order. The server rejects the whole result if any
	// command is invalid, so count what the commands spend.
	Commands []Command `json:"commands"`
	// Memory comes back as the next turn's Turn.Memory. Build it with
	// NewMemory.
	Memory Memory `json:"memory"`
	// Logs are stored in the game log, at most MaxLogLines.
	Logs []LogLine `json:"logs"`
}

// MarshalJSON writes empty commands and logs as [], as the contract requires.
func (result Result) MarshalJSON() ([]byte, error) {
	type plain Result
	if result.Commands == nil {
		result.Commands = []Command{}
	}
	if result.Logs == nil {
		result.Logs = []LogLine{}
	}
	return json.Marshal(plain(result))
}
