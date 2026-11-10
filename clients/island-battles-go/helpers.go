package islandbattles

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

// NeutralTeamColor is the team colour of castles and soldiers that belong to
// no player's team.
const NeutralTeamColor uint = 0

// The game log's limits, per player per generation, as the server enforces
// them.
const (
	// MaxLogLines is how many lines a result may return.
	MaxLogLines = 100
	// MaxLogMessageLength is the longest message, in characters (Unicode
	// code points).
	MaxLogMessageLength = 500
	// MaxLoggerLength is the longest logger name, in characters.
	MaxLoggerLength = 100
)

// MaxCommands is how many commands a result may return.
const MaxCommands = 1000

// PlayerLogger is the logger name of the lines Log, Logf and LogAt build.
const PlayerLogger = "island_battles.player"

// cutNote ends a message that was cut to fit MaxLogMessageLength.
var cutNote = fmt.Sprintf("… (cut at %d characters)", MaxLogMessageLength)

// SendSoldierGroup builds a command that sends soldierAmount soldiers from a
// castle your team owns to any other castle.
func SendSoldierGroup(originCastleID, destinationCastleID, soldierAmount uint) Command {
	return newCommand(SendSoldierGroupCommand, SendSoldierGroupParameters{
		OriginCastleID: originCastleID, DestinationCastleID: destinationCastleID, SoldierAmount: soldierAmount,
	})
}

// UpgradeCastle builds a command that buys the next level of an upgrade type
// for a castle your team owns. The castle pays the level's cost in soldiers;
// NextUpgradeCost says how many.
func UpgradeCastle(castleID uint, upgradeType UpgradeType) Command {
	return newCommand(UpgradeCastleCommand, UpgradeCastleParameters{CastleID: castleID, UpgradeType: upgradeType})
}

func newCommand(commandType CommandType, parameters any) Command {
	// Both parameter types are plain structs, which always encode.
	encoded, _ := json.Marshal(parameters)
	return Command{CommandType: commandType, Parameters: encoded}
}

// NextUpgradeCost is how many soldiers it costs the castle to buy its next
// level of an upgrade type, read from the turn's upgrade table. It returns
// false when the castle is at the type's maximum level. Upgrades bought
// earlier in the same result are not counted: add them to the castle's level.
func NextUpgradeCost(turn Turn, castle Castle, upgradeType UpgradeType) (uint, bool) {
	level, found := turn.UpgradeLevel(upgradeType, castle.Upgrades.Level(upgradeType)+1)
	return level.SoldierCost, found
}

// UpgradeLevel looks up one level of an upgrade type in the turn's upgrade
// table, and returns false when the table has no such level.
func (turn Turn) UpgradeLevel(upgradeType UpgradeType, level int) (UpgradeLevel, bool) {
	for _, upgrade := range turn.Upgrades {
		if upgrade.Type != upgradeType {
			continue
		}
		for _, entry := range upgrade.Levels {
			if entry.Level == level {
				return entry, true
			}
		}
	}
	return UpgradeLevel{}, false
}

// Log builds an info line for the game log.
func Log(message string) LogLine {
	return LogAt(LogInfo, message)
}

// Logf builds an info line for the game log, formatted as fmt.Sprintf does.
func Logf(format string, arguments ...any) LogLine {
	return LogAt(LogInfo, fmt.Sprintf(format, arguments...))
}

// LogAt builds a game log line of a level. Like Log and Logf, it makes the
// message storable, replacing a NUL character or invalid UTF-8 with U+FFFD,
// and cuts a message longer than MaxLogMessageLength characters so that,
// ending in "… (cut at 500 characters)", it fits. The line's Logger is
// PlayerLogger; set another on the line if you like.
func LogAt(level LogLevel, message string) LogLine {
	return LogLine{Level: level, Logger: PlayerLogger, Message: cutMessage(cleanText(message))}
}

// cleanText replaces what stored text can't hold, NUL and invalid UTF-8,
// with U+FFFD.
func cleanText(text string) string {
	return strings.ReplaceAll(strings.ToValidUTF8(text, "�"), "\x00", "�")
}

// cutMessage fits a message into one line, saying where it was cut.
func cutMessage(message string) string {
	if utf8.RuneCountInString(message) <= MaxLogMessageLength {
		return message
	}
	runes := []rune(message)
	return string(runes[:MaxLogMessageLength-utf8.RuneCountInString(cutNote)]) + cutNote
}

// TeamType is how another entity's team relates to an entity's.
type TeamType string

// The team relations CompareTeam returns.
const (
	Friendly TeamType = "friendly"
	Neutral  TeamType = "neutral"
	Enemy    TeamType = "enemy"
)

// Entity is something on the board: a Castle or a SoldierGroup.
type Entity interface {
	// Position is where the entity is, in map units.
	Position() (x, y float64)
	// Team is the entity's team colour.
	Team() uint
}

// Position is where the castle is.
func (castle Castle) Position() (x, y float64) { return castle.X, castle.Y }

// Team is the castle's team colour.
func (castle Castle) Team() uint { return castle.TeamColor }

// Distance is the straight-line distance to another castle or soldier group,
// in map units.
func (castle Castle) Distance(other Entity) float64 { return distance(castle, other) }

// CompareTeam says whether other is friendly, neutral or an enemy to the
// castle. A neutral other is Neutral even when the castle is neutral too.
func (castle Castle) CompareTeam(other Entity) TeamType { return compareTeam(castle, other) }

// Position is where the group is.
func (group SoldierGroup) Position() (x, y float64) { return group.X, group.Y }

// Team is the group's team colour.
func (group SoldierGroup) Team() uint { return group.TeamColor }

// Distance is the straight-line distance to another castle or soldier group,
// in map units.
func (group SoldierGroup) Distance(other Entity) float64 { return distance(group, other) }

// CompareTeam says whether other is friendly, neutral or an enemy to the
// group. A neutral other is Neutral even when the group is neutral too.
func (group SoldierGroup) CompareTeam(other Entity) TeamType { return compareTeam(group, other) }

// DestinationCastle is the castle the group is marching to, from the
// generation the group is in. It returns false when the generation doesn't
// have that castle.
func (group SoldierGroup) DestinationCastle(generation Generation) (Castle, bool) {
	return generation.Castle(group.DestinationCastleID)
}

// DistanceLeft is how far the group still has to go to its destination
// castle, in map units, not turns. It returns false when the generation
// doesn't have that castle.
func (group SoldierGroup) DistanceLeft(generation Generation) (float64, bool) {
	destination, found := group.DestinationCastle(generation)
	if !found {
		return 0, false
	}
	return group.Distance(destination), true
}

// Castle finds a castle of the generation by its ID.
func (generation Generation) Castle(id uint) (Castle, bool) {
	for _, castle := range generation.Castles {
		if castle.ID == id {
			return castle, true
		}
	}
	return Castle{}, false
}

func distance(from, to Entity) float64 {
	fromX, fromY := from.Position()
	toX, toY := to.Position()
	return math.Hypot(toX-fromX, toY-fromY)
}

func compareTeam(entity, other Entity) TeamType {
	switch other.Team() {
	case NeutralTeamColor:
		return Neutral
	case entity.Team():
		return Friendly
	}
	return Enemy
}
