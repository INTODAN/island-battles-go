// Package defaultbots holds the three default bots, levels 1 to 3, that the
// Island Battles backend plays itself when a player picks a default bot in
// the lobby. They are plain islandbattles.Play functions, so you can read,
// copy and change them like any bot of your own; examples/level1 to level3
// are their programs.
//
// Every bot is stateless: each turn it gets an islandbattles.Turn, with the
// memory it returned last turn, and answers with an islandbattles.Result.
// They are deterministic: the same turn always gives the same result, so
// they use no randomness at all.
//
// This file holds what the levels share: a sorted view of the board, travel
// times, and a forecast of how a castle's soldiers change as groups land.
package defaultbots

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"unicode/utf8"

	islandbattles "github.com/INTODAN/island-battles-go/clients/island-battles-go"
)

// board is a bot's view of one generation: the castles and soldier groups
// in ID order, its own team, and the turn's upgrade table.
type board struct {
	team    uint
	castles []islandbattles.Castle
	groups  []islandbattles.SoldierGroup
	index   map[uint]int
	turn    islandbattles.Turn
}

func newBoard(turn islandbattles.Turn) board {
	castles := slices.Clone(turn.Generation.Castles)
	slices.SortFunc(castles, func(left, right islandbattles.Castle) int { return cmp.Compare(left.ID, right.ID) })
	groups := slices.Clone(turn.Generation.SoldierGroups)
	slices.SortFunc(groups, func(left, right islandbattles.SoldierGroup) int { return cmp.Compare(left.ID, right.ID) })
	index := make(map[uint]int, len(castles))
	for position, castle := range castles {
		index[castle.ID] = position
	}
	return board{team: turn.TeamColor, castles: castles, groups: groups, index: index, turn: turn}
}

func (board board) castle(id uint) (islandbattles.Castle, bool) {
	position, found := board.index[id]
	if !found {
		return islandbattles.Castle{}, false
	}
	return board.castles[position], true
}

// owned lists the bot's castles in ID order.
func (board board) owned() []islandbattles.Castle {
	owned := []islandbattles.Castle{}
	for _, castle := range board.castles {
		if castle.TeamColor == board.team {
			owned = append(owned, castle)
		}
	}
	return owned
}

// others lists every castle the bot doesn't own, neutral ones included, in
// ID order.
func (board board) others() []islandbattles.Castle {
	others := []islandbattles.Castle{}
	for _, castle := range board.castles {
		if castle.TeamColor != board.team {
			others = append(others, castle)
		}
	}
	return others
}

// nextGroupID is above every group on the board, so groups sent this turn
// land after them, as the simulation lands groups in ID order.
func (board board) nextGroupID() uint {
	if len(board.groups) == 0 {
		return 0
	}
	return board.groups[len(board.groups)-1].ID + 1
}

// fallbackSpeed is the speed of level 1, for a turn whose upgrade table
// lacks it. The server always sends the full table.
const fallbackSpeed = 0.5

// speed is how far a group with these upgrades moves per turn, read from the
// turn's upgrade table. A level the table doesn't have moves at level 1's
// speed.
func (board board) speed(upgrades islandbattles.Upgrades) float64 {
	level, found := board.turn.UpgradeLevel(islandbattles.SpeedUpgrade, upgrades.Level(islandbattles.SpeedUpgrade))
	if !found || level.Value <= 0 {
		level, found = board.turn.UpgradeLevel(islandbattles.SpeedUpgrade, 1)
	}
	if !found || level.Value <= 0 {
		return fallbackSpeed
	}
	return level.Value
}

// travelTurns is the turn, counting this one as 1, at whose end a group that
// moves speed per turn has covered the distance.
func travelTurns(distance, speed float64) int {
	return max(1, int(math.Ceil(distance/speed-1e-9)))
}

// arrival is a soldier group that lands on a castle at the end of a turn.
// Groups land in ID order, and a group sent this turn has an ID above every
// group on the board.
type arrival struct {
	turn     int
	id       uint
	team     uint
	soldiers uint
}

func compareArrivals(left, right arrival) int {
	return cmp.Or(cmp.Compare(left.turn, right.turn), cmp.Compare(left.id, right.id))
}

// arrivals lists the groups on the board that are heading for a castle, by
// when they land. It leaves out clashes on the way and the pause after one,
// which a bot can't see.
func (board board) arrivals(castle islandbattles.Castle) []arrival {
	arrivals := []arrival{}
	for _, group := range board.groups {
		if group.DestinationCastleID != castle.ID {
			continue
		}
		turns := travelTurns(group.Distance(castle), board.speed(group.Upgrades))
		arrivals = append(arrivals, arrival{turn: turns, id: group.ID, team: group.TeamColor, soldiers: group.Soldiers})
	}
	slices.SortFunc(arrivals, compareArrivals)
	return arrivals
}

// forecast follows a castle to the end of a turn, counting this one as 1, as
// the simulation plays it: every turn the castle grows by one soldier, then
// the turn's arrivals land in order. A friendly group reinforces the castle,
// and a hostile one takes it when it has more soldiers than the castle.
func forecast(castle islandbattles.Castle, arrivals []arrival, turns int) (owner, soldiers uint) {
	owner, soldiers = castle.TeamColor, castle.Soldiers
	arrivals = slices.SortedFunc(slices.Values(arrivals), compareArrivals)
	next := 0
	for turn := 1; turn <= turns; turn++ {
		soldiers++
		for ; next < len(arrivals) && arrivals[next].turn <= turn; next++ {
			landing := arrivals[next]
			switch {
			case landing.team == owner:
				soldiers += landing.soldiers
			case landing.soldiers > soldiers:
				owner, soldiers = landing.team, landing.soldiers-soldiers
			default:
				soldiers -= landing.soldiers
			}
		}
	}
	return owner, soldiers
}

// garrisonToHold is how many of its soldiers a castle of team must keep this
// turn so that none of the arrivals takes it, counting the soldiers it grows
// and the friendly groups that land before each hostile one. It is 0 when the
// castle holds even with none.
func garrisonToHold(arrivals []arrival, team uint) uint {
	balance, needed := 0, 0
	for _, landing := range slices.SortedFunc(slices.Values(arrivals), compareArrivals) {
		grown := balance + landing.turn
		if landing.team == team {
			balance += int(landing.soldiers)
			continue
		}
		needed = max(needed, int(landing.soldiers)-grown)
		balance -= int(landing.soldiers)
	}
	return uint(needed)
}

// logLine is an info line of a default bot's game log, from the logger
// island_battles.bots.level1 to level3, cut to the length a line may have.
func logLine(level uint, format string, arguments ...any) islandbattles.LogLine {
	message := fmt.Sprintf(format, arguments...)
	if utf8.RuneCountInString(message) > islandbattles.MaxLogMessageLength {
		runes := []rune(message)
		message = string(runes[:islandbattles.MaxLogMessageLength-1]) + "…"
	}
	return islandbattles.LogLine{Level: islandbattles.LogInfo, Logger: fmt.Sprintf("island_battles.bots.level%d", level), Message: message}
}
