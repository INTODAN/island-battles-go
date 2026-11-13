// Package lobbyadapter binds Island Battles to the lobby the way a separate
// game would: it serves the lobby's signed hooks (github.com/INTODAN/mpl/gamehooks) at
// HooksPath, reads lobbies through the lobby's game API, and declares the
// game to the lobby with the LOBBY_GAMES entry of LobbyGame.
package lobbyadapter

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/INTODAN/mpl/domain"
	"github.com/INTODAN/mpl/gamehooks"
	"islandBattles/backend/dal"
	"islandBattles/backend/models"
	"islandBattles/backend/services"

	"gorm.io/gorm"
)

// GameKey identifies Island Battles lobbies.
const GameKey = models.LobbyGameKey

// Lobbies reads a lobby of Island Battles as the game does, through the
// lobby's game API: gamehooks.GameAPIClient.
type Lobbies = services.LobbyReader

// seats reads the members of a lobby, seats among them, or returns nil
// without lobbies, which checks no seats.
func seats(lobbies Lobbies, lobbyID string) func(context.Context) ([]domain.Member, error) {
	if lobbies == nil {
		return nil
	}
	return func(ctx context.Context) ([]domain.Member, error) {
		snapshot, err := lobbies.Snapshot(ctx, lobbyID)
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

// Registration is Island Battles' side of the lobby's hooks, as Go
// functions, which Hooks serves. Its settings validator reads default maps
// from database. Every member chooses a bot in their member settings,
// {"bot": {...}}, before getting ready, so the ready check refuses a member
// without one with services.BotNotChosenProblemType. The host can add bot
// seats, whose seat_settings {"bot_level": N} name a default bot that the
// backend plays. While serverBots are enabled, a member's bot, and a seat's,
// may also be a hosted bot at a URL or a bot package uploaded to the lobby,
// which the validators and the ready check check against database and
// lobbies (see services.BotRules). The start binds the game of the lobby's
// round, once, pushes its first generation with publisher, and sends the turns of hosted and uploaded bots with sender, nil for none. A lobby plays
// one game per round, and a member can reopen it for the next round once the
// game of the current one has ended.
//
// The lobby runs the ready check inside the transaction of a ready or start
// request, so it reads as little as it can: a member getting ready is
// checked from the request alone unless their bot is an uploaded one, whose
// package it reads; at the start, with server bots on, it also reads the
// lobby's seats through lobbies, which the game API reads without waiting
// for the lobby's lock (services.BotRules.ReadyGate).
//
// The key, start rules and retention that the lobby applies are those of
// LobbyGame's configuration; the registration's own are not read.
func Registration(database *gorm.DB, publisher services.Publisher, sender *services.TurnSender, serverBots services.ServerBots, lobbies Lobbies) domain.GameRegistration {
	rules := services.BotRules{Database: database, ServerBots: serverBots}
	return domain.GameRegistration{
		Key: GameKey,
		ValidateSettings: func(ctx context.Context, _ string, settings json.RawMessage, teams []domain.TeamDefinition) error {
			return validateSettings(ctx, database, settings, teams)
		},
		ReadyGate: func(ctx context.Context, lobbyID string) (domain.ReadyCheck, error) {
			return rules.ReadyGate(ctx, lobbyID, seats(lobbies, lobbyID))
		},
		ValidateSeat:           rules.ValidateBotSeat,
		ValidateMemberSettings: rules.ValidateMemberSettings,
		GameOver: func(ctx context.Context, lobbyID string, round int64) (bool, error) {
			return gameOver(ctx, database, lobbyID, round)
		},
		StartGame: func(ctx context.Context, snapshot domain.Snapshot) (string, error) {
			return startGame(ctx, database, publisher, sender, snapshot)
		},
	}
}

// startGame binds the game of a started lobby's round and initializes its
// first generation: services.RunLobbyGame. It is idempotent by lobby and
// round, so the lobby's retried start returns the same game. The lobby
// checked the start, and sends a snapshot of an Island Battles lobby with
// ordered teams and a frozen roster; anything else is refused.
//
// The start answers within the lobby's hook timeout, gamehooks.DefaultTimeout,
// whichever way the hook was called. When Go bots alone play the first turn,
// RunLobbyGame keeps playing their turns until shortly before ctx's deadline,
// and later game requests play the rest. In the backend's own process ctx
// carries the deadline of the lobby's call; a request that came over the
// network carries none, so startGame sets the same one from the time the
// request arrived, and the start answers in about as long in both modes.
func startGame(ctx context.Context, database *gorm.DB, publisher services.Publisher, sender *services.TurnSender, snapshot domain.Snapshot) (string, error) {
	if err := checkStarted(snapshot); err != nil {
		return "", err
	}
	// An earlier deadline, the in-process call's, stays.
	ctx, cancel := context.WithTimeout(ctx, startHookTimeout)
	defer cancel()
	result, err := services.RunLobbyGame(ctx, database, publisher, sender, snapshot)
	if err != nil {
		return "", err
	}
	return strconv.FormatUint(uint64(result.GameID), 10), nil
}

// startHookTimeout is how long the lobby waits for the start hook.
const startHookTimeout = gamehooks.DefaultTimeout

// checkStarted refuses a snapshot that is not of a started Island Battles
// lobby with a frozen roster, teams numbered 1 to n, and roster entries on
// those teams. It sorts the snapshot's teams by their order.
func checkStarted(snapshot domain.Snapshot) error {
	if snapshot.Lobby.GameKey != GameKey || domain.ValidateUUID(snapshot.Lobby.ID) != nil {
		return errors.New("the snapshot is not of an Island Battles lobby")
	}
	if snapshot.Lobby.Phase != domain.PhaseStarted || len(snapshot.StartRoster) == 0 || len(snapshot.Teams) == 0 {
		return errors.New("the lobby has not started")
	}
	slices.SortFunc(snapshot.Teams, func(left, right domain.Team) int {
		return cmp.Compare(left.Order, right.Order)
	})
	teamOrders := make(map[string]int, len(snapshot.Teams))
	for index, team := range snapshot.Teams {
		if domain.ValidateUUID(team.ID) != nil || team.Order != index+1 || teamOrders[team.ID] != 0 {
			return errors.New("the lobby's teams are not numbered 1 to n")
		}
		teamOrders[team.ID] = team.Order
	}
	for _, entry := range snapshot.StartRoster {
		if entry.TeamOrder < 1 || teamOrders[entry.TeamID] != entry.TeamOrder {
			return errors.New("the start roster names a team the lobby doesn't have")
		}
	}
	return nil
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
