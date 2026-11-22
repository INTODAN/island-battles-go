package services

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/INTODAN/mpl/domain"
	"islandBattles/backend/dal"
	"islandBattles/backend/models"

	"gorm.io/gorm"
)

// The lobby's game_requirement problem types of a rated lobby's ready gate,
// in the order the gate checks them: a member without an account, a member
// whose bot plays under no bot name of theirs, a member whose named bot isn't
// a hosted bot, a member whose bot name was deleted since they chose it, a
// member whose bot name has no URL, and at the start two members of one
// account and a bot seat that isn't a default bot level, or a level seated
// twice. Any lobby refuses a hosted bot that plays at its name's URL, one
// without a url of its own, with RatedBotNameGoneProblemType when the name
// was deleted and BotNameURLRequiredProblemType when it has no URL.
const (
	RatedSignInRequiredProblemType    = "urn:island-battles:problem:rated-sign-in-required"
	RatedBotNameRequiredProblemType   = "urn:island-battles:problem:rated-bot-name-required"
	RatedHostedBotRequiredProblemType = "urn:island-battles:problem:rated-hosted-bot-required"
	RatedBotNameGoneProblemType       = "urn:island-battles:problem:rated-bot-name-gone"
	BotNameURLRequiredProblemType     = "urn:island-battles:problem:bot-name-url-required"
	RatedSameAccountProblemType       = "urn:island-battles:problem:rated-same-account"
	RatedSeatProblemType              = "urn:island-battles:problem:rated-seat"
	// A bot chosen by its competitor ID that is no longer registered, in
	// any lobby, and in a rated lobby an away bot that is paused.
	RatedBotNotListedProblemType = "urn:island-battles:problem:rated-bot-not-listed"
	RatedBotPausedProblemType    = "urn:island-battles:problem:rated-bot-paused"
	// The daily caps of the bots that play away, at the start only.
	RatedPairLimitProblemType   = "urn:island-battles:problem:rated-pair-limit"
	RatedBotDayLimitProblemType = "urn:island-battles:problem:rated-bot-day-limit"
)

// MaxAwayGamesPerDay is how many rated games a bot plays away in a UTC day,
// against all opponents together. Two competitors, at least one of them
// away, play at most two rated games a day: one with each on the lower team
// colour.
const MaxAwayGamesPerDay = 10

// The sentences that a rated lobby's ready gate tells a member getting ready,
// which the clients show as they are.
const (
	ratedSignInMessage      = "This lobby is rated. Sign in with Google in the 3D client to play in it."
	ratedBotNameMessage     = "This lobby is rated. Play by hand, or choose one of your hosted bot names or a registered bot."
	ratedHostedBotMessage   = "This lobby is rated. Rated bots run at their own URL: choose one of your hosted bot names, or play by hand."
	ratedSameAccountMessage = "A rated game needs a different account on each team."
	// RatedSeatRule is the rule of a rated lobby's bot seats, which every
	// refusal of a seat starts with.
	RatedSeatRule = "A rated game allows only default bot seats, each level once, and registered bots, each once."
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

// checkBotName returns the live bot name name of the member's account,
// compared without regard to case, and refuses a name that isn't one. A
// guest has none.
func (rules BotRules) checkBotName(ctx context.Context, accountID, name string) (models.Competitor, error) {
	notYours := reject(422, name+" isn't one of your bot names.")
	if accountID == "" {
		return models.Competitor{}, notYours
	}
	names, err := readBotNames(rules.Database.WithContext(ctx), []string{accountID})
	if err != nil {
		if ctx.Err() != nil {
			return models.Competitor{}, ctx.Err()
		}
		return models.Competitor{}, fmt.Errorf("%w: read the account's bot names: %w", domain.ErrUnavailable, err)
	}
	named, found := names.find(accountID, name)
	if !found {
		return models.Competitor{}, notYours
	}
	return named, nil
}

// playsAtNameURL reports whether a member's bot plays at its bot name's URL:
// a named hosted bot in a rated lobby, whatever its own url, and elsewhere a
// named hosted bot without a url of its own.
func playsAtNameURL(bot models.MemberBotView, rated bool) bool {
	return bot.Kind == models.HostedBot && bot.Name != "" && (rated || bot.URL == "")
}

// checkNameURLs refuses members whose hosted bot plays at its bot name's URL
// (playsAtNameURL) when the name was deleted since they chose it, or has no
// URL, with the members' live bot names. At the start it names the members.
func checkNameURLs(members []domain.ReadyMember, names botNames, rated, atStart bool) error {
	var gone, goneNames, missing, missingNames []string
	for _, member := range members {
		bot, err := MemberBot(member.Settings, true)
		if err != nil || !playsAtNameURL(bot, rated) {
			continue
		}
		named, found := names.find(member.AccountID, bot.Name)
		switch {
		case !found:
			gone = append(gone, fmt.Sprintf("%s's bot name %s", member.DisplayName, bot.Name))
			goneNames = append(goneNames, bot.Name)
		case named.URL == nil:
			missing = append(missing, fmt.Sprintf("%s's bot name %s", member.DisplayName, *named.Name))
			missingNames = append(missingNames, *named.Name)
		}
	}
	switch {
	case len(gone) > 0 && atStart:
		return domain.GameRequirement(RatedBotNameGoneProblemType, strings.Join(gone, ", ")+pluralVerb(len(gone), " no longer exists", " no longer exist")+": they must choose another.")
	case len(gone) > 0:
		return domain.GameRequirement(RatedBotNameGoneProblemType, "Your bot name "+goneNames[0]+" no longer exists. Choose another.")
	case len(missing) > 0 && atStart:
		return domain.GameRequirement(BotNameURLRequiredProblemType, strings.Join(missing, ", ")+pluralVerb(len(missing), " has no URL yet: they must add its hosted bot's URL", " have no URL yet: they must add their hosted bots' URLs")+" on the account page.")
	case len(missing) > 0:
		return domain.GameRequirement(BotNameURLRequiredProblemType, "Your bot name "+missingNames[0]+" has no URL yet. Add its hosted bot's URL on the account page, then get ready again.")
	}
	return nil
}

// checkUnratedBots is the part of the ready gate of a lobby that isn't rated
// that reads bot names: a hosted bot without a url plays at its name's URL,
// so the name must still exist and have one, and a bot chosen by its
// competitor ID, a member's or, at the start, a seat's, must still be one of
// the member's names or listed (checkChosenBots). It reads the members' bot
// names, and the chosen bots, only when such a bot is chosen. lobby is the
// start's snapshot, nil for a member getting ready.
func (rules BotRules) checkUnratedBots(ctx context.Context, lobby *domain.Snapshot, members []domain.ReadyMember, atStart bool) error {
	needed := false
	for _, member := range members {
		if bot, err := MemberBot(member.Settings, true); err == nil && playsAtNameURL(bot, false) {
			needed = true
		}
	}
	seats := activeSeats(lobby)
	ids := chosenCompetitorIDs(members, seats)
	if !needed && len(ids) == 0 {
		return nil
	}
	database := rules.Database.WithContext(ctx)
	names, err := readBotNames(database, namedAccounts(members))
	if err != nil {
		return unavailable(ctx, "read the members' bot names", err)
	}
	if err := checkNameURLs(members, names, false, atStart); err != nil {
		return err
	}
	chosen, err := readChosenBots(database, ids)
	if err != nil {
		return unavailable(ctx, "read the chosen bots", err)
	}
	return checkChosenBots(members, seats, chosen, false, atStart)
}

// unavailable reports a failed read of the ready gate or a validator: the
// context's error for a cancelled request, and otherwise one that wraps
// domain.ErrUnavailable.
func unavailable(ctx context.Context, action string, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return fmt.Errorf("%w: %s: %w", domain.ErrUnavailable, action, err)
}

// activeSeats are the active bot seats of a lobby, none without one.
func activeSeats(lobby *domain.Snapshot) []domain.Member {
	if lobby == nil {
		return nil
	}
	var seats []domain.Member
	for _, member := range lobby.Members {
		if member.Active && member.IsSeat() {
			seats = append(seats, member)
		}
	}
	return seats
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
// and plays by hand or with a hosted bot under a live bot name with a URL,
// one of theirs or a listed bot, and an away bot isn't paused; at the start
// also no two competitors share an account, the lobby's active seats are
// default bot levels, each level once, and listed bots, each once, and the
// away bots have rated games left today (checkAwayCaps). A member without a
// valid bot is left to checkMembersReady. It reads the members' bot names
// and the chosen bots in a query each, and at the start the competitors and
// their games of the day.
func (rules BotRules) checkRated(ctx context.Context, lobby domain.Snapshot, members []domain.ReadyMember, atStart bool) error {
	database := rules.Database.WithContext(ctx)
	names, err := readBotNames(database, namedAccounts(members))
	if err != nil {
		return unavailable(ctx, "read the members' bot names", err)
	}
	var seats []domain.Member
	if atStart {
		seats = activeSeats(&lobby)
	}
	chosen, err := readChosenBots(database, chosenCompetitorIDs(members, seats))
	if err != nil {
		return unavailable(ctx, "read the chosen bots", err)
	}
	if err := checkRatedRoster(members, seats, atStart, names, chosen); err != nil || !atStart {
		return err
	}
	roster, err := resolveRoster(ctx, database, lobby, members, seats, names, chosen, false)
	if domain.ErrorCodeOf(err) == domain.CodeGameRequirement {
		return err
	}
	if err != nil {
		return unavailable(ctx, "read the competitors", err)
	}
	if err := checkAwayCaps(database, roster, time.Now()); err != nil {
		if domain.ErrorCodeOf(err) == domain.CodeGameRequirement {
			return err
		}
		return unavailable(ctx, "count the away games", err)
	}
	return nil
}

// checkRatedRoster checks members and seats against a rated lobby's rules,
// with the members' live bot names and the bots chosen by competitor ID, all
// but the caps, which read the day's games. At the start its refusals name
// the members, or the seats, that break them, since the host who sees them
// can't act for them.
func checkRatedRoster(members []domain.ReadyMember, seats []domain.Member, atStart bool, names botNames, chosen chosenBots) error {
	var guests, nameless, notHosted []string
	for _, member := range members {
		if member.AccountID == "" {
			guests = append(guests, member.DisplayName)
			continue
		}
		bot, err := MemberBot(member.Settings, true)
		switch {
		case err != nil, bot.Kind == models.ManualBot:
		case bot.Kind == models.DefaultBot, bot.Name == "" && bot.CompetitorID == "":
			nameless = append(nameless, member.DisplayName)
		case bot.Kind != models.HostedBot:
			// A named localhost or uploaded bot: a rated bot runs at its
			// name's URL.
			notHosted = append(notHosted, member.DisplayName)
		}
	}
	switch {
	case len(guests) > 0 && atStart:
		return domain.GameRequirement(RatedSignInRequiredProblemType, "This lobby is rated, and "+strings.Join(guests, ", ")+" didn't sign in: only players signed in with Google in the 3D client can play in it.")
	case len(guests) > 0:
		return domain.GameRequirement(RatedSignInRequiredProblemType, ratedSignInMessage)
	case len(nameless) > 0 && atStart:
		return domain.GameRequirement(RatedBotNameRequiredProblemType, "This lobby is rated: waiting for "+strings.Join(nameless, ", ")+" to play by hand, or to choose one of their hosted bot names or a registered bot.")
	case len(nameless) > 0:
		return domain.GameRequirement(RatedBotNameRequiredProblemType, ratedBotNameMessage)
	case len(notHosted) > 0 && atStart:
		return domain.GameRequirement(RatedHostedBotRequiredProblemType, "This lobby is rated, and rated bots run at their own URL: waiting for "+strings.Join(notHosted, ", ")+" to choose one of their hosted bot names, or to play by hand.")
	case len(notHosted) > 0:
		return domain.GameRequirement(RatedHostedBotRequiredProblemType, ratedHostedBotMessage)
	}
	if err := checkNameURLs(members, names, true, atStart); err != nil {
		return err
	}
	if err := checkChosenBots(members, seats, chosen, true, atStart); err != nil {
		return err
	}
	if !atStart {
		return nil
	}
	if err := checkOneAccountEach(members, seats, names, chosen); err != nil {
		return err
	}
	if remove := ratedSeatsToRemove(seats); len(remove) > 0 {
		return domain.GameRequirement(RatedSeatProblemType, RatedSeatRule+" Remove "+strings.Join(remove, ", ")+".")
	}
	return nil
}

// ratedSeatsToRemove names the seats that a rated game doesn't allow: every
// seat that is neither a default bot level nor a bot chosen by its
// competitor ID, and every seat of a level, or of a bot, after its first.
func ratedSeatsToRemove(seats []domain.Member) []string {
	var remove []string
	levels, bots := map[uint]bool{}, map[string]bool{}
	for _, seat := range seats {
		settings, err := seatSettings(seat.SeatSettings, true)
		switch {
		case err != nil:
			remove = append(remove, seat.DisplayName)
		case settings.BotLevel != nil && !levels[*settings.BotLevel]:
			levels[*settings.BotLevel] = true
		case settings.BotCompetitorID != "" && !bots[settings.BotCompetitorID]:
			bots[settings.BotCompetitorID] = true
		default:
			remove = append(remove, seat.DisplayName)
		}
	}
	return remove
}

// checkRatedSeat refuses a new seat of a rated lobby that is neither a
// default bot level nor a bot chosen by its competitor ID, or whose level or
// bot an active seat of the lobby has. A seat request sent again after a
// lost answer is refused too, as a second seat of its level or bot, but the
// first one stands.
func checkRatedSeat(lobby domain.Snapshot, seat models.BotSeatSettingsView) error {
	if seat.BotLevel == nil && seat.BotCompetitorID == "" {
		return reject(422, RatedSeatRule)
	}
	for _, member := range activeSeats(&lobby) {
		existing, err := seatSettings(member.SeatSettings, true)
		if err != nil {
			continue
		}
		sameLevel := existing.BotLevel != nil && seat.BotLevel != nil && *existing.BotLevel == *seat.BotLevel
		sameBot := existing.BotCompetitorID != "" && existing.BotCompetitorID == seat.BotCompetitorID
		if sameLevel || sameBot {
			return reject(422, RatedSeatRule+" This lobby already has "+member.DisplayName+".")
		}
	}
	return nil
}

// CheckRatedSeats refuses to turn a lobby rated while one of its active
// seats breaks a rated game's rule of seats, with a *models.LobbySettingsError
// on the rated field, whose message names the seats to remove. A listed
// bot's seat stays; the ready gate checks that it is still listed.
func CheckRatedSeats(lobby domain.Snapshot) error {
	if remove := ratedSeatsToRemove(activeSeats(&lobby)); len(remove) > 0 {
		return models.NewLobbySettingsError("rated", RatedSeatRule+" Remove "+strings.Join(remove, ", ")+".")
	}
	return nil
}

// ratedCompetitors resolves the competitor of each roster player of a rated
// game's start, by player ID, after checking the roster as the ready gate
// does at the start, all but the caps, which the start checks with the
// away bots' rows locked (lockAwayCaps): a person's manual competitor when
// they play by hand, which Accounts.Ensure creates for an account new to the
// game, their bot name's when their hosted bot plays under one, a listed
// bot's, away, when a member of another account chose it, a seat's listed
// bot, away, and a default bot seat's house competitor. The start plays a
// bot name at its URL. A roster it can't resolve fails the start hook,
// which runs after the lobby committed the start: the host's start then
// answers 502 game_unavailable, and the lobby stays started, without a game,
// until the host starts it again or reopens it. So a rated game never starts
// with a player who has no competitor.
func ratedCompetitors(ctx context.Context, database *gorm.DB, snapshot domain.Snapshot) (map[string]rosterCompetitor, error) {
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
			people = append(people, readyMember(member))
		}
	}
	names, err := readBotNames(database, namedAccounts(people))
	if err != nil {
		return nil, err
	}
	chosen, err := readChosenBots(database, chosenCompetitorIDs(people, seats))
	if err != nil {
		return nil, err
	}
	if err := checkRatedRoster(people, seats, true, names, chosen); err != nil {
		return nil, err
	}
	return resolveRoster(ctx, database, snapshot, people, seats, names, chosen, true)
}

// readyMember is a person of the lobby as the ready gate sees them.
func readyMember(member domain.Member) domain.ReadyMember {
	accountID := ""
	if member.AccountID != nil {
		accountID = *member.AccountID
	}
	return domain.ReadyMember{PlayerID: member.PlayerID, DisplayName: member.DisplayName, AccountID: accountID, Settings: member.MemberSettings}
}

// playNameURLs gives each roster player of an unrated game's start whose
// hosted bot plays at a bot name's URL its URL, read in a query or two: a
// person's hosted bot without a url of its own, under one of their names,
// and a bot chosen by its competitor ID, a person's or a seat's. The ready
// gate checked the names and the chosen bots; one deleted or no longer
// listed since fails the start, as a rated start's does, before the game is
// bound.
func playNameURLs(database *gorm.DB, snapshot domain.Snapshot, chosen map[string]frozenBot) error {
	var people []domain.ReadyMember
	var seats []domain.Member
	for _, member := range snapshot.Members {
		bot, found := chosen[member.PlayerID]
		switch {
		case !found || bot.Kind != models.HostedBot:
		case member.IsSeat() && bot.CompetitorID != "":
			seats = append(seats, member)
		case !member.IsSeat() && (bot.CompetitorID != "" || bot.Name != "" && bot.URL == ""):
			people = append(people, readyMember(member))
		}
	}
	if len(people) == 0 && len(seats) == 0 {
		return nil
	}
	names, err := readBotNames(database, namedAccounts(people))
	if err != nil {
		return err
	}
	if err := checkNameURLs(people, names, false, true); err != nil {
		return err
	}
	bots, err := readChosenBots(database, chosenCompetitorIDs(people, seats))
	if err != nil {
		return err
	}
	if err := checkChosenBots(people, seats, bots, false, true); err != nil {
		return err
	}
	for _, seat := range seats {
		bot := chosen[seat.PlayerID]
		bot.URL = *bots[bot.CompetitorID].competitor.URL
		chosen[seat.PlayerID] = bot
	}
	for _, person := range people {
		bot := chosen[person.PlayerID]
		if bot.CompetitorID != "" {
			bot.URL = *bots[bot.CompetitorID].competitor.URL
		} else {
			named, _ := names.find(person.AccountID, bot.Name)
			bot.URL = *named.URL
		}
		chosen[person.PlayerID] = bot
	}
	return nil
}

// pluralVerb is singular for one named bot and plural for several.
func pluralVerb(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}
