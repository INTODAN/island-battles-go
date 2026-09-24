// Package lobbyadapter connects the generic lobby module to Island Battles.
package lobbyadapter

import (
	"context"
	"errors"

	"islandBattles/backend/models"
	"islandBattles/backend/services"
	"islandBattles/lobby/application"
	"islandBattles/lobby/domain"

	"gorm.io/gorm"
)

// NewService binds started Island Battles lobbies through the shared service.
func NewService(database *gorm.DB, lobbies domain.SnapshotReader) (*application.GameBinding[models.BindingResult], error) {
	if database == nil {
		return nil, errors.New("lobby adapter: nil database")
	}
	return application.NewGameBinding("island-battles", lobbies, func(ctx context.Context, snapshot domain.Snapshot) (models.BindingResult, error) {
		return services.RunLobbyGame(database.WithContext(ctx), snapshot)
	})
}

// WithGameStart adds Island Battles behavior without changing the generic lobby.
func WithGameStart(lobbies domain.Application, binding *application.GameBinding[models.BindingResult]) domain.Application {
	return &gameStartingApplication{Application: lobbies, binding: binding}
}

type gameStartingApplication struct {
	domain.Application
	binding *application.GameBinding[models.BindingResult]
}

func (service *gameStartingApplication) Start(ctx context.Context, principal domain.Principal, scope domain.Scope) (domain.Snapshot, error) {
	snapshot, err := service.Application.Start(ctx, principal, scope)
	if err != nil {
		return domain.Snapshot{}, err
	}
	if scope.GameKey == "island-battles" {
		if _, err := service.binding.Bind(ctx, principal, scope); err != nil {
			return domain.Snapshot{}, err
		}
	}
	return snapshot, nil
}
