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
// sender sends the turns of hosted and uploaded bots; nil sends none.
func NewService(database *gorm.DB, lobbies domain.SnapshotReader, publisher services.Publisher, sender *services.TurnSender) (*application.GameBinding[models.BindingResult], error) {
	if database == nil || publisher == nil {
		return nil, errors.New("lobby adapter: nil database or publisher")
	}
	return application.NewGameBinding(GameKey, lobbies, func(ctx context.Context, snapshot domain.Snapshot) (models.BindingResult, error) {
		return services.RunLobbyGame(ctx, database, publisher, sender, snapshot)
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
	if scope.GameKey == GameKey {
		if _, err := service.binding.Bind(ctx, principal, scope); err != nil {
			return domain.Snapshot{}, err
		}
	}
	return snapshot, nil
}
