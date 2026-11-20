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
	return &models.GameOutcomeView{Reason: game.Outcome.Reason, WinnerTeamColor: game.Outcome.WinnerTeamColor, Summary: describeOutcome(game),
		Departures: departureViews(game.Outcome.Departures, false), Ratings: game.Outcome.Ratings}
}

// describeOutcome sums up the outcome of a game that has ended. The teams
// that resigned or left a real-time game come first, then the result.
func describeOutcome(game *models.Game) string {
	outcome := game.Outcome
	var result string
	switch outcome.Reason {
	case models.GameOutcomeTurnTimeout:
		return describeTimeoutOutcome(game)
	case models.GameOutcomeAbandoned:
		result = describeAbandonedOutcome(game)
	default:
		result = describeResult(game)
	}
	clauses := make([]string, 0, len(outcome.Departures)+1)
	for _, departure := range outcome.Departures {
		clauses = append(clauses, describeDeparture(game, departure))
	}
	// After a departure the result continues the sentence: "Red resigned;
	// the game was abandoned: …", "Red resigned; draw: no team is left". A
	// winner's team name keeps its capital.
	if len(clauses) > 0 && (strings.HasPrefix(result, "The game") || strings.HasPrefix(result, "Draw")) {
		result = strings.ToLower(result[:1]) + result[1:]
	}
	return strings.Join(append(clauses, result), "; ")
}

// describeResult names the winner of a game that ended by the end rule, or
// says why it is a draw.
func describeResult(game *models.Game) string {
	outcome := game.Outcome
	// Without a winner the game is a draw, whatever its reason.
	if outcome.WinnerTeamColor == nil {
		switch {
		case outcome.Reason == models.GameOutcomeAllEliminated && leftInFinalTurn(outcome):
			return "Draw: no team is left"
		case outcome.Reason == models.GameOutcomeAllEliminated:
			return "Draw: the last teams were eliminated in the same turn"
		case outcome.Reason == models.GameOutcomeGenerationCap:
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

// leftInFinalTurn reports whether a team resigned or left in the turn that
// ended the game, so that "eliminated" wouldn't say how the last teams went.
func leftInFinalTurn(outcome *models.GameOutcome) bool {
	for _, departure := range outcome.Departures {
		if departure.Turn != nil && *departure.Turn == outcome.FinalTurn {
			return true
		}
	}
	return false
}

// describeAbandonedOutcome sums up a real-time game that nobody played for its
// turn limit: "The game was abandoned: nobody played for 30 s; Blue wins with
// the most castles".
func describeAbandonedOutcome(game *models.Game) string {
	outcome := game.Outcome
	result := "draw with equal castle counts"
	switch {
	case outcome.WinnerTeamColor != nil:
		result = teamName(game, *outcome.WinnerTeamColor) + " wins with the most castles"
	case len(outcome.Survivors) == 0:
		result = "draw: no team is left"
	}
	limit := ""
	if game.TurnTimeoutSeconds != nil {
		limit = " for " + describeTimeout(*game.TurnTimeoutSeconds)
	}
	return fmt.Sprintf("The game was abandoned: nobody played%s; %s", limit, result)
}

// describeDeparture names a team that resigned or left a real-time game, such
// as "Red resigned" or "Red left the game: Sam didn't respond for 30 s".
func describeDeparture(game *models.Game, departure models.Departure) string {
	team := teamName(game, departure.Color)
	if departure.Kind == models.DepartureResigned {
		return team + " resigned"
	}
	within := ""
	if game.TurnTimeoutSeconds != nil {
		within = " for " + describeDuration(silenceLimit(game))
	}
	return fmt.Sprintf("%s left the game: %s didn't respond%s", team, joinNames(departure.Players), within)
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
	// Watcher is a replay for someone outside the roster: it asks the lobby
	// page for the watcher's demo and log routes, and shows the game log
	// only once the game has ended, since a watcher belongs to no team.
	Watcher bool
	// DemoFormat and DemoSchemaVersion are the demo files the viewer opens.
	DemoFormat        string
	DemoSchemaVersion int
}

// RenderGameReplay embeds the replay in a self-contained spectator view.
// html/template escapes the JSON for script context, including player names.
func RenderGameReplay(replay GameReplay) (string, error) {
	return renderReplayPage(replayPage{Replay: &replay})
}

// RenderWatchReplay embeds the replay for a watcher outside the roster.
func RenderWatchReplay(replay GameReplay) (string, error) {
	return renderReplayPage(replayPage{Replay: &replay, Watcher: true})
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
