package dal

import (
	"fmt"

	"islandBattles/backend/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// WithFullGeneration is a scope that preloads all associations of a Generation
func WithFullGeneration(db *gorm.DB) *gorm.DB {
	return db.Preload("Map.Castles").
		Preload("SoldierGroups.OriginCastle").
		Preload("SoldierGroups.DestinationCastle").
		Preload("Commands")
}

func RefreshGeneration(db *gorm.DB, generation *models.Generation) error {
	return db.Scopes(WithFullGeneration).First(&generation, generation.ID).Error
}

func CreateGeneration(db *gorm.DB, generation *models.Generation) (uint, error) {
	err := db.Create(&generation).Error
	if err != nil {
		return 0, err
	}
	return generation.ID, nil
}

func GetGeneration(db *gorm.DB, generationID uint) (*models.Generation, error) {
	var generation models.Generation
	err := db.Scopes(WithFullGeneration).First(&generation, generationID).Error
	if err != nil {
		return nil, err
	}
	return &generation, nil
}

func GetLatestGenerationForGame(db *gorm.DB, gameID uint) (*models.Generation, error) {
	var generation models.Generation
	err := db.Scopes(WithFullGeneration).
		Where(&models.Generation{GameID: gameID}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "id"}, Desc: true}).
		First(&generation).Error
	if err != nil {
		return nil, err
	}
	return &generation, nil
}

func CountGenerationsForGame(db *gorm.DB, gameID uint) (int64, error) {
	var count int64
	err := db.Model(&models.Generation{}).Where(&models.Generation{GameID: gameID}).Count(&count).Error
	return count, err
}

// LoadGameGenerations returns every generation of a game in ID order, with
// what the board view reads: the map's castles and the soldier groups, whose
// OriginCastle and DestinationCastle are filled in from those castles in
// memory. Commands are not loaded. Preloads are batched across generations,
// so the number of queries does not grow with the length of the game.
func LoadGameGenerations(db *gorm.DB, gameID uint) ([]models.Generation, error) {
	// GORM drops zero-valued struct conditions, which would match every game.
	if gameID == 0 {
		return nil, nil
	}
	var generations []models.Generation
	err := db.Preload("Map.Castles").Preload("SoldierGroups").
		Where(&models.Generation{GameID: gameID}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "id"}}).
		Find(&generations).Error
	if err != nil {
		return nil, err
	}
	castles := map[uint]models.Castle{}
	for _, generation := range generations {
		for _, castle := range generation.Map.Castles {
			castles[castle.ID] = castle
		}
	}
	for generationIndex := range generations {
		groups := generations[generationIndex].SoldierGroups
		for groupIndex := range groups {
			origin, originFound := castles[groups[groupIndex].OriginCastleID]
			destination, destinationFound := castles[groups[groupIndex].DestinationCastleID]
			if !originFound || !destinationFound {
				return nil, fmt.Errorf("soldier group %d references a castle outside game %d", groups[groupIndex].ID, gameID)
			}
			groups[groupIndex].OriginCastle, groups[groupIndex].DestinationCastle = origin, destination
		}
	}
	return generations, nil
}

func CreateGenerationFromTemplate(db *gorm.DB, mapTemplateID uint, gameID uint) (uint, error) {
	mapID, err := CreateMapFromTemplate(db, mapTemplateID)
	if err != nil {
		return 0, err
	}

	generation := models.Generation{
		MapID:  mapID,
		GameID: gameID,
	}

	generationID, err := CreateGeneration(db, &generation)
	if err != nil {
		return 0, err
	}
	return generationID, nil
}
