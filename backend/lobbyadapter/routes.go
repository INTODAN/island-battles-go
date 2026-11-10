package lobbyadapter

import (
	"islandBattles/backend/models"
	"islandBattles/lobby/application"
	"islandBattles/lobby/domain"
	"islandBattles/lobby/httpapi"

	"github.com/danielgtaylor/huma/v2"
)

// Register installs the Island Battles endpoint outside the lobby-only SDK.
func Register(api huma.API, binding *application.GameBinding[models.BindingResult], authenticator domain.Authenticator) {
	httpapi.RegisterGameBinding(api, huma.Operation{
		OperationID: "createIslandGameFromLobby",
		Path:        "/game/from-lobby",
		Tags:        []string{"Island Battles"},
		Summary:     "Initialize or return a playable Island Battles game from a started lobby",
		Description: "An active host initializes generation one of the game of the lobby's current round, or retries a failed initialization. Repeated requests return the same game and frozen team mapping; each frozen roster player must complete their turn to advance. A lobby that a member reopened after its game and the host started again gets a new game for its new round.",
	}, binding, authenticator)
}
