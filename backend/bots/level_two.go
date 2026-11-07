package bots

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"islandBattles/backend/models"
)

// LevelTwo attacks: each of its castles sends a group to the nearest castle
// it can take, enemy or neutral, when its soldiers are enough to beat the
// defenders that castle will have when the group arrives. Otherwise it waits
// and grows. It doesn't defend, buy upgrades or keep memory.
type LevelTwo struct{}

// attackMargin is how many soldiers an attack sends beyond the defenders it
// expects, for the soldiers it may lose on the way.
func attackMargin(defenders uint) uint { return 2 + defenders/10 }

// planned is a group a bot sends this turn, as an arrival at its target.
type planned map[uint][]arrival

func (plans planned) add(board board, origin, target models.CastleView, soldiers uint) {
	plans[target.ID] = append(plans[target.ID], arrival{
		turn: travelTurns(distance(origin, target), speed(origin.Upgrades)), id: board.nextGroupID() + uint(plans.count()),
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
func (board board) attack(origin, target models.CastleView, plans planned) (soldiers, defenders uint, turns int, ok bool) {
	turns = travelTurns(distance(origin, target), speed(origin.Upgrades))
	owner, defenders := forecast(target, append(board.arrivals(target), plans[target.ID]...), turns)
	if owner == board.team {
		return 0, 0, turns, false
	}
	return defenders + 1 + attackMargin(defenders), defenders, turns, true
}

// byDistance lists castles from the nearest to origin, then by ID.
func byDistance(origin models.CastleView, castles []models.CastleView) []models.CastleView {
	sorted := slices.Clone(castles)
	slices.SortStableFunc(sorted, func(left, right models.CastleView) int {
		return cmp.Or(cmp.Compare(distance(origin, left), distance(origin, right)), cmp.Compare(left.ID, right.ID))
	})
	return sorted
}

func (LevelTwo) Play(_ context.Context, turn models.BotTurn) (models.BotTurnResult, error) {
	board := newBoard(turn)
	result := models.BotTurnResult{Commands: []models.PublicGenerationCommand{}}
	plans := planned{}
	attacks := []string{}
	var strongest *models.CastleView
	for _, origin := range board.owned() {
		if strongest == nil || origin.Soldiers > strongest.Soldiers {
			strongest = &origin
		}
		for _, target := range byDistance(origin, board.others()) {
			soldiers, defenders, turns, ok := board.attack(origin, target, plans)
			if !ok || soldiers > origin.Soldiers {
				continue
			}
			result.Commands = append(result.Commands, sendCommand(origin.ID, target.ID, soldiers))
			plans.add(board, origin, target, soldiers)
			attacks = append(attacks, fmt.Sprintf("castle %d attacks %s castle %d with %d soldiers, expecting %d defenders in %d turns", origin.ID, describeOwner(target, board.team), target.ID, soldiers, defenders, turns))
			break
		}
	}
	switch {
	case strongest == nil:
		result.Logs = []models.GenerationLogLine{logLine(2, "I have no castles left; passing.")}
	case len(attacks) == 0:
		result.Logs = []models.GenerationLogLine{logLine(2, "No castle I can take yet; growing. My strongest castle %d has %d soldiers.", strongest.ID, strongest.Soldiers)}
	default:
		result.Logs = []models.GenerationLogLine{logLine(2, "Attacking: %s.", strings.Join(attacks, "; "))}
	}
	return result, nil
}

// describeOwner says who owns a castle the bot doesn't own.
func describeOwner(castle models.CastleView, team uint) string {
	switch castle.TeamColor {
	case models.NeutralTeamColor:
		return "neutral"
	case team:
		return "my"
	}
	return "enemy"
}
