package services

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"time"

	"islandBattles/backend/dal"
	"islandBattles/backend/models"
	"islandBattles/backend/observability"
	"islandBattles/lobby/domain"
)

// Demo saves a lobby's game as a demo for a roster player: every stored
// generation so far, and for each the game log lines that the player may see
// now, by the rule Logs applies. It takes no game lock, like Replay: the game
// is read first, so a game that ends meanwhile is saved by the stricter rule,
// and a turn that commits meanwhile can at most add a frame without its
// outcome. The number of queries doesn't grow with the length of the game.
func (service *GenerationService) Demo(ctx context.Context, principal domain.Principal, lobbyID string) (models.Demo, error) {
	database := service.Database.WithContext(ctx)
	game, err := dal.FindLobbyGame(database, lobbyID)
	if err != nil {
		return models.Demo{}, err
	}
	observability.Annotate(ctx, slog.Uint64("game_id", uint64(game.ID)))
	if err := service.authorize(ctx, principal, game); err != nil {
		return models.Demo{}, err
	}
	if game, err = service.applyOverdueDeadline(ctx, game); err != nil {
		return models.Demo{}, err
	}
	generations, err := dal.LoadGameGenerations(database, game.ID)
	if err != nil {
		return models.Demo{}, err
	}
	players, err := dal.LoadFrozenPlayers(database, game.ID)
	if err != nil {
		return models.Demo{}, err
	}
	caller, err := frozenPlayer(players, principal.PlayerID)
	if err != nil {
		return models.Demo{}, err
	}
	generationIDs := make([]uint, len(generations))
	for index, generation := range generations {
		generationIDs[index] = generation.ID
	}
	// Only the lines the caller may see are loaded.
	var visiblePlayerIDs []string
	for _, player := range players {
		if game.Outcome != nil || player.TeamColor == caller.TeamColor {
			visiblePlayerIDs = append(visiblePlayerIDs, player.PlayerID)
		}
	}
	lines, err := dal.LoadPlayersLogs(database, generationIDs, visiblePlayerIDs)
	if err != nil {
		return models.Demo{}, err
	}
	replay := gameReplay(game, generations)
	demo := models.Demo{Format: models.DemoFormat, SchemaVersion: models.DemoSchemaVersion, SavedAt: time.Now().UTC().Truncate(time.Second),
		GameID: game.ID, Teams: replay.Teams, Outcome: replay.Outcome, Frames: replay.Frames, Logs: make([]models.GenerationLogs, 0, len(generations))}
	// The lines are ordered by generation, as the frames are, so each frame
	// takes the next run of lines.
	next := 0
	for _, generationID := range generationIDs {
		first := next
		for next < len(lines) && lines[next].GenerationID == generationID {
			next++
		}
		demo.Logs = append(demo.Logs, generationLogs(game, generationID, players, caller, lines[first:next]))
	}
	return demo, nil
}

// DemoFileName names a demo's file after its game and its last generation's
// number in the game.
func DemoFileName(demo models.Demo) string {
	return fmt.Sprintf("island-battles-game-%d-generation-%d.json", demo.GameID, len(demo.Frames))
}

// WriteCompressedDemo writes a demo as WriteDemo does, compressed with gzip
// as it is encoded, so the whole file is never held in memory, packed or
// not. Bots' log lines repeat their wording, so a demo packs to a fraction of
// its size: a two-player game at the generation cap with full logs of
// varying bot-like lines packs from 33 to under 5 MiB.
func WriteCompressedDemo(writer io.Writer, demo models.Demo) error {
	compressor, err := gzip.NewWriterLevel(writer, gzip.DefaultCompression)
	if err != nil {
		return err
	}
	if err := WriteDemo(compressor, demo); err != nil {
		return err
	}
	return compressor.Close()
}

// WriteDemo writes a demo as JSON, one frame and one generation's logs at a
// time, so a long game is never encoded whole in memory. It writes what
// json.Marshal would, except that it doesn't escape <, > and &, which a file
// doesn't need, and that it puts each frame and each generation's logs on a
// line of its own.
func WriteDemo(writer io.Writer, demo models.Demo) error {
	buffered := bufio.NewWriterSize(writer, 64*1024)
	encoder := json.NewEncoder(buffered)
	encoder.SetEscapeHTML(false)
	var err error
	text := func(value string) {
		if err == nil {
			_, err = buffered.WriteString(value)
		}
	}
	// Encode ends every value with a line break, which JSON allows between
	// tokens.
	value := func(value any) {
		if err == nil {
			err = encoder.Encode(value)
		}
	}
	text(`{"format":`)
	value(demo.Format)
	text(`,"schema_version":`)
	value(demo.SchemaVersion)
	text(`,"saved_at":`)
	value(demo.SavedAt)
	text(`,"game_id":`)
	value(demo.GameID)
	text(`,"teams":`)
	value(demo.Teams)
	text(`,"outcome":`)
	value(demo.Outcome)
	text(`,"frames":[`)
	for index := range demo.Frames {
		if index > 0 {
			text(",")
		}
		value(demo.Frames[index])
	}
	text(`],"logs":[`)
	for index := range demo.Logs {
		if index > 0 {
			text(",")
		}
		value(demo.Logs[index])
	}
	text("]}\n")
	if err != nil {
		return err
	}
	return buffered.Flush()
}
