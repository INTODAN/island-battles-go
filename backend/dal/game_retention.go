package dal

import (
	"errors"
	"fmt"
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
	// soldier groups, commands, player inputs, game log lines and requests.
	Generations int
	// Games counts games deleted with their frozen players, bot memories and team bindings,
	// once they had no generations left.
	Games int
}

// PurgeRetiredGames deletes up to generationLimit generations of at most
// gameLimit retired games, lowest game IDs first, children first for the
// foreign keys, and then each retired game it empties. Every game is purged
// in its own transaction of about 16 statements, which gameLimit bounds. A game whose
// purge fails is rolled back and left for a later pass, and the others carry
// on, so one broken game never stops the rest. The error joins every failure,
// and the counts include only the games that committed. Rows are removed for
// real, never soft-deleted. Map templates are never touched: a game's maps
// are copies of them.
func PurgeRetiredGames(database *gorm.DB, generationLimit, gameLimit int) (GamePurge, error) {
	var games []models.Game
	if err := database.Unscoped().Select("ID").Where(clause.Neq{Column: deletedAtColumn, Value: nil}).
		Order(clause.OrderByColumn{Column: idColumn}).Limit(gameLimit).Find(&games).Error; err != nil {
		return GamePurge{}, err
	}
	var purge GamePurge
	var failures []error
	for _, game := range games {
		budget := generationLimit - purge.Generations
		if budget == 0 {
			break
		}
		generations, deleted, err := purgeRetiredGame(database, game.ID, budget)
		if err != nil {
			failures = append(failures, fmt.Errorf("purge retired game %d: %w", game.ID, err))
			continue
		}
		purge.Generations += generations
		if deleted {
			purge.Games++
		}
	}
	return purge, errors.Join(failures...)
}

// purgeRetiredGame deletes up to limit generations of one retired game, and
// the game itself once it has none left. It skips a game that another pass
// has locked, so passes on several hosts take different games.
func purgeRetiredGame(database *gorm.DB, gameID uint, limit int) (generations int, deleted bool, err error) {
	err = database.Transaction(func(transaction *gorm.DB) error {
		// The session keeps Unscoped for every statement without carrying
		// one statement's conditions into the next.
		unscoped := transaction.Unscoped().Session(&gorm.Session{})
		var games []models.Game
		if err := unscoped.Select("ID").Where(clause.Eq{Column: idColumn, Value: gameID}, clause.Neq{Column: deletedAtColumn, Value: nil}).
			Limit(1).Clauses(skipLockedGames).Find(&games).Error; err != nil {
			return err
		}
		if len(games) == 0 {
			return nil
		}
		inGame := clause.Eq{Column: gameIDColumn, Value: gameID}
		var batch []models.Generation
		if err := unscoped.Select("ID", "MapID").Where(inGame).
			Order(clause.OrderByColumn{Column: idColumn}).Limit(limit).Find(&batch).Error; err != nil {
			return err
		}
		if err := deleteGenerations(unscoped, inGame, batch); err != nil {
			return err
		}
		generations = len(batch)
		// A batch that isn't full took every generation that was left.
		if len(batch) == limit {
			var remaining []models.Generation
			if err := unscoped.Select("ID").Where(inGame).Limit(1).Find(&remaining).Error; err != nil {
				return err
			}
			if len(remaining) > 0 {
				return nil
			}
		}
		for _, child := range []any{&models.GenerationRequest{}, &models.GamePlayerBotMemory{}, &models.FrozenGamePlayer{}, &models.TeamBindingRecord{}} {
			if err := unscoped.Where(inGame).Delete(child).Error; err != nil {
				return err
			}
		}
		result := unscoped.Where(clause.Eq{Column: idColumn, Value: gameID}).Delete(&models.Game{})
		deleted = result.RowsAffected == 1
		return result.Error
	})
	if err != nil {
		return 0, false, err
	}
	return generations, deleted, nil
}

// deleteGenerations deletes generations of the game inGame matches, with
// everything that references them, and then their maps and castles. A
// generation's soldier groups reference only its own map's castles, and every
// generation has a map of its own.
func deleteGenerations(unscoped *gorm.DB, inGame clause.Expression, generations []models.Generation) error {
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
	if err := unscoped.Where(inGame, inGenerations).Delete(&models.GenerationRequest{}).Error; err != nil {
		return err
	}
	for _, child := range []any{&models.GenerationInput{}, &models.GenerationLog{}, &models.Command{}, &models.SoldierGroup{}} {
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
