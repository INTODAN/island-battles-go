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
	Generations        []Generation
}

// GameTeam is a team that took part in the battle, with its lobby name and
// the display names its players had at the start.
type GameTeam struct {
	Color   uint     `json:"color"`
	Name    string   `json:"name"`
	Players []string `json:"players"`
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
)

// GameOutcome is how a finished game ended.
type GameOutcome struct {
	Reason GameOutcomeReason `json:"reason"`
	// WinnerTeamColor is nil for a draw.
	WinnerTeamColor *uint `json:"winner_team_color"`
}
