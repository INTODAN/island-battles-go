package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"islandBattles/backend/models"
	"islandBattles/backend/simulation"

	"github.com/jackc/pgx/v5/pgtype"
	"gorm.io/gorm"
)

// A headless match is a whole game played in memory: no database, no lobby,
// no Centrifugo and no HTTP. Every seat is a HeadlessPlayer, which is given
// the same BotTurn a bot gets from the server and answers a BotTurnResult.
// The answer is checked with the rules of a completing POST
// /generation/commands batch, the turn is played by simulation.Simulate, and
// the game ends by simulation.Outcome, so a bot meets the same rules as in a
// lobby's game. It is what backend/cmd/headless-match runs, for playing bots
// against each other quickly.
//
// It differs from a lobby's game in three ways. Turns have no deadline: a
// player that fails, or takes longer than HeadlessMatch.TurnTimeout, passes
// the turn, as a default bot that fails does, and the result counts it,
// rather than its team losing by turn_timeout. The teams answer in colour
// order. And nobody leaves: there are no departures and no real-time turns.

// HeadlessPlayer plays one team's turns of a headless match. A player that
// returns an error, or a result that the rules reject, passes the turn. When
// ctx is done the turn's time is up: Play should return, and its answer is
// not used.
type HeadlessPlayer interface {
	Play(ctx context.Context, turn models.BotTurn) (models.BotTurnResult, error)
}

// HeadlessSeat is one team of a headless match: the name the demo and the
// result show for it, and who plays it.
type HeadlessSeat struct {
	Name   string
	Player HeadlessPlayer
}

// HeadlessMatch describes a headless match. Seats[index] plays team colour
// index+1, so Map needs exactly len(Seats) teams.
type HeadlessMatch struct {
	GameID uint
	Map    models.Map
	Seats  []HeadlessSeat
	// MaxGenerations is the game's last generation; 0 means
	// models.DefaultMaxGenerations.
	MaxGenerations int
	// TurnTimeout is how long a player may take for one turn before it passes
	// the turn. 0 means no limit.
	TurnTimeout time.Duration
}

// HeadlessSeatResult is how one seat's game went.
type HeadlessSeatResult struct {
	Name      string `json:"name"`
	TeamColor uint   `json:"team_color"`
	// Castles and Soldiers are the team's on the final board; Soldiers counts
	// those in its castles and in its groups.
	Castles  int  `json:"castles"`
	Soldiers uint `json:"soldiers"`
	// EliminatedAtTurn is the number of the first generation without the
	// team, or 0 for a team that was in the game to the end.
	EliminatedAtTurn int `json:"eliminated_at_turn"`
	TurnsPlayed      int `json:"turns_played"`
	Commands         int `json:"commands"`
	// FailedTurns counts the turns the seat passed because its player failed,
	// was too slow or answered something the rules reject. Failures holds the
	// first MaxHeadlessFailures of them.
	FailedTurns int                   `json:"failed_turns"`
	Failures    []HeadlessTurnFailure `json:"failures"`
	// TotalTurnMilliseconds and SlowestTurnMilliseconds measure the player's
	// Play calls.
	TotalTurnMilliseconds   float64 `json:"total_turn_milliseconds"`
	SlowestTurnMilliseconds float64 `json:"slowest_turn_milliseconds"`
	SlowestTurn             int     `json:"slowest_turn"`
}

// HeadlessTurnFailure is one turn that a seat passed, and why.
type HeadlessTurnFailure struct {
	Turn    int    `json:"turn"`
	Message string `json:"message"`
}

// MaxHeadlessFailures is how many of a seat's failed turns a result lists.
const MaxHeadlessFailures = 5

// HeadlessResult is a played headless match. Demo is the game as a demo, the
// format that Save demo writes and the demo viewer opens, with every team's
// game log.
type HeadlessResult struct {
	Outcome     models.GameOutcomeView `json:"outcome"`
	Generations int                    `json:"generations"`
	Seats       []HeadlessSeatResult   `json:"seats"`
	Demo        models.Demo            `json:"-"`
}

// headlessFirstGenerationID is the ID of a headless match's first generation.
// The server's generation IDs are shared by every game, so a game's first one
// is never 1; here it isn't either, so that a bot which reads generation.id
// as the turn number plays as badly as it would on the server.
const headlessFirstGenerationID = 5538

// generationFromState is the stored generation of a board, turn number in its
// game, as the bots and the command checks see it.
func generationFromState(id uint, number int, state simulation.GameState) *models.Generation {
	generation := &models.Generation{Model: gorm.Model{ID: id}, Number: number}
	castles := map[uint]models.Castle{}
	for _, castle := range state.SortedCastles() {
		stored := models.Castle{CastleFields: models.CastleFields{TeamColor: castle.TeamColor, Position: castle.Position, PersistentCastleID: castle.ID, Upgrades: castle.Upgrades.Clone()},
			SoldierAmount: castle.Soldiers, BurstDebt: castle.BurstDebt}
		castles[castle.ID] = stored
		generation.Map.Castles = append(generation.Map.Castles, stored)
	}
	for _, group := range state.SortedSoldierGroups() {
		generation.SoldierGroups = append(generation.SoldierGroups, models.SoldierGroup{
			SoldierGroupFields: models.SoldierGroupFields{SoldierAmount: group.Soldiers}, PersistentSoldierGroupID: group.ID, TeamColor: group.TeamColor,
			OriginCastle: castles[group.OriginCastleID], DestinationCastle: castles[group.DestinationCastleID],
			MovementPauseGenerations: group.MovementPauseGenerations, ClashRadius: group.ClashRadius, Position: group.Position, Upgrades: group.Upgrades.Clone(),
		})
	}
	return generation
}

// stateFromMap is the board of a game's first generation on gameMap.
func stateFromMap(gameMap models.Map) simulation.GameState {
	state := simulation.GameState{Map: gameMap.MapFields, Castles: map[uint]*simulation.Castle{}, SoldierGroups: map[uint]*simulation.SoldierGroup{}}
	for _, castle := range gameMap.Castles {
		state.Castles[castle.PersistentCastleID] = &simulation.Castle{ID: castle.PersistentCastleID, TeamColor: castle.TeamColor, Soldiers: castle.SoldierAmount,
			Position: pgtype.Point{P: castle.Position.P, Valid: true}, Upgrades: castle.Upgrades.Clone()}
	}
	return state
}

// PlayHeadlessMatch plays match to its outcome. It returns an error only for
// a match that can't be played, such as one whose seats don't fit its map, or
// when ctx is done; a player's failures are in the result.
func PlayHeadlessMatch(ctx context.Context, match HeadlessMatch) (HeadlessResult, error) {
	if len(match.Seats) < 2 {
		return HeadlessResult{}, errors.New("a headless match needs at least two seats")
	}
	if uint(len(match.Seats)) != match.Map.PlayerAmount {
		return HeadlessResult{}, fmt.Errorf("the map %q is for %d teams, and the match has %d seats", match.Map.MapName, match.Map.PlayerAmount, len(match.Seats))
	}
	maxGenerations := match.MaxGenerations
	if maxGenerations == 0 {
		maxGenerations = models.DefaultMaxGenerations
	}
	if maxGenerations < 2 {
		return HeadlessResult{}, fmt.Errorf("max generations must be at least 2, not %d", maxGenerations)
	}
	gameID := max(match.GameID, 1)
	game := &models.Game{Model: gorm.Model{ID: gameID}, MaxGenerations: maxGenerations}
	seats := make([]HeadlessSeatResult, len(match.Seats))
	for index, seat := range match.Seats {
		color := uint(index + 1)
		game.Teams = append(game.Teams, models.GameTeam{Color: color, Name: seat.Name, Players: []string{seat.Name}})
		seats[index] = HeadlessSeatResult{Name: seat.Name, TeamColor: color, Failures: []HeadlessTurnFailure{}}
	}
	demo := models.Demo{Format: models.DemoFormat, SchemaVersion: models.DemoSchemaVersion, GameID: gameID, Teams: game.Teams}
	state := stateFromMap(match.Map)
	memories := make([]models.BotMemory, len(match.Seats))
	var clashes []models.Clash
	for number := 1; ; number++ {
		generation := generationFromState(uint(headlessFirstGenerationID+number-1), number, state)
		generation.Clashes = clashes
		demo.Frames = append(demo.Frames, GenerationView(generation))
		logs := models.GenerationLogs{GenerationID: generation.ID, Players: []models.PlayerGenerationLogs{}, HiddenTeamColors: []uint{}}
		standing := standingTeams(generation, game.Teams)
		for index := range seats {
			if seats[index].EliminatedAtTurn == 0 && !slices.Contains(standing, seats[index].TeamColor) {
				seats[index].EliminatedAtTurn = number
			}
		}
		if game.Outcome != nil {
			demo.Logs = append(demo.Logs, logs)
			break
		}
		inputs := []models.GenerationInput{}
		batches := []simulation.TeamBatch{}
		for index, seat := range match.Seats {
			color := uint(index + 1)
			if !slices.Contains(standing, color) {
				continue
			}
			if err := ctx.Err(); err != nil {
				return HeadlessResult{}, err
			}
			turn := models.BotTurn{GameID: gameID, TurnNumber: number, MaxGenerations: maxGenerations, TeamColor: color, Upgrades: models.UpgradeTable(), Memory: memories[index]}
			started := time.Now()
			played, err := playHeadlessTurn(ctx, seat.Player, match.TurnTimeout, turn, generation, inputs)
			milliseconds := float64(time.Since(started).Microseconds()) / 1000
			result := &seats[index]
			result.TurnsPlayed++
			result.TotalTurnMilliseconds += milliseconds
			if milliseconds > result.SlowestTurnMilliseconds {
				result.SlowestTurnMilliseconds, result.SlowestTurn = milliseconds, number
			}
			if err != nil {
				if ctx.Err() != nil {
					return HeadlessResult{}, ctx.Err()
				}
				result.FailedTurns++
				if len(result.Failures) < MaxHeadlessFailures {
					result.Failures = append(result.Failures, HeadlessTurnFailure{Turn: number, Message: err.Error()})
				}
				played = goBotTurn{memory: memories[index], logs: []models.GenerationLogLine{{Level: models.GenerationLogError, Logger: GoBotLogger,
					Message: truncateLogMessage(fmt.Sprintf("%s failed this turn, so it passes: %v", seat.Name, err))}}}
			}
			memories[index] = played.memory
			result.Commands += len(played.commands)
			rawCommands, err := json.Marshal(append([]models.CommandFields{}, played.commands...))
			if err != nil {
				return HeadlessResult{}, err
			}
			inputs = append(inputs, models.GenerationInput{GenerationID: generation.ID, PlayerID: fmt.Sprint(index), Commands: rawCommands, Completed: true})
			batches = append(batches, simulation.TeamBatch{Color: color, Commands: played.commands})
			logs.Players = append(logs.Players, models.PlayerGenerationLogs{DisplayName: seat.Name, TeamColor: color, Lines: append([]models.GenerationLogLine{}, played.logs...)})
		}
		demo.Logs = append(demo.Logs, logs)
		turnCommands, err := simulation.TurnCommands(nil, batches)
		if err != nil {
			return HeadlessResult{}, err
		}
		decoded, err := simulation.DecodeCommands(append(turnCommands, simulation.EndOfTurnCommands()...))
		if err != nil {
			return HeadlessResult{}, err
		}
		var events []simulation.Event
		if state, events, err = simulation.Simulate(state, decoded); err != nil {
			return HeadlessResult{}, fmt.Errorf("turn %d: %w", number, err)
		}
		clashes = simulation.Clashes(events)
		if outcome := simulation.Outcome(state, game.Teams, number+1 >= maxGenerations); outcome != nil {
			outcome.FinalTurn = number + 1
			game.Outcome = outcome
		}
	}
	castles := state.CastleCounts()
	for index := range seats {
		color := seats[index].TeamColor
		seats[index].Castles = castles[color]
		for _, castle := range state.Castles {
			if castle.TeamColor == color {
				seats[index].Soldiers += castle.Soldiers
			}
		}
		for _, group := range state.SoldierGroups {
			if group.TeamColor == color {
				seats[index].Soldiers += group.Soldiers
			}
		}
	}
	demo.Outcome = outcomeView(game)
	demo.SavedAt = time.Now().UTC().Truncate(time.Second)
	return HeadlessResult{Outcome: *demo.Outcome, Generations: len(demo.Frames), Seats: seats, Demo: demo}, nil
}

// playHeadlessTurn has player play turn on generation, within timeout when it
// isn't 0, and checks the answer as runGoBot checks a default bot's: against
// the commands that the teams before it stored this turn, inputs.
func playHeadlessTurn(ctx context.Context, player HeadlessPlayer, timeout time.Duration, turn models.BotTurn, generation *models.Generation, inputs []models.GenerationInput) (played goBotTurn, err error) {
	if turn.Generation, err = goBotGeneration(generation, turn.TeamColor, inputs); err != nil {
		return goBotTurn{}, err
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	type answer struct {
		result models.BotTurnResult
		err    error
	}
	answers := make(chan answer, 1)
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				answers <- answer{err: fmt.Errorf("the bot panicked: %v", recovered)}
			}
		}()
		result, err := player.Play(ctx, turn)
		answers <- answer{result: result, err: err}
	}()
	var result models.BotTurnResult
	select {
	case <-ctx.Done():
		if timeout > 0 && errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return goBotTurn{}, fmt.Errorf("the bot didn't answer within %s", timeout)
		}
		return goBotTurn{}, ctx.Err()
	case answered := <-answers:
		if answered.err != nil {
			return goBotTurn{}, answered.err
		}
		result = answered.result
	}
	if result.Commands == nil || len(result.Commands) > 1000 {
		return goBotTurn{}, errors.New("commands must be an array of at most 1000 commands")
	}
	if err := validateLogLines(result.Logs); err != nil {
		return goBotTurn{}, err
	}
	if played.memory, err = validateBotMemory(result.Memory); err != nil {
		return goBotTurn{}, err
	}
	if played.commands, err = validateCommands(generation, turn.TeamColor, inputs, result.Commands); err != nil {
		return goBotTurn{}, err
	}
	played.logs = result.Logs
	return played, nil
}
