package calculations

import (
	"fmt"
	"math"
	"sort"

	"islandBattles/backend/dal/commandHandlers/move_soldier_groups/internal"

	"github.com/jackc/pgx/v5/pgtype"
)

const epsilon = 1e-9

// CalculateClashPositions checks the entire movement interval for entry into
// either enemy group's clash radius (the larger radius, not the sum). Groups
// stop at their own first contact position for this turn. Simultaneous connected
// contacts form one clash; a group cannot fight twice in the same turn.
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
	clashed := make([]bool, len(movements))
	clashes := []internal.Clash{}
	for first := 0; first < len(contacts); {
		last := first + 1
		for last < len(contacts) && math.Abs(contacts[last].time-contacts[first].time) < epsilon {
			last++
		}
		// Union only simultaneous contacts whose groups did not clash earlier.
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
			if clashed[contact.left] || clashed[contact.right] {
				continue
			}
			participating[contact.left], participating[contact.right] = true, true
			parents[root(contact.right)] = root(contact.left)
		}
		components := map[int]int{}
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
				SoldierGroup: movement.SoldierGroup, Position: positionAt(movement, contacts[first].time),
			})
			clashed[index] = true
		}
		first = last
	}
	return clashes, nil
}

// Solve |relativeStart + time*relativeVelocity| <= radius for the first time
// in [0,1]. Closest approach avoids dividing by nearly stationary axes.
func contactTime(left, right internal.SoldierGroupMoveDelta, radius float64) (float64, bool) {
	startX, startY := left.Start.P.X-right.Start.P.X, left.Start.P.Y-right.Start.P.Y
	if math.Hypot(startX, startY) <= radius+epsilon {
		return 0, true
	}
	velocityX := (left.End.P.X - left.Start.P.X) - (right.End.P.X - right.Start.P.X)
	velocityY := (left.End.P.Y - left.Start.P.Y) - (right.End.P.Y - right.Start.P.Y)
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
	if entryTime < -epsilon || entryTime > 1+epsilon {
		return 0, false
	}
	return math.Max(0, math.Min(1, entryTime)), true
}

func positionAt(movement internal.SoldierGroupMoveDelta, time float64) pgtype.Point {
	return pgtype.Point{P: pgtype.Vec2{
		X: movement.Start.P.X + time*(movement.End.P.X-movement.Start.P.X),
		Y: movement.Start.P.Y + time*(movement.End.P.Y-movement.Start.P.Y),
	}, Valid: true}
}
