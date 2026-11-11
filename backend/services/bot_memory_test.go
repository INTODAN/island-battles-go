package services_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"islandBattles/backend/models"
	"islandBattles/backend/services"
	"islandBattles/lobby/domain"
)

func withMemory(request models.RegisterGenerationCommands, memory string) models.RegisterGenerationCommands {
	request.Memory = models.BotMemory(memory)
	return request
}

// assertMemory compares memory with want as JSON values, keeping every digit
// of numbers. An empty want means no memory.
func assertMemory(t *testing.T, memory models.BotMemory, want string) {
	t.Helper()
	if want == "" {
		if len(memory) != 0 {
			t.Fatalf("memory=%s, want none", memory)
		}
		return
	}
	decode := func(raw []byte) any {
		var value any
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&value); err != nil {
			t.Fatalf("decode %s: %v", raw, err)
		}
		return value
	}
	if !reflect.DeepEqual(decode(memory), decode([]byte(want))) {
		t.Fatalf("memory=%s, want %s", memory, want)
	}
}

func currentMemory(t *testing.T, fixture turnFixture, player int) models.GenerationState {
	t.Helper()
	state, err := fixture.service.Current(context.Background(), fixture.players[player], fixture.state.LobbyID)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func countBotMemories(t *testing.T, fixture turnFixture) int64 {
	t.Helper()
	var count int64
	if err := fixture.database.Model(&models.GamePlayerBotMemory{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	return count
}

// pushedMemory returns the memory in the latest generation event pushed to a
// player's channel.
func pushedMemory(t *testing.T, fixture turnFixture, player int) models.BotMemory {
	t.Helper()
	channel, err := domain.PlayerChannel(fixture.players[player].PlayerID)
	if err != nil {
		t.Fatal(err)
	}
	events := fixture.publisher.GenerationEvents(t)
	for index := len(events) - 1; index >= 0; index-- {
		if events[index].State.Channel == channel {
			return events[index].State.Memory
		}
	}
	t.Fatalf("no event was pushed to %s", channel)
	return nil
}

func register(t *testing.T, fixture turnFixture, player int, request models.RegisterGenerationCommands) models.GenerationState {
	t.Helper()
	state, err := fixture.service.Register(context.Background(), fixture.players[player], request)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestBotMemoryRoundTripsAcrossTurns(t *testing.T) {
	fixture := newTurnFixture(t)
	assertMemory(t, fixture.state.Memory, "")
	// A number past float64 precision and nested values come back unchanged.
	first := `{"turn": 1, "big": 9007199254740993, "plans": [{"target": 3, "note": "<attack> & hold"}]}`
	completed := register(t, fixture, 0, withMemory(batch(fixture.state, true), first))
	if !completed.Submitted || completed.GenerationID != fixture.state.GenerationID {
		t.Fatalf("the turn did not complete: %+v", completed)
	}
	assertMemory(t, completed.Memory, first)
	// While the other player plays, the completed turn's memory is returned.
	assertMemory(t, currentMemory(t, fixture, 0).Memory, first)
	next := register(t, fixture, 1, batch(fixture.state, true))
	if next.GenerationID == fixture.state.GenerationID {
		t.Fatal("the turn did not advance")
	}
	assertMemory(t, next.Memory, "")
	// Each player's event carries their own memory.
	assertMemory(t, pushedMemory(t, fixture, 0), first)
	assertMemory(t, pushedMemory(t, fixture, 1), "")
	second := currentMemory(t, fixture, 0)
	if second.GenerationID != next.GenerationID {
		t.Fatalf("player 0 reads generation %d, want %d", second.GenerationID, next.GenerationID)
	}
	assertMemory(t, second.Memory, first)
	assertMemory(t, currentMemory(t, fixture, 1).Memory, "")

	// The next turn's memory replaces it, for each player separately.
	register(t, fixture, 0, withMemory(batch(second, true), `{"turn":2}`))
	register(t, fixture, 1, withMemory(batch(second, true), `{"bot":"red"}`))
	assertMemory(t, pushedMemory(t, fixture, 0), `{"turn":2}`)
	assertMemory(t, pushedMemory(t, fixture, 1), `{"bot":"red"}`)
	third := currentMemory(t, fixture, 0)
	assertMemory(t, third.Memory, `{"turn":2}`)
	assertMemory(t, currentMemory(t, fixture, 1).Memory, `{"bot":"red"}`)
	if count := countBotMemories(t, fixture); count != 2 {
		t.Fatalf("stored %d memories, want 2", count)
	}

	// A completed turn without memory clears it; an explicit null does too.
	assertMemory(t, register(t, fixture, 0, batch(third, true)).Memory, "")
	assertMemory(t, currentMemory(t, fixture, 0).Memory, "")
	assertMemory(t, register(t, fixture, 1, withMemory(batch(third, true), "null")).Memory, "")
	assertMemory(t, currentMemory(t, fixture, 1).Memory, "")
	if count := countBotMemories(t, fixture); count != 0 {
		t.Fatalf("stored %d memories, want none", count)
	}
}

func TestBotMemoryIsSavedOnlyByTheCompletingBatch(t *testing.T) {
	fixture := newTurnFixture(t)
	open := register(t, fixture, 0, withMemory(batch(fixture.state, false, send(0, 1, 1)), `{"draft":1}`))
	assertMemory(t, open.Memory, "")
	assertMemory(t, currentMemory(t, fixture, 0).Memory, "")
	if count := countBotMemories(t, fixture); count != 0 {
		t.Fatalf("an open batch stored %d memories", count)
	}
	assertMemory(t, register(t, fixture, 0, withMemory(batch(fixture.state, true), `{"final":1}`)).Memory, `{"final":1}`)
	register(t, fixture, 1, batch(fixture.state, true))

	// An open batch on the next turn keeps the stored memory.
	second := currentMemory(t, fixture, 0)
	open = register(t, fixture, 0, withMemory(batch(second, false), `{"draft":2}`))
	assertMemory(t, open.Memory, `{"final":1}`)
	assertMemory(t, currentMemory(t, fixture, 0).Memory, `{"final":1}`)
	assertMemory(t, register(t, fixture, 0, withMemory(batch(second, true), `{"final":2}`)).Memory, `{"final":2}`)
	assertMemory(t, currentMemory(t, fixture, 0).Memory, `{"final":2}`)
}

func TestBotMemoryRetryStoresItOnce(t *testing.T) {
	fixture := newTurnFixture(t)
	ctx := context.Background()
	first := withMemory(batch(fixture.state, true), `{"turn":1}`)
	register(t, fixture, 0, first)
	retried := register(t, fixture, 0, first)
	assertMemory(t, retried.Memory, `{"turn":1}`)
	// Memory is part of the request hash.
	changed := withMemory(first, `{"turn":"changed"}`)
	_, err := fixture.service.Register(ctx, fixture.players[0], changed)
	assertTurnStatus(t, err, 409)
	if _, err := fixture.service.Register(ctx, fixture.players[0], withMemory(first, "")); err == nil {
		t.Fatal("a retry without the memory was accepted")
	} else {
		assertTurnStatus(t, err, 409)
	}
	assertTurnCounts(t, fixture, 1, 1, 1, 2)

	// A late retry of the earlier turn doesn't bring its memory back.
	register(t, fixture, 1, batch(fixture.state, true))
	second := currentMemory(t, fixture, 0)
	register(t, fixture, 0, withMemory(batch(second, true), `{"turn":2}`))
	late := register(t, fixture, 0, first)
	if late.GenerationID != second.GenerationID {
		t.Fatalf("the retry returned generation %d, want the current %d", late.GenerationID, second.GenerationID)
	}
	assertMemory(t, late.Memory, `{"turn":2}`)
	assertMemory(t, currentMemory(t, fixture, 0).Memory, `{"turn":2}`)
	if count := countBotMemories(t, fixture); count != 1 {
		t.Fatalf("stored %d memories, want 1", count)
	}

	// Absent and null memory are the same batch.
	third := batch(second, false)
	register(t, fixture, 1, third)
	assertMemory(t, register(t, fixture, 1, withMemory(third, "null")).Memory, "")
}

func TestTooLargeBotMemoryIsRejectedAndStoresNothing(t *testing.T) {
	fixture := newTurnFixture(t)
	ctx := context.Background()
	// {"x":"…"} with its string filling the limit exactly.
	padding := models.MaxBotMemoryBytes - len(`{"x":""}`)
	largest := `{"x":"` + strings.Repeat("a", padding) + `"}`
	tooLarge := `{"x":"` + strings.Repeat("a", padding+1) + `"}`
	for _, complete := range []bool{true, false} {
		request := withLogs(withMemory(batch(fixture.state, complete, send(0, 1, 1)), tooLarge), logLine(models.GenerationLogInfo, "remember everything"))
		_, err := fixture.service.Register(ctx, fixture.players[0], request)
		var rejection *services.GenerationError
		if !errors.As(err, &rejection) || rejection.Status != 422 || rejection.Type != services.BotMemoryTooLargeProblemType {
			t.Fatalf("complete=%t: error=%v, want a 422 of type %s", complete, err, services.BotMemoryTooLargeProblemType)
		}
		if !strings.Contains(rejection.Detail, "65537 bytes") {
			t.Fatalf("detail=%q does not name the size", rejection.Detail)
		}
	}
	// Nothing of the batches was stored: no input, request, log line or memory.
	assertTurnCounts(t, fixture, 1, 0, 0, 2)
	var lines int64
	if err := fixture.database.Model(&models.GenerationLog{}).Count(&lines).Error; err != nil {
		t.Fatal(err)
	}
	if lines != 0 || countBotMemories(t, fixture) != 0 {
		t.Fatalf("stored %d log lines and %d memories", lines, countBotMemories(t, fixture))
	}
	if state := currentMemory(t, fixture, 0); state.Submitted {
		t.Fatal("a rejected batch completed the turn")
	}

	// Only other values than objects are invalid, and with no problem type.
	for _, invalid := range []string{`[1]`, `"memory"`, `3`} {
		_, err := fixture.service.Register(ctx, fixture.players[0], withMemory(batch(fixture.state, true), invalid))
		var rejection *services.GenerationError
		if !errors.As(err, &rejection) || rejection.Status != 422 || rejection.Type != "" {
			t.Fatalf("memory %s: error=%v, want a plain 422", invalid, err)
		}
	}

	// The limit counts compact JSON, so whitespace around values is free.
	spaced := "{\n  \"x\": \"" + strings.Repeat("a", padding) + "\"\n}"
	assertMemory(t, register(t, fixture, 0, withMemory(batch(fixture.state, true), spaced)).Memory, largest)
	assertMemory(t, currentMemory(t, fixture, 0).Memory, largest)
}

func TestBotMemoryIsStoredAsSent(t *testing.T) {
	fixture := newTurnFixture(t)
	// Numbers a database JSON type would expand to 131072 digits, and escapes
	// it would reject, stay byte for byte as the compact input.
	sent := `{ "a": 1e131071, "b": [1E2, 2.50], "z": 1, "a": 2, "s": "\u0000 \ud800" }`
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(sent)); err != nil {
		t.Fatal(err)
	}
	want := compact.String()
	completed := register(t, fixture, 0, withMemory(batch(fixture.state, true), sent))
	if string(completed.Memory) != want {
		t.Fatalf("completed memory=%s, want %s", completed.Memory, want)
	}
	if got := currentMemory(t, fixture, 0).Memory; string(got) != want {
		t.Fatalf("current memory=%.100s (%d bytes), want %s", got, len(got), want)
	}
	register(t, fixture, 1, batch(fixture.state, true))
	if got := pushedMemory(t, fixture, 0); string(got) != want {
		t.Fatalf("pushed memory=%.100s (%d bytes), want %s", got, len(got), want)
	}
}
