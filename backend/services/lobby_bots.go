package services

import (
	"context"
	"fmt"
	"strings"

	"islandBattles/backend/bots"
	"islandBattles/backend/dal"
	"islandBattles/backend/models"
	"islandBattles/lobby/domain"

	"gorm.io/gorm"
)

// BotNotChosenProblemType is the lobby's game_requirement problem type for a
// member who tries to get ready, or is on the roster of a start, before
// choosing a bot.
const BotNotChosenProblemType = "urn:island-battles:problem:bot-not-chosen"

// botNotChosenMessage tells the player what to do; the lobby shows it.
const botNotChosenMessage = "Choose your bot before getting ready: your localhost bot or a default bot level."

// LobbyBots keeps the bot that each member of a waiting Island Battles lobby
// chooses. Every member must choose one before getting ready, and the game
// copies the roster's choices when it starts.
type LobbyBots struct {
	Database *gorm.DB
	// Lobbies authorizes the caller's membership and reads the lobby's phase
	// and members.
	Lobbies domain.SnapshotReader
	// GameKey is the lobby game key of Island Battles.
	GameKey string
}

// Choose stores the caller's bot for a waiting lobby, replacing an earlier
// choice, and returns every member's choice. A lobby that has started, or
// closed, conflicts.
func (service LobbyBots) Choose(ctx context.Context, principal domain.Principal, lobbyID string, choice models.BotChoiceView) (models.LobbyBotsView, error) {
	if err := ValidateBotChoice(choice); err != nil {
		return models.LobbyBotsView{}, err
	}
	scope := domain.Scope{GameKey: service.GameKey, LobbyID: lobbyID}
	before, err := service.Lobbies.Snapshot(ctx, principal, scope)
	if err != nil {
		return models.LobbyBotsView{}, err
	}
	if before.Lobby.Phase != domain.PhaseWaiting {
		return models.LobbyBotsView{}, notWaiting()
	}
	record := models.LobbyBotChoice{LobbyID: lobbyID, PlayerID: principal.PlayerID, Kind: choice.Kind, Level: choice.Level}
	if err := dal.SaveBotChoice(service.Database.WithContext(ctx), record); err != nil {
		return models.LobbyBotsView{}, err
	}
	// The start copies the choices after it commits, so a lobby that is
	// still waiting now will start with this choice. One that started
	// meanwhile may have copied the earlier choice, so the caller learns
	// that the choice came too late.
	after, err := service.Lobbies.Snapshot(ctx, principal, scope)
	if err != nil {
		return models.LobbyBotsView{}, err
	}
	if after.Lobby.Phase != domain.PhaseWaiting {
		return models.LobbyBotsView{}, notWaiting()
	}
	return service.view(ctx, after)
}

// List returns every member's bot choice, for any member of the lobby.
func (service LobbyBots) List(ctx context.Context, principal domain.Principal, lobbyID string) (models.LobbyBotsView, error) {
	snapshot, err := service.Lobbies.Snapshot(ctx, principal, domain.Scope{GameKey: service.GameKey, LobbyID: lobbyID})
	if err != nil {
		return models.LobbyBotsView{}, err
	}
	return service.view(ctx, snapshot)
}

func (service LobbyBots) view(ctx context.Context, snapshot domain.Snapshot) (models.LobbyBotsView, error) {
	choices, err := dal.LoadBotChoices(service.Database.WithContext(ctx), snapshot.Lobby.ID)
	if err != nil {
		return models.LobbyBotsView{}, err
	}
	view := models.LobbyBotsView{LobbyID: snapshot.Lobby.ID, Members: make([]models.LobbyMemberBotView, 0, len(snapshot.Members))}
	for _, member := range snapshot.Members {
		entry := models.LobbyMemberBotView{PlayerID: member.PlayerID, DisplayName: member.DisplayName}
		if choice, found := choices[member.PlayerID]; found {
			chosen := choice.View()
			entry.Bot = &chosen
		}
		view.Members = append(view.Members, entry)
	}
	return view, nil
}

func notWaiting() error {
	return reject(409, "the lobby has started or closed; bots can be chosen only while it is waiting")
}

// ValidateBotChoice accepts the localhost bot without a level, or a default
// bot with a level that bots.Defaults lists.
func ValidateBotChoice(choice models.BotChoiceView) error {
	switch choice.Kind {
	case models.LocalhostBot:
		if choice.Level != nil {
			return reject(422, "level is only for kind default")
		}
		return nil
	case models.DefaultBot:
		levels := []string{}
		for _, bot := range bots.Defaults() {
			if choice.Level != nil && *choice.Level == bot.Level {
				return nil
			}
			levels = append(levels, fmt.Sprint(bot.Level))
		}
		return reject(422, "kind default needs a level that GET /bots/defaults lists: "+strings.Join(levels, ", "))
	default:
		return reject(422, "kind must be localhost or default")
	}
}

// BotReadyCheck is the lobby's ready gate for Island Battles: it loads a
// lobby's bot choices, and lets a member get ready only once they have one.
// A choice can be changed but never removed while the lobby waits, so a
// member who had one when the gate loaded still has one.
func BotReadyCheck(database *gorm.DB, lobbyID string) (domain.ReadyCheck, error) {
	choices, err := dal.LoadBotChoices(database, lobbyID)
	if err != nil {
		return nil, fmt.Errorf("%w: read bot choices: %w", domain.ErrUnavailable, err)
	}
	return func(playerID string) error {
		if _, chosen := choices[playerID]; !chosen {
			return domain.GameRequirement(BotNotChosenProblemType, botNotChosenMessage)
		}
		return nil
	}, nil
}

// lobbyBotLevels returns the default bot levels that the roster's players
// chose, by player ID. A player who chose their localhost bot is absent. The
// start requires a choice of every roster player, so only a lobby that started
// before choices were required has players without one; their own clients
// play, as before.
func lobbyBotLevels(database *gorm.DB, snapshot domain.Snapshot) (map[string]uint, error) {
	choices, err := dal.LoadBotChoices(database, snapshot.Lobby.ID)
	if err != nil {
		return nil, fmt.Errorf("read bot choices: %w", err)
	}
	levels := map[string]uint{}
	for _, entry := range snapshot.StartRoster {
		choice, found := choices[entry.PlayerID]
		if found && choice.Kind == models.DefaultBot && choice.Level != nil {
			levels[entry.PlayerID] = *choice.Level
		}
	}
	return levels, nil
}
