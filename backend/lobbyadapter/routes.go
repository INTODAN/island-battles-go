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
		Summary:     "Create or return an automatic Island Battles game from a started lobby",
		Description: "An active host creates the game and runs an automatic battle, or retries a failed launch. Replays return the same game, frozen team mapping, and saved simulation.",
	}, binding, authenticator)
}
