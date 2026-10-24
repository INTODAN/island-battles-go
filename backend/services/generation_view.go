package services

import (
	"sort"

	"islandBattles/backend/models"
)

// generationView is the one public board projection of a generation. Player
// states, their notifications and replay frames all use it, so they share one
// JSON shape. Castles and soldier groups are sorted by persistent ID, and every
// list is empty rather than null.
func generationView(generation *models.Generation) models.GenerationView {
	view := models.GenerationView{
		ID:            generation.ID,
		Castles:       make([]models.CastleView, 0, len(generation.Map.Castles)),
		SoldierGroups: make([]models.SoldierGroupView, 0, len(generation.SoldierGroups)),
		Clashes:       append([]models.Clash{}, generation.Clashes...),
	}
	for _, castle := range generation.Map.Castles {
		view.Castles = append(view.Castles, models.CastleView{ID: castle.PersistentCastleID, TeamColor: castle.TeamColor, Soldiers: castle.SoldierAmount, X: castle.Position.P.X, Y: castle.Position.P.Y})
	}
	for _, group := range generation.SoldierGroups {
		view.SoldierGroups = append(view.SoldierGroups, models.SoldierGroupView{ID: group.PersistentSoldierGroupID, TeamColor: group.TeamColor, Soldiers: group.SoldierAmount, X: group.Position.P.X, Y: group.Position.P.Y, OriginCastleID: group.OriginCastle.PersistentCastleID, DestinationCastleID: group.DestinationCastle.PersistentCastleID, ClashRadius: group.EffectiveClashRadius()})
	}
	sort.Slice(view.Castles, func(left, right int) bool { return view.Castles[left].ID < view.Castles[right].ID })
	sort.Slice(view.SoldierGroups, func(left, right int) bool { return view.SoldierGroups[left].ID < view.SoldierGroups[right].ID })
	return view
}
