package services_test

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math/rand"
	"strings"
	"testing"

	"github.com/INTODAN/mpl/domain"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
)

// modelCastle is one castle in an independent model of the command rules.
type modelCastle struct {
	teamColor  uint
	unreserved uint
	levels     map[models.UpgradeType]int
}

type modelRejection struct {
	status      int
	problemType string
}

// missingCastleID is never on the board.
const missingCastleID = 99

// Random batches of sends and upgrades, from two players on two teams, must
// be accepted or rejected exactly as an independent model of the rules says,
// and every turn of accepted batches must apply, retries included.
func TestRandomTurnsFollowTheRulesAndAlwaysApply(t *testing.T) {
	for seed := int64(1); seed <= 3; seed++ {
		t.Run(fmt.Sprint(seed), func(t *testing.T) { playRandomTurns(t, seed) })
	}
}

func playRandomTurns(t *testing.T, seed int64) {
	fixture := newTurnFixture(t)
	ctx := context.Background()
	random := rand.New(rand.NewSource(seed))
	costs := map[models.UpgradeType][]uint{}
	for _, upgrade := range models.UpgradeTable() {
		for _, level := range upgrade.Levels {
			costs[upgrade.Type] = append(costs[upgrade.Type], level.SoldierCost)
		}
	}
	teamColors := map[string]uint{}
	for _, player := range fixture.players {
		current, err := fixture.service.Current(ctx, player, fixture.state.LobbyID)
		if err != nil {
			t.Fatal(err)
		}
		teamColors[player.PlayerID] = current.TeamColor
	}
	state := fixture.state
	accepted, rejected := 0, 0
	for turn := 0; turn < 60 && state.Outcome == nil; turn++ {
		board := map[uint]*modelCastle{}
		castleIDs := []uint{missingCastleID}
		for _, castle := range state.Generation.Castles {
			board[castle.ID] = &modelCastle{teamColor: castle.TeamColor, unreserved: castle.Soldiers, levels: maps.Clone(castle.Upgrades)}
			castleIDs = append(castleIDs, castle.ID)
		}
		type acceptedRequest struct {
			player  domain.Principal
			request models.RegisterGenerationCommands
		}
		var acceptedRequests []acceptedRequest
		for batchIndex := 0; batchIndex < 1+random.Intn(8); batchIndex++ {
			player := fixture.players[random.Intn(len(fixture.players))]
			teamColor := teamColors[player.PlayerID]
			// A rejected batch leaves the model unchanged.
			trial := map[uint]*modelCastle{}
			for castleID, castle := range board {
				trial[castleID] = &modelCastle{castle.teamColor, castle.unreserved, maps.Clone(castle.levels)}
			}
			// Mostly castles the player's team owns, so most batches can pass.
			pickCastle := func() uint {
				if random.Intn(10) < 8 {
					owned := []uint{}
					for castleID, castle := range trial {
						if castle.teamColor == teamColor {
							owned = append(owned, castleID)
						}
					}
					if len(owned) > 0 {
						return owned[random.Intn(len(owned))]
					}
				}
				return castleIDs[random.Intn(len(castleIDs))]
			}
			var expected *modelRejection
			var commands []models.PublicGenerationCommand
			for commandIndex := 0; commandIndex < 1+random.Intn(3); commandIndex++ {
				if random.Intn(4) == 0 {
					origin, destination := pickCastle(), castleIDs[1+random.Intn(len(castleIDs)-1)]
					if random.Intn(20) == 0 {
						destination = missingCastleID
					}
					amount := uint(1 + random.Intn(4))
					commands = append(commands, send(origin, destination, amount))
					if expected != nil {
						continue
					}
					switch {
					case origin == destination:
						expected = &modelRejection{422, ""}
					case trial[destination] == nil || trial[origin] == nil:
						expected = &modelRejection{422, services.CastleNotFoundProblemType}
					case trial[origin].teamColor != teamColor:
						expected = &modelRejection{403, services.CastleNotOwnedProblemType}
					case amount > trial[origin].unreserved:
						expected = &modelRejection{409, services.InsufficientSoldiersProblemType}
					default:
						trial[origin].unreserved -= amount
					}
					continue
				}
				castleID := pickCastle()
				upgradeType := []models.UpgradeType{models.SpeedUpgrade, models.ClashStopGenerationsUpgrade}[random.Intn(2)]
				if random.Intn(10) == 0 {
					upgradeType = "armor"
				}
				commands = append(commands, upgrade(castleID, upgradeType))
				if expected != nil {
					continue
				}
				castle := trial[castleID]
				switch {
				case costs[upgradeType] == nil:
					expected = &modelRejection{422, services.UnknownUpgradeTypeProblemType}
				case castle == nil:
					expected = &modelRejection{422, services.CastleNotFoundProblemType}
				case castle.teamColor != teamColor:
					expected = &modelRejection{403, services.CastleNotOwnedProblemType}
				case castle.levels[upgradeType] >= len(costs[upgradeType]):
					expected = &modelRejection{409, services.UpgradeMaxLevelProblemType}
				case costs[upgradeType][castle.levels[upgradeType]] > castle.unreserved:
					expected = &modelRejection{409, services.InsufficientSoldiersProblemType}
				default:
					castle.unreserved -= costs[upgradeType][castle.levels[upgradeType]]
					castle.levels[upgradeType]++
				}
			}
			request := batch(state, false, commands...)
			_, err := fixture.service.Register(ctx, player, request)
			if expected == nil {
				if err != nil {
					t.Fatalf("turn %d: the model accepts %s but the server answers %v", turn, describeCommands(commands), err)
				}
				board = trial
				accepted++
				acceptedRequests = append(acceptedRequests, acceptedRequest{player, request})
				continue
			}
			rejected++
			var rejection *services.GenerationError
			if !errors.As(err, &rejection) || rejection.Status != expected.status || rejection.Type != expected.problemType {
				t.Fatalf("turn %d: %s: got %v, want %d %q", turn, describeCommands(commands), err, expected.status, expected.problemType)
			}
		}
		// A retried request is answered again without reserving twice.
		for _, retry := range acceptedRequests {
			if _, err := fixture.service.Register(ctx, retry.player, retry.request); err != nil {
				t.Fatalf("turn %d: retry failed: %v", turn, err)
			}
		}
		var next models.GenerationState
		for _, player := range fixture.players {
			var err error
			if next, err = fixture.service.Register(ctx, player, batch(state, true)); err != nil {
				t.Fatalf("turn %d: completing the turn failed: %v", turn, err)
			}
		}
		if next.GenerationID == state.GenerationID {
			t.Fatalf("turn %d did not advance", turn)
		}
		for _, castle := range next.Generation.Castles {
			if !maps.Equal(castle.Upgrades, board[castle.ID].levels) {
				t.Fatalf("turn %d: castle %d has levels %v, the model %v", turn, castle.ID, castle.Upgrades, board[castle.ID].levels)
			}
		}
		state = next
	}
	if accepted == 0 || rejected == 0 {
		t.Fatalf("accepted %d and rejected %d batches; both must happen", accepted, rejected)
	}
}

func describeCommands(commands []models.PublicGenerationCommand) string {
	descriptions := make([]string, 0, len(commands))
	for _, command := range commands {
		descriptions = append(descriptions, fmt.Sprintf("%s%s", command.CommandType, command.Parameters))
	}
	return strings.Join(descriptions, " ")
}
