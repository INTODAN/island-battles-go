package lobbyadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"islandBattles/backend/dal"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
	"islandBattles/lobby/domain"

	"gorm.io/gorm"
)

// GameKey identifies Island Battles lobbies.
const GameKey = models.LobbyGameKey

// Lobbies reads a lobby as the game does, through the lobby's game API. The
// start's ready check reads the lobby's bot seats with it. The lobby service
// implements the API but is created after its registration, so the host sets
// API once the service exists; while it is nil, no seats are read.
type Lobbies struct {
	API domain.GameAPI
}

// seats reads the members of a lobby, seats among them.
func (lobbies *Lobbies) seats(lobbyID string) func(context.Context) ([]domain.Member, error) {
	if lobbies == nil || lobbies.API == nil {
		return nil
	}
	return func(ctx context.Context) ([]domain.Member, error) {
		snapshot, err := lobbies.API.GameSnapshot(ctx, domain.Scope{GameKey: GameKey, LobbyID: lobbyID})
		switch {
		case err == nil:
			return snapshot.Members, nil
		case ctx.Err() != nil:
			return nil, ctx.Err()
		default:
			return nil, fmt.Errorf("%w: read the lobby's seats: %w", domain.ErrUnavailable, err)
		}
	}
}

// Registration declares Island Battles to the lobby policy. Its settings
// validator reads default maps from database. Every team owns castles on the
// map, so a game can't start with an empty team. Every member chooses a bot
// in their member settings, {"bot": {...}}, before getting ready, so the
// ready gate refuses a member without one with
// services.BotNotChosenProblemType. The host can add bot seats, whose
// seat_settings {"bot_level": N} name a default bot that the backend plays.
// While serverBots are enabled, a member's bot, and a seat's, may also be a
// hosted bot at a URL or a bot package uploaded to the lobby, which the
// validators and the ready gate check against database and lobbies (see
// services.BotRules). A lobby plays one game per round, and a member can
// reopen it for the next round once the game of the current one has ended.
func Registration(database *gorm.DB, serverBots services.ServerBots, lobbies *Lobbies) domain.GameRegistration {
	rules := services.BotRules{Database: database, ServerBots: serverBots}
	return domain.GameRegistration{
		Key: GameKey,
		ValidateSettings: func(ctx context.Context, _ string, settings json.RawMessage, teams []domain.TeamDefinition) error {
			return validateSettings(ctx, database, settings, teams)
		},
		StartRules: domain.StartRules{EveryTeamNeedsMember: true},
		ReadyGate: func(ctx context.Context, lobbyID string) (domain.ReadyCheck, error) {
			return rules.ReadyGate(ctx, lobbyID, lobbies.seats(lobbyID))
		},
		ValidateSeat:           rules.ValidateBotSeat,
		ValidateMemberSettings: rules.ValidateMemberSettings,
		GameOver: func(ctx context.Context, lobbyID string, round int64) (bool, error) {
			return gameOver(ctx, database, lobbyID, round)
		},
	}
}

// gameOver reports whether the game of a lobby round has ended, so that a
// member can reopen the lobby for another game. A round whose start bound no
// game counts as over, so the lobby can't be stuck. A failed read is
// reported as unavailable.
func gameOver(ctx context.Context, database *gorm.DB, lobbyID string, round int64) (bool, error) {
	over, err := dal.LobbyRoundEnded(database.WithContext(ctx), lobbyID, round)
	switch {
	case err == nil:
		return over, nil
	case ctx.Err() != nil:
		return false, ctx.Err()
	default:
		return false, fmt.Errorf("%w: read the lobby's game: %w", domain.ErrUnavailable, err)
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
