package lobbyadapter

import "islandBattles/lobby/domain"

// GameKey identifies Island Battles lobbies.
const GameKey = "island-battles"

// Registration declares Island Battles to the lobby policy.
func Registration() domain.GameRegistration {
	return domain.GameRegistration{Key: GameKey}
}
