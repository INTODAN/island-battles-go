package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/INTODAN/mpl/domain"
	"islandBattles/backend/bots"
	"islandBattles/backend/dal"
	"islandBattles/backend/hostedbot"
	"islandBattles/backend/models"

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

// LocalhostPortTakenProblemType is the lobby's game_requirement problem type
// for a start where a person would relay two localhost bots on one port:
// their own and a localhost bot seat's that they added, or two such seats'.
const LocalhostPortTakenProblemType = "urn:island-battles:problem:localhost-port-taken"

// LocalhostSeatRelayGoneProblemType is the lobby's game_requirement problem
// type for a start with a localhost bot seat whose adder, the only person
// who may relay it, is no longer in the lobby: nobody would play its turns,
// so its team would lose at the first deadline, or, without a turn limit,
// the game would wait for it forever.
const LocalhostSeatRelayGoneProblemType = "urn:island-battles:problem:localhost-seat-relay-gone"

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
	// Compute is the monthly guard on the bot runners' compute (#226): a
	// start whose roster has an uploaded bot is refused once it is spent.
	// Nil never refuses.
	Compute *BotComputeGuard
}

// BotRules are Island Battles' bot rules for its lobbies: which bots a
// person may choose in their member settings and the host may seat, and
// whether the members may get ready and the lobby start with them.
type BotRules struct {
	Database   *gorm.DB
	ServerBots ServerBots
	// Lobbies reads a lobby's snapshot: the localhost bot seats and the
	// host's own localhost bot, whose ports a new localhost bot seat's must
	// differ from, and, at the start, the lobby's seats. Nil checks no
	// seats.
	Lobbies LobbyReader
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
	shape := `member_settings must be {"bot": {"kind": "localhost"}}, with an optional "port" from 1024 to 65535, or {"bot": {"kind": "default", "level": N}} with a level that GET /bots/defaults lists`
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
	case bot.Port != nil && bot.Kind != models.LocalhostBot:
		return reject(422, "port is only for kind localhost")
	case bot.Port != nil && !validLocalhostPort(*bot.Port):
		return reject(422, localhostPortMessage)
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

// localhostPortMessage refuses a localhost bot's port out of range.
var localhostPortMessage = fmt.Sprintf("a localhost bot's port must be from %d to %d, such as %d", models.MinLocalhostPort, models.MaxLocalhostPort, models.DefaultLocalhostPort)

// validLocalhostPort reports whether port may be a localhost bot's.
func validLocalhostPort(port uint) bool {
	return port >= models.MinLocalhostPort && port <= models.MaxLocalhostPort
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
// lobby. At the start the check also reads the lobby's snapshot through
// Lobbies, since the lobby passes no seats to the check: each person relays
// their own localhost bot and the localhost bot seats that they added, which
// need a port each, and, while server bots are offered, a bot seat whose package is gone
// stops the start too. Without Lobbies it checks no seats. A start with an
// uploaded bot, a member's or a seat's, is also refused once the bot runners
// have used this month's compute (ServerBots.Compute, #226); getting ready
// is not.
//
// The lobby runs the check while it holds the lobby's lock, so the check
// reads as little as it can: nothing for a member getting ready without an
// uploaded bot, and the lobby's packages, and at the start the runners'
// cached compute, only when a member or a seat has an uploaded bot. The
// game API reads the snapshot without waiting for the lobby's lock.
func (rules BotRules) ReadyGate(ctx context.Context, lobbyID string) (domain.ReadyCheck, error) {
	return func(members []domain.ReadyMember, atStart bool) error {
		return rules.checkReady(ctx, lobbyID, members, atStart)
	}, nil
}

// snapshot reads the lobby through Lobbies, or returns nil without them.
func (rules BotRules) snapshot(ctx context.Context, lobbyID string) (*domain.Snapshot, error) {
	if rules.Lobbies == nil {
		return nil, nil
	}
	snapshot, err := rules.Lobbies.Snapshot(ctx, lobbyID)
	switch {
	case err == nil:
		return &snapshot, nil
	case ctx.Err() != nil:
		return nil, ctx.Err()
	default:
		return nil, fmt.Errorf("%w: read the lobby's seats: %w", domain.ErrUnavailable, err)
	}
}

func (rules BotRules) checkReady(ctx context.Context, lobbyID string, members []domain.ReadyMember, atStart bool) error {
	serverBots := rules.ServerBots.Enabled
	var lobby *domain.Snapshot
	if atStart {
		var err error
		if lobby, err = rules.snapshot(ctx, lobbyID); err != nil {
			return err
		}
	}
	if !serverBots {
		if err := checkMembersReady(members, atStart, false, nil); err != nil {
			return err
		}
		return checkLocalhostPorts(lobby, members)
	}
	var packageSeats []domain.Member
	if lobby != nil {
		for _, member := range lobby.Members {
			if !member.Active || !member.IsSeat() {
				continue
			}
			if settings, err := seatSettings(member.SeatSettings, true); err == nil && settings.BotPackageID != "" {
				packageSeats = append(packageSeats, member)
			}
		}
	}
	var owners map[string]string
	if len(packageSeats) > 0 || choseUploadedBot(members) {
		records, err := dal.ListBotPackages(rules.Database.WithContext(ctx), lobbyID)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("%w: read the lobby's bot packages: %w", domain.ErrUnavailable, err)
		}
		owners = make(map[string]string, len(records))
		for _, record := range records {
			owners[record.PackageID] = record.OwnerPlayerID
		}
	}
	if err := checkMembersReady(members, atStart, true, owners); err != nil {
		return err
	}
	var goneSeats []string
	for _, member := range packageSeats {
		if settings, _ := seatSettings(member.SeatSettings, true); owners[settings.BotPackageID] == "" {
			goneSeats = append(goneSeats, member.DisplayName)
		}
	}
	if len(goneSeats) > 0 {
		return domain.GameRequirement(BotPackageGoneProblemType, "The uploaded bot of "+strings.Join(goneSeats, ", ")+" was replaced or deleted: remove the seat and add the bot again.")
	}
	if err := checkLocalhostPorts(lobby, members); err != nil {
		return err
	}
	if atStart && (len(packageSeats) > 0 || choseUploadedBot(members)) {
		return rules.checkCompute(ctx)
	}
	return nil
}

// relayPorts are the ports of the localhost bots that relayer's game client
// relays, by port, each with the name of whose it is: their own localhost
// bot, from settings, their member settings, and every active localhost bot
// seat that they added. A port taken twice is listed under its first owner,
// and taken returns it.
func relayPorts(lobby domain.Snapshot, relayer string, settings json.RawMessage) (owners map[uint]string, taken []uint) {
	owners = map[uint]string{}
	add := func(port uint, owner string) {
		if _, found := owners[port]; found {
			taken = append(taken, port)
			return
		}
		owners[port] = owner
	}
	if bot, err := MemberBot(settings, true); err == nil && bot.Kind == models.LocalhostBot {
		add(models.LocalhostPort(bot.Port), "your own localhost bot")
	}
	for _, member := range lobby.Members {
		if !member.Active || !member.IsSeat() {
			continue
		}
		if seat, err := seatSettings(member.SeatSettings, true); err == nil && seat.BotLocalhostPort != nil && seat.BotRelayPlayerID == relayer {
			add(*seat.BotLocalhostPort, member.DisplayName)
		}
	}
	return owners, taken
}

// sameLocalhostSeat reports whether an active seat of the lobby has the
// localhost bot seat settings seat: the same seat, whose request the host
// sends again after a lost answer. The lobby checks the request ID only
// after the seat's settings, so the port check must not refuse a seat its
// own port. A second seat with the same settings, under another request ID,
// is refused at the start.
func sameLocalhostSeat(lobby domain.Snapshot, seat models.BotSeatSettingsView) bool {
	for _, member := range lobby.Members {
		if !member.Active || !member.IsSeat() {
			continue
		}
		existing, err := seatSettings(member.SeatSettings, true)
		if err == nil && existing.BotLocalhostPort != nil && *existing.BotLocalhostPort == *seat.BotLocalhostPort && existing.BotRelayPlayerID == seat.BotRelayPlayerID {
			return true
		}
	}
	return false
}

// freePort is the first port from the default one up that no localhost bot
// in owners takes.
func freePort(owners map[uint]string) uint {
	port := models.DefaultLocalhostPort
	for owners[port] != "" && port < models.MaxLocalhostPort {
		port++
	}
	return port
}

// memberSettings are a member's own settings: those of the ready check's
// members, which the lobby passes from its own transaction, or else the
// snapshot's.
func memberSettings(lobby domain.Snapshot, members []domain.ReadyMember, playerID string) json.RawMessage {
	for _, member := range members {
		if member.PlayerID == playerID {
			return member.Settings
		}
	}
	for _, member := range lobby.Members {
		if member.PlayerID == playerID {
			return member.MemberSettings
		}
	}
	return nil
}

// checkLocalhostPorts refuses a start with a localhost bot seat whose adder
// is no longer an active person in the lobby, since nobody else may relay
// it, and one where a person's game client would relay two localhost bots on
// one port: their own and a localhost bot seat that they added, or two such
// seats. Each person relays only their own seats, so two people's bots may
// share a port. lobby is nil when the check reads no snapshot.
func checkLocalhostPorts(lobby *domain.Snapshot, members []domain.ReadyMember) error {
	if lobby == nil {
		return nil
	}
	people := map[string]bool{}
	for _, member := range lobby.Members {
		if member.Active && !member.IsSeat() {
			people[member.PlayerID] = true
		}
	}
	for _, member := range lobby.Members {
		if !member.Active || !member.IsSeat() {
			continue
		}
		if seat, err := seatSettings(member.SeatSettings, true); err == nil && seat.BotLocalhostPort != nil && !people[seat.BotRelayPlayerID] {
			return domain.GameRequirement(LocalhostSeatRelayGoneProblemType, fmt.Sprintf("Remove %s: the player who added it left, and only they can relay it. The host can add it again from their own page.", member.DisplayName))
		}
	}
	for _, member := range lobby.Members {
		if !member.Active || member.IsSeat() {
			continue
		}
		if _, taken := relayPorts(*lobby, member.PlayerID, memberSettings(*lobby, members, member.PlayerID)); len(taken) > 0 {
			return domain.GameRequirement(LocalhostPortTakenProblemType, fmt.Sprintf("Two of the localhost bots that %s relays use port %d: give each its own port, with island-battles-bot serve --port N, and remove the seat or choose the port again.", member.DisplayName, taken[0]))
		}
	}
	return nil
}

// checkCompute refuses a start with an uploaded bot once the bot runners
// have used this month's compute budget. A failed read of their compute is
// reported as unavailable.
func (rules BotRules) checkCompute(ctx context.Context) error {
	if rules.ServerBots.Compute == nil {
		return nil
	}
	usage, err := rules.ServerBots.Compute.Usage(ctx)
	switch {
	case err != nil && ctx.Err() != nil:
		return ctx.Err()
	case err != nil:
		return fmt.Errorf("%w: read the bot runners' compute: %w", domain.ErrUnavailable, err)
	case !usage.Available:
		return domain.GameRequirement(BotComputeProblemType, BotComputeSpentMessage(usage.Month))
	}
	return nil
}

// choseUploadedBot reports whether a member's settings name an uploaded bot.
func choseUploadedBot(members []domain.ReadyMember) bool {
	for _, member := range members {
		if bot, err := MemberBot(member.Settings, true); err == nil && bot.Kind == models.UploadedBot {
			return true
		}
	}
	return false
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
	// Port is a localhost bot's port, 0 for the default one.
	Port uint
	// RelayPlayerID is the roster player who relays a localhost bot seat:
	// the one who added it, or nobody when they are not on the roster.
	RelayPlayerID string
}

// defaultBot is a default bot of a level.
func defaultBot(level uint) frozenBot { return frozenBot{Kind: models.DefaultBot, Level: level} }

// lobbyBots returns the roster's bots, by player ID, from the start's
// snapshot: each bot seat's, and the one in each player's member settings.
// The player who added a localhost bot seat relays it, when they are a
// person on the roster; the start check refuses a seat whose adder left, so
// nobody relays it only when a retried start no longer finds them. The
// start requires a bot of
// every roster player, so a player is absent only when a retried start no
// longer finds them among the members, because they left since; their own
// client plays, as for a localhost bot on the default port.
func lobbyBots(snapshot domain.Snapshot) (map[string]frozenBot, error) {
	members := map[string]domain.Member{}
	for _, member := range snapshot.Members {
		members[member.PlayerID] = member
	}
	people := map[string]bool{}
	for _, entry := range snapshot.StartRoster {
		if member, found := members[entry.PlayerID]; found && !member.IsSeat() {
			people[entry.PlayerID] = true
		}
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
			case settings.BotLocalhostPort != nil:
				seat := frozenBot{Kind: models.LocalhostBot, Port: *settings.BotLocalhostPort}
				if people[settings.BotRelayPlayerID] {
					seat.RelayPlayerID = settings.BotRelayPlayerID
				}
				chosen[entry.PlayerID] = seat
			case settings.BotURL != "":
				chosen[entry.PlayerID] = frozenBot{Kind: models.HostedBot, URL: settings.BotURL}
			default:
				chosen[entry.PlayerID] = frozenBot{Kind: models.UploadedBot, PackageID: settings.BotPackageID}
			}
			continue
		}
		bot, err := MemberBot(member.MemberSettings, true)
		if err != nil {
			continue
		}
		frozen := frozenBot{Kind: bot.Kind, URL: bot.URL, PackageID: bot.PackageID}
		if bot.Level != nil {
			frozen.Level = *bot.Level
		}
		if bot.Port != nil {
			frozen.Port = *bot.Port
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
	case models.LocalhostBot:
		port := models.LocalhostPort(nil)
		if bot.Port != 0 {
			port = bot.Port
		}
		player.BotPort = &port
		if bot.RelayPlayerID != "" {
			relay := bot.RelayPlayerID
			player.BotRelayPlayerID = &relay
		}
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
	localhost := fmt.Sprintf(`{"bot_localhost_port": N, "bot_relay_player_id": "<your player ID>"} with a port from %d to %d`, models.MinLocalhostPort, models.MaxLocalhostPort)
	if !serverBots {
		return `seat_settings must be {"bot_level": N} with a level that GET /bots/defaults lists (` + strings.Join(levels, ", ") + `) or ` + localhost
	}
	return `seat_settings must be {"bot_level": N} with a level that GET /bots/defaults lists (` + strings.Join(levels, ", ") + `), ` + localhost + `, {"bot_url": "https://…"} or {"bot_package_id": "<a package of this lobby>"}`
}

// portDigits matches a localhost bot seat's port typed as text.
var portDigits = regexp.MustCompile(`^"[0-9]{1,5}"$`)

// numericPort reads a localhost bot seat's port that the lobby page's text
// field sent as a string of digits, {"bot_localhost_port": "41415"}, as the
// number it names. Other settings come back as they are.
func numericPort(settings json.RawMessage) json.RawMessage {
	var fields map[string]json.RawMessage
	if json.Unmarshal(settings, &fields) != nil {
		return settings
	}
	port, found := fields["bot_localhost_port"]
	if !found || !portDigits.Match(port) {
		return settings
	}
	digits := strings.TrimLeft(string(port[1:len(port)-1]), "0")
	if digits == "" {
		digits = "0"
	}
	fields["bot_localhost_port"] = json.RawMessage(digits)
	numeric, err := json.Marshal(fields)
	if err != nil {
		return settings
	}
	return numeric
}

// seatSettings reads a bot seat's seat_settings and checks their shape: one
// of {"bot_level": N} with a level that GET /bots/defaults lists,
// {"bot_localhost_port": N, "bot_relay_player_id": "…"} with a port from 1024
// to 65535, as a number or a string of digits, and a player ID, and, with
// serverBots, {"bot_url": "…"} or {"bot_package_id": "…"}. It reads
// nothing.
func seatSettings(settings json.RawMessage, serverBots bool) (models.BotSeatSettingsView, error) {
	invalid := errors.New(seatSettingsShape(serverBots))
	var decoded models.BotSeatSettingsView
	decoder := json.NewDecoder(bytes.NewReader(numericPort(settings)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return models.BotSeatSettingsView{}, invalid
	}
	set := 0
	for _, present := range []bool{decoded.BotLevel != nil, decoded.BotLocalhostPort != nil, decoded.BotURL != "", decoded.BotPackageID != ""} {
		if present {
			set++
		}
	}
	if set != 1 || (!serverBots && decoded.BotLevel == nil && decoded.BotLocalhostPort == nil) {
		return models.BotSeatSettingsView{}, invalid
	}
	if (decoded.BotLocalhostPort != nil) != (decoded.BotRelayPlayerID != "") || (decoded.BotRelayPlayerID != "" && !isUUID(decoded.BotRelayPlayerID)) {
		return models.BotSeatSettingsView{}, invalid
	}
	if decoded.BotLevel != nil {
		if _, found := bots.ForLevel(*decoded.BotLevel); !found {
			return models.BotSeatSettingsView{}, invalid
		}
	}
	if decoded.BotLocalhostPort != nil && !validLocalhostPort(*decoded.BotLocalhostPort) {
		return models.BotSeatSettingsView{}, errors.New(localhostPortMessage)
	}
	return decoded, nil
}

// BotSeatName is the display name of a bot seat of a level.
func BotSeatName(level uint) string { return fmt.Sprintf("Bot (level %d)", level) }

// LocalhostBotSeatName is the display name of a localhost bot seat.
func LocalhostBotSeatName(port uint) string { return fmt.Sprintf("Localhost bot (port %d)", port) }

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
// Whatever the server bots, a seat may be a localhost bot on a port of the
// host's machine, named after its port, such as "Localhost bot (port
// 41415)". Its settings name the host who adds it, bot_relay_player_id,
// since the lobby records no seat's adder: only that player's game client
// relays it, so a later host is never sent to ports that someone else chose.
// Through Lobbies it checks that they are the lobby's host, and that the port
// differs from their own localhost bot's and every other localhost bot
// seat's that they added, unless the seat is already there with these
// settings, when the host sends its request again. The start checks the
// ports again, since the host can change their own port later. A host who
// sends another's player ID while the host changes, within the moment
// between this check and the lobby's, can still name them.
func (rules BotRules) ValidateBotSeat(ctx context.Context, lobbyID string, settings json.RawMessage) (string, error) {
	decoded, err := seatSettings(settings, rules.ServerBots.Enabled)
	if err != nil {
		return "", err
	}
	switch {
	case decoded.BotLevel != nil:
		return BotSeatName(*decoded.BotLevel), nil
	case decoded.BotLocalhostPort != nil:
		lobby, err := rules.snapshot(ctx, lobbyID)
		if err != nil {
			return "", err
		}
		port := *decoded.BotLocalhostPort
		if lobby != nil {
			if lobby.Lobby.HostPlayerID == nil || *lobby.Lobby.HostPlayerID != decoded.BotRelayPlayerID {
				return "", reject(422, "bot_relay_player_id must be your own player ID, as the lobby's host: only the tab of the host who adds a localhost bot seat relays it")
			}
			if sameLocalhostSeat(*lobby, decoded) {
				return LocalhostBotSeatName(port), nil
			}
			owners, _ := relayPorts(*lobby, decoded.BotRelayPlayerID, memberSettings(*lobby, nil, decoded.BotRelayPlayerID))
			if owner, taken := owners[port]; taken {
				return "", reject(422, fmt.Sprintf("port %d is already taken by %s: give each localhost bot its own port, such as %d, and serve it with island-battles-bot serve --port N", port, owner, freePort(owners)))
			}
		}
		return LocalhostBotSeatName(port), nil
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
