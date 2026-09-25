package move_soldier_groups

import (
	"islandBattles/backend/dal"
	"islandBattles/backend/dal/commandHandlers/move_soldier_groups/internal/calculations"
	"islandBattles/backend/dal/commandHandlers/move_soldier_groups/internal/db_updates"
	"islandBattles/backend/models"

	"gorm.io/gorm"
)

type MoveSoldierGroupsParams struct{}

func MoveSoldierGroupsHandler(db *gorm.DB, generation *models.Generation, parameters MoveSoldierGroupsParams) error {
	soldierGroupsPositions, err := calculations.CalculateInitialPositions(generation.SoldierGroups)
	if err != nil {
		return err
	}
	clashPositions, err := calculations.CalculateClashPositions(soldierGroupsPositions)
	if err != nil {
		return err
	}
	clashes := make([]models.ClashEvent, 0, len(clashPositions))
	for index, clash := range clashPositions {
		event := models.ClashEvent{ID: uint(index + 1), Time: clash.Time}
		for _, participant := range clash.Participants {
			group := participant.SoldierGroup
			event.Participants = append(event.Participants, models.ClashParticipant{
				GroupID: group.PersistentSoldierGroupID, TeamColor: group.TeamColor,
				X: participant.Position.P.X, Y: participant.Position.P.Y, ClashRadius: group.EffectiveClashRadius(),
				SoldiersBefore: participant.SoldiersBefore, SoldiersAfter: participant.RemainingSoldiers,
			})
		}
		clashes = append(clashes, event)
	}
	if err := dal.StoreGenerationClashes(db, generation, clashes); err != nil {
		return err
	}

	err = db_updates.UpdateSoldierGroupAmounts(db, clashPositions)
	if err != nil {
		return err
	}

	err = db_updates.UpdateSoldierGroupPositions(db, soldierGroupsPositions)
	if err != nil {
		return err
	}

	return db.Model(generation).Association("SoldierGroups").Find(&generation.SoldierGroups)
}
