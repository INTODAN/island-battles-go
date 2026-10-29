package dal

import (
	"time"

	"islandBattles/backend/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Game retention removes a game in two steps. RetireExpiredGames soft-deletes
// the game row, which hides the game from every request at once, because all
// of them find and lock it through the default scope. PurgeRetiredGames then
// removes its rows for real over as many passes as it takes, so no single
// transaction deletes a whole long game.

// skipLockedGames leaves games that a request has locked to a later pass. A
// turn or read holds its game's row lock, so a pass never waits for one and
// never retires a game while a turn on it is in progress.
var skipLockedGames = clause.Locking{Strength: clause.LockingStrengthUpdate, Options: clause.LockingOptionsSkipLocked}

// The tables of models.Game and models.Generation, which the expiry check
// names to correlate its subquery.
const (
	gamesTable       = "games"
	generationsTable = "generations"
)

var (
	idColumn        = clause.Column{Name: "id"}
	gameIDColumn    = clause.Column{Name: "game_id"}
	deletedAtColumn = clause.Column{Name: "deleted_at"}
)

// RetireExpiredGames soft-deletes up to limit games with no generation
// created at or after before, finished or not. A game that never got a
// generation counts from its own creation. It returns how many it retired.
//
// The candidates are locked first, skipping games that a request holds, and
// checked again under the lock in a new statement. A turn that committed a
// new generation between the two keeps its game.
func RetireExpiredGames(database *gorm.DB, before time.Time, limit int) (int, error) {
	retired := 0
	err := database.Transaction(func(transaction *gorm.DB) error {
		var candidates []models.Game
		if err := transaction.Select("ID").Where(expiredGame(transaction, before)).
			Order(clause.OrderByColumn{Column: idColumn}).Limit(limit).Clauses(skipLockedGames).
			Find(&candidates).Error; err != nil {
			return err
		}
		if len(candidates) == 0 {
			return nil
		}
		result := transaction.Where(clause.IN{Column: idColumn, Values: gameIDs(candidates)}).
			Where(expiredGame(transaction, before)).Delete(&models.Game{})
		retired = int(result.RowsAffected)
		return result.Error
	})
	return retired, err
}

// expiredGame matches games created before the cutoff with no generation
// created since.
func expiredGame(database *gorm.DB, before time.Time) clause.Expression {
	newerGenerations := database.Session(&gorm.Session{NewDB: true}).Unscoped().Model(&models.Generation{}).Select("1").Where(
		clause.Eq{Column: clause.Column{Table: generationsTable, Name: "game_id"}, Value: clause.Column{Table: gamesTable, Name: "id"}},
		clause.Gte{Column: clause.Column{Table: generationsTable, Name: "created_at"}, Value: before},
	)
	return clause.And(
		clause.Lt{Column: clause.Column{Table: gamesTable, Name: "created_at"}, Value: before},
		clause.Expr{SQL: "NOT EXISTS (?)", Vars: []any{newerGenerations}},
	)
}

// GamePurge counts the rows that one PurgeRetiredGames call deleted.
type GamePurge struct {
	// Generations counts generations deleted with their maps, castles,
	// soldier groups, commands, player inputs and requests.
	Generations int
	// Games counts games deleted with their frozen players and team bindings,
	// once they had no generations left.
	Games int
}

// PurgeRetiredGames deletes, in one transaction, up to limit generations of
// retired games, children first for the foreign keys. Then it deletes the
// retired games it found with no generations left. Rows are removed for real,
// never soft-deleted. Map templates are never touched: a game's maps are
// copies of them.
func PurgeRetiredGames(database *gorm.DB, limit int) (GamePurge, error) {
	var purge GamePurge
	err := database.Transaction(func(transaction *gorm.DB) error {
		// The session keeps Unscoped for every statement without carrying
		// one statement's conditions into the next.
		unscoped := transaction.Unscoped().Session(&gorm.Session{})
		// Locking the retired games lets passes on other hosts take other games.
		var games []models.Game
		if err := unscoped.Select("ID").Where(clause.Neq{Column: deletedAtColumn, Value: nil}).
			Order(clause.OrderByColumn{Column: idColumn}).Limit(limit).Clauses(skipLockedGames).
			Find(&games).Error; err != nil {
			return err
		}
		if len(games) == 0 {
			return nil
		}
		inGames := clause.IN{Column: gameIDColumn, Values: gameIDs(games)}
		var generations []models.Generation
		if err := unscoped.Select("ID", "MapID").Where(inGames).
			Order(clause.OrderByColumn{Column: idColumn}).Limit(limit).Find(&generations).Error; err != nil {
			return err
		}
		if err := deleteGenerations(unscoped, inGames, generations); err != nil {
			return err
		}
		purge.Generations = len(generations)
		var remaining []uint
		if err := unscoped.Model(&models.Generation{}).Distinct("game_id").Where(inGames).Pluck("game_id", &remaining).Error; err != nil {
			return err
		}
		emptyGames := make([]any, 0, len(games))
		for _, game := range games {
			if !containsID(remaining, game.ID) {
				emptyGames = append(emptyGames, game.ID)
			}
		}
		if len(emptyGames) == 0 {
			return nil
		}
		inEmptyGames := clause.IN{Column: gameIDColumn, Values: emptyGames}
		for _, child := range []any{&models.GenerationRequest{}, &models.FrozenGamePlayer{}, &models.TeamBindingRecord{}} {
			if err := unscoped.Where(inEmptyGames).Delete(child).Error; err != nil {
				return err
			}
		}
		result := unscoped.Where(clause.IN{Column: idColumn, Values: emptyGames}).Delete(&models.Game{})
		purge.Games = int(result.RowsAffected)
		return result.Error
	})
	return purge, err
}

// deleteGenerations deletes generations of the games inGames matches, with
// everything that references them, and then their maps and castles. A
// generation's soldier groups reference only its own map's castles, and every
// generation has a map of its own.
func deleteGenerations(unscoped *gorm.DB, inGames clause.Expression, generations []models.Generation) error {
	if len(generations) == 0 {
		return nil
	}
	generationIDs := make([]any, len(generations))
	mapIDs := make([]any, len(generations))
	for index, generation := range generations {
		generationIDs[index] = generation.ID
		mapIDs[index] = generation.MapID
	}
	inGenerations := clause.IN{Column: clause.Column{Name: "generation_id"}, Values: generationIDs}
	// Requests are keyed by game first, which their primary key indexes.
	if err := unscoped.Where(inGames, inGenerations).Delete(&models.GenerationRequest{}).Error; err != nil {
		return err
	}
	for _, child := range []any{&models.GenerationInput{}, &models.Command{}, &models.SoldierGroup{}} {
		if err := unscoped.Where(inGenerations).Delete(child).Error; err != nil {
			return err
		}
	}
	if err := unscoped.Where(clause.IN{Column: idColumn, Values: generationIDs}).Delete(&models.Generation{}).Error; err != nil {
		return err
	}
	if err := unscoped.Where(clause.IN{Column: clause.Column{Name: "map_id"}, Values: mapIDs}).Delete(&models.Castle{}).Error; err != nil {
		return err
	}
	return unscoped.Where(clause.IN{Column: idColumn, Values: mapIDs}).Delete(&models.Map{}).Error
}

func gameIDs(games []models.Game) []any {
	ids := make([]any, len(games))
	for index, game := range games {
		ids[index] = game.ID
	}
	return ids
}

func containsID(ids []uint, id uint) bool {
	for _, candidate := range ids {
		if candidate == id {
			return true
		}
	}
	return false
}
