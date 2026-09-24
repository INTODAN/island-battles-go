package calculations

import (
	"fmt"
	"math"
	"sort"

	"islandBattles/backend/dal/commandHandlers/move_soldier_groups/internal"
	"islandBattles/backend/models"

	"github.com/jackc/pgx/v5/pgtype"
)

const epsilon = 1e-9

// CalculateClashPositions checks the entire movement interval for entry into
// either enemy group's clash radius (the larger radius, not the sum). Groups
// that survive continue moving and can fight again. Simultaneous connected
// contacts form one clash, excluding groups destroyed at earlier contacts.
func CalculateClashPositions(movements []internal.SoldierGroupMoveDelta) ([]internal.Clash, error) {
	type contact struct {
		time        float64
		left, right int
	}
	contacts := []contact{}
	for _, movement := range movements {
		radius := movement.SoldierGroup.EffectiveClashRadius()
		if radius < 0 || math.IsNaN(radius) || math.IsInf(radius, 0) {
			return nil, fmt.Errorf("invalid soldier group clash radius: %v", radius)
		}
	}
	for left := range movements {
		for right := left + 1; right < len(movements); right++ {
			if movements[left].SoldierGroup.TeamColor == movements[right].SoldierGroup.TeamColor {
				continue
			}
			radius := math.Max(movements[left].SoldierGroup.EffectiveClashRadius(), movements[right].SoldierGroup.EffectiveClashRadius())
			if time, meets := contactTime(movements[left], movements[right], radius); meets {
				contacts = append(contacts, contact{time: time, left: left, right: right})
			}
		}
	}
	sort.SliceStable(contacts, func(left, right int) bool { return contacts[left].time < contacts[right].time })
	remaining := make(map[*models.SoldierGroup]uint, len(movements))
	for _, movement := range movements {
		remaining[movement.SoldierGroup] = movement.SoldierGroup.SoldierAmount
	}
	clashes := []internal.Clash{}
	for first := 0; first < len(contacts); {
		last := first + 1
		for last < len(contacts) && math.Abs(contacts[last].time-contacts[first].time) < epsilon {
			last++
		}
		// Dead groups cannot shield survivors or connect separate fights.
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
		for _, contact := range contacts[first:last] {
			if remaining[movements[contact.left].SoldierGroup] == 0 || remaining[movements[contact.right].SoldierGroup] == 0 {
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
				clashes = append(clashes, internal.Clash{Time: contacts[first].time})
			}
			clashes[clashIndex].Participants = append(clashes[clashIndex].Participants, internal.ClashParticipant{
				SoldierGroup: movement.SoldierGroup, Position: positionAt(movement, contacts[first].time), RemainingSoldiers: remaining[movement.SoldierGroup],
			})
		}
		for index := firstClash; index < len(clashes); index++ {
			resolveClash(&clashes[index])
			for _, participant := range clashes[index].Participants {
				remaining[participant.SoldierGroup] = participant.RemainingSoldiers
			}
		}
		first = last
	}
	return clashes, nil
}

// Solve |relativeStart + time*relativeVelocity| <= radius for the first time
// in [0,1]. Closest approach avoids dividing by nearly stationary axes.
func contactTime(left, right internal.SoldierGroupMoveDelta, radius float64) (float64, bool) {
	// Arrival wins a tie with contact, including a group already at its castle.
	endTime := math.Inf(1)
	for _, movement := range []internal.SoldierGroupMoveDelta{left, right} {
		if movement.ArrivalTime != nil {
			endTime = math.Min(endTime, *movement.ArrivalTime)
		}
	}
	if endTime <= epsilon {
		return 0, false
	}
	startX, startY := left.Start.P.X-right.Start.P.X, left.Start.P.Y-right.Start.P.Y
	if math.Hypot(startX, startY) <= radius+epsilon {
		return 0, true
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
	if entryTime < -epsilon || entryTime > 1+epsilon || entryTime >= endTime-epsilon {
		return 0, false
	}
	return math.Max(0, math.Min(1, entryTime)), true
}

func positionAt(movement internal.SoldierGroupMoveDelta, time float64) pgtype.Point {
	if movement.ArrivalTime != nil && time >= *movement.ArrivalTime {
		return movement.End
	}
	velocity := velocity(movement)
	return pgtype.Point{P: pgtype.Vec2{
		X: movement.Start.P.X + time*velocity.X,
		Y: movement.Start.P.Y + time*velocity.Y,
	}, Valid: true}
}

func velocity(movement internal.SoldierGroupMoveDelta) pgtype.Vec2 {
	duration := 1.0
	if movement.ArrivalTime != nil {
		duration = *movement.ArrivalTime
	}
	if duration <= epsilon {
		return pgtype.Vec2{}
	}
	return pgtype.Vec2{X: (movement.End.P.X - movement.Start.P.X) / duration, Y: (movement.End.P.Y - movement.Start.P.Y) / duration}
}

// A team survives only when it outnumbers all opposing teams together. Resolve
// losses in logical group order, independent of database or map iteration order.
func resolveClash(clash *internal.Clash) {
	sort.SliceStable(clash.Participants, func(left, right int) bool {
		return clash.Participants[left].SoldierGroup.LogicalSoldierGroupID < clash.Participants[right].SoldierGroup.LogicalSoldierGroupID
	})
	totals := map[uint]uint{}
	for _, participant := range clash.Participants {
		totals[participant.SoldierGroup.TeamColor] += participant.RemainingSoldiers
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
		loss := min(participant.RemainingSoldiers, losses[team])
		participant.RemainingSoldiers -= loss
		losses[team] -= loss
	}
}
