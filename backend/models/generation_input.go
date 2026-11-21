package models

import (
	"encoding/json"
	"time"
)

// FrozenGamePlayer fixes both turn participation and authority at game start.
type FrozenGamePlayer struct {
	GameID    uint   `gorm:"primaryKey;autoIncrement:false"`
	PlayerID  string `gorm:"type:uuid;primaryKey"`
	TeamColor uint   `gorm:"not null"`
	// DisplayName is the player's lobby name at the start. The game log names
	// players by it, because the lobby is deleted later.
	DisplayName string `gorm:"not null;default:''"`
	// BotKind says who plays this player, copied at the start from the bot in
	// the player's lobby member settings, or from the settings of a bot seat
	// that the host added. The player's own client relays a localhost bot;
	// the server plays the others, and nothing plays a manual player, whose
	// client sends their commands.
	BotKind BotKind `gorm:"column:bot_kind;not null;default:'localhost'"`
	// BotPort is a localhost bot's port on 127.0.0.1, for BotKind localhost
	// only; nil is DefaultLocalhostPort.
	BotPort *uint `gorm:"column:bot_port"`
	// BotRelayPlayerID is the roster player whose game client relays a
	// localhost bot seat: the player who added the seat, as its settings'
	// bot_relay_player_id records. Nil for a person, who relays their own
	// localhost bot, for every other kind, and for a seat whose adder a
	// retried start no longer finds on the roster, which nobody relays.
	BotRelayPlayerID *string `gorm:"column:bot_relay_player_id;type:uuid"`
	// BotLevel is the level of the default bot that the backend plays for
	// this player, one of bots.Defaults, for BotKind default only. The
	// backend plays it as soon as each generation is created, so it never
	// times out.
	BotLevel *uint
	// BotURL is a hosted bot's URL, for BotKind hosted only.
	BotURL *string `gorm:"column:bot_url"`
	// BotPackageID, BotPackageKey, BotPackageLanguage and
	// BotPackageSizeBytes name an uploaded bot's package, for BotKind
	// uploaded only: its ID, its zip's key in the bot package bucket, its
	// language and its zip's size, as they were at the start. All but the
	// ID are nil when the package was deleted in the moment between the
	// start's ready check and the copy; that player passes every turn.
	BotPackageID        *string `gorm:"column:bot_package_id;type:uuid"`
	BotPackageKey       *string `gorm:"column:bot_package_key"`
	BotPackageLanguage  *string `gorm:"column:bot_package_language"`
	BotPackageSizeBytes *int64  `gorm:"column:bot_package_size_bytes"`
	// CompetitorID is the competitor that plays this player in a rated game,
	// island_competitors.id, with no foreign key, since competitors outlive
	// games and games never hold them back: an account's manual competitor
	// for a person who plays by hand, a bot name's for a named bot, and a
	// house bot's for a default bot seat. Nil in a game that isn't rated.
	CompetitorID *string `gorm:"column:competitor_id;type:uuid;index"`
	// Away is set in a rated game when CompetitorID is a bot name that plays
	// away: in a bot seat, or chosen by a member of another account. The
	// daily caps count these rows (checkAwayCaps), and a missed turn pauses
	// the bot (rateGame).
	Away bool `gorm:"column:away;not null;default:false"`
	// BotName is the bot name of CompetitorID when it is a bot name, as it
	// was at the start, which GenerationState.bot shows. Nil otherwise.
	BotName *string `gorm:"column:bot_name"`
	// LastSeenAt is when the server last heard from the player in a real-time
	// game: the game's start, then the latest game request made as them, with
	// a player token or a submit token, written at most every SeenWriteInterval.
	LastSeenAt time.Time `gorm:"not null;default:now()"`
	CreatedAt  time.Time
}

type GenerationInput struct {
	GenerationID uint   `gorm:"primaryKey;autoIncrement:false"`
	PlayerID     string `gorm:"type:uuid;primaryKey"`
	Commands     []byte `gorm:"type:jsonb;not null"`
	Completed    bool   `gorm:"not null"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Request IDs cannot be reused for another generation by the same game/player.
type GenerationRequest struct {
	GameID       uint   `gorm:"primaryKey;autoIncrement:false"`
	PlayerID     string `gorm:"type:uuid;primaryKey"`
	RequestID    string `gorm:"type:uuid;primaryKey"`
	GenerationID uint   `gorm:"not null"`
	PayloadHash  string `gorm:"not null"`
	CreatedAt    time.Time
}

// PublicGenerationCommand keeps raw parameters so the service can decode them
// strictly and reject other command types with 403. The published schema
// describes it as one of SendSoldierGroupCommand, UpgradeCastleCommand and
// BurstCastleCommand, chosen by command_type.
type PublicGenerationCommand struct {
	CommandType CommandType     `json:"command_type"`
	Parameters  json.RawMessage `json:"parameters"`
}

// SendSoldierGroupCommand, UpgradeCastleCommand and BurstCastleCommand only
// describe the shapes of PublicGenerationCommand in the published schema.
type SendSoldierGroupCommand struct {
	CommandType CommandType                `json:"command_type" enum:"send_soldier_group"`
	Parameters  SendSoldierGroupParameters `json:"parameters"`
}

type UpgradeCastleCommand struct {
	CommandType CommandType             `json:"command_type" enum:"upgrade_castle"`
	Parameters  UpgradeCastleParameters `json:"parameters"`
}

type BurstCastleCommand struct {
	CommandType CommandType           `json:"command_type" enum:"burst_castle"`
	Parameters  BurstCastleParameters `json:"parameters"`
}

// SendSoldierGroupParameters orders soldiers from an owned castle. Pointers let
// validation distinguish a missing ID from castle 0.
type SendSoldierGroupParameters struct {
	OriginCastleID      *uint `json:"origin_castle_id" nullable:"false" doc:"Castle your team owns."`
	DestinationCastleID *uint `json:"destination_castle_id" nullable:"false" doc:"Any other castle."`
	SoldierAmount       *uint `json:"soldier_amount" minimum:"1" nullable:"false" doc:"Soldiers to send; counts against soldiers already reserved by your team this turn."`
}

// UpgradeCastleParameters buys the next level of one upgrade type for an owned
// castle. A pointer lets validation distinguish a missing ID from castle 0.
type UpgradeCastleParameters struct {
	CastleID    *uint        `json:"castle_id" nullable:"false" doc:"Castle your team owns. It pays the level's soldier_cost from GET /game/upgrades."`
	UpgradeType *UpgradeType `json:"upgrade_type" nullable:"false" doc:"The type to raise by one level. Several upgrades of a castle in one turn each buy the next level."`
}

// BurstCastleParameters orders a burst for an owned castle. Pointers let
// validation distinguish a missing field from 0.
type BurstCastleParameters struct {
	CastleID      *uint `json:"castle_id" nullable:"false" doc:"Castle your team owns, with no burst_debt and no other burst this turn."`
	SoldierAmount *uint `json:"soldier_amount" minimum:"1" nullable:"false" doc:"Soldiers the burst adds, at most the castle's burst_potential value from GET /game/upgrades, counting the turn's upgrades: every upgrade of its batch, before or after it, and of the turn's earlier batches. The castle gets them in the next generation, on top of its production, and repays them from its later production."`
}

type RegisterGenerationCommands struct {
	GameID       uint                      `json:"game_id" minimum:"1"`
	GenerationID uint                      `json:"generation_id" minimum:"1"`
	RequestID    string                    `json:"request_id" format:"uuid" doc:"Identifies this batch. Retry a lost request with the same UUID and body; use a new UUID for every new batch."`
	Complete     *bool                     `json:"complete,omitempty" doc:"Seals your turn after this batch. Defaults to true."`
	Commands     []PublicGenerationCommand `json:"commands" maxItems:"1000" nullable:"false"`
	Logs         []GenerationLogLine       `json:"logs,omitempty" maxItems:"100" doc:"Game log lines written since your previous batch for this generation, appended in order after the lines already stored. At most 100 lines per player per generation, each of at most 500 characters; a batch that would break either limit is rejected whole with 422 and problem type urn:island-battles:problem:game-log-limit."`
	// Memory is empty when the batch has none; it is left out of the payload
	// hash then, so batches without memory hash as they always have.
	Memory BotMemory `json:"memory,omitempty" doc:"Your bot's memory, the BotTurnResult.memory it returned: any JSON object of at most 64 KiB as compact JSON. Only the batch with complete true saves it, replacing your previous memory, and an absent or null memory clears it; other batches ignore it. GenerationState.memory returns it. Larger memory is rejected whole with 422 and problem type urn:island-battles:problem:bot-memory-too-large."`
}

type GenerationState struct {
	GameID       uint           `json:"game_id" minimum:"1"`
	LobbyID      string         `json:"lobby_id" format:"uuid"`
	LobbyRound   int64          `json:"lobby_round" minimum:"1" doc:"The lobby round that the game plays. A lobby plays one game per round, and the /game/from-lobby/{lobby_id} routes serve its newest game: compare this with the lobby snapshot's lobby.round to tell whether the game is the one the lobby started last."`
	GenerationID uint           `json:"generation_id" minimum:"1"`
	PlayerID     string         `json:"player_id" format:"uuid"`
	TeamColor    uint           `json:"team_color" minimum:"1"`
	Channel      string         `json:"channel" doc:"Your personal Centrifugo channel, which receives GenerationEvent after each turn and GameEndedEvent after the last one."`
	Submitted    bool           `json:"submitted" doc:"Whether you completed this generation's turn."`
	Generation   GenerationView `json:"generation"`
	// TurnNumber is the generation's position in its game, from 1, as the DAL
	// numbers it; MaxGenerations is the game's max_generations.
	TurnNumber     int `json:"turn_number" minimum:"1" doc:"This generation's turn number in the game, from 1: the game's first generation is turn 1, and every turn adds one, the n of the replay's Turn n / m. generation_id is not the turn number: it is the server's ID of the generation, which increases with every turn but doesn't start at 1. BotTurn.turn_number is the same number."`
	MaxGenerations int `json:"max_generations" minimum:"2" doc:"The game's last generation, the same for the whole game. The turn that creates generation max_generations ends the game, if nothing has ended it before, so turn max_generations - 1 is the last one players play. BotTurn.max_generations is the same number."`
	// Outcome is nil, and left out, while the game runs.
	Outcome *GameOutcomeView `json:"outcome,omitempty" doc:"How the game ended. Absent while the game runs. A finished game keeps its final generation and rejects every command."`

	// LogLinesStored lets a restarted client continue counting toward the
	// game log limit. Pushed events carry 0: a new generation has no lines.
	LogLinesStored uint `json:"log_lines_stored" doc:"How many game log lines you have stored for this generation, out of the 100 a player may store per generation. 0 when there are none. A client that restarts during a turn continues counting from it."`

	Memory BotMemory `json:"memory" doc:"The memory sent with your latest completed turn of this game, for your bot's next BotTurn.memory. Null before your first completed turn, and after a completed turn without memory."`

	Rated bool `json:"rated" doc:"Whether the game is rated: its lobby's rated setting at the start. Every team of a rated game is played by a competitor, a signed-in player's hand play or bot name, a bot from the leaderboard or a house bot, and bot.name names the bot name that plays."`

	Bot BotChoiceView `json:"bot" doc:"Your bot in this game, the one you chose in the lobby, copied when the game started. For kind localhost your client delivers each turn to your bot, on bot.port, which submits its answer. For kind default the backend plays your turns itself as soon as each generation is created. For kinds hosted and uploaded the server plays your turns with your bot, without your client. For kind manual you play by hand in the 3D client, which sends your commands; nothing plays or relays you. Only for kind localhost, and for relayed_bots, does your client relay anything."`

	RelayedBots []RelayedBotView `json:"relayed_bots" nullable:"false" doc:"The localhost bot seats that you added to the lobby as its host, which only you relay: your client reads each one's turn with GET /game/from-lobby/{lobby_id}/turn-delivery?player_id= and delivers it to 127.0.0.1 on the seat's port, as it does for your own localhost bot. Empty for everyone else."`

	// TurnDeadlineAt is nil when the lobby set no turn timeout, and once the
	// game has ended.
	TurnDeadlineAt *time.Time `json:"turn_deadline_at" nullable:"true" doc:"When this generation's turn ends. In a real-time game (turn_pace_milliseconds isn't null) it is the generation's creation time plus the turn pace: the server then plays the turn with the commands that arrived, and a player who sent nothing passes. Otherwise it is the creation time plus the lobby's turn_timeout_seconds, when the turn times out: if a player of a team hasn't completed it by then, the team loses and the game ends. Null when turns have no time limit, and once the game has ended. A client waiting for the turn to end calls POST /game/from-lobby/{lobby_id}/deadline once it has passed, because the server runs no timers."`
	// TurnPaceMilliseconds is Game.TurnPaceMilliseconds: nil when the game
	// waits for every player.
	TurnPaceMilliseconds *uint `json:"turn_pace_milliseconds" nullable:"true" minimum:"500" maximum:"5000" doc:"The game's turn pace in milliseconds when the game is real-time, because a player on its roster plays by hand: each turn ends this long after its generation was created, at turn_deadline_at, and plays with the commands that arrived, without waiting for anyone. It still plays early once every player completed it. Null when the game waits for every player to complete each turn."`
	// Departures are the game's applied departures; a resignation still
	// waiting for its turn isn't listed.
	Departures []DepartureView `json:"departures" nullable:"false" doc:"The teams that have resigned or left this real-time game so far, in the order they left, from the turn that took them out. Empty in every other game. A resignation shows here once the turn that applies it has played; before that, the answer of POST /game/from-lobby/{lobby_id}/resign is its only sign."`
}

// GameOutcomeView is how a finished game ended.
type GameOutcomeView struct {
	Reason          GameOutcomeReason `json:"reason" enum:"last_team_standing,all_eliminated,generation_cap,turn_timeout,abandoned" doc:"last_team_standing: one team was left after every other team lost all its castles and soldier groups, or resigned or left the game. all_eliminated: the last teams were eliminated, or left, in the same turn, a draw. generation_cap: the game reached its last generation; the team with the most castles won, and equal counts are a draw. turn_timeout: in a game that waits for every player, a turn passed its deadline; every team with a player who hadn't completed it lost, and among the other teams still standing the team with the most castles won, while equal counts, or no team left, are a draw. abandoned: nobody played a real-time game for its turn limit, so it ended on its open generation; among the teams still in it, except one whose resignation was waiting for the turn, the team with the most castles won, and equal counts, or no team left, are a draw."`
	WinnerTeamColor *uint             `json:"winner_team_color" minimum:"1" nullable:"true" doc:"The winning team, or null for a draw."`
	Summary         string            `json:"summary" doc:"The outcome in one sentence, naming the winning team, the teams that resigned or left a real-time game, and for turn_timeout the teams that lost and their players who didn't answer."`
	// Departures is optional in the schema only so that demos saved before it
	// existed still read; the server always writes it.
	Departures []DepartureView `json:"departures" required:"false" nullable:"false" doc:"The teams that resigned or left a real-time game, in the order they left. Always present, and empty for most games; a demo saved before this field existed has none, which reads as empty."`
	// Ratings is nil, and left out, for a game that isn't rated.
	Ratings []RatingChange `json:"ratings,omitempty" nullable:"false" doc:"How the game changed each team's competitor's Elo rating, one per team in colour order, for a rated game only: absent when the game isn't rated. The winner gains and every other team loses, each against the winner alone. Without a winner, the teams still in the game when it ended draw each other, and each team that was eliminated, timed out, resigned or left loses to each of them; when no team was left, every pair of teams counts as a draw."`
}

// DepartureView is a team that left a real-time game: it resigned, or the
// server didn't hear from its player for the game's turn limit.
type DepartureView struct {
	TeamColor  uint          `json:"team_color" minimum:"1"`
	Kind       DepartureKind `json:"kind" enum:"resigned,left" doc:"resigned: its player resigned. left: the server didn't hear from its player for the game's turn limit (or one turn plus 5 seconds, when that is longer) by the time a turn ended."`
	Players    []string      `json:"players" nullable:"false" doc:"The display name the player who resigned or went silent had at the start."`
	TurnNumber *int          `json:"turn_number" minimum:"1" nullable:"true" doc:"The turn number of the first generation in which the team is out: its castles turned neutral, keeping their soldiers, and its soldier groups were removed. Null only in an abandoned game's outcome, for a resignation that was still waiting for its turn to play."`
}

const (
	GenerationEventSchemaVersion = 1
	GenerationEventType          = "game.next_generation"
	GameEndedEventType           = "game.ended"
)

// GenerationEvent is published on the player's personal channel after a turn
// that did not end the game.
type GenerationEvent struct {
	SchemaVersion int             `json:"schema_version" enum:"1"`
	Type          string          `json:"type" enum:"game.next_generation"`
	State         GenerationState `json:"state"`
}

// GameEndedEvent replaces GenerationEvent for the turn that ends the game, and
// is the game's last event.
type GameEndedEvent struct {
	SchemaVersion int             `json:"schema_version" enum:"1"`
	Type          string          `json:"type" enum:"game.ended"`
	State         GenerationState `json:"state" doc:"The final generation, with its outcome."`
}

// API views are published without their View suffix, e.g. as Castle.
type GenerationView struct {
	ID            uint               `json:"id" minimum:"1" doc:"The server's ID of the generation. It increases with every turn but is not the turn number, which BotTurn.turn_number and GenerationState.turn_number give: it doesn't start at 1."`
	Castles       []CastleView       `json:"castles" nullable:"false"`
	SoldierGroups []SoldierGroupView `json:"soldier_groups" nullable:"false"`
	Clashes       []Clash            `json:"clashes" nullable:"false"`
}

type CastleView struct {
	ID        uint     `json:"id" doc:"Castle ID, stable for the whole game."`
	TeamColor uint     `json:"team_color" doc:"Owning team; 0 is neutral."`
	Soldiers  uint     `json:"soldiers"`
	X         float64  `json:"x"`
	Y         float64  `json:"y"`
	Upgrades  Upgrades `json:"upgrades" doc:"Every upgrade type's level. GET /game/upgrades lists the levels and what the next one costs."`
	BurstDebt uint     `json:"burst_debt" doc:"Soldiers the castle still owes for its last burst. A burst arrives in the generation right after the turn that orders it, on top of that generation's production, and becomes the debt. Each later generation's production repays the debt first, and only what is left over grows the castle. A castle with debt can't burst. A capture clears it."`
}

type SoldierGroupView struct {
	ID                  uint     `json:"id" doc:"Soldier group ID, never reassigned within a game."`
	TeamColor           uint     `json:"team_color" doc:"Owning team; 0 is neutral."`
	Soldiers            uint     `json:"soldiers"`
	X                   float64  `json:"x"`
	Y                   float64  `json:"y"`
	OriginCastleID      uint     `json:"origin_castle_id"`
	DestinationCastleID uint     `json:"destination_castle_id"`
	ClashRadius         float64  `json:"clash_radius" exclusiveMinimum:"0" doc:"Enemies clash when their centers are within the larger of their radii."`
	Upgrades            Upgrades `json:"upgrades" doc:"Every upgrade type's level, copied from the origin castle when the group left."`
}
