package commandHandlers

import (
	"islandBattles/backend/dal"
	"islandBattles/backend/models"
	"math"

	"github.com/jackc/pgx/v5/pgtype"
	"gorm.io/gorm"
)

type MoveSoldierGroupsParams struct{}
type Line struct {
	Start     pgtype.Point
	End       pgtype.Point
	TeamColor uint
}

func MoveSoldierGroupsHandler(db *gorm.DB, generation *models.Generation, parameters MoveSoldierGroupsParams) error {
	moveVectors := make(map[uint]Line)
	for i := range generation.SoldierGroups {
		soldierGroup := &generation.SoldierGroups[i]
		newPosition := MoveTowards(soldierGroup.Position, soldierGroup.DestinationCastle.Position)
		moveVectors[soldierGroup.ID] = Line{Start: soldierGroup.Position, End: newPosition, TeamColor: soldierGroup.TeamColor}
	}
	clashes := CalculateClashes(db, moveVectors)
	for clashedSoldierGroupID, clashedPosition := range clashes {
		vector := moveVectors[clashedSoldierGroupID]
		vector.End = clashedPosition
		moveVectors[clashedSoldierGroupID] = vector
	}

	err := db.Model(generation).Association("SoldierGroups").Find(&generation.SoldierGroups)
	if err != nil {
		return err
	}

	for i := range generation.SoldierGroups {
		soldierGroup := &generation.SoldierGroups[i]
		err := dal.UpdateSoldierGroupPosition(db, soldierGroup, moveVectors[soldierGroup.ID].End)
		if err != nil {
			return err
		}
	}

	return nil
}

// MoveTowards creates a new position that is closer to the destination by models.SoldierGroupSpeedPerGeneration points
func MoveTowards(currentPosition pgtype.Point, destination pgtype.Point) pgtype.Point {
	dx := destination.P.X - currentPosition.P.X
	dy := destination.P.Y - currentPosition.P.Y
	distance := math.Sqrt(dx*dx + dy*dy)

	if distance <= models.SoldierGroupSpeedPerGeneration {
		return destination
	}

	ratio := models.SoldierGroupSpeedPerGeneration / distance
	newX := currentPosition.P.X + dx*ratio
	newY := currentPosition.P.Y + dy*ratio

	return pgtype.Point{
		P:     pgtype.Vec2{X: newX, Y: newY},
		Valid: true,
	}
}

// CalculateClashes Receives all the vectors of all the SoldierGroups, and checks whether two soldier groups collided,
// aka whether their vectors intersect. If they do, return in the map the intersection point of the soldier groups.
// If a soldier group clashes in multiple positions, return the clash with the earliest clash (The one closest to the Start)
// Calculate the intersection based both on whether the vectors intersect and whether the soldier groups would hit each other
// knowing they move through the line at a constant speed.
func CalculateClashes(db *gorm.DB, moveVectors map[uint]Line) map[uint]pgtype.Point {
	type ClashInfo struct {
		Time  float64
		Point pgtype.Point
	}
	clashes := make(map[uint]ClashInfo)
	const epsilon = 1e-9

	// Convert map to slice for stable iteration order (optional but good for testing if we sort, but generic is fine)
	var ids []uint
	for id := range moveVectors {
		ids = append(ids, id)
	}

	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			id1, id2 := ids[i], ids[j]
			l1, l2 := moveVectors[id1], moveVectors[id2]

			if l1.Start == l2.Start || l1.TeamColor == l2.TeamColor {
				continue
			}

			// Calculate collision time t
			// P1(t) = S1 + t(E1-S1)
			// P2(t) = S2 + t(E2-S2)
			// Solve P1(t) = P2(t) => t(V1 - V2) = S2 - S1

			v1x, v1y := l1.End.P.X-l1.Start.P.X, l1.End.P.Y-l1.Start.P.Y
			v2x, v2y := l2.End.P.X-l2.Start.P.X, l2.End.P.Y-l2.Start.P.Y

			dVx, dVy := v1x-v2x, v1y-v2y
			dSx, dSy := l2.Start.P.X-l1.Start.P.X, l2.Start.P.Y-l1.Start.P.Y

			var t float64
			valid := false

			// Check X dimension
			if math.Abs(dVx) > epsilon {
				tx := dSx / dVx
				// Verify Y dimension
				if math.Abs(dVy) > epsilon {
					ty := dSy / dVy
					if math.Abs(tx-ty) < epsilon {
						t = tx
						valid = true
					}
				} else {
					// dVy ~ 0, check if positions match in Y
					// S1y + t*V1y = S2y + t*V2y => t*dVy = dSy => 0 = dSy
					if math.Abs(dSy) < epsilon {
						t = tx
						valid = true
					}
				}
			} else {
				// dVx ~ 0, check positions in X
				if math.Abs(dSx) < epsilon {
					// Use Y to find t
					if math.Abs(dVy) > epsilon {
						t = dSy / dVy
						valid = true
					} else {
						// Both velocities parallel and positions match -> overlapping at t=0 (and potentially others)
						if math.Abs(dSy) < epsilon {
							t = 0
							valid = true
						}
					}
				}
			}

			if valid && t >= 0 && t <= 1 {
				// Calculate collision point
				collisionX := l1.Start.P.X + t*v1x
				collisionY := l1.Start.P.Y + t*v1y
				collisionPoint := pgtype.Point{
					P:     pgtype.Vec2{X: collisionX, Y: collisionY},
					Valid: true,
				}

				// Update clashes for id1
				if existing, ok := clashes[id1]; !ok || t < existing.Time {
					clashes[id1] = ClashInfo{Time: t, Point: collisionPoint}
				}
				// Update clashes for id2
				if existing, ok := clashes[id2]; !ok || t < existing.Time {
					clashes[id2] = ClashInfo{Time: t, Point: collisionPoint}
				}
			}
		}
	}

	result := make(map[uint]pgtype.Point)
	for id, info := range clashes {
		result[id] = info.Point
	}
	return result
}
