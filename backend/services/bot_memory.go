package services

import (
	"fmt"

	"islandBattles/backend/models"
)

// BotMemoryTooLargeProblemType is the problem type of the 422 that rejects a
// batch whose memory is larger than models.MaxBotMemoryBytes.
const BotMemoryTooLargeProblemType = "urn:island-battles:problem:bot-memory-too-large"

// validateBotMemory checks a batch's memory before the turn is locked and
// returns it as compact JSON, the form it is measured and stored in. No
// memory, or a JSON null, is valid and returns nil.
func validateBotMemory(memory models.BotMemory) (models.BotMemory, error) {
	compact, err := memory.Compact()
	if err != nil || (compact != nil && compact[0] != '{') {
		return nil, reject(422, "memory must be a JSON object or null")
	}
	if len(compact) > models.MaxBotMemoryBytes {
		return nil, &GenerationError{Status: 422, Type: BotMemoryTooLargeProblemType,
			Detail: fmt.Sprintf("memory is %d bytes as compact JSON, and may be at most %d (64 KiB)", len(compact), models.MaxBotMemoryBytes)}
	}
	return compact, nil
}
