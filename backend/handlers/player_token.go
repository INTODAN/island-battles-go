package handlers

import (
	"context"
	"errors"
	"log/slog"

	"github.com/INTODAN/mpl/domain"
	"github.com/INTODAN/mpl/playertoken"
	"islandBattles/backend/models"
	"islandBattles/backend/observability"

	"github.com/danielgtaylor/huma/v2"
)

// PlayerAuthenticator tells which player sends a game request.
type PlayerAuthenticator interface {
	// AuthenticatePlayer reads the request's Authorization header. A missing,
	// expired or otherwise invalid credential is playertoken.ErrInvalid.
	AuthenticatePlayer(ctx context.Context, authorization string) (domain.Principal, error)
}

// PlayerTokens authenticates game requests with the lobby's player tokens for
// the island-battles game, Authorization: Bearer <token>, which it verifies
// without the lobby's database.
type PlayerTokens struct{ Verifier *playertoken.Verifier }

// AuthenticatePlayer returns the token's player, and adds their ID to the
// request's error line.
func (tokens PlayerTokens) AuthenticatePlayer(ctx context.Context, authorization string) (domain.Principal, error) {
	token, found := playertoken.FromAuthorization(authorization)
	if !found || tokens.Verifier == nil {
		return domain.Principal{}, playertoken.ErrInvalid
	}
	player, err := tokens.Verifier.Verify(ctx, token, models.LobbyGameKey)
	if err != nil {
		return domain.Principal{}, err
	}
	observability.Annotate(ctx, slog.String("player_id", player.ID))
	return domain.Principal{PlayerID: player.ID, DisplayName: player.Name}, nil
}

// authenticate returns the player of a request, or the problem to answer:
// 401 for a missing, expired or invalid token, and 503 when the lobby's keys
// can't be read, whose cause goes on the request's error line.
func (handler *LobbyGame) authenticate(ctx context.Context, authorization string) (domain.Principal, error) {
	return authenticatePlayer(ctx, handler.Players, authorization)
}

// authenticatePlayer is authenticate for any game route.
func authenticatePlayer(ctx context.Context, players PlayerAuthenticator, authorization string) (domain.Principal, error) {
	if players == nil {
		return domain.Principal{}, huma.Error401Unauthorized("authentication required")
	}
	principal, err := players.AuthenticatePlayer(ctx, authorization)
	if errors.Is(err, playertoken.ErrInvalid) {
		return domain.Principal{}, huma.Error401Unauthorized("authentication required")
	}
	if err != nil {
		observability.RecordError(ctx, err)
		return domain.Principal{}, huma.Error503ServiceUnavailable("game is temporarily unavailable")
	}
	return principal, nil
}
