package lobbyadapter

import (
	"context"
	"encoding/json"

	"islandBattles/backend/models"
	"islandBattles/lobby/domain"
)

// GameKey identifies Island Battles lobbies.
const GameKey = "island-battles"

// Registration declares Island Battles to the lobby policy.
func Registration() domain.GameRegistration {
	return domain.GameRegistration{Key: GameKey, ValidateSettings: validateSettings}
}

func validateSettings(_ context.Context, settings json.RawMessage) error {
	_, err := models.ParseLobbySettings(settings)
	return err
}
