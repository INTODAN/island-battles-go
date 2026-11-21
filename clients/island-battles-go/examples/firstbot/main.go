// Command firstbot is the first bot of the README: it sends one soldier from
// its first castle with soldiers to the first enemy castle, counts its turns
// in its memory, and logs how each game ended.
package main

import (
	"context"

	islandbattles "github.com/INTODAN/island-battles-go/clients/island-battles-go"
)

type memory struct {
	Turns int `json:"turns"`
}

func play(_ context.Context, turn islandbattles.Turn) (islandbattles.Result, error) {
	var result islandbattles.Result
attack:
	for _, own := range turn.Generation.Castles {
		for _, other := range turn.Generation.Castles {
			if own.TeamColor == turn.TeamColor && own.Soldiers > 0 && own.CompareTeam(other) == islandbattles.Enemy {
				result.Commands = append(result.Commands, islandbattles.SendSoldierGroup(own.ID, other.ID, 1))
				result.Logs = append(result.Logs, islandbattles.Logf("castle %d attacks castle %d", own.ID, other.ID))
				break attack
			}
		}
	}
	var remembered memory
	_ = turn.Memory.Decode(&remembered) // the first turn has no memory: Turns stays 0
	remembered.Turns++
	var err error
	result.Memory, err = islandbattles.NewMemory(remembered)
	return result, err
}

// gameEnded hears how each game ended, with the memory of its last turn. It
// could also save the game's demo: islandbattles.DownloadDemo(ctx, ended.Demo).
func gameEnded(ctx context.Context, ended islandbattles.GameEndedDelivery) error {
	var remembered memory
	_ = ended.Memory.Decode(&remembered)
	islandbattles.Logger(ctx).Info("the game ended", "outcome", ended.Outcome.Summary, "turns", remembered.Turns)
	return nil
}

func main() {
	islandbattles.Main(play, islandbattles.OnGameEnd(gameEnded))
}
