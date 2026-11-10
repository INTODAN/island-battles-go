package services

import (
	"bytes"
	_ "embed"
	"fmt"
	"html/template"
	"strings"

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
	LobbyID string `json:"lobby_id"`
	// LobbyRound is the lobby round that the game plays, so the page can
	// tell the lobby's games apart.
	LobbyRound int64                   `json:"lobby_round"`
	Teams      []models.GameTeam       `json:"teams"`
	Frames     []models.GenerationView `json:"frames"`
	// Outcome is nil while the game runs. Once it is set, the last frame is
	// final.
	Outcome *models.GameOutcomeView `json:"outcome"`
}

// gameReplay builds the replay of a game from its generations in ID order,
// as dal.LoadGameGenerations returns them.
func gameReplay(game *models.Game, generations []models.Generation) GameReplay {
	replay := GameReplay{GameID: game.ID, LobbyID: lobbyID(game), LobbyRound: game.LobbyRound, Teams: game.Teams, Frames: make([]models.GenerationView, 0, len(generations)), Outcome: outcomeView(game)}
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
	if outcome.Reason == models.GameOutcomeTurnTimeout {
		return describeTimeoutOutcome(game)
	}
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
	winner := teamName(game, *outcome.WinnerTeamColor)
	switch outcome.Reason {
	case models.GameOutcomeLastTeamStanding:
		return winner + " wins as the last team standing"
	case models.GameOutcomeGenerationCap:
		return winner + " wins with the most castles at the generation limit"
	}
	return winner + " wins"
}

// describeTimeoutOutcome sums up a game that ended at a turn's deadline: each
// team that timed out, with its players who didn't answer, and then the
// winner, or that the other teams drew. A game where every team still in it
// timed out, and so has no survivors, is a draw: "Draw: Blue's and Red's bots didn't answer within
// 30 s".
func describeTimeoutOutcome(game *models.Game) string {
	outcome := game.Outcome
	if len(outcome.Survivors) == 0 && outcome.WinnerTeamColor == nil {
		bots := make([]string, 0, len(outcome.TimedOut))
		for _, team := range outcome.TimedOut {
			bots = append(bots, teamName(game, team.Color)+"'s")
		}
		within := ""
		if game.TurnTimeoutSeconds != nil {
			within = " within " + describeTimeout(*game.TurnTimeoutSeconds)
		}
		return fmt.Sprintf("Draw: %s bots didn't answer%s", joinNames(bots), within)
	}
	clauses := make([]string, 0, len(outcome.TimedOut)+1)
	for _, team := range outcome.TimedOut {
		clauses = append(clauses, describeTimedOutTeam(game, team))
	}
	switch {
	case outcome.WinnerTeamColor != nil:
		clauses = append(clauses, teamName(game, *outcome.WinnerTeamColor)+" wins")
	default:
		clauses = append(clauses, "the other teams draw")
	}
	return strings.Join(clauses, "; ")
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
