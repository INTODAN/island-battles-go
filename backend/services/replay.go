package services

import (
	"bytes"
	_ "embed"
	"fmt"
	"html/template"

	"islandBattles/backend/models"
)

//go:embed replay.html
var replayHTML string

var replayTemplate = template.Must(template.New("replay").Parse(replayHTML))

// GameReplay is the data of the replay page. Only its teams and outcome are
// stored, on the game. The frames are derived from the stored generations on
// every read, in the board view that players receive, so the page appends live
// generations unchanged. A demo holds the same teams, frames and outcome.
type GameReplay struct {
	GameID uint `json:"game_id"`
	// LobbyID names the game in the paths that the page asks the lobby page
	// to fetch for it, such as the game log's and the demo's.
	LobbyID string                  `json:"lobby_id"`
	Teams   []models.GameTeam       `json:"teams"`
	Frames  []models.GenerationView `json:"frames"`
	// Outcome is nil while the game runs. Once it is set, the last frame is
	// final.
	Outcome *models.GameOutcomeView `json:"outcome"`
}

// gameReplay builds the replay of a game from its generations in ID order,
// as dal.LoadGameGenerations returns them.
func gameReplay(game *models.Game, generations []models.Generation) GameReplay {
	replay := GameReplay{GameID: game.ID, LobbyID: lobbyID(game), Teams: game.Teams, Frames: make([]models.GenerationView, 0, len(generations)), Outcome: outcomeView(game)}
	if replay.Teams == nil {
		replay.Teams = []models.GameTeam{}
	}
	for index := range generations {
		replay.Frames = append(replay.Frames, GenerationView(&generations[index]))
	}
	return replay
}

// lobbyID is the ID of the lobby that started the game, or empty for a game
// that no lobby started.
func lobbyID(game *models.Game) string {
	if game.LobbyID == nil {
		return ""
	}
	return *game.LobbyID
}

// outcomeView is how the game ended, as players, the replay and demos see
// it, or nil while it runs.
func outcomeView(game *models.Game) *models.GameOutcomeView {
	if game.Outcome == nil {
		return nil
	}
	return &models.GameOutcomeView{Reason: game.Outcome.Reason, WinnerTeamColor: game.Outcome.WinnerTeamColor, Summary: describeOutcome(game)}
}

// describeOutcome sums up the outcome of a game that has ended.
func describeOutcome(game *models.Game) string {
	outcome := game.Outcome
	// Without a winner the game is a draw, whatever its reason.
	if outcome.WinnerTeamColor == nil {
		switch outcome.Reason {
		case models.GameOutcomeAllEliminated:
			return "Draw: the last teams were eliminated in the same turn"
		case models.GameOutcomeGenerationCap:
			return "Draw: equal castle counts at the generation limit"
		}
		return "Draw"
	}
	winner := fmt.Sprintf("Team %d", *outcome.WinnerTeamColor)
	for _, team := range game.Teams {
		if team.Color == *outcome.WinnerTeamColor {
			winner = team.Name
			break
		}
	}
	switch outcome.Reason {
	case models.GameOutcomeLastTeamStanding:
		return winner + " wins as the last team standing"
	case models.GameOutcomeGenerationCap:
		return winner + " wins with the most castles at the generation limit"
	}
	return winner + " wins"
}

// replayPage is the data of the page template, which renders both the replay
// and the demo viewer, so they draw a game with the same code. The replay
// embeds its game. The viewer embeds none, and opens a demo file instead.
type replayPage struct {
	Replay *GameReplay
	Viewer bool
	// DemoFormat and DemoSchemaVersion are the demo files the viewer opens.
	DemoFormat        string
	DemoSchemaVersion int
}

// RenderGameReplay embeds the replay in a self-contained spectator view.
// html/template escapes the JSON for script context, including player names.
func RenderGameReplay(replay GameReplay) (string, error) {
	return renderReplayPage(replayPage{Replay: &replay})
}

// RenderDemoViewer renders the demo viewer, the replay page without a game.
// It loads viewer.mjs from beside itself to open demo files.
func RenderDemoViewer() (string, error) {
	return renderReplayPage(replayPage{Viewer: true, DemoFormat: models.DemoFormat, DemoSchemaVersion: models.DemoSchemaVersion})
}

func renderReplayPage(page replayPage) (string, error) {
	var output bytes.Buffer
	if err := replayTemplate.Execute(&output, page); err != nil {
		return "", err
	}
	return output.String(), nil
}
