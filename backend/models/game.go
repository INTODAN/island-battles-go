package models

import "gorm.io/gorm"

type Game struct {
	gorm.Model
	// LobbyID and LobbyRound name the lobby round that the game plays: a
	// lobby plays one game per round, and its newest game answers for it.
	LobbyID    *string `gorm:"type:uuid;uniqueIndex:idx_games_lobby_round,priority:1"`
	LobbyRound int64   `gorm:"not null;default:1;uniqueIndex:idx_games_lobby_round,priority:2"`
	// Teams and Outcome are the replay's static metadata. Its frames are derived
	// from the stored generations. The game keeps these itself because its
	// lobby is deleted soon after the start. Teams is saved with the first
	// generation, so a game with teams is initialized.
	Teams []GameTeam `gorm:"serializer:json;type:jsonb" json:"-"`
	// Outcome is nil while the game is running.
	Outcome *GameOutcome `gorm:"serializer:json;type:jsonb" json:"-"`
	// NextSoldierGroupID is never decremented, so a departed group's ID is not reassigned.
	NextSoldierGroupID uint `gorm:"not null" json:"-"`
	// TurnTimeoutSeconds is how long each generation's turn lasts, from the
	// lobby's settings. It is saved with the teams when the game is
	// initialized. Nil means turns have no time limit.
	TurnTimeoutSeconds *uint `json:"-"`
	// MaxGenerations is the game's last generation, from the lobby's settings.
	// It is saved with the teams when the game is initialized. The column's
	// default gives games stored before the setting the old limit of 300.
	MaxGenerations int `gorm:"not null;default:300" json:"-"`
	// TurnPaceMilliseconds makes the game real-time: each turn ends this long
	// after the previous turn's end (Generation.TurnDeadlineAt), the first
	// one this long after the start, and the server then plays it with the
	// commands that arrived. It is the lobby's turn pace, saved at the start
	// only when a frozen roster player plays by hand (ManualBot). Nil means
	// the game waits for every player, and TurnTimeoutSeconds applies.
	TurnPaceMilliseconds *uint `json:"-"`
	// Departures lists the teams that left a real-time game, by resigning or
	// by going silent, in the order they left. A resignation waits here, with
	// a nil Turn, for the turn that takes the team out.
	Departures []Departure `gorm:"serializer:json;type:jsonb" json:"-"`
	// Rated is the lobby's rated setting, saved with the teams when the game
	// is initialized. Each frozen player of a rated game has a competitor.
	Rated       bool `gorm:"not null;default:false" json:"-"`
	Generations []Generation
}

// GameTeam is a team that took part in the battle, with its lobby name and
// the display names its players had at the start.
type GameTeam struct {
	Color   uint     `json:"color" minimum:"1" doc:"The team colour that castles and soldier groups carry as team_color."`
	Name    string   `json:"name"`
	Players []string `json:"players" nullable:"false" doc:"The display names the team's players had at the start."`
}

// GameOutcomeReason says why a game ended.
type GameOutcomeReason string

const (
	// GameOutcomeLastTeamStanding means one team was left, and it won.
	GameOutcomeLastTeamStanding GameOutcomeReason = "last_team_standing"
	// GameOutcomeAllEliminated means every remaining team was eliminated in
	// the same turn, which is a draw.
	GameOutcomeAllEliminated GameOutcomeReason = "all_eliminated"
	// GameOutcomeGenerationCap means the game reached the generation cap. The
	// team with the most castles won, and equal castle counts are a draw.
	GameOutcomeGenerationCap GameOutcomeReason = "generation_cap"
	// GameOutcomeTurnTimeout means a turn passed its deadline. Every team
	// with a player who hadn't completed it lost. Among the other teams still
	// standing, the team with the most castles won, and equal castle counts,
	// or no team left, are a draw.
	GameOutcomeTurnTimeout GameOutcomeReason = "turn_timeout"
	// GameOutcomeAbandoned means nobody played a real-time game for its turn
	// limit, so it ended on its open generation. Among the teams still in it,
	// leaving out a team whose resignation was waiting for the turn, the team
	// with the most castles won, and equal castle counts, or no team left,
	// are a draw.
	GameOutcomeAbandoned GameOutcomeReason = "abandoned"
)

// GameOutcome is how a finished game ended.
type GameOutcome struct {
	Reason GameOutcomeReason `json:"reason"`
	// WinnerTeamColor is nil for a draw.
	WinnerTeamColor *uint `json:"winner_team_color"`
	// TimedOut lists the teams that lost a turn_timeout game, in colour
	// order. It is empty for every other reason.
	TimedOut []TimedOutTeam `json:"timed_out,omitempty"`
	// Survivors lists the colours of the teams that were still in the game
	// when it ended, in colour order: those with castles or soldier groups,
	// leaving out a team that timed out in a turn_timeout game and one whose
	// resignation was waiting for the turn in an abandoned game. The winner
	// is one of them, or they drew; without any, every team was eliminated,
	// timed out, resigned or left, which is a draw. A rated game without a
	// winner rates each other team as a loss to each survivor.
	Survivors []uint `json:"survivors,omitempty"`
	// Departures lists the teams that resigned or left a real-time game, as
	// Game.Departures had them when it ended. A resignation still waiting for
	// its turn when the game was abandoned keeps a nil Turn.
	Departures []Departure `json:"departures,omitempty"`
	// FinalTurn is the number of the game's last generation when a turn
	// ended it (playTurn), and 0 for an outcome saved without a turn. It
	// tells a draw of teams that left in the last turn from one of teams
	// eliminated.
	FinalTurn int `json:"final_turn,omitempty"`
	// Ratings are the rating changes of a rated game's competitors, one per
	// team in colour order, applied in the transaction that stored the
	// outcome. Nil for a game that isn't rated.
	Ratings []RatingChange `json:"ratings,omitempty"`
}

// DepartureKind says how a team left a real-time game.
type DepartureKind string

const (
	// DepartureResigned means a player of the team resigned.
	DepartureResigned DepartureKind = "resigned"
	// DepartureLeft means the server didn't hear from a player of the team
	// for the game's silence limit.
	DepartureLeft DepartureKind = "left"
)

// Departure is a team that left a real-time game. Its castles turned neutral
// and kept their soldiers, and its soldier groups were removed, in the turn
// that created generation Turn.
type Departure struct {
	Color uint          `json:"color"`
	Kind  DepartureKind `json:"kind"`
	// Players are the display names, from the start, of the player who
	// resigned or of the players who went silent.
	Players []string `json:"players"`
	// Turn is the number of the generation in which the team is out, or nil
	// while a resignation waits for the open turn to play.
	Turn *int `json:"turn"`
}

// TimedOutTeam is a team that lost because some of its players didn't
// complete a turn before its deadline.
type TimedOutTeam struct {
	Color uint `json:"color"`
	// Players are the display names, from the start, of the team's players
	// who didn't complete the turn, sorted.
	Players []string `json:"players"`
}
