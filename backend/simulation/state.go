// Package simulation holds the game rules. It works on an in-memory GameState
// and never touches the database: the dal package loads a GameState and saves
// the result as a new generation.
package simulation

import (
	"fmt"
	"maps"
	"slices"

	"islandBattles/backend/models"

	"github.com/jackc/pgx/v5/pgtype"
)

// GameState is one generation's board. Castles and soldier groups are keyed by
// their persistent IDs, which stay the same across generations.
type GameState struct {
	Map           models.MapFields
	Castles       map[uint]*Castle
	SoldierGroups map[uint]*SoldierGroup
	// NextSoldierGroupID is the ID the next spawned group receives. It never
	// decreases, so a departed group's ID is never reassigned.
	NextSoldierGroupID uint
}

type Castle struct {
	ID        uint
	TeamColor uint
	Soldiers  uint
	Position  pgtype.Point
	Upgrades  models.Upgrades
}

type SoldierGroup struct {
	ID                       uint
	TeamColor                uint
	Soldiers                 uint
	Position                 pgtype.Point
	OriginCastleID           uint
	DestinationCastleID      uint
	MovementPauseGenerations uint
	ClashRadius              float64
	// Upgrades is a snapshot of the origin castle's upgrades when the group left.
	Upgrades models.Upgrades
}

// EffectiveClashRadius supplies the default for groups created without a radius.
func (group SoldierGroup) EffectiveClashRadius() float64 {
	if group.ClashRadius == 0 {
		return models.DefaultClashRadius
	}
	return group.ClashRadius
}

// Clone returns a deep copy, so changing it leaves the original untouched.
func (state GameState) Clone() GameState {
	clone := GameState{
		Map:                state.Map,
		Castles:            make(map[uint]*Castle, len(state.Castles)),
		SoldierGroups:      make(map[uint]*SoldierGroup, len(state.SoldierGroups)),
		NextSoldierGroupID: state.NextSoldierGroupID,
	}
	for id, castle := range state.Castles {
		castleCopy := *castle
		castleCopy.Upgrades = maps.Clone(castle.Upgrades)
		clone.Castles[id] = &castleCopy
	}
	for id, group := range state.SoldierGroups {
		groupCopy := *group
		groupCopy.Upgrades = maps.Clone(group.Upgrades)
		clone.SoldierGroups[id] = &groupCopy
	}
	return clone
}

// SortedCastles lists the castles in persistent ID order.
func (state GameState) SortedCastles() []*Castle {
	return sortedByID(state.Castles)
}

// SortedSoldierGroups lists the soldier groups in persistent ID order, which
// every rule uses so results never depend on map iteration order.
func (state GameState) SortedSoldierGroups() []*SoldierGroup {
	return sortedByID(state.SoldierGroups)
}

// CastleCounts counts the castles each team owns. Neutral castles count under
// models.NeutralTeamColor; teams without castles are absent.
func (state GameState) CastleCounts() map[uint]int {
	counts := map[uint]int{}
	for _, castle := range state.Castles {
		counts[castle.TeamColor]++
	}
	return counts
}

// SoldierGroupCounts counts the soldier groups each team has in the field;
// teams without groups are absent.
func (state GameState) SoldierGroupCounts() map[uint]int {
	counts := map[uint]int{}
	for _, group := range state.SoldierGroups {
		counts[group.TeamColor]++
	}
	return counts
}

func (state GameState) castle(id uint) (*Castle, error) {
	castle, exists := state.Castles[id]
	if !exists {
		return nil, fmt.Errorf("castle %d not found", id)
	}
	return castle, nil
}

func sortedByID[T any](values map[uint]*T) []*T {
	ids := slices.Sorted(maps.Keys(values))
	sorted := make([]*T, 0, len(ids))
	for _, id := range ids {
		sorted = append(sorted, values[id])
	}
	return sorted
}
