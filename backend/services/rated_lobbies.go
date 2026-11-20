package services

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/INTODAN/mpl/domain"
	"islandBattles/backend/dal"
	"islandBattles/backend/models"

	"gorm.io/gorm"
)

// The lobby's game_requirement problem types of a rated lobby's ready gate,
// in the order the gate checks them: a member without an account, a member
// whose bot plays under no bot name of theirs, a member whose bot name was
// deleted since they chose it, and at the start two members of one account
// and a bot seat that isn't a default bot level, or a level seated twice.
const (
	RatedSignInRequiredProblemType  = "urn:island-battles:problem:rated-sign-in-required"
	RatedBotNameRequiredProblemType = "urn:island-battles:problem:rated-bot-name-required"
	RatedBotNameGoneProblemType     = "urn:island-battles:problem:rated-bot-name-gone"
	RatedSameAccountProblemType     = "urn:island-battles:problem:rated-same-account"
	RatedSeatProblemType            = "urn:island-battles:problem:rated-seat"
)

// The sentences that a rated lobby's ready gate tells a member getting ready,
// which the clients show as they are.
const (
	ratedSignInMessage      = "This lobby is rated. Sign in with Google in the 3D client to play in it."
	ratedBotNameMessage     = "This lobby is rated. Play by hand, or choose one of your bot names for your bot."
	ratedSameAccountMessage = "A rated game needs a different account on each team."
	// RatedSeatRule is the rule of a rated lobby's bot seats, which every
	// refusal of a seat starts with.
	RatedSeatRule = "A rated game allows only default bot seats, each level once."
)

// LobbyRated reports whether a lobby's game_settings make its game rated.
// It reads the rated field alone; settings that aren't an object aren't
// rated.
func LobbyRated(settings json.RawMessage) bool {
	var decoded struct {
		Rated bool `json:"rated"`
	}
	return json.Unmarshal(settings, &decoded) == nil && decoded.Rated
}

// botNameOwner identifies a bot name of an account, by its name key.
type botNameOwner struct {
	accountID, nameKey string
}

// botNames are live bot names, by account and name key.
type botNames map[botNameOwner]models.Competitor

// find returns the account's live bot name name, compared without regard to
// case.
func (names botNames) find(accountID, name string) (models.Competitor, bool) {
	competitor, found := names[botNameOwner{accountID: accountID, nameKey: models.BotNameKey(name)}]
	return competitor, found
}

// namedAccounts are the accounts of the members whose bot plays under a
// name, each once.
func namedAccounts(members []domain.ReadyMember) []string {
	accounts := []string{}
	for _, member := range members {
		bot, err := MemberBot(member.Settings, true)
		if err == nil && bot.Name != "" && member.AccountID != "" && !slices.Contains(accounts, member.AccountID) {
			accounts = append(accounts, member.AccountID)
		}
	}
	return accounts
}

// readBotNames reads the live bot names of accounts, in one query. It reads
// nothing for no accounts.
func readBotNames(database *gorm.DB, accounts []string) (botNames, error) {
	competitors, err := dal.LiveBotNames(database, accounts)
	if err != nil {
		return nil, err
	}
	names := botNames{}
	for _, competitor := range competitors {
		if competitor.AccountID != nil && competitor.NameKey != nil {
			names[botNameOwner{accountID: *competitor.AccountID, nameKey: *competitor.NameKey}] = competitor
		}
	}
	return names, nil
}

// checkBotName refuses a bot name that isn't a live bot name of the
// member's account, compared without regard to case. A guest has none.
func (rules BotRules) checkBotName(ctx context.Context, accountID, name string) error {
	notYours := reject(422, name+" isn't one of your bot names.")
	if accountID == "" {
		return notYours
	}
	names, err := readBotNames(rules.Database.WithContext(ctx), []string{accountID})
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w: read the account's bot names: %w", domain.ErrUnavailable, err)
	}
	if _, found := names.find(accountID, name); !found {
		return notYours
	}
	return nil
}

// mayFailRated reports whether a member getting ready could break a rated
// lobby's rules, so the gate must read the lobby to learn whether it is
// rated: everyone but a signed-in member who plays by hand.
func mayFailRated(members []domain.ReadyMember) bool {
	for _, member := range members {
		if bot, err := MemberBot(member.Settings, true); member.AccountID == "" || err != nil || bot.Kind != models.ManualBot {
			return true
		}
	}
	return false
}

// checkRated is the ready gate of a rated lobby: each member is signed in
// and plays by hand or with a bot under one of their live bot names, and, at
// the start, no two members share an account and the lobby's active seats
// are default bot levels, each level once. A member without a valid bot is
// left to checkMembersReady. It reads the members' bot names in one query.
func (rules BotRules) checkRated(ctx context.Context, lobby domain.Snapshot, members []domain.ReadyMember, atStart bool) error {
	names, err := readBotNames(rules.Database.WithContext(ctx), namedAccounts(members))
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w: read the members' bot names: %w", domain.ErrUnavailable, err)
	}
	var seats []domain.Member
	if atStart {
		for _, member := range lobby.Members {
			if member.Active && member.IsSeat() {
				seats = append(seats, member)
			}
		}
	}
	return checkRatedRoster(members, seats, atStart, names)
}

// checkRatedRoster checks members and seats against a rated lobby's rules,
// with the members' live bot names. At the start its refusals name the
// members, or the seats, that break them, since the host who sees them can't
// act for them.
func checkRatedRoster(members []domain.ReadyMember, seats []domain.Member, atStart bool, names botNames) error {
	var guests, nameless, gone, goneNames []string
	for _, member := range members {
		if member.AccountID == "" {
			guests = append(guests, member.DisplayName)
			continue
		}
		bot, err := MemberBot(member.Settings, true)
		switch {
		case err != nil, bot.Kind == models.ManualBot:
		case bot.Kind == models.DefaultBot, bot.Name == "":
			nameless = append(nameless, member.DisplayName)
		default:
			if _, found := names.find(member.AccountID, bot.Name); !found {
				gone = append(gone, fmt.Sprintf("%s's bot name %s", member.DisplayName, bot.Name))
				goneNames = append(goneNames, bot.Name)
			}
		}
	}
	switch {
	case len(guests) > 0 && atStart:
		return domain.GameRequirement(RatedSignInRequiredProblemType, "This lobby is rated, and "+strings.Join(guests, ", ")+" didn't sign in: only players signed in with Google in the 3D client can play in it.")
	case len(guests) > 0:
		return domain.GameRequirement(RatedSignInRequiredProblemType, ratedSignInMessage)
	case len(nameless) > 0 && atStart:
		return domain.GameRequirement(RatedBotNameRequiredProblemType, "This lobby is rated: waiting for "+strings.Join(nameless, ", ")+" to play by hand, or to choose one of their bot names for their bot.")
	case len(nameless) > 0:
		return domain.GameRequirement(RatedBotNameRequiredProblemType, ratedBotNameMessage)
	case len(gone) > 0 && atStart:
		return domain.GameRequirement(RatedBotNameGoneProblemType, strings.Join(gone, ", ")+" no longer exists: they must choose another.")
	case len(gone) > 0:
		return domain.GameRequirement(RatedBotNameGoneProblemType, "Your bot name "+goneNames[0]+" no longer exists. Choose another.")
	}
	if !atStart {
		return nil
	}
	accounts := map[string]string{}
	for _, member := range members {
		if first, found := accounts[member.AccountID]; found {
			return domain.GameRequirement(RatedSameAccountProblemType, first+" and "+member.DisplayName+" are the same account. "+ratedSameAccountMessage)
		}
		accounts[member.AccountID] = member.DisplayName
	}
	if remove := ratedSeatsToRemove(seats); len(remove) > 0 {
		return domain.GameRequirement(RatedSeatProblemType, RatedSeatRule+" Remove "+strings.Join(remove, ", ")+".")
	}
	return nil
}

// ratedSeatsToRemove names the seats that a rated game doesn't allow: every
// seat that isn't a default bot level, and every seat of a level after its
// first.
func ratedSeatsToRemove(seats []domain.Member) []string {
	var remove []string
	levels := map[uint]bool{}
	for _, seat := range seats {
		settings, err := seatSettings(seat.SeatSettings, true)
		if err != nil || settings.BotLevel == nil || levels[*settings.BotLevel] {
			remove = append(remove, seat.DisplayName)
			continue
		}
		levels[*settings.BotLevel] = true
	}
	return remove
}

// checkRatedSeat refuses a new seat of a rated lobby that isn't a default
// bot level, or whose level an active seat of the lobby has. A seat request
// sent again after a lost answer is refused too, as a second seat of its
// level, but the first one stands.
func checkRatedSeat(lobby domain.Snapshot, seat models.BotSeatSettingsView) error {
	if seat.BotLevel == nil {
		return reject(422, RatedSeatRule)
	}
	for _, member := range lobby.Members {
		if !member.Active || !member.IsSeat() {
			continue
		}
		if existing, err := seatSettings(member.SeatSettings, true); err == nil && existing.BotLevel != nil && *existing.BotLevel == *seat.BotLevel {
			return reject(422, RatedSeatRule+" This lobby already has "+member.DisplayName+".")
		}
	}
	return nil
}

// CheckRatedSeats refuses to turn a lobby rated while one of its active
// seats breaks a rated game's rule of seats, with a *models.LobbySettingsError
// on the rated field, whose message names the seats to remove.
func CheckRatedSeats(lobby domain.Snapshot) error {
	var seats []domain.Member
	for _, member := range lobby.Members {
		if member.Active && member.IsSeat() {
			seats = append(seats, member)
		}
	}
	if remove := ratedSeatsToRemove(seats); len(remove) > 0 {
		return models.NewLobbySettingsError("rated", RatedSeatRule+" Remove "+strings.Join(remove, ", ")+".")
	}
	return nil
}

// ratedCompetitors resolves the competitor of each roster player of a rated
// game's start, by player ID, after checking the roster as the ready gate
// does at the start: a person's manual competitor when they play by hand,
// which Accounts.Ensure creates for an account new to the game, their bot
// name's when their bot plays under one, and a default bot seat's house
// competitor. A roster it can't resolve fails the start hook, which runs
// after the lobby committed the start: the host's start then answers 502
// game_unavailable, and the lobby stays started, without a game, until the
// host starts it again or reopens it. So a rated game never starts with a
// player who has no competitor.
func ratedCompetitors(ctx context.Context, database *gorm.DB, snapshot domain.Snapshot, chosen map[string]frozenBot) (map[string]models.Competitor, error) {
	membersByPlayer := map[string]domain.Member{}
	for _, member := range snapshot.Members {
		membersByPlayer[member.PlayerID] = member
	}
	var people []domain.ReadyMember
	var seats []domain.Member
	for _, entry := range snapshot.StartRoster {
		member, found := membersByPlayer[entry.PlayerID]
		switch {
		case !found:
			// A retried start that no longer finds a player can't tell who
			// they were.
			return nil, domain.GameRequirement(RatedSignInRequiredProblemType, "This lobby is rated, and a player of its start left: start again once every team has a signed-in player.")
		case member.IsSeat():
			seats = append(seats, member)
		default:
			accountID := ""
			if member.AccountID != nil {
				accountID = *member.AccountID
			}
			people = append(people, domain.ReadyMember{PlayerID: member.PlayerID, DisplayName: member.DisplayName, AccountID: accountID, Settings: member.MemberSettings})
		}
	}
	names, err := readBotNames(database, namedAccounts(people))
	if err != nil {
		return nil, err
	}
	if err := checkRatedRoster(people, seats, true, names); err != nil {
		return nil, err
	}
	competitors := make(map[string]models.Competitor, len(snapshot.StartRoster))
	for _, person := range people {
		bot := chosen[person.PlayerID]
		if bot.Kind == models.ManualBot {
			if err := (Accounts{Database: database}).Ensure(ctx, person.AccountID, person.DisplayName); err != nil {
				return nil, err
			}
			hand, err := dal.FindManualCompetitor(database, person.AccountID)
			if err != nil {
				return nil, err
			}
			if hand == nil {
				return nil, fmt.Errorf("the hand play of %s was not stored", person.DisplayName)
			}
			competitors[person.PlayerID] = *hand
			continue
		}
		named, found := names.find(person.AccountID, bot.Name)
		if !found {
			return nil, domain.GameRequirement(RatedBotNameRequiredProblemType, "This lobby is rated: waiting for "+person.DisplayName+" to play by hand, or to choose one of their bot names for their bot.")
		}
		competitors[person.PlayerID] = named
	}
	for _, seat := range seats {
		house, err := dal.FindOrCreateHouseCompetitor(database, chosen[seat.PlayerID].Level)
		if err != nil {
			return nil, err
		}
		competitors[seat.PlayerID] = house
	}
	return competitors, nil
}
