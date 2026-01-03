package move_soldier_groups

import (
	"islandBattles/backend/models"
	"math"

	"github.com/jackc/pgx/v5/pgtype"
)

type clashInfo struct {
	Time  float64
	Point pgtype.Point
}

// calculateClashPositions Calculates all the clashes that occurred between the soldier groups received.
// X1(t) = start1 + t * distance1
// X2(t) = start2 + t * distance2
// t(distance1 - distance2) = start2 - start1
// t = (start2 - start1) / (distance1 - distance2)
// calculate tX, tY
// make sure tX == tY -+ epsilon
// check that 0<=t<=1 to make sure intersection happens in time frame
// Finally on collision, calculate the X, Y for the collision

func calculateClashPositions(soldierGroupsPositions []soldierGroupMoveDelta) map[pgtype.Point][]*models.SoldierGroup {
	clashes := make(map[soldierGroupMoveDelta]clashInfo)
	const epsilon = 1e-9

	for _, line1 := range soldierGroupsPositions {
		for _, line2 := range soldierGroupsPositions {
			if line1.Start == line2.Start || line1.SoldierGroup.TeamColor == line2.SoldierGroup.TeamColor {
				continue
			}

			distance1 := calculateDistance(line1)
			distance2 := calculateDistance(line2)

			tX := calculateTime(line1.Start.P.X, line2.Start.P.X, distance1.X, distance2.X)
			tY := calculateTime(line1.Start.P.Y, line2.Start.P.Y, distance1.Y, distance2.Y)

			if t := tX; math.Abs(tX-tY) < epsilon && 0 <= t && t <= 1 {
				// We have a clash in the time frame.
				collisionX := calculatePosition(line1.Start.P.X, distance1.X, t)
				collisionY := calculatePosition(line1.Start.P.Y, distance1.Y, t)

				clash := clashInfo{
					Time: t,
					Point: pgtype.Point{
						P:     pgtype.Vec2{X: collisionX, Y: collisionY},
						Valid: true,
					},
				}

				updateClashes(line1, clash, clashes)
				updateClashes(line2, clash, clashes)
			}
		}
	}

	clashPositions := make(map[pgtype.Point][]*models.SoldierGroup)
	for curSoldierGroupMoveDelta, clashInfo := range clashes {
		clashPositions[clashInfo.Point] = append(clashPositions[clashInfo.Point], curSoldierGroupMoveDelta.SoldierGroup)
	}

	return clashPositions
}

// updateClashes making sure to update clashes only if an earlier clash is detected
func updateClashes(
	line soldierGroupMoveDelta,
	clash clashInfo,
	clashes map[soldierGroupMoveDelta]clashInfo,
) {
	if existing, ok := clashes[line]; !ok || clash.Time < existing.Time {
		clashes[line] = clash
	}
}

func calculateDistance(line soldierGroupMoveDelta) pgtype.Vec2 {
	return pgtype.Vec2{X: line.End.P.X - line.Start.P.X, Y: line.End.P.Y - line.Start.P.Y}
}

func calculateTime(start1 float64, start2 float64, distance1 float64, distance2 float64) float64 {
	return (start2 - start1) / (distance1 - distance2)
}

func calculatePosition(start float64, distance float64, t float64) float64 {
	return start + t*distance
}
