package simulation

import (
	"fmt"
	"math"
	"sort"

	"islandBattles/backend/models"

	"github.com/jackc/pgx/v5/pgtype"
)

const epsilon = 1e-9

// calculateClashPositions resolves contacts in time order and updates movement
// plans when a balanced clash stops groups. Recheck paths after every clash so
// stopped survivors remain vulnerable to later enemies.
func calculateClashPositions(movements []soldierGroupMoveDelta) ([]resolvedClash, error) {
	type contact struct {
		time        float64
		left, right int
	}
	remaining := make(map[*SoldierGroup]uint, len(movements))
	movementIndices := make(map[*SoldierGroup]int, len(movements))
	for index, movement := range movements {
		radius := movement.SoldierGroup.EffectiveClashRadius()
		if radius < 0 || math.IsNaN(radius) || math.IsInf(radius, 0) {
			return nil, fmt.Errorf("invalid soldier group clash radius: %v", radius)
		}
		remaining[movement.SoldierGroup] = movement.SoldierGroup.Soldiers
		movementIndices[movement.SoldierGroup] = index
	}
	clashes := []resolvedClash{}
	currentTime := 0.0
	for {
		contacts := []contact{}
		nextTime := math.Inf(1)
		for left := range movements {
			for right := left + 1; right < len(movements); right++ {
				leftGroup, rightGroup := movements[left].SoldierGroup, movements[right].SoldierGroup
				if remaining[leftGroup] == 0 || remaining[rightGroup] == 0 || leftGroup.TeamColor == rightGroup.TeamColor {
					continue
				}
				radius := math.Max(leftGroup.EffectiveClashRadius(), rightGroup.EffectiveClashRadius())
				if time, meets := contactTimeFrom(movements[left], movements[right], radius, currentTime); meets {
					contacts = append(contacts, contact{time: time, left: left, right: right})
					nextTime = math.Min(nextTime, time)
				}
			}
		}
		if len(contacts) == 0 {
			break
		}
		parents := make([]int, len(movements))
		for index := range parents {
			parents[index] = index
		}
		root := func(index int) int {
			for parents[index] != index {
				index = parents[index]
			}
			return index
		}
		participating := make([]bool, len(movements))
		for _, contact := range contacts {
			if math.Abs(contact.time-nextTime) >= epsilon {
				continue
			}
			participating[contact.left], participating[contact.right] = true, true
			parents[root(contact.right)] = root(contact.left)
		}
		components := map[int]int{}
		firstClash := len(clashes)
		for index, movement := range movements {
			if !participating[index] {
				continue
			}
			component := root(index)
			clashIndex, exists := components[component]
			if !exists {
				clashIndex = len(clashes)
				components[component] = clashIndex
				clashes = append(clashes, resolvedClash{Time: nextTime})
			}
			clashes[clashIndex].Participants = append(clashes[clashIndex].Participants, clashParticipant{
				SoldierGroup: movement.SoldierGroup, Position: positionAt(movement, nextTime), SoldiersBefore: remaining[movement.SoldierGroup],
			})
		}
		for index := firstClash; index < len(clashes); index++ {
			clash := &clashes[index]
			if shouldStop(clash) {
				// For two groups this is their contact midpoint; simultaneous multi-group
				// clashes use the average of all participant positions.
				midpoint := pgtype.Point{Valid: true}
				for _, participant := range clash.Participants {
					midpoint.P.X += participant.Position.P.X / float64(len(clash.Participants))
					midpoint.P.Y += participant.Position.P.Y / float64(len(clash.Participants))
				}
				for _, participant := range clash.Participants {
					stopGenerations, err := participant.SoldierGroup.Upgrades.Value(models.ClashStopGenerationsUpgrade)
					if err != nil {
						return nil, err
					}
					movement := &movements[movementIndices[participant.SoldierGroup]]
					movement.Start, movement.End, movement.StartTime = midpoint, midpoint, nextTime
					movement.ArrivalTime = nil
					if midpoint == movement.Destination {
						time := nextTime
						movement.ArrivalTime = &time
					}
					movement.PauseGenerations = max(movement.PauseGenerations, uint(stopGenerations)-1)
				}
			}
			resolveClash(clash)
			for _, participant := range clash.Participants {
				remaining[participant.SoldierGroup] = participant.SoldiersAfter
			}
		}
		// Each clash eliminates at least one opposing team, so recomputing cannot
		// repeat combat forever at the same timestamp.
		currentTime = nextTime
	}
	return clashes, nil
}

// clashEvents numbers resolved clashes in time order, starting at firstID, and
// converts them into the events stored with the generation.
func clashEvents(clashes []resolvedClash, firstID uint) []models.Clash {
	events := make([]models.Clash, 0, len(clashes))
	for index, clash := range clashes {
		event := models.Clash{ID: firstID + uint(index), Time: clash.Time, Participants: make([]models.ClashParticipant, 0, len(clash.Participants))}
		for _, participant := range clash.Participants {
			group := participant.SoldierGroup
			event.Participants = append(event.Participants, models.ClashParticipant{
				GroupID: group.ID, TeamColor: group.TeamColor,
				X: participant.Position.P.X, Y: participant.Position.P.Y, ClashRadius: group.EffectiveClashRadius(),
				SoldiersBefore: participant.SoldiersBefore, SoldiersAfter: participant.SoldiersAfter,
			})
		}
		events = append(events, event)
	}
	return events
}

// Compare pre-casualty group counts, so resolving the clash first does not change
// the result. Exactly half still stops; subtraction avoids rounding odd counts or
// overflowing when testing twice the smaller count.
func shouldStop(clash *resolvedClash) bool {
	for left, participant := range clash.Participants {
		for _, opponent := range clash.Participants[left+1:] {
			if participant.SoldierGroup.TeamColor == opponent.SoldierGroup.TeamColor {
				continue
			}
			smaller, larger := min(participant.SoldiersBefore, opponent.SoldiersBefore), max(participant.SoldiersBefore, opponent.SoldiersBefore)
			if smaller >= larger-smaller {
				return true
			}
		}
	}
	return false
}

// Solve |relativeStart + time*relativeVelocity| <= radius for the first time
// in [0,1]. Closest approach avoids dividing by nearly stationary axes.
func contactTimeFrom(left, right soldierGroupMoveDelta, radius, from float64) (float64, bool) {
	// Arrival wins a tie with contact, including a group already at its castle.
	endTime := math.Inf(1)
	for _, movement := range []soldierGroupMoveDelta{left, right} {
		if movement.ArrivalTime != nil {
			endTime = math.Min(endTime, *movement.ArrivalTime)
		}
	}
	if endTime <= from+epsilon {
		return 0, false
	}
	leftPosition, rightPosition := positionAt(left, from), positionAt(right, from)
	startX, startY := leftPosition.P.X-rightPosition.P.X, leftPosition.P.Y-rightPosition.P.Y
	if math.Hypot(startX, startY) <= radius+epsilon {
		return from, true
	}
	leftVelocity, rightVelocity := velocity(left), velocity(right)
	velocityX := leftVelocity.X - rightVelocity.X
	velocityY := leftVelocity.Y - rightVelocity.Y
	speedSquared := velocityX*velocityX + velocityY*velocityY
	if speedSquared <= epsilon*epsilon {
		return 0, false
	}
	closestTime := -(startX*velocityX + startY*velocityY) / speedSquared
	closestDistance := math.Hypot(startX+closestTime*velocityX, startY+closestTime*velocityY)
	if closestDistance > radius+epsilon {
		return 0, false
	}
	entryTime := closestTime - math.Sqrt(math.Max(0, radius*radius-closestDistance*closestDistance)/speedSquared)
	if entryTime < -epsilon || entryTime > 1-from+epsilon || from+entryTime >= endTime-epsilon {
		return 0, false
	}
	return math.Max(0, math.Min(1, from+entryTime)), true
}

func positionAt(movement soldierGroupMoveDelta, time float64) pgtype.Point {
	if movement.ArrivalTime != nil && time >= *movement.ArrivalTime {
		return movement.End
	}
	velocity := velocity(movement)
	return pgtype.Point{P: pgtype.Vec2{
		X: movement.Start.P.X + (time-movement.StartTime)*velocity.X,
		Y: movement.Start.P.Y + (time-movement.StartTime)*velocity.Y,
	}, Valid: true}
}

func velocity(movement soldierGroupMoveDelta) pgtype.Vec2 {
	duration := 1.0 - movement.StartTime
	if movement.ArrivalTime != nil {
		duration = *movement.ArrivalTime - movement.StartTime
	}
	if duration <= epsilon {
		return pgtype.Vec2{}
	}
	return pgtype.Vec2{X: (movement.End.P.X - movement.Start.P.X) / duration, Y: (movement.End.P.Y - movement.Start.P.Y) / duration}
}

// A team survives only when it outnumbers all opposing teams together. Resolve
// losses in persistent group ID order, independent of database or map iteration order.
// Losses are computed from SoldiersBefore, and SoldiersAfter is the only count this writes.
func resolveClash(clash *resolvedClash) {
	sort.SliceStable(clash.Participants, func(left, right int) bool {
		return clash.Participants[left].SoldierGroup.ID < clash.Participants[right].SoldierGroup.ID
	})
	totals := map[uint]uint{}
	for _, participant := range clash.Participants {
		totals[participant.SoldierGroup.TeamColor] += participant.SoldiersBefore
	}
	losses := map[uint]uint{}
	for team, total := range totals {
		for enemy, amount := range totals {
			if enemy != team {
				// Saturate at the team's total, avoiding overflow while summing enemies.
				losses[team] += min(amount, total-losses[team])
			}
		}
	}
	for index := range clash.Participants {
		participant := &clash.Participants[index]
		team := participant.SoldierGroup.TeamColor
		loss := min(participant.SoldiersBefore, losses[team])
		participant.SoldiersAfter = participant.SoldiersBefore - loss
		losses[team] -= loss
	}
}
