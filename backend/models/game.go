package models

import "gorm.io/gorm"

type Game struct {
	gorm.Model
	LobbyID *string `gorm:"type:uuid;uniqueIndex"`
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
	Generations        []Generation
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
)

// GameOutcome is how a finished game ended.
type GameOutcome struct {
	Reason GameOutcomeReason `json:"reason"`
	// WinnerTeamColor is nil for a draw.
	WinnerTeamColor *uint `json:"winner_team_color"`
	// TimedOut lists the teams that lost a turn_timeout game, in colour
	// order. It is empty for every other reason.
	TimedOut []TimedOutTeam `json:"timed_out,omitempty"`
	// Survivors lists the colours of the teams that were still in a
	// turn_timeout game and didn't time out, in colour order. The winner is
	// one of them, or they drew; without any, every team still in the game
	// timed out, which is a draw.
	Survivors []uint `json:"survivors,omitempty"`
}

// TimedOutTeam is a team that lost because some of its players didn't
// complete a turn before its deadline.
type TimedOutTeam struct {
	Color uint `json:"color"`
	// Players are the display names, from the start, of the team's players
	// who didn't complete the turn, sorted.
	Players []string `json:"players"`
}
