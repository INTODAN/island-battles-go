package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"islandBattles/backend/dal"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
	"islandBattles/lobby/adminmcp"
	lobbypostgres "islandBattles/lobby/postgres"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// The game tables' columns that the tools filter, group and order by.
var (
	idColumn        = clause.Column{Name: "id"}
	gameIDColumn    = clause.Column{Name: "game_id"}
	lobbyIDColumn   = clause.Column{Name: "lobby_id"}
	outcomeColumn   = clause.Column{Name: "outcome"}
	createdAtColumn = clause.Column{Name: "created_at"}
)

// GamesOverview is the Island Battles part of the overview tool's answer.
type GamesOverview struct {
	RunningCount               int64  `json:"running_count" jsonschema:"Games without an outcome, including abandoned ones that retention hasn't deleted yet."`
	FinishedCount              int64  `json:"finished_count"`
	GenerationsLastHourCount   int64  `json:"generations_last_hour_count" jsonschema:"Generations created in the last hour, one per turn played."`
	GenerationsLastHourGameIDs []uint `json:"generations_last_hour_game_ids" jsonschema:"The games those generations belong to."`
}

// islandBattlesGames adds Island Battles games to the lobby tools.
type islandBattlesGames struct{}

var _ adminmcp.Games = islandBattlesGames{}

func (islandBattlesGames) Overview(transaction *gorm.DB, now time.Time) (any, error) {
	var overview GamesOverview
	if err := transaction.Model(&models.Game{}).Where(clause.Eq{Column: outcomeColumn, Value: nil}).Count(&overview.RunningCount).Error; err != nil {
		return nil, err
	}
	if err := transaction.Model(&models.Game{}).Where(clause.Neq{Column: outcomeColumn, Value: nil}).Count(&overview.FinishedCount).Error; err != nil {
		return nil, err
	}
	var generations []models.Generation
	if err := transaction.Select("GameID").Where(clause.Gte{Column: createdAtColumn, Value: now.Add(-time.Hour)}).
		Find(&generations).Error; err != nil {
		return nil, err
	}
	overview.GenerationsLastHourCount = int64(len(generations))
	overview.GenerationsLastHourGameIDs = []uint{}
	for _, generation := range generations {
		if !slices.Contains(overview.GenerationsLastHourGameIDs, generation.GameID) {
			overview.GenerationsLastHourGameIDs = append(overview.GenerationsLastHourGameIDs, generation.GameID)
		}
	}
	slices.Sort(overview.GenerationsLastHourGameIDs)
	return overview, nil
}

func (islandBattlesGames) GameIDs(transaction *gorm.DB, lobbyIDs []string) (map[string]any, error) {
	values := make([]any, len(lobbyIDs))
	for index, lobbyID := range lobbyIDs {
		values[index] = lobbyID
	}
	var games []models.Game
	// Ordered by round, so each lobby maps to its newest game.
	if err := transaction.Select("ID", "LobbyID").Where(clause.IN{Column: lobbyIDColumn, Values: values}).Order(clause.OrderByColumn{Column: clause.Column{Name: "lobby_round"}}).Find(&games).Error; err != nil {
		return nil, err
	}
	gameIDs := make(map[string]any, len(games))
	for _, game := range games {
		if game.LobbyID != nil {
			gameIDs[*game.LobbyID] = game.ID
		}
	}
	return gameIDs, nil
}

// ListGamesInput filters list_games.
type ListGamesInput struct {
	Status string `json:"status,omitempty" jsonschema:"Only running games, which have no outcome yet, or finished ones."`
	Limit  int    `json:"limit,omitempty" jsonschema:"The most games to return, 50 by default and at most 200."`
}

// GameTeam is a team that took part in a game, with the display names its
// players had at the start.
type GameTeam struct {
	Color   uint     `json:"color"`
	Name    string   `json:"name"`
	Players []string `json:"players"`
}

// RosterPlayer is a player on a game's frozen roster.
type RosterPlayer struct {
	PlayerID    string  `json:"player_id"`
	DisplayName *string `json:"display_name" jsonschema:"The player's name, or null once lobby retention deleted the player."`
	TeamColor   uint    `json:"team_color"`
}

// Outcome is how a finished game ended.
type Outcome struct {
	Reason          string  `json:"reason" jsonschema:"last_team_standing, all_eliminated (a draw), generation_cap or turn_timeout (a turn passed its deadline)."`
	WinnerTeamColor *uint   `json:"winner_team_color" jsonschema:"The winning team, or null for a draw."`
	WinnerTeamName  *string `json:"winner_team_name"`
}

// GameSummary is one game in list_games, and the head of get_game.
type GameSummary struct {
	ID                 uint           `json:"id"`
	LobbyID            *string        `json:"lobby_id"`
	Status             string         `json:"status" jsonschema:"running or finished."`
	CreatedAt          time.Time      `json:"created_at"`
	Teams              []GameTeam     `json:"teams"`
	Players            []RosterPlayer `json:"players"`
	GenerationCount    int64          `json:"generation_count" jsonschema:"Generations so far; the current one is turn generation_count."`
	LastGenerationAt   *time.Time     `json:"last_generation_at"`
	Outcome            *Outcome       `json:"outcome"`
	RetentionDeletesAt time.Time      `json:"retention_deletes_at" jsonschema:"When retention deletes the game: ISLAND_BATTLES_GAME_RETENTION_PERIOD, 7 days by default, after its latest generation, unless another turn is played first."`
}

// GameList is list_games' answer.
type GameList struct {
	Games     []GameSummary `json:"games"`
	Truncated bool          `json:"truncated" jsonschema:"Whether the limit left games out."`
}

// GetGameInput names one game.
type GetGameInput struct {
	GameID  uint   `json:"game_id,omitempty" jsonschema:"The game's ID."`
	LobbyID string `json:"lobby_id,omitempty" jsonschema:"The lobby the game was started from, instead of game_id."`
}

// PlayerTurn says whether a roster player has completed the current turn.
type PlayerTurn struct {
	RosterPlayer
	Completed bool `json:"completed"`
}

// TeamState is a team's forces in a generation. Color 0 is neutral.
type TeamState struct {
	Color             uint    `json:"color"`
	Name              *string `json:"name"`
	Castles           int     `json:"castles"`
	SoldiersInCastles uint    `json:"soldiers_in_castles"`
	SoldierGroups     int     `json:"soldier_groups"`
	SoldiersInGroups  uint    `json:"soldiers_in_groups"`
}

// CurrentGeneration is a game's latest generation, whose turn is being played
// unless the game is finished.
type CurrentGeneration struct {
	ID         uint         `json:"id"`
	Turn       int64        `json:"turn"`
	CreatedAt  time.Time    `json:"created_at"`
	TurnStatus []PlayerTurn `json:"turn_status" jsonschema:"Which roster players have completed this generation's turn. The turn ends when all have."`
	Teams      []TeamState  `json:"teams"`
}

// GameDetail is get_game's answer.
type GameDetail struct {
	GameSummary
	CurrentGeneration *CurrentGeneration `json:"current_generation" jsonschema:"Null before the game's first generation."`
}

// GetGenerationInput names one generation of a game.
type GetGenerationInput struct {
	GameID       uint `json:"game_id" jsonschema:"The game's ID."`
	GenerationID uint `json:"generation_id,omitempty" jsonschema:"The generation's ID."`
	Turn         int  `json:"turn,omitempty" jsonschema:"The generation's turn, from 1, instead of generation_id."`
	IncludeBoard bool `json:"include_board,omitempty" jsonschema:"Also return the whole board, as players receive it."`
}

// PlayerCommands is what one roster player submitted for a generation's turn.
type PlayerCommands struct {
	RosterPlayer
	Completed bool       `json:"completed"`
	Commands  any        `json:"commands" jsonschema:"The commands the player submitted, as the player API takes them."`
	UpdatedAt *time.Time `json:"updated_at"`
}

// AppliedCommand is one of the commands that produced a generation from the
// one before it.
type AppliedCommand struct {
	Type       string `json:"type"`
	Parameters any    `json:"parameters"`
}

// GenerationDetail is get_generation's answer.
type GenerationDetail struct {
	GameID          uint                   `json:"game_id"`
	ID              uint                   `json:"id"`
	Turn            int64                  `json:"turn"`
	CreatedAt       time.Time              `json:"created_at"`
	Current         bool                   `json:"current" jsonschema:"Whether this is the game's latest generation."`
	PlayerCommands  []PlayerCommands       `json:"player_commands" jsonschema:"Each roster player's commands for this generation's turn, and whether they completed it."`
	AppliedCommands []AppliedCommand       `json:"applied_commands" jsonschema:"The commands that produced this generation from the previous one, including the turn's own end-of-turn commands."`
	Clashes         []models.Clash         `json:"clashes" jsonschema:"The clashes of the turn that produced this generation."`
	Board           *models.GenerationView `json:"board,omitempty" jsonschema:"The whole board, the GenerationView that players receive, when include_board is set."`
}

// addGameTools registers list_games, get_game and get_generation.
func addGameTools(server *mcp.Server, database *gorm.DB, gameRetentionPeriod time.Duration) {
	reader := gameReader{retentionPeriod: gameRetentionPeriod}
	mcp.AddTool(server, &mcp.Tool{
		Name: "list_games",
		Description: "Lists Island Battles games, newest first, optionally only running or finished ones. For each: ID, lobby ID, teams with their players, " +
			"the frozen roster of player IDs, generation count, the latest generation's time, the outcome, and when retention deletes the game. " +
			"Use get_game for a game's current turn and forces.",
		Annotations: adminmcp.ReadOnlyAnnotations("List games"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListGamesInput) (*mcp.CallToolResult, GameList, error) {
		var list GameList
		err := adminmcp.ReadOnly(ctx, database, func(transaction *gorm.DB) error {
			var err error
			list, err = reader.listGames(transaction, input)
			return err
		})
		return nil, list, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_game",
		Description: "Describes one Island Battles game, found by game_id or by the lobby_id it was started from (the lobby's newest game, since a lobby plays one game per round). Returns everything list_games does, " +
			"plus the current generation: its turn, which roster players have completed it, and each team's castles, soldiers in castles, " +
			"soldier groups and soldiers in groups. Use get_generation for commands and clashes.",
		Annotations: adminmcp.ReadOnlyAnnotations("Get game"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetGameInput) (*mcp.CallToolResult, GameDetail, error) {
		var detail GameDetail
		err := adminmcp.ReadOnly(ctx, database, func(transaction *gorm.DB) error {
			var err error
			detail, err = reader.getGame(transaction, input)
			return err
		})
		return nil, detail, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "get_generation",
		Description: "Describes one generation of an Island Battles game, by generation_id or by turn (from 1): each roster player's commands for its turn and whether they completed it, " +
			"the commands that produced it, and its clashes. With include_board, also the whole board that players receive, which can be large.",
		Annotations: adminmcp.ReadOnlyAnnotations("Get generation"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetGenerationInput) (*mcp.CallToolResult, GenerationDetail, error) {
		var detail GenerationDetail
		err := adminmcp.ReadOnly(ctx, database, func(transaction *gorm.DB) error {
			var err error
			detail, err = reader.getGeneration(transaction, input)
			return err
		})
		return nil, detail, err
	})
}

// gameReader answers the game tools from the game tables.
type gameReader struct {
	// retentionPeriod is how long the backend keeps a game after its latest
	// generation, ISLAND_BATTLES_GAME_RETENTION_PERIOD.
	retentionPeriod time.Duration
}

// summaryFields are the game columns a summary reads.
var summaryFields = []string{"ID", "CreatedAt", "LobbyID", "Teams", "Outcome"}

func (reader gameReader) listGames(transaction *gorm.DB, input ListGamesInput) (GameList, error) {
	limit := input.Limit
	switch {
	case limit == 0:
		limit = adminmcp.DefaultListLimit
	case limit < 0 || limit > adminmcp.MaxListLimit:
		return GameList{}, fmt.Errorf("limit must be from 1 to %d", adminmcp.MaxListLimit)
	}
	query := transaction.Select(summaryFields)
	switch input.Status {
	case "":
	case "running":
		query = query.Where(clause.Eq{Column: outcomeColumn, Value: nil})
	case "finished":
		query = query.Where(clause.Neq{Column: outcomeColumn, Value: nil})
	default:
		return GameList{}, errors.New("status must be running or finished")
	}
	var games []models.Game
	// One more than the limit says whether the limit left games out.
	if err := query.Order(clause.OrderByColumn{Column: idColumn, Desc: true}).Limit(limit + 1).Find(&games).Error; err != nil {
		return GameList{}, fmt.Errorf("read games: %w", err)
	}
	list := GameList{Truncated: len(games) > limit}
	summaries, err := reader.summarizeGames(transaction, games[:min(limit, len(games))])
	if err != nil {
		return GameList{}, err
	}
	list.Games = summaries
	return list, nil
}

// generationStats counts one game's generations.
type generationStats struct {
	GameID           uint
	GenerationCount  int64
	LastGenerationAt time.Time
}

// summarizeGames summarizes games, in their order, with a fixed number of
// queries.
func (reader gameReader) summarizeGames(transaction *gorm.DB, games []models.Game) ([]GameSummary, error) {
	gameIDs := make([]any, len(games))
	for index, game := range games {
		gameIDs[index] = game.ID
	}
	inGames := clause.IN{Column: gameIDColumn, Values: gameIDs}
	var stats []generationStats
	if err := transaction.Model(&models.Generation{}).Select("game_id, count(*) AS generation_count, max(created_at) AS last_generation_at").
		Where(inGames).Clauses(clause.GroupBy{Columns: []clause.Column{gameIDColumn}}).Scan(&stats).Error; err != nil {
		return nil, fmt.Errorf("count generations: %w", err)
	}
	statsByGame := make(map[uint]generationStats, len(stats))
	for _, stat := range stats {
		statsByGame[stat.GameID] = stat
	}
	rosters, err := loadRosters(transaction, inGames)
	if err != nil {
		return nil, err
	}
	summaries := make([]GameSummary, 0, len(games))
	for _, game := range games {
		summary := GameSummary{
			ID: game.ID, LobbyID: game.LobbyID, Status: "running", CreatedAt: game.CreatedAt.UTC(),
			Teams: make([]GameTeam, 0, len(game.Teams)), Players: rosters[game.ID],
		}
		if summary.Players == nil {
			summary.Players = []RosterPlayer{}
		}
		for _, team := range game.Teams {
			summary.Teams = append(summary.Teams, GameTeam{Color: team.Color, Name: team.Name, Players: append([]string{}, team.Players...)})
		}
		lastActivity := game.CreatedAt
		if stat, found := statsByGame[game.ID]; found {
			summary.GenerationCount = stat.GenerationCount
			lastGenerationAt := stat.LastGenerationAt.UTC()
			summary.LastGenerationAt = &lastGenerationAt
			lastActivity = stat.LastGenerationAt
		}
		summary.RetentionDeletesAt = lastActivity.Add(reader.retentionPeriod).UTC()
		if game.Outcome != nil {
			summary.Status = "finished"
			summary.Outcome = &Outcome{Reason: string(game.Outcome.Reason), WinnerTeamColor: game.Outcome.WinnerTeamColor}
			if game.Outcome.WinnerTeamColor != nil {
				summary.Outcome.WinnerTeamName = teamName(game.Teams, *game.Outcome.WinnerTeamColor)
			}
		}
		summaries = append(summaries, summary)
	}
	return summaries, nil
}

// loadRosters returns the frozen rosters of the games inGames matches, with
// the players' display names, which the lobby keeps until its retention
// deletes the players.
func loadRosters(transaction *gorm.DB, inGames clause.Expression) (map[uint][]RosterPlayer, error) {
	var players []models.FrozenGamePlayer
	if err := transaction.Where(inGames).Order(clause.OrderByColumn{Column: clause.Column{Name: "team_color"}}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: "player_id"}}).Find(&players).Error; err != nil {
		return nil, fmt.Errorf("read rosters: %w", err)
	}
	playerIDs := make([]any, len(players))
	for index, player := range players {
		playerIDs[index] = player.PlayerID
	}
	var records []lobbypostgres.PlayerRecord
	if err := transaction.Select("ID", "DisplayName").Where(clause.IN{Column: idColumn, Values: playerIDs}).Find(&records).Error; err != nil {
		return nil, fmt.Errorf("read players: %w", err)
	}
	names := make(map[string]string, len(records))
	for _, record := range records {
		names[record.ID] = record.DisplayName
	}
	rosters := map[uint][]RosterPlayer{}
	for _, player := range players {
		rosterPlayer := RosterPlayer{PlayerID: player.PlayerID, TeamColor: player.TeamColor}
		if name, found := names[player.PlayerID]; found {
			rosterPlayer.DisplayName = &name
		}
		rosters[player.GameID] = append(rosters[player.GameID], rosterPlayer)
	}
	return rosters, nil
}

func teamName(teams []models.GameTeam, color uint) *string {
	for _, team := range teams {
		if team.Color == color {
			name := team.Name
			return &name
		}
	}
	return nil
}

// findGame finds the game that a game ID names, or a lobby's newest game. A game that
// retention has retired is found no more.
func findGame(transaction *gorm.DB, gameID uint, lobbyID string) (models.Game, error) {
	var games []models.Game
	switch {
	case gameID != 0 && lobbyID == "":
		if err := transaction.Select(summaryFields).Where(&models.Game{Model: gorm.Model{ID: gameID}}).Limit(1).Find(&games).Error; err != nil {
			return models.Game{}, fmt.Errorf("read the game: %w", err)
		}
	case lobbyID != "" && gameID == 0:
		if err := uuid.Validate(lobbyID); err != nil {
			return models.Game{}, errors.New("lobby_id must be a UUID")
		}
		if err := transaction.Select(summaryFields).Where(&models.Game{LobbyID: &lobbyID}).Order(clause.OrderByColumn{Column: clause.Column{Name: "lobby_round"}, Desc: true}).Limit(1).Find(&games).Error; err != nil {
			return models.Game{}, fmt.Errorf("read the game: %w", err)
		}
	default:
		return models.Game{}, errors.New("pass either game_id or lobby_id")
	}
	if len(games) == 0 {
		return models.Game{}, errors.New("no such game; retention may have deleted it")
	}
	return games[0], nil
}

func (reader gameReader) getGame(transaction *gorm.DB, input GetGameInput) (GameDetail, error) {
	game, err := findGame(transaction, input.GameID, input.LobbyID)
	if err != nil {
		return GameDetail{}, err
	}
	summaries, err := reader.summarizeGames(transaction, []models.Game{game})
	if err != nil {
		return GameDetail{}, err
	}
	detail := GameDetail{GameSummary: summaries[0]}
	var generations []models.Generation
	if err := transaction.Select("ID", "MapID", "CreatedAt").Where(&models.Generation{GameID: game.ID}).
		Order(clause.OrderByColumn{Column: idColumn, Desc: true}).Limit(1).Find(&generations).Error; err != nil {
		return GameDetail{}, fmt.Errorf("read the current generation: %w", err)
	}
	if len(generations) == 0 {
		return detail, nil
	}
	generation := generations[0]
	current := CurrentGeneration{
		ID: generation.ID, Turn: detail.GenerationCount, CreatedAt: generation.CreatedAt.UTC(),
		TurnStatus: make([]PlayerTurn, 0, len(detail.Players)), Teams: []TeamState{},
	}
	var inputs []models.GenerationInput
	if err := transaction.Select("PlayerID", "Completed").Where(&models.GenerationInput{GenerationID: generation.ID}).Find(&inputs).Error; err != nil {
		return GameDetail{}, fmt.Errorf("read the turn's commands: %w", err)
	}
	for _, player := range detail.Players {
		turn := PlayerTurn{RosterPlayer: player}
		for _, input := range inputs {
			if input.PlayerID == player.PlayerID {
				turn.Completed = input.Completed
			}
		}
		current.TurnStatus = append(current.TurnStatus, turn)
	}
	var castles []models.Castle
	if err := transaction.Select("TeamColor", "SoldierAmount").Where(&models.Castle{MapID: generation.MapID}).Find(&castles).Error; err != nil {
		return GameDetail{}, fmt.Errorf("read castles: %w", err)
	}
	var groups []models.SoldierGroup
	if err := transaction.Select("TeamColor", "SoldierAmount").Where(&models.SoldierGroup{GenerationID: generation.ID}).Find(&groups).Error; err != nil {
		return GameDetail{}, fmt.Errorf("read soldier groups: %w", err)
	}
	teams := map[uint]*TeamState{}
	team := func(color uint) *TeamState {
		if teams[color] == nil {
			teams[color] = &TeamState{Color: color, Name: teamName(game.Teams, color)}
		}
		return teams[color]
	}
	for _, gameTeam := range game.Teams {
		team(gameTeam.Color)
	}
	for _, castle := range castles {
		state := team(castle.TeamColor)
		state.Castles++
		state.SoldiersInCastles += castle.SoldierAmount
	}
	for _, group := range groups {
		state := team(group.TeamColor)
		state.SoldierGroups++
		state.SoldiersInGroups += group.SoldierAmount
	}
	for _, state := range teams {
		current.Teams = append(current.Teams, *state)
	}
	slices.SortFunc(current.Teams, func(left, right TeamState) int { return int(left.Color) - int(right.Color) })
	detail.CurrentGeneration = &current
	return detail, nil
}

func (reader gameReader) getGeneration(transaction *gorm.DB, input GetGenerationInput) (GenerationDetail, error) {
	if input.GameID == 0 {
		return GenerationDetail{}, errors.New("game_id is required")
	}
	game, err := findGame(transaction, input.GameID, "")
	if err != nil {
		return GenerationDetail{}, err
	}
	inGame := &models.Generation{GameID: game.ID}
	var total int64
	if err := transaction.Model(&models.Generation{}).Where(inGame).Count(&total).Error; err != nil {
		return GenerationDetail{}, fmt.Errorf("count generations: %w", err)
	}
	var generations []models.Generation
	var turn int64
	generationFields := []string{"ID", "GameID", "MapID", "CreatedAt", "Clashes"}
	switch {
	case input.GenerationID != 0 && input.Turn == 0:
		if err := transaction.Select(generationFields).Where(&models.Generation{Model: gorm.Model{ID: input.GenerationID}, GameID: game.ID}).
			Limit(1).Find(&generations).Error; err != nil {
			return GenerationDetail{}, fmt.Errorf("read the generation: %w", err)
		}
		if len(generations) > 0 {
			if err := transaction.Model(&models.Generation{}).Where(inGame).
				Where(clause.Lte{Column: idColumn, Value: input.GenerationID}).Count(&turn).Error; err != nil {
				return GenerationDetail{}, fmt.Errorf("count generations: %w", err)
			}
		}
	case input.Turn > 0 && input.GenerationID == 0:
		if err := transaction.Select(generationFields).Where(inGame).Order(clause.OrderByColumn{Column: idColumn}).
			Offset(input.Turn - 1).Limit(1).Find(&generations).Error; err != nil {
			return GenerationDetail{}, fmt.Errorf("read the generation: %w", err)
		}
		turn = int64(input.Turn)
	default:
		return GenerationDetail{}, errors.New("pass either generation_id or a turn from 1")
	}
	if len(generations) == 0 {
		return GenerationDetail{}, fmt.Errorf("game %d has no such generation; it has %d", game.ID, total)
	}
	generation := generations[0]
	detail := GenerationDetail{
		GameID: game.ID, ID: generation.ID, Turn: turn, CreatedAt: generation.CreatedAt.UTC(), Current: turn == total,
		Clashes: append([]models.Clash{}, generation.Clashes...),
	}
	rosters, err := loadRosters(transaction, clause.Eq{Column: gameIDColumn, Value: game.ID})
	if err != nil {
		return GenerationDetail{}, err
	}
	inputs, err := dal.LoadGenerationInputs(transaction, generation.ID)
	if err != nil {
		return GenerationDetail{}, fmt.Errorf("read the turn's commands: %w", err)
	}
	detail.PlayerCommands = make([]PlayerCommands, 0, len(rosters[game.ID]))
	for _, player := range rosters[game.ID] {
		submitted := PlayerCommands{RosterPlayer: player, Commands: []any{}}
		for _, playerInput := range inputs {
			if playerInput.PlayerID != player.PlayerID {
				continue
			}
			submitted.Completed = playerInput.Completed
			updatedAt := playerInput.UpdatedAt.UTC()
			submitted.UpdatedAt = &updatedAt
			if err := json.Unmarshal(playerInput.Commands, &submitted.Commands); err != nil {
				return GenerationDetail{}, fmt.Errorf("read %s's commands: %w", player.PlayerID, err)
			}
		}
		detail.PlayerCommands = append(detail.PlayerCommands, submitted)
	}
	var commands []models.Command
	if err := transaction.Select("CommandType", "Parameters").Where(&models.Command{GenerationID: generation.ID}).
		Order(clause.OrderByColumn{Column: idColumn}).Find(&commands).Error; err != nil {
		return GenerationDetail{}, fmt.Errorf("read the applied commands: %w", err)
	}
	detail.AppliedCommands = make([]AppliedCommand, 0, len(commands))
	for _, command := range commands {
		applied := AppliedCommand{Type: string(command.CommandType)}
		if len(command.Parameters) > 0 {
			if err := json.Unmarshal(command.Parameters, &applied.Parameters); err != nil {
				return GenerationDetail{}, fmt.Errorf("read an applied command: %w", err)
			}
		}
		detail.AppliedCommands = append(detail.AppliedCommands, applied)
	}
	if input.IncludeBoard {
		var full []models.Generation
		if err := transaction.Scopes(dal.WithFullGeneration).Where(&models.Generation{Model: gorm.Model{ID: generation.ID}}).
			Limit(1).Find(&full).Error; err != nil {
			return GenerationDetail{}, fmt.Errorf("read the board: %w", err)
		}
		if len(full) == 0 {
			return GenerationDetail{}, errors.New("the generation disappeared while reading it")
		}
		board := services.GenerationView(&full[0])
		detail.Board = &board
	}
	return detail, nil
}
