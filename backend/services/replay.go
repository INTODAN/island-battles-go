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
// generations unchanged.
type GameReplay struct {
	GameID  uint                    `json:"game_id"`
	Teams   []models.GameTeam       `json:"teams"`
	Frames  []models.GenerationView `json:"frames"`
	Outcome string                  `json:"outcome"`
}

// gameReplay builds the replay of a game from its generations in ID order,
// each loaded with dal.WithFullGeneration.
func gameReplay(game *models.Game, generations []models.Generation) GameReplay {
	replay := GameReplay{GameID: game.ID, Teams: game.Teams, Frames: make([]models.GenerationView, 0, len(generations)), Outcome: describeOutcome(game)}
	if replay.Teams == nil {
		replay.Teams = []models.GameTeam{}
	}
	for index := range generations {
		replay.Frames = append(replay.Frames, generationView(&generations[index]))
	}
	return replay
}

func describeOutcome(game *models.Game) string {
	outcome := game.Outcome
	if outcome == nil {
		return "Waiting for player turns"
	}
	winner := "Nobody"
	if outcome.WinnerTeamColor != nil {
		winner = fmt.Sprintf("Team %d", *outcome.WinnerTeamColor)
		for _, team := range game.Teams {
			if team.Color == *outcome.WinnerTeamColor {
				winner = team.Name
				break
			}
		}
	}
	switch {
	case outcome.Reason == models.GameOutcomeLastTeamStanding:
		return winner + " wins as the last team standing"
	case outcome.Reason == models.GameOutcomeAllEliminated:
		return "Draw: the last teams were eliminated in the same turn"
	case outcome.Reason == models.GameOutcomeGenerationCap && outcome.WinnerTeamColor != nil:
		return winner + " wins with the most castles at the generation limit"
	case outcome.Reason == models.GameOutcomeGenerationCap:
		return "Draw: equal castle counts at the generation limit"
	}
	return "Game over"
}

// RenderGameReplay embeds the replay in a self-contained spectator view.
// html/template escapes the JSON for script context, including player names.
func RenderGameReplay(replay GameReplay) (string, error) {
	var output bytes.Buffer
	if err := replayTemplate.Execute(&output, replay); err != nil {
		return "", err
	}
	return output.String(), nil
}
