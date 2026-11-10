package defaultbots

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	islandbattles "github.com/INTODAN/island-battles-go/clients/island-battles-go"
)

// LevelTwo attacks: each of its castles sends a group to the nearest castle
// it can take, enemy or neutral, when its soldiers are enough to beat the
// defenders that castle will have when the group arrives. Otherwise it waits
// and grows. It doesn't defend, buy upgrades or keep memory.
func LevelTwo(_ context.Context, turn islandbattles.Turn) (islandbattles.Result, error) {
	board := newBoard(turn)
	result := islandbattles.Result{Commands: []islandbattles.Command{}}
	plans := planned{}
	attacks := []string{}
	var strongest *islandbattles.Castle
	for _, origin := range board.owned() {
		if strongest == nil || origin.Soldiers > strongest.Soldiers {
			strongest = &origin
		}
		for _, target := range byDistance(origin, board.others()) {
			soldiers, defenders, turns, ok := board.attack(origin, target, plans)
			if !ok || soldiers > origin.Soldiers {
				continue
			}
			result.Commands = append(result.Commands, islandbattles.SendSoldierGroup(origin.ID, target.ID, soldiers))
			plans.add(board, origin, target, soldiers)
			attacks = append(attacks, fmt.Sprintf("castle %d attacks %s castle %d with %d soldiers, expecting %d defenders in %d turns", origin.ID, describeOwner(target, board.team), target.ID, soldiers, defenders, turns))
			break
		}
	}
	switch {
	case strongest == nil:
		result.Logs = []islandbattles.LogLine{logLine(2, "I have no castles left; passing.")}
	case len(attacks) == 0:
		result.Logs = []islandbattles.LogLine{logLine(2, "No castle I can take yet; growing. My strongest castle %d has %d soldiers.", strongest.ID, strongest.Soldiers)}
	default:
		result.Logs = []islandbattles.LogLine{logLine(2, "Attacking: %s.", strings.Join(attacks, "; "))}
	}
	return result, nil
}

// attackMargin is how many soldiers an attack sends beyond the defenders it
// expects, for the soldiers it may lose on the way.
func attackMargin(defenders uint) uint { return 2 + defenders/10 }

// planned is a group a bot sends this turn, as an arrival at its target.
type planned map[uint][]arrival

func (plans planned) add(board board, origin, target islandbattles.Castle, soldiers uint) {
	plans[target.ID] = append(plans[target.ID], arrival{
		turn: travelTurns(origin.Distance(target), board.speed(origin.Upgrades)), id: board.nextGroupID() + uint(plans.count()),
		team: board.team, soldiers: soldiers,
	})
}

func (plans planned) count() int {
	count := 0
	for _, arrivals := range plans {
		count += len(arrivals)
	}
	return count
}

// attack is the smallest group that takes target from origin, and when it
// lands. ok is false when the castle will be the bot's anyway.
func (board board) attack(origin, target islandbattles.Castle, plans planned) (soldiers, defenders uint, turns int, ok bool) {
	turns = travelTurns(origin.Distance(target), board.speed(origin.Upgrades))
	owner, defenders := forecast(target, append(board.arrivals(target), plans[target.ID]...), turns)
	if owner == board.team {
		return 0, 0, turns, false
	}
	return defenders + 1 + attackMargin(defenders), defenders, turns, true
}

// byDistance lists castles from the nearest to origin, then by ID.
func byDistance(origin islandbattles.Castle, castles []islandbattles.Castle) []islandbattles.Castle {
	sorted := slices.Clone(castles)
	slices.SortStableFunc(sorted, func(left, right islandbattles.Castle) int {
		return cmp.Or(cmp.Compare(origin.Distance(left), origin.Distance(right)), cmp.Compare(left.ID, right.ID))
	})
	return sorted
}

// describeOwner says who owns a castle the bot doesn't own.
func describeOwner(castle islandbattles.Castle, team uint) string {
	switch castle.TeamColor {
	case islandbattles.NeutralTeamColor:
		return "neutral"
	case team:
		return "my"
	}
	return "enemy"
}
