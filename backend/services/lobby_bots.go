package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"islandBattles/backend/bots"
	"islandBattles/backend/dal"
	"islandBattles/backend/hostedbot"
	"islandBattles/backend/models"
	"islandBattles/lobby/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// BotNotChosenProblemType is the lobby's game_requirement problem type for a
// member who tries to get ready, or is on the roster of a start, before
// choosing a bot.
const BotNotChosenProblemType = "urn:island-battles:problem:bot-not-chosen"

// BotPackageGoneProblemType is the lobby's game_requirement problem type for
// a member, or a bot seat, whose uploaded bot is no longer a ready package of
// the lobby: its owner replaced or deleted it. A member must choose it again;
// the host removes the seat and adds it again.
const BotPackageGoneProblemType = "urn:island-battles:problem:bot-package-gone"

// botNotChosenMessage tells the player what to do; the lobby shows it.
const botNotChosenMessage = "Choose your bot before getting ready: your localhost bot or a default bot level."

// packageGoneMessage tells a member whose uploaded bot is gone what to do.
const packageGoneMessage = "Your uploaded bot was replaced; choose it again."

// notYourPackageMessage tells a member who chose another member's package
// what to do.
const notYourPackageMessage = "Your uploaded bot must be a package you uploaded; choose one of yours."

// ServerBots configures the bots that the server plays without a player's
// tab: a hosted bot at a URL and an uploaded bot package. They are offered
// only while Enabled (ISLAND_BATTLES_SERVER_BOTS, on by default); otherwise a
// player's bot is their localhost bot or a default bot.
type ServerBots struct {
	Enabled bool
	// Resolver looks up a hosted bot's host; nil is the system's.
	Resolver hostedbot.Resolver
	// LoopbackHosts also accepts a hosted bot on this machine, such as
	// http://127.0.0.1:41414/turn (hostedbot.Loopback), which breaks the
	// URL rules. Only the local server host sets it, for development, as its
	// local runners deliver to such a URL.
	LoopbackHosts bool
}

// BotRules are Island Battles' bot rules for its lobbies: which bots a
// person may choose in their member settings and the host may seat, and
// whether the members may get ready and the lobby start with them.
type BotRules struct {
	Database   *gorm.DB
	ServerBots ServerBots
}

// MemberSettings are Island Battles' member settings in the lobby, which
// each person sets for themselves with the lobby's setLobbyMemberSettings:
// the bot that plays for them. The game copies each roster player's bot when
// the lobby starts.
type MemberSettings struct {
	Bot *models.MemberBotView `json:"bot"`
}

// memberSettingsShape names the member settings that the server takes.
func memberSettingsShape(serverBots bool) string {
	shape := `member_settings must be {"bot": {"kind": "localhost"}} or {"bot": {"kind": "default", "level": N}} with a level that GET /bots/defaults lists`
	if serverBots {
		shape += `, {"bot": {"kind": "hosted", "url": "https://…"}} or {"bot": {"kind": "uploaded", "package_id": "<your package in this lobby>"}}`
	}
	return shape
}

// MemberBot reads a person's member settings and returns their bot, checking
// its shape only: {"bot": {"kind": "localhost"}}, {"bot": {"kind":
// "default", "level": N}} with a level that GET /bots/defaults lists, and,
// with serverBots, {"bot": {"kind": "hosted", "url": "…"}} or {"bot":
// {"kind": "uploaded", "package_id": "…"}}. It reads nothing, so it checks
// neither the URL's host nor the package; ValidateMemberSettings does. Its
// errors are written for the person who sets the settings.
func MemberBot(settings json.RawMessage, serverBots bool) (models.MemberBotView, error) {
	var decoded MemberSettings
	decoder := json.NewDecoder(bytes.NewReader(settings))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil || decoded.Bot == nil {
		return models.MemberBotView{}, errors.New(memberSettingsShape(serverBots))
	}
	bot := *decoded.Bot
	if err := checkMemberBotShape(bot, serverBots); err != nil {
		return models.MemberBotView{}, err
	}
	return bot, nil
}

// checkMemberBotShape checks that a member's bot is of a kind that the
// server offers, with its kind's one field, if any, and no other.
func checkMemberBotShape(bot models.MemberBotView, serverBots bool) error {
	if !serverBots && (bot.URL != "" || bot.PackageID != "") {
		// The fields of the server's bots, which it doesn't offer.
		return errors.New(memberSettingsShape(false))
	}
	switch {
	case bot.Kind == models.LocalhostBot, bot.Kind == models.DefaultBot:
	case serverBots && (bot.Kind == models.HostedBot || bot.Kind == models.UploadedBot):
	case serverBots:
		return reject(422, "kind must be localhost, default, hosted or uploaded")
	default:
		return reject(422, "kind must be localhost or default")
	}
	switch {
	case bot.Level != nil && bot.Kind != models.DefaultBot:
		return reject(422, "level is only for kind default")
	case bot.URL != "" && bot.Kind != models.HostedBot:
		return reject(422, "url is only for kind hosted")
	case bot.PackageID != "" && bot.Kind != models.UploadedBot:
		return reject(422, "package_id is only for kind uploaded")
	}
	switch bot.Kind {
	case models.DefaultBot:
		if bot.Level == nil {
			return reject(422, defaultLevelMessage("kind default needs a level"))
		}
		if _, found := bots.ForLevel(*bot.Level); !found {
			return reject(422, defaultLevelMessage("kind default needs a level"))
		}
	case models.HostedBot:
		if bot.URL == "" {
			return reject(422, "kind hosted needs a url: where your bot server takes turns, such as https://bot.example.com/turn")
		}
	case models.UploadedBot:
		if bot.PackageID == "" {
			return reject(422, "kind uploaded needs a package_id: one of your packages in this lobby")
		}
	}
	return nil
}

// defaultLevelMessage lists the default bots' levels after a sentence.
func defaultLevelMessage(sentence string) string {
	levels := []string{}
	for _, bot := range bots.Defaults() {
		levels = append(levels, fmt.Sprint(bot.Level))
	}
	return sentence + " that GET /bots/defaults lists: " + strings.Join(levels, ", ")
}

// ValidateMemberSettings is Island Battles' member settings validator for the
// lobby: the settings name the person's bot, as MemberBot reads it. A hosted
// bot's URL must follow the rules of hostedbot.Check, and an uploaded bot
// must be a ready package of the lobby. The validator doesn't learn who sets
// the settings, so the ready check refuses a package of someone else.
func (rules BotRules) ValidateMemberSettings(ctx context.Context, lobbyID string, settings json.RawMessage) error {
	bot, err := MemberBot(settings, rules.ServerBots.Enabled)
	if err != nil {
		return err
	}
	switch bot.Kind {
	case models.HostedBot:
		return rules.checkURL(ctx, bot.URL)
	case models.UploadedBot:
		_, err := rules.readyPackage(ctx, lobbyID, bot.PackageID)
		return err
	}
	return nil
}

// checkURL checks a hosted bot's URL, resolving its host. A cancelled
// request is reported as such, not as invalid settings.
func (rules BotRules) checkURL(ctx context.Context, url string) error {
	if rules.ServerBots.LoopbackHosts && hostedbot.Loopback(url) {
		return nil
	}
	_, err := hostedbot.Check(ctx, rules.ServerBots.Resolver, url)
	return err
}

// readyPackage returns a ready package of the lobby, or an error for the
// person who chose it.
func (rules BotRules) readyPackage(ctx context.Context, lobbyID, packageID string) (models.BotPackage, error) {
	missing := reject(422, "this lobby has no uploaded bot with this package_id: choose one of the lobby's packages, from GET /game/lobbies/{lobby_id}/bot-packages")
	if !isUUID(packageID) {
		return models.BotPackage{}, missing
	}
	record, err := dal.FindBotPackage(rules.Database.WithContext(ctx), lobbyID, packageID)
	if err != nil {
		if ctx.Err() != nil {
			return models.BotPackage{}, ctx.Err()
		}
		return models.BotPackage{}, fmt.Errorf("%w: read the bot package: %w", domain.ErrUnavailable, err)
	}
	if record == nil || record.Status != models.BotPackageReady {
		return models.BotPackage{}, missing
	}
	return *record, nil
}

// isUUID reports whether value is a UUID in its canonical form, as package
// IDs are.
func isUUID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.String() == value
}

// ReadyGate is the lobby's ready gate for Island Battles: a member may get
// ready, and a roster start, only once their member settings name a bot,
// and an uploaded bot only while it is a ready package of theirs in the
// lobby. While server bots are offered, the gate reads the lobby's ready
// packages before the check, and the lobby's members with seats, since the
// lobby passes no seats to the check: at the start, a bot seat whose package
// is gone stops it too. seats may be nil, which checks no seats.
func (rules BotRules) ReadyGate(ctx context.Context, lobbyID string, seats func(context.Context) ([]domain.Member, error)) (domain.ReadyCheck, error) {
	if !rules.ServerBots.Enabled {
		return func(members []domain.ReadyMember, atStart bool) error {
			return checkMembersReady(members, atStart, false, nil)
		}, nil
	}
	records, err := dal.ListBotPackages(rules.Database.WithContext(ctx), lobbyID)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: read the lobby's bot packages: %w", domain.ErrUnavailable, err)
	}
	owners := make(map[string]string, len(records))
	for _, record := range records {
		owners[record.PackageID] = record.OwnerPlayerID
	}
	var goneSeats []string
	if seats != nil {
		members, err := seats(ctx)
		if err != nil {
			return nil, err
		}
		for _, member := range members {
			if !member.IsSeat() {
				continue
			}
			settings, err := seatSettings(member.SeatSettings, true)
			if err == nil && settings.BotPackageID != "" && owners[settings.BotPackageID] == "" {
				goneSeats = append(goneSeats, member.DisplayName)
			}
		}
	}
	return func(members []domain.ReadyMember, atStart bool) error {
		if err := checkMembersReady(members, atStart, true, owners); err != nil {
			return err
		}
		if atStart && len(goneSeats) > 0 {
			return domain.GameRequirement(BotPackageGoneProblemType, "The uploaded bot of "+strings.Join(goneSeats, ", ")+" was replaced or deleted: remove the seat and add the bot again.")
		}
		return nil
	}, nil
}

// checkMembersReady refuses members without a bot, then members whose
// uploaded bot isn't a ready package of theirs in owners, by package ID.
func checkMembersReady(members []domain.ReadyMember, atStart, serverBots bool, owners map[string]string) error {
	var missing, gone, foreign []string
	for _, member := range members {
		bot, err := MemberBot(member.Settings, serverBots)
		switch {
		case err != nil:
			missing = append(missing, member.DisplayName)
		case bot.Kind != models.UploadedBot:
		case owners[bot.PackageID] == "":
			gone = append(gone, member.DisplayName)
		case owners[bot.PackageID] != member.PlayerID:
			foreign = append(foreign, member.DisplayName)
		}
	}
	switch {
	case len(missing) > 0 && atStart:
		return domain.GameRequirement(BotNotChosenProblemType, botNotChosenAtStartMessage(missing))
	case len(missing) > 0:
		return domain.GameRequirement(BotNotChosenProblemType, botNotChosenMessage)
	case len(gone) > 0 && atStart:
		return domain.GameRequirement(BotPackageGoneProblemType, "The uploaded bot of "+strings.Join(gone, ", ")+" was replaced: they must choose it again.")
	case len(gone) > 0:
		return domain.GameRequirement(BotPackageGoneProblemType, packageGoneMessage)
	case len(foreign) > 0 && atStart:
		return domain.GameRequirement(BotPackageGoneProblemType, strings.Join(foreign, ", ")+" chose a package that someone else uploaded: they must choose one of theirs.")
	case len(foreign) > 0:
		return domain.GameRequirement(BotPackageGoneProblemType, notYourPackageMessage)
	}
	return nil
}

// botNotChosenAtStartMessage names the members that the host is waiting for.
func botNotChosenAtStartMessage(names []string) string {
	return "Waiting for " + strings.Join(names, ", ") + " to choose a bot: their localhost bot or a default bot level."
}

// frozenBot is the bot that a roster player plays with, as the start copies
// it from the lobby: their member settings' bot, or a bot seat's.
type frozenBot struct {
	Kind      models.BotKind
	Level     uint
	URL       string
	PackageID string
}

// defaultBot is a default bot of a level.
func defaultBot(level uint) frozenBot { return frozenBot{Kind: models.DefaultBot, Level: level} }

// lobbyBots returns the bots that the server plays for the roster, by
// player ID, from the start's snapshot: each bot seat's, and the one in
// each player's member settings that isn't their localhost bot. A player
// who chose their localhost bot is absent. The start requires a bot of every
// roster player, so a player is absent otherwise only when a retried start
// no longer finds them among the members, because they left since; their own
// client plays.
func lobbyBots(snapshot domain.Snapshot) (map[string]frozenBot, error) {
	members := map[string]domain.Member{}
	for _, member := range snapshot.Members {
		members[member.PlayerID] = member
	}
	chosen := map[string]frozenBot{}
	for _, entry := range snapshot.StartRoster {
		member, found := members[entry.PlayerID]
		if !found {
			continue
		}
		if member.IsSeat() {
			settings, err := seatSettings(member.SeatSettings, true)
			if err != nil {
				return nil, fmt.Errorf("bot seat %s: %w", member.ID, err)
			}
			switch {
			case settings.BotLevel != nil:
				chosen[entry.PlayerID] = defaultBot(*settings.BotLevel)
			case settings.BotURL != "":
				chosen[entry.PlayerID] = frozenBot{Kind: models.HostedBot, URL: settings.BotURL}
			default:
				chosen[entry.PlayerID] = frozenBot{Kind: models.UploadedBot, PackageID: settings.BotPackageID}
			}
			continue
		}
		bot, err := MemberBot(member.MemberSettings, true)
		if err != nil || bot.Kind == models.LocalhostBot {
			continue
		}
		frozen := frozenBot{Kind: bot.Kind, URL: bot.URL, PackageID: bot.PackageID}
		if bot.Level != nil {
			frozen.Level = *bot.Level
		}
		chosen[entry.PlayerID] = frozen
	}
	return chosen, nil
}

// freeze copies a roster player's bot into their frozen record. An uploaded
// bot's package is read in the start's transaction, for its key and
// language; a package deleted since the ready check leaves them unset.
func (bot frozenBot) freeze(transaction *gorm.DB, lobbyID string, player *models.FrozenGamePlayer) error {
	player.BotKind = bot.Kind
	switch bot.Kind {
	case models.DefaultBot:
		level := bot.Level
		player.BotLevel = &level
	case models.HostedBot:
		url := bot.URL
		player.BotURL = &url
	case models.UploadedBot:
		packageID := bot.PackageID
		player.BotPackageID = &packageID
		if !isUUID(packageID) {
			return nil
		}
		record, err := dal.FindBotPackage(transaction, lobbyID, packageID)
		if err != nil {
			return err
		}
		if record != nil && record.Status == models.BotPackageReady {
			key, language, size := record.S3Key, record.Language, record.SizeBytes
			player.BotPackageKey, player.BotPackageLanguage, player.BotPackageSizeBytes = &key, &language, &size
		}
	}
	return nil
}

// seatSettingsShape names the seat settings that the server takes.
func seatSettingsShape(serverBots bool) string {
	levels := []string{}
	for _, bot := range bots.Defaults() {
		levels = append(levels, fmt.Sprint(bot.Level))
	}
	if !serverBots {
		return `seat_settings must be {"bot_level": N} with a level that GET /bots/defaults lists: ` + strings.Join(levels, ", ")
	}
	return `seat_settings must be {"bot_level": N} with a level that GET /bots/defaults lists (` + strings.Join(levels, ", ") + `), {"bot_url": "https://…"} or {"bot_package_id": "<a package of this lobby>"}`
}

// seatSettings reads a bot seat's seat_settings and checks their shape: one
// of {"bot_level": N} with a level that GET /bots/defaults lists, and, with
// serverBots, {"bot_url": "…"} or {"bot_package_id": "…"}. It reads nothing.
func seatSettings(settings json.RawMessage, serverBots bool) (models.BotSeatSettingsView, error) {
	invalid := errors.New(seatSettingsShape(serverBots))
	var decoded models.BotSeatSettingsView
	decoder := json.NewDecoder(bytes.NewReader(settings))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return models.BotSeatSettingsView{}, invalid
	}
	set := 0
	for _, present := range []bool{decoded.BotLevel != nil, decoded.BotURL != "", decoded.BotPackageID != ""} {
		if present {
			set++
		}
	}
	if set != 1 || (!serverBots && decoded.BotLevel == nil) {
		return models.BotSeatSettingsView{}, invalid
	}
	if decoded.BotLevel != nil {
		if _, found := bots.ForLevel(*decoded.BotLevel); !found {
			return models.BotSeatSettingsView{}, invalid
		}
	}
	return decoded, nil
}

// BotSeatName is the display name of a bot seat of a level.
func BotSeatName(level uint) string { return fmt.Sprintf("Bot (level %d)", level) }

// HostedBotSeatName is the display name of a hosted bot's seat.
const HostedBotSeatName = "Hosted bot"

// maxSeatName is the longest display name the lobby takes for a seat.
const maxSeatName = 40

// UploadedBotSeatName is the display name of an uploaded bot's seat, after
// its package's owner, such as "Alex's bot", shortening a long name to fit
// the lobby's 40 characters.
func UploadedBotSeatName(ownerName string) string {
	const suffix = "'s bot"
	owner := strings.TrimSpace(ownerName)
	if owner == "" {
		return "Uploaded bot"
	}
	room := maxSeatName - utf8.RuneCountInString(suffix)
	if utf8.RuneCountInString(owner) > room {
		owner = strings.TrimSpace(string([]rune(owner)[:room-1])) + "…"
	}
	return owner + suffix
}

// ValidateBotSeat is Island Battles' seat validator for the lobby: a seat is
// a default bot of the level in its settings, which the backend plays, named
// after the level, such as "Bot (level 2)". With server bots it may instead
// be a hosted bot at a URL, under the rules of a member's hosted bot, named
// "Hosted bot", or any ready package of the lobby, named after its owner.
func (rules BotRules) ValidateBotSeat(ctx context.Context, lobbyID string, settings json.RawMessage) (string, error) {
	decoded, err := seatSettings(settings, rules.ServerBots.Enabled)
	if err != nil {
		return "", err
	}
	switch {
	case decoded.BotLevel != nil:
		return BotSeatName(*decoded.BotLevel), nil
	case decoded.BotURL != "":
		if err := rules.checkURL(ctx, decoded.BotURL); err != nil {
			return "", err
		}
		return HostedBotSeatName, nil
	default:
		record, err := rules.readyPackage(ctx, lobbyID, decoded.BotPackageID)
		if err != nil {
			return "", err
		}
		return UploadedBotSeatName(record.OwnerName), nil
	}
}
