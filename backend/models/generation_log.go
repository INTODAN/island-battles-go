package models

import "time"

// Game log limits, per player per generation. Clients cut and count their
// lines to fit; the server rejects a batch that would break them.
const (
	MaxGenerationLogLines         = 100
	MaxGenerationLogMessageLength = 500
	MaxGenerationLogLoggerLength  = 100
)

// GenerationLogLevel is the severity of a game log line.
type GenerationLogLevel string

const (
	GenerationLogDebug   GenerationLogLevel = "debug"
	GenerationLogInfo    GenerationLogLevel = "info"
	GenerationLogWarning GenerationLogLevel = "warning"
	GenerationLogError   GenerationLogLevel = "error"
)

// Valid reports whether level is one of the four game log levels.
func (level GenerationLogLevel) Valid() bool {
	switch level {
	case GenerationLogDebug, GenerationLogInfo, GenerationLogWarning, GenerationLogError:
		return true
	}
	return false
}

// GenerationLog is one line of a player's game log, stored with the
// generation whose turn the player's bot wrote it for. It is game data, and
// is deleted with its generation.
type GenerationLog struct {
	ID     uint `gorm:"primaryKey"`
	GameID uint `gorm:"not null"`
	// The index orders a player's lines and serves both the line count that
	// enforces the limit and the reads, which load a whole generation.
	GenerationID uint               `gorm:"not null;uniqueIndex:idx_generation_logs_line,priority:1"`
	PlayerID     string             `gorm:"type:uuid;not null;uniqueIndex:idx_generation_logs_line,priority:2"`
	Sequence     uint               `gorm:"not null;uniqueIndex:idx_generation_logs_line,priority:3"`
	Level        GenerationLogLevel `gorm:"not null"`
	Logger       string             `gorm:"not null"`
	Message      string             `gorm:"not null"`
	CreatedAt    time.Time
}

// GenerationLogLine is one game log line, as a bot sends it with its commands
// and as the logs endpoint returns it.
type GenerationLogLine struct {
	Level   GenerationLogLevel `json:"level" enum:"debug,info,warning,error"`
	Logger  string             `json:"logger" maxLength:"100" doc:"The name of the logger that wrote the line, such as island_battles.player."`
	Message string             `json:"message" minLength:"1" maxLength:"500" doc:"The line, of 1 to 500 Unicode code points. A client cuts a longer line to fit and says so at its end."`
}

// GenerationLogs is the game log of one generation, as one roster player may
// see it.
type GenerationLogs struct {
	GenerationID uint                   `json:"generation_id" minimum:"1"`
	Players      []PlayerGenerationLogs `json:"players" nullable:"false" doc:"Every player of the teams you can see, ordered by team colour and then display name, with their lines in the order they were written."`
	// HiddenTeamColors never says whether a hidden team logged anything.
	HiddenTeamColors []uint `json:"hidden_team_colors" nullable:"false" doc:"Teams whose logs you can't see yet. While the game runs you see only your own team's logs; once it has an outcome you see every team's."`
}

// PlayerGenerationLogs is one player's lines for a generation. It names the
// player by the display name they had at the start, never by ID.
type PlayerGenerationLogs struct {
	DisplayName string              `json:"display_name"`
	TeamColor   uint                `json:"team_color" minimum:"1"`
	Lines       []GenerationLogLine `json:"lines" nullable:"false"`
}
