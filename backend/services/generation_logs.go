package services

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/INTODAN/mpl/domain"
	"islandBattles/backend/dal"
	"islandBattles/backend/models"
	"islandBattles/backend/observability"

	"gorm.io/gorm"
)

// GameLogLimitProblemType is the problem type of the 422 that rejects a batch
// whose game log lines break a limit: too many lines for the player's
// generation, or a line that is too long.
const GameLogLimitProblemType = "urn:island-battles:problem:game-log-limit"

func rejectGameLogLimit(detail string) error {
	return &GenerationError{Status: 422, Detail: detail, Type: GameLogLimitProblemType}
}

// validateLogLines checks a batch's game log lines on their own, before the
// turn is locked. The line count across batches is checked by
// appendGenerationLogs.
func validateLogLines(lines []models.GenerationLogLine) error {
	if len(lines) > models.MaxGenerationLogLines {
		return rejectGameLogLimit(fmt.Sprintf("game log limit: at most %d lines per player per generation, and this batch has %d", models.MaxGenerationLogLines, len(lines)))
	}
	for index, line := range lines {
		if !line.Level.Valid() {
			return reject(422, fmt.Sprintf("logs[%d].level must be debug, info, warning or error", index))
		}
		if utf8.RuneCountInString(line.Logger) > models.MaxGenerationLogLoggerLength {
			return reject(422, fmt.Sprintf("logs[%d].logger must have at most %d characters", index, models.MaxGenerationLogLoggerLength))
		}
		length := utf8.RuneCountInString(line.Message)
		if length == 0 {
			return reject(422, fmt.Sprintf("logs[%d].message must not be empty", index))
		}
		if length > models.MaxGenerationLogMessageLength {
			return rejectGameLogLimit(fmt.Sprintf("game log limit: logs[%d].message has %d characters, and a line may have at most %d", index, length, models.MaxGenerationLogMessageLength))
		}
		// PostgreSQL text can't hold NUL.
		if strings.ContainsRune(line.Message, 0) || strings.ContainsRune(line.Logger, 0) {
			return reject(422, fmt.Sprintf("logs[%d] must not contain NUL characters", index))
		}
	}
	return nil
}

// appendGenerationLogs stores a batch's lines after the player's earlier
// lines for the generation. It runs under the game lock, so the count it
// checks can't change before the lines are stored.
func appendGenerationLogs(transaction *gorm.DB, gameID, generationID uint, playerID string, lines []models.GenerationLogLine) error {
	if len(lines) == 0 {
		return nil
	}
	stored, err := dal.CountGenerationLogs(transaction, generationID, playerID)
	if err != nil {
		return err
	}
	if int(stored)+len(lines) > models.MaxGenerationLogLines {
		return rejectGameLogLimit(fmt.Sprintf("game log limit: at most %d lines per player per generation; %d are stored and this batch has %d", models.MaxGenerationLogLines, stored, len(lines)))
	}
	records := make([]models.GenerationLog, len(lines))
	for index, line := range lines {
		records[index] = models.GenerationLog{GameID: gameID, GenerationID: generationID, PlayerID: playerID,
			Sequence: uint(stored) + uint(index) + 1, Level: line.Level, Logger: line.Logger, Message: line.Message}
	}
	return dal.StoreGenerationLogs(transaction, records)
}

// countStoredLogLines fills in how many game log lines the state's player has
// stored for its generation.
func countStoredLogLines(transaction *gorm.DB, state *models.GenerationState) error {
	stored, err := dal.CountGenerationLogs(transaction, state.GenerationID, state.PlayerID)
	state.LogLinesStored = uint(stored)
	return err
}

// Logs returns a generation's game log for a roster player. While the game
// runs, the caller sees only their own team's lines; once it has an outcome,
// every team's. It takes no game lock, like Replay: the game is read first,
// so a game that ends meanwhile is shown by the stricter rule.
func (service *GenerationService) Logs(ctx context.Context, principal domain.Principal, lobbyID string, generationID uint) (models.GenerationLogs, error) {
	database := service.Database.WithContext(ctx)
	game, err := dal.FindLobbyGame(database, lobbyID)
	if err != nil {
		return models.GenerationLogs{}, err
	}
	observability.Annotate(ctx, slog.Uint64("game_id", uint64(game.ID)), slog.Uint64("generation_id", uint64(generationID)))
	if err := service.authorize(ctx, principal, game); err != nil {
		return models.GenerationLogs{}, err
	}
	if game, err = service.applyOverdueDeadline(ctx, game); err != nil {
		return models.GenerationLogs{}, err
	}
	// Checked after authorization, so only roster players learn of it.
	if generationID == 0 {
		return models.GenerationLogs{}, reject(422, "generation_id must be a positive generation ID")
	}
	found, err := dal.GameHasGeneration(database, game.ID, generationID)
	if err != nil {
		return models.GenerationLogs{}, err
	}
	if !found {
		return models.GenerationLogs{}, reject(404, "generation not found")
	}
	players, err := dal.LoadFrozenPlayers(database, game.ID)
	if err != nil {
		return models.GenerationLogs{}, err
	}
	caller, err := frozenPlayer(players, principal.PlayerID)
	if err != nil {
		return models.GenerationLogs{}, err
	}
	lines, err := dal.LoadGenerationLogs(database, generationID)
	if err != nil {
		return models.GenerationLogs{}, err
	}
	return generationLogs(game, generationID, players, caller, lines), nil
}

// generationLogs groups a generation's lines by player and leaves out the
// teams the caller can't see yet.
func generationLogs(game *models.Game, generationID uint, players []models.FrozenGamePlayer, caller models.FrozenGamePlayer, lines []models.GenerationLog) models.GenerationLogs {
	visible := func(teamColor uint) bool { return game.Outcome != nil || teamColor == caller.TeamColor }
	logs := models.GenerationLogs{GenerationID: generationID, Players: []models.PlayerGenerationLogs{}, HiddenTeamColors: []uint{}}
	for _, team := range game.Teams {
		if !visible(team.Color) {
			logs.HiddenTeamColors = append(logs.HiddenTeamColors, team.Color)
		}
	}
	slices.Sort(logs.HiddenTeamColors)
	byPlayer := map[string][]models.GenerationLogLine{}
	for _, line := range lines {
		byPlayer[line.PlayerID] = append(byPlayer[line.PlayerID], models.GenerationLogLine{Level: line.Level, Logger: line.Logger, Message: line.Message})
	}
	visiblePlayers := slices.DeleteFunc(slices.Clone(players), func(player models.FrozenGamePlayer) bool { return !visible(player.TeamColor) })
	// Equal names keep the roster's player ID order, which the response
	// doesn't show but keeps stable across reads.
	slices.SortStableFunc(visiblePlayers, func(left, right models.FrozenGamePlayer) int {
		return cmp.Or(cmp.Compare(left.TeamColor, right.TeamColor), cmp.Compare(left.DisplayName, right.DisplayName))
	})
	for _, player := range visiblePlayers {
		playerLines := byPlayer[player.PlayerID]
		if playerLines == nil {
			playerLines = []models.GenerationLogLine{}
		}
		logs.Players = append(logs.Players, models.PlayerGenerationLogs{DisplayName: player.DisplayName, TeamColor: player.TeamColor, Lines: playerLines})
	}
	return logs
}
