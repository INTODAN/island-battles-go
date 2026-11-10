package islandbattles

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// MemoryLimitBytes is the most memory a result may return, as compact JSON:
// 64 KiB.
const MemoryLimitBytes = 64 * 1024

// ErrMemoryTooLarge is the error of a result whose memory is over
// MemoryLimitBytes as compact JSON.
var ErrMemoryTooLarge = errors.New("memory is too large")

// Memory is what a bot remembers between turns: a JSON object, which the
// server stores and gives back as the next turn's Turn.Memory. A bot keeps
// nothing else between turns. Empty means none, which is null in JSON.
//
// Decode it into your own struct with Decode, and build the next one with
// NewMemory.
type Memory []byte

// NewMemory encodes value, usually a struct of your own, as the memory for
// the next turn. value must encode as a JSON object, or null for no memory.
func NewMemory(value any) (Memory, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("memory is not JSON: %w", err)
	}
	memory := Memory(encoded)
	if err := memory.check(); err != nil {
		return nil, err
	}
	return memory.compact()
}

// Decode reads the memory into target, a pointer to your own struct. Without
// memory, on the first turn, it leaves target as it is and returns nil.
func (memory Memory) Decode(target any) error {
	if len(memory) == 0 {
		return nil
	}
	return json.Unmarshal(memory, target)
}

// Size is the memory's size as compact JSON, the size the limit counts. No
// memory is 4 bytes, the size of null.
func (memory Memory) Size() (int, error) {
	compact, err := memory.compact()
	if err != nil {
		return 0, err
	}
	if len(compact) == 0 {
		return len("null"), nil
	}
	return len(compact), nil
}

// MarshalJSON writes the memory as it is, or null when there is none.
func (memory Memory) MarshalJSON() ([]byte, error) {
	if len(memory) == 0 {
		return []byte("null"), nil
	}
	return memory, nil
}

// UnmarshalJSON keeps the raw JSON, so numbers and keys reach the next turn
// unchanged. A JSON null leaves no memory.
func (memory *Memory) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		*memory = nil
		return nil
	}
	*memory = append((*memory)[:0], data...)
	return nil
}

// compact is the memory as compact JSON, or nil for none or null.
func (memory Memory) compact() (Memory, error) {
	if len(memory) == 0 {
		return nil, nil
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, memory); err != nil {
		return nil, fmt.Errorf("memory is not JSON: %w", err)
	}
	if bytes.Equal(compact.Bytes(), []byte("null")) {
		return nil, nil
	}
	return compact.Bytes(), nil
}

// check reports memory that isn't a JSON object or null, or is over the limit.
func (memory Memory) check() error {
	compact, err := memory.compact()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidResult, err)
	}
	if len(compact) == 0 {
		return nil
	}
	if compact[0] != '{' {
		return fmt.Errorf("%w: memory must be a JSON object or null", ErrInvalidResult)
	}
	if len(compact) > MemoryLimitBytes {
		return fmt.Errorf("%w: memory is %d bytes as compact JSON; the limit is %d (64 KiB)", ErrMemoryTooLarge, len(compact), MemoryLimitBytes)
	}
	return nil
}
