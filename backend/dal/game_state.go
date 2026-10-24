package dal

import (
	"fmt"

	"islandBattles/backend/models"
	"islandBattles/backend/simulation"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// LoadGameState locks the game and reads its latest generation into memory with
// a fixed number of queries, however many castles and groups it has. The lock
// keeps the game's next soldier group ID until SaveGeneration writes it back,
// so concurrent turns of one game run one after the other.
func LoadGameState(database *gorm.DB, gameID uint) (simulation.GameState, error) {
	var games []models.Game
	if err := database.Clauses(clause.Locking{Strength: "UPDATE"}).Select("ID", "NextSoldierGroupID").
		Where(&models.Game{Model: gorm.Model{ID: gameID}}).Limit(1).Find(&games).Error; err != nil {
		return simulation.GameState{}, err
	}
	if len(games) == 0 {
		return simulation.GameState{}, fmt.Errorf("game %d: %w", gameID, gorm.ErrRecordNotFound)
	}
	var generations []models.Generation
	if err := database.Preload("Map.Castles").Preload("SoldierGroups").
		Where(&models.Generation{GameID: gameID}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "id"}, Desc: true}).
		Limit(1).Find(&generations).Error; err != nil {
		return simulation.GameState{}, err
	}
	if len(generations) == 0 {
		return simulation.GameState{}, fmt.Errorf("game %d has no generation: %w", gameID, gorm.ErrRecordNotFound)
	}
	generation := generations[0]
	state := simulation.GameState{
		Map:                generation.Map.MapFields,
		Castles:            make(map[uint]*simulation.Castle, len(generation.Map.Castles)),
		SoldierGroups:      make(map[uint]*simulation.SoldierGroup, len(generation.SoldierGroups)),
		NextSoldierGroupID: games[0].NextSoldierGroupID,
	}
	persistentCastleIDs := make(map[uint]uint, len(generation.Map.Castles))
	for _, castle := range generation.Map.Castles {
		persistentCastleIDs[castle.ID] = castle.PersistentCastleID
		state.Castles[castle.PersistentCastleID] = &simulation.Castle{
			ID: castle.PersistentCastleID, TeamColor: castle.TeamColor, Soldiers: castle.SoldierAmount,
			Position: castle.Position, Upgrades: castle.Upgrades,
		}
	}
	for _, group := range generation.SoldierGroups {
		originID, originExists := persistentCastleIDs[group.OriginCastleID]
		destinationID, destinationExists := persistentCastleIDs[group.DestinationCastleID]
		if !originExists || !destinationExists {
			return simulation.GameState{}, fmt.Errorf("soldier group %d references a castle outside generation %d", group.PersistentSoldierGroupID, generation.ID)
		}
		state.SoldierGroups[group.PersistentSoldierGroupID] = &simulation.SoldierGroup{
			ID: group.PersistentSoldierGroupID, TeamColor: group.TeamColor, Soldiers: group.SoldierAmount,
			Position: group.Position, OriginCastleID: originID, DestinationCastleID: destinationID,
			MovementPauseGenerations: group.MovementPauseGenerations, ClashRadius: group.ClashRadius, Upgrades: group.Upgrades,
		}
	}
	return state, nil
}

// SaveGeneration stores state as the game's next generation together with the
// commands that produced it and the turn's clashes. It writes each table with a
// single batched insert and returns the new generation's ID.
func SaveGeneration(database *gorm.DB, gameID uint, state simulation.GameState, commands []models.CommandFields, clashes []models.Clash) (uint, error) {
	castles := state.SortedCastles()
	gameMap := models.Map{MapFields: state.Map, Castles: make([]models.Castle, 0, len(castles))}
	for _, castle := range castles {
		gameMap.Castles = append(gameMap.Castles, models.Castle{
			CastleFields: models.CastleFields{
				TeamColor: castle.TeamColor, Position: castle.Position, PersistentCastleID: castle.ID, Upgrades: castle.Upgrades.Clone(),
			},
			SoldierAmount: castle.Soldiers,
		})
	}
	if err := database.Create(&gameMap).Error; err != nil {
		return 0, err
	}
	generation := models.Generation{GameID: gameID, MapID: gameMap.ID, Clashes: clashes}
	if err := database.Omit(clause.Associations).Create(&generation).Error; err != nil {
		return 0, err
	}
	castleIDs := make(map[uint]uint, len(gameMap.Castles))
	for _, castle := range gameMap.Castles {
		castleIDs[castle.PersistentCastleID] = castle.ID
	}
	groups := make([]models.SoldierGroup, 0, len(state.SoldierGroups))
	for _, group := range state.SortedSoldierGroups() {
		groups = append(groups, models.SoldierGroup{
			SoldierGroupFields: models.SoldierGroupFields{
				OriginCastleID: castleIDs[group.OriginCastleID], DestinationCastleID: castleIDs[group.DestinationCastleID], SoldierAmount: group.Soldiers,
			},
			GenerationID: generation.ID, PersistentSoldierGroupID: group.ID, TeamColor: group.TeamColor,
			MovementPauseGenerations: group.MovementPauseGenerations, ClashRadius: group.EffectiveClashRadius(),
			Position: group.Position, Upgrades: group.Upgrades.Clone(),
		})
	}
	if len(groups) > 0 {
		if err := database.Omit(clause.Associations).Create(&groups).Error; err != nil {
			return 0, err
		}
	}
	records := make([]models.Command, 0, len(commands))
	for _, command := range commands {
		records = append(records, models.Command{CommandFields: command, GenerationID: generation.ID})
	}
	if len(records) > 0 {
		if err := database.Create(&records).Error; err != nil {
			return 0, err
		}
	}
	err := database.Model(&models.Game{Model: gorm.Model{ID: gameID}}).Select("NextSoldierGroupID").
		Updates(&models.Game{NextSoldierGroupID: state.NextSoldierGroupID}).Error
	if err != nil {
		return 0, err
	}
	return generation.ID, nil
}
