package move_soldier_groups

import (
	"islandBattles/backend/models"
	"math"

	"github.com/jackc/pgx/v5/pgtype"
)

// calculateClashPositions Receives all the vectors of all the SoldierGroups, and checks whether two soldier groups collided,
// aka whether their vectors intersect. If they do, return in the map the intersection point of the soldier groups.
// If a soldier group clashes in multiple positions, return the clash with the earliest clash (The one closest to the Start)
// Calculate the intersection based both on whether the vectors intersect and whether the soldier groups would hit each other
// knowing they move through the line at a constant speed.
func calculateClashPositions(soldierGroupsPositions []soldierGroupMoveDelta) map[pgtype.Point][]*models.SoldierGroup {
	type ClashInfo struct {
		Time  float64
		Point pgtype.Point
	}
	clashes := make(map[soldierGroupMoveDelta]ClashInfo)
	const epsilon = 1e-9

	for _, l1 := range soldierGroupsPositions {
		for _, l2 := range soldierGroupsPositions {
			if l1.Start == l2.Start || l1.SoldierGroup.TeamColor == l2.SoldierGroup.TeamColor {
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
				if existing, ok := clashes[l1]; !ok || t < existing.Time {
					clashes[l1] = ClashInfo{Time: t, Point: collisionPoint}
				}
				// Update clashes for id2
				if existing, ok := clashes[l2]; !ok || t < existing.Time {
					clashes[l2] = ClashInfo{Time: t, Point: collisionPoint}
				}
			}
		}
	}

	clashPositions := make(map[pgtype.Point][]*models.SoldierGroup)
	for soldierGroupMoveDelta, info := range clashes {
		clashPositions[info.Point] = append(clashPositions[info.Point], soldierGroupMoveDelta.SoldierGroup)
	}

	return clashPositions
}
