package services

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/INTODAN/mpl/domain"
	"islandBattles/backend/dal"
	"islandBattles/backend/models"

	"gorm.io/gorm"
)

// Bots chosen by their competitor ID (#412): a member's
// {"kind": "hosted", "competitor_id": "…"} or a seat's
// {"bot_competitor_id": "…"}. Such a bot plays away when it sits in a seat,
// or when the member who chose it doesn't own it; one that the member owns
// plays as if they had named it. An away bot must be registered: a live bot
// name with a URL, of an account that isn't hidden, rated or not. In a rated lobby it also must not be
// paused, and it counts towards the daily caps (checkAwayCaps).

// chosenBot is a bot name chosen by its competitor ID: its row, soft-deleted
// or not, its owner's display name, and whether anyone may choose it
// (registered).
type chosenBot struct {
	competitor models.Competitor
	owner      string
	registered bool
}

// chosenBots are the bots chosen by competitor ID, by that ID. An ID that no
// competitor has is missing.
type chosenBots map[string]chosenBot

// name is how a refusal names the bot: its name, or "The bot" once it was
// deleted, which cleared it.
func (bot chosenBot) name() string {
	if bot.competitor.Name == nil {
		return "The bot"
	}
	return *bot.competitor.Name
}

// ownedBy reports whether the account owns the bot.
func (bot chosenBot) ownedBy(accountID string) bool {
	return accountID != "" && bot.competitor.AccountID != nil && *bot.competitor.AccountID == accountID
}

// readChosenBots reads the competitors with these IDs, soft-deleted ones too,
// and their accounts, in two queries; no IDs read nothing. A bot is
// registered as GET /bots/registered lists it: a live bot name with a URL,
// of an account that the game knows and that isn't hidden, rated or not.
func readChosenBots(database *gorm.DB, competitorIDs []string) (chosenBots, error) {
	chosen := chosenBots{}
	if len(competitorIDs) == 0 {
		return chosen, nil
	}
	competitors, err := dal.CompetitorsByID(database, competitorIDs)
	if err != nil {
		return nil, err
	}
	accounts, err := dal.AccountsByID(database, competitorAccounts(competitors))
	if err != nil {
		return nil, err
	}
	for _, competitor := range competitors {
		if competitor.Kind != models.BotCompetitor || competitor.AccountID == nil {
			continue
		}
		account, known := accounts[*competitor.AccountID]
		chosen[competitor.ID] = chosenBot{competitor: competitor, owner: account.DisplayName,
			registered: !competitor.DeletedAt.Valid && competitor.URL != nil && known && !account.Hidden}
	}
	return chosen, nil
}

// chosenCompetitorIDs are the competitor IDs that the members' hosted bots
// and the seats name, each once.
func chosenCompetitorIDs(members []domain.ReadyMember, seats []domain.Member) []string {
	var ids []string
	add := func(id string) {
		if id != "" && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	for _, member := range members {
		if bot, err := MemberBot(member.Settings, true); err == nil {
			add(bot.CompetitorID)
		}
	}
	for _, seat := range seats {
		if settings, err := seatSettings(seat.SeatSettings, true); err == nil {
			add(settings.BotCompetitorID)
		}
	}
	return ids
}

// pausedMessage says that an away bot is paused.
func pausedMessage(name string) string {
	return name + " is paused after it missed a turn, until its owner resumes it."
}

// notListedMessage says that a bot chosen by its competitor ID is no longer
// registered: deleted, without a URL, or of a hidden account.
func notListedMessage(name string) string {
	return name + " is no longer a registered bot with a URL."
}

// checkChosenBots refuses the members' and the seats' bots chosen by
// competitor ID that can't play: a member's own bot name that was deleted or
// has no URL, an away bot that is no longer registered, and in a
// rated lobby an away bot that is paused. A member getting ready is told to
// choose another; at the start the refusal names whom the host waits for, or
// the seat to remove.
func checkChosenBots(members []domain.ReadyMember, seats []domain.Member, chosen chosenBots, rated, atStart bool) error {
	for _, member := range members {
		settings, err := MemberBot(member.Settings, true)
		if err != nil || settings.Kind != models.HostedBot || settings.CompetitorID == "" {
			continue
		}
		bot, found := chosen[settings.CompetitorID]
		own := found && bot.ownedBy(member.AccountID)
		var problemType, detail, startDetail string
		switch {
		case own && bot.competitor.DeletedAt.Valid:
			problemType, detail, startDetail = RatedBotNameGoneProblemType, "The bot name you chose no longer exists. Choose another.", member.DisplayName+"'s bot name no longer exists: they must choose another."
		case own && bot.competitor.URL == nil:
			problemType = BotNameURLRequiredProblemType
			detail = "Your bot name " + bot.name() + " has no URL yet. Add its hosted bot's URL on the account page, then get ready again."
			startDetail = member.DisplayName + "'s bot name " + bot.name() + " has no URL yet: they must add its hosted bot's URL on the account page."
		case own:
			continue
		case !found || !bot.registered:
			problemType, detail, startDetail = RatedBotNotListedProblemType, notListedMessage(bot.name())+" Choose another bot.", bot.name()+", which "+member.DisplayName+" chose, is no longer a registered bot with a URL: they must choose another bot."
		case rated && bot.competitor.AwayPausedAt != nil:
			problemType, detail, startDetail = RatedBotPausedProblemType, pausedMessage(bot.name())+" Choose another bot.", strings.TrimSuffix(pausedMessage(bot.name()), ".")+": "+member.DisplayName+" must choose another bot."
		default:
			continue
		}
		if atStart {
			return domain.GameRequirement(problemType, startDetail)
		}
		return domain.GameRequirement(problemType, detail)
	}
	for _, seat := range seats {
		settings, err := seatSettings(seat.SeatSettings, true)
		if err != nil || settings.BotCompetitorID == "" {
			continue
		}
		switch bot, found := chosen[settings.BotCompetitorID]; {
		case !found || !bot.registered:
			return domain.GameRequirement(RatedBotNotListedProblemType, notListedMessage(bot.name())+" Remove "+seat.DisplayName+".")
		case rated && bot.competitor.AwayPausedAt != nil:
			return domain.GameRequirement(RatedBotPausedProblemType, pausedMessage(bot.name())+" Remove "+seat.DisplayName+".")
		}
	}
	return nil
}

// accountEntry is a competitor of a rated roster as the rule of one account,
// one competitor sees it.
type accountEntry struct {
	label, accountID, owner, competitorID string
	hand                                  bool
}

// checkOneAccountEach refuses a rated roster in which two competitors belong
// to one account: a member's hand play, any bot name chosen or seated, and
// the owner of a listed bot that someone else chose. A house bot has no
// account. The members' bots were checked, so each resolves.
func checkOneAccountEach(members []domain.ReadyMember, seats []domain.Member, names botNames, chosen chosenBots) error {
	var entries []accountEntry
	for _, member := range members {
		bot, err := MemberBot(member.Settings, true)
		switch {
		case err != nil:
		case bot.Kind == models.ManualBot:
			entries = append(entries, accountEntry{label: member.DisplayName, accountID: member.AccountID, owner: member.DisplayName, hand: true})
		case bot.CompetitorID != "":
			named := chosen[bot.CompetitorID]
			entry := accountEntry{label: named.name(), accountID: member.AccountID, owner: member.DisplayName, competitorID: bot.CompetitorID}
			if !named.ownedBy(member.AccountID) && named.competitor.AccountID != nil {
				entry.accountID, entry.owner = *named.competitor.AccountID, named.owner
			}
			entries = append(entries, entry)
		case bot.Name != "":
			entry := accountEntry{label: bot.Name, accountID: member.AccountID, owner: member.DisplayName}
			if named, found := names.find(member.AccountID, bot.Name); found {
				entry.label, entry.competitorID = *named.Name, named.ID
			}
			entries = append(entries, entry)
		}
	}
	for _, seat := range seats {
		if settings, err := seatSettings(seat.SeatSettings, true); err == nil && settings.BotCompetitorID != "" {
			named := chosen[settings.BotCompetitorID]
			if named.competitor.AccountID != nil {
				entries = append(entries, accountEntry{label: named.name(), accountID: *named.competitor.AccountID, owner: named.owner, competitorID: settings.BotCompetitorID})
			}
		}
	}
	for index, entry := range entries {
		for _, first := range entries[:index] {
			var sentence string
			switch {
			case first.competitorID != "" && first.competitorID == entry.competitorID:
				sentence = entry.label + " can play for one team only."
			case first.accountID != entry.accountID:
				continue
			case first.hand && entry.hand:
				sentence = first.label + " and " + entry.label + " are the same account."
			case first.hand || entry.hand:
				hand, bot := first, entry
				if entry.hand {
					hand, bot = entry, first
				}
				sentence = bot.label + " is " + bot.owner + "'s bot, and " + hand.label + " plays by hand."
			default:
				sentence = first.label + " and " + entry.label + " are both " + first.owner + "'s bots."
			}
			return domain.GameRequirement(RatedSameAccountProblemType, sentence+" "+ratedSameAccountMessage)
		}
	}
	return nil
}

// rosterCompetitor is the competitor of a roster player of a rated game: who
// plays them, whether away, and the team they play on.
type rosterCompetitor struct {
	// Competitor is nil only at the ready gate, for an account's hand play
	// or a house bot that the game doesn't have yet, which has no games.
	Competitor *models.Competitor
	// Label is how the caps' refusals name it: a bot's name, the member's
	// display name for hand play, Bot (level N) for a house bot.
	Label string
	Away  bool
	// Color is the team's colour, its order in the lobby, and TeamName the
	// team's name there.
	Color    uint
	TeamName string
}

// resolveRoster finds the competitor of each member and seat of a rated
// roster that checkRatedRoster passed, by player ID. With create, as the
// start does, it creates the account and hand play of an account new to the
// game, and a house bot's competitor, and refuses a member without a bot;
// without, as the ready gate does, it creates nothing and leaves those nil,
// and skips a member without a bot, for checkMembersReady to refuse.
func resolveRoster(ctx context.Context, database *gorm.DB, lobby domain.Snapshot, members []domain.ReadyMember, seats []domain.Member, names botNames, chosen chosenBots, create bool) (map[string]rosterCompetitor, error) {
	teams := map[string]domain.Team{}
	for _, team := range lobby.Teams {
		teams[team.ID] = team
	}
	onTeam := func(playerID string, competitor rosterCompetitor) rosterCompetitor {
		for _, member := range lobby.Members {
			if member.PlayerID == playerID && member.TeamID != nil {
				team := teams[*member.TeamID]
				competitor.Color, competitor.TeamName = uint(team.Order), team.Name
			}
		}
		return competitor
	}
	roster := make(map[string]rosterCompetitor, len(members)+len(seats))
	for _, person := range members {
		bot, err := MemberBot(person.Settings, true)
		switch {
		case err != nil && create:
			return nil, domain.GameRequirement(BotNotChosenProblemType, botNotChosenAtStartMessage([]string{person.DisplayName}))
		case err != nil:
			continue
		}
		entry := rosterCompetitor{Label: person.DisplayName}
		switch {
		case bot.Kind == models.ManualBot:
			if create {
				if err := (Accounts{Database: database}).Ensure(ctx, person.AccountID, person.DisplayName); err != nil {
					return nil, err
				}
			}
			hand, err := dal.FindManualCompetitor(database, person.AccountID)
			if err != nil {
				return nil, err
			}
			if hand == nil && create {
				return nil, fmt.Errorf("the hand play of %s was not stored", person.DisplayName)
			}
			entry.Competitor = hand
		case bot.CompetitorID != "":
			named := chosen[bot.CompetitorID]
			competitor := named.competitor
			entry.Competitor, entry.Label, entry.Away = &competitor, named.name(), !named.ownedBy(person.AccountID)
		default:
			named, found := names.find(person.AccountID, bot.Name)
			if !found {
				return nil, domain.GameRequirement(RatedBotNameRequiredProblemType, "This lobby is rated: waiting for "+person.DisplayName+" to play by hand, or to choose one of their hosted bot names or a registered bot.")
			}
			entry.Competitor, entry.Label = &named, *named.Name
		}
		roster[person.PlayerID] = onTeam(person.PlayerID, entry)
	}
	for _, seat := range seats {
		settings, err := seatSettings(seat.SeatSettings, true)
		if err != nil {
			// checkRatedRoster refused a seat that is no rated seat.
			return nil, err
		}
		entry := rosterCompetitor{}
		switch {
		case settings.BotCompetitorID != "":
			named := chosen[settings.BotCompetitorID]
			competitor := named.competitor
			entry.Competitor, entry.Label, entry.Away = &competitor, named.name(), true
		case create:
			house, err := dal.FindOrCreateHouseCompetitor(database, *settings.BotLevel)
			if err != nil {
				return nil, err
			}
			entry.Competitor, entry.Label = &house, BotSeatName(*settings.BotLevel)
		default:
			house, err := dal.FindHouseCompetitor(database, *settings.BotLevel)
			if err != nil {
				return nil, err
			}
			entry.Competitor, entry.Label = house, BotSeatName(*settings.BotLevel)
		}
		roster[seat.PlayerID] = onTeam(seat.PlayerID, entry)
	}
	return roster, nil
}

// utcDay is the start of the UTC day of at, from which the caps count.
func utcDay(at time.Time) time.Time {
	year, month, day := at.UTC().Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

// lockAwayCaps is the caps' check of a rated start, in the transaction that
// stores its frozen players: it locks the away bots' rows, in the order of
// their IDs as rateGame does, until the transaction ends, so that two starts
// at once can't both pass a cap: the second waits, and then counts the
// first's game. It refuses an away bot deleted, left without a URL or paused
// since the roster's check, and then checks the caps (checkAwayCaps).
func lockAwayCaps(transaction *gorm.DB, roster map[string]rosterCompetitor, now time.Time) error {
	var awayIDs []string
	for _, entry := range roster {
		if entry.Away && !slices.Contains(awayIDs, entry.Competitor.ID) {
			awayIDs = append(awayIDs, entry.Competitor.ID)
		}
	}
	if len(awayIDs) == 0 {
		return nil
	}
	locked, err := dal.LockCompetitors(transaction, awayIDs)
	if err != nil {
		return err
	}
	for _, competitor := range locked {
		name := chosenBot{competitor: competitor}.name()
		switch {
		case competitor.DeletedAt.Valid || competitor.URL == nil:
			return domain.GameRequirement(RatedBotNotListedProblemType, notListedMessage(name)+" Choose another bot, and start again.")
		case competitor.AwayPausedAt != nil:
			return domain.GameRequirement(RatedBotPausedProblemType, pausedMessage(name)+" Choose another bot, and start again.")
		}
	}
	return checkAwayCaps(transaction, roster, now)
}

// checkAwayCaps refuses a rated start whose away bots used a cap of the UTC
// day of now, counting the rated games started since the day began,
// finished or not, as their frozen players record them. A roster without
// an away bot is never counted. Per bot: a bot plays away in at most
// MaxAwayGamesPerDay rated games a day. Per pair: two competitors, at least
// one of them away, play at most one rated game a day with each of them on
// the lower team colour, which a swap of their teams frees.
func checkAwayCaps(database *gorm.DB, roster map[string]rosterCompetitor, now time.Time) error {
	var players []rosterCompetitor
	var ids []string
	for _, entry := range roster {
		if entry.Competitor != nil {
			players = append(players, entry)
			ids = append(ids, entry.Competitor.ID)
		}
	}
	if !slices.ContainsFunc(players, func(entry rosterCompetitor) bool { return entry.Away }) {
		return nil
	}
	slices.SortFunc(players, func(a, b rosterCompetitor) int { return cmp.Compare(a.Color, b.Color) })
	past, err := dal.RatedPlayersSince(database, ids, utcDay(now))
	if err != nil {
		return err
	}
	return capRefusal(players, past)
}

// capRefusal is the caps' rule over a start's competitors, in colour order,
// and the frozen players of the day's earlier rated games that hold them.
func capRefusal(players []rosterCompetitor, past []models.FrozenGamePlayer) error {
	games := map[uint]map[string]models.FrozenGamePlayer{}
	awayGames := map[string]int{}
	for _, player := range past {
		if games[player.GameID] == nil {
			games[player.GameID] = map[string]models.FrozenGamePlayer{}
		}
		games[player.GameID][*player.CompetitorID] = player
		if player.Away {
			awayGames[*player.CompetitorID]++
		}
	}
	for _, player := range players {
		if player.Away && awayGames[player.Competitor.ID] >= MaxAwayGamesPerDay {
			return domain.GameRequirement(RatedBotDayLimitProblemType, fmt.Sprintf("%s has played its %d rated games away from its owner today. Play it again tomorrow (UTC).", player.Label, MaxAwayGamesPerDay))
		}
	}
	for index, lower := range players {
		for _, higher := range players[index+1:] {
			if !lower.Away && !higher.Away {
				continue
			}
			lowerUsed, higherUsed := false, false
			for _, game := range games {
				first, foundFirst := game[lower.Competitor.ID]
				second, foundSecond := game[higher.Competitor.ID]
				if !foundFirst || !foundSecond || (!first.Away && !second.Away) {
					continue
				}
				if first.TeamColor < second.TeamColor {
					lowerUsed = true
				} else {
					higherUsed = true
				}
			}
			switch {
			case lowerUsed && higherUsed:
				return domain.GameRequirement(RatedPairLimitProblemType, lower.Label+" and "+higher.Label+" have played both their rated games today. Play again tomorrow (UTC).")
			case lowerUsed:
				return domain.GameRequirement(RatedPairLimitProblemType, lower.Label+" and "+higher.Label+" have played their rated game with "+lower.Label+" on "+lower.TeamName+" today. Swap their teams, or play again tomorrow (UTC).")
			}
		}
	}
	return nil
}

// awayPauseReason tells a bot's owner which turn it missed, such as "Missed
// turn 12 of a rated game on 21 November", the day in UTC: the turn it timed
// out in, or the turn in which a real-time game's silence rule took it out.
func awayPauseReason(turn int, at time.Time) string {
	day := at.UTC().Format("2 January")
	if turn < 1 {
		return "Missed a turn of a rated game on " + day
	}
	return fmt.Sprintf("Missed turn %d of a rated game on %s", turn, day)
}

// AwayBotSeatName is the display name of a listed bot's seat, after the bot
// and its owner, such as "Kraken (Dana)", shortening a long owner's name to
// fit the lobby's 40 characters.
func AwayBotSeatName(name, ownerName string) string {
	owner := strings.TrimSpace(ownerName)
	if owner == "" {
		return name
	}
	room := maxSeatName - utf8.RuneCountInString(name) - len(" ()")
	if utf8.RuneCountInString(owner) > room {
		owner = strings.TrimSpace(string([]rune(owner)[:max(room-1, 0)])) + "…"
	}
	return name + " (" + owner + ")"
}
