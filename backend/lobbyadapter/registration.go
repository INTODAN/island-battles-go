package lobbyadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"islandBattles/backend/models"
	"islandBattles/backend/services"
	"islandBattles/lobby/domain"

	"gorm.io/gorm"
)

// GameKey identifies Island Battles lobbies.
const GameKey = "island-battles"

// Registration declares Island Battles to the lobby policy. Its settings
// validator reads default maps from database. Every team owns castles on the
// map, so a game can't start with an empty team. Every member chooses a bot
// before getting ready, so the ready gate refuses a member without one with
// services.BotNotChosenProblemType. The host can add bot seats, whose
// seat_settings {"bot_level": N} name a default bot that the backend plays.
func Registration(database *gorm.DB) domain.GameRegistration {
	return domain.GameRegistration{
		Key: GameKey,
		ValidateSettings: func(ctx context.Context, settings json.RawMessage, teams []domain.TeamDefinition) error {
			return validateSettings(ctx, database, settings, teams)
		},
		StartRules: domain.StartRules{EveryTeamNeedsMember: true},
		ReadyGate: func(ctx context.Context, lobbyID string) (domain.ReadyCheck, error) {
			return services.BotReadyCheck(database.WithContext(ctx), lobbyID)
		},
		ValidateSeat: func(_ context.Context, settings json.RawMessage) (string, error) {
			return services.ValidateBotSeat(settings)
		},
	}
}

// validateSettings accepts no map choice, or a map that exists and is made for
// the lobby's teams. A failed database read is reported as unavailable rather
// than as invalid settings.
func validateSettings(ctx context.Context, database *gorm.DB, settings json.RawMessage, teams []domain.TeamDefinition) error {
	_, err := services.LobbyMap(database.WithContext(ctx), settings, len(teams))
	var invalidSettings *models.LobbySettingsError
	switch {
	case err == nil, errors.As(err, &invalidSettings):
		return err
	case ctx.Err() != nil:
		return ctx.Err()
	default:
		return fmt.Errorf("%w: read default maps: %w", domain.ErrUnavailable, err)
	}
}
