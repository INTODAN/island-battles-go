package retention_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"islandBattles/backend/dal"
	"islandBattles/backend/retention"
)

// recordingStore counts passes and returns the results it is given.
type recordingStore struct {
	mutex   sync.Mutex
	passes  int
	befores []time.Time
	limits  []int
	retired int
	purge   dal.GamePurge
	err     error
	// block, when set, holds every pass until it is closed.
	block   chan struct{}
	entered chan struct{}
	// contextErrors records the pass context's error when it started.
	contextErrors []error
}

func (store *recordingStore) RetireExpiredGames(ctx context.Context, before time.Time, limit int) (int, error) {
	if store.entered != nil {
		store.entered <- struct{}{}
	}
	if store.block != nil {
		<-store.block
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.passes++
	store.befores = append(store.befores, before)
	store.limits = append(store.limits, limit)
	store.contextErrors = append(store.contextErrors, ctx.Err())
	return store.retired, store.err
}

func (store *recordingStore) PurgeRetiredGames(_ context.Context, limit int) (dal.GamePurge, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.limits = append(store.limits, limit)
	return store.purge, store.err
}

func (store *recordingStore) passCount() int {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.passes
}

// fakeClock is advanced by hand.
type fakeClock struct{ now time.Time }

func (clock *fakeClock) Now() time.Time { return clock.now }

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newRetention(t *testing.T, store retention.Store, clock *fakeClock) *retention.Retention {
	t.Helper()
	runner, err := retention.New(store, retention.DefaultPolicy(), clock.Now, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	return runner
}

func TestDefaultPolicyKeepsGamesForSevenDaysInSmallBatches(t *testing.T) {
	policy := retention.DefaultPolicy()
	if err := policy.Validate(); err != nil {
		t.Fatal(err)
	}
	if policy.Period != 7*24*time.Hour || policy.BatchSize != 10 || policy.PassInterval != time.Minute {
		t.Fatalf("default policy = %+v", policy)
	}
	for name, invalid := range map[string]retention.Policy{
		"no period":          {BatchSize: 10, PassInterval: time.Minute},
		"no interval":        {Period: time.Hour, BatchSize: 10},
		"empty batch":        {Period: time.Hour, PassInterval: time.Minute},
		"batch over maximum": {Period: time.Hour, BatchSize: retention.MaxBatchSize + 1, PassInterval: time.Minute},
	} {
		if err := invalid.Validate(); !errors.Is(err, retention.ErrInvalidPolicy) {
			t.Errorf("%s: Validate() = %v, want ErrInvalidPolicy", name, err)
		}
		if _, err := retention.New(&recordingStore{}, invalid, time.Now, discardLogger()); err == nil {
			t.Errorf("%s: New() accepted an invalid policy", name)
		}
	}
}

func TestPassesRunAtMostOncePerIntervalWithoutBacklog(t *testing.T) {
	store := &recordingStore{}
	clock := &fakeClock{now: time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)}
	runner := newRetention(t, store, clock)

	runner.RunIfDue(context.Background())
	if store.passCount() != 1 || !store.befores[0].Equal(clock.now.Add(-retention.DefaultPeriod)) {
		t.Fatalf("passes=%d befores=%v, want one pass with a cutoff 7 days ago", store.passCount(), store.befores)
	}
	if store.limits[0] != 10 || store.limits[1] != 10 {
		t.Fatalf("limits = %v, want the batch size for both steps", store.limits)
	}
	clock.now = clock.now.Add(59 * time.Second)
	runner.RunIfDue(context.Background())
	if store.passCount() != 1 {
		t.Fatalf("a pass ran within the interval: passes=%d", store.passCount())
	}
	clock.now = clock.now.Add(time.Second)
	runner.RunIfDue(context.Background())
	if store.passCount() != 2 {
		t.Fatalf("no pass ran after the interval: passes=%d", store.passCount())
	}
}

func TestAFullBatchMakesTheNextRequestRunAnotherPass(t *testing.T) {
	for name, store := range map[string]*recordingStore{
		"retired a full batch":            {retired: 10},
		"deleted a full generation batch": {purge: dal.GamePurge{Generations: 10}},
		"deleted a full game batch":       {purge: dal.GamePurge{Generations: 3, Games: 10}},
	} {
		clock := &fakeClock{now: time.Now()}
		runner := newRetention(t, store, clock)
		runner.RunIfDue(context.Background())
		runner.RunIfDue(context.Background())
		if store.passCount() != 2 {
			t.Errorf("%s: passes=%d, want the second request to continue the backlog", name, store.passCount())
		}
	}
	partial := &recordingStore{retired: 9, purge: dal.GamePurge{Generations: 9, Games: 2}}
	runner := newRetention(t, partial, &fakeClock{now: time.Now()})
	runner.RunIfDue(context.Background())
	runner.RunIfDue(context.Background())
	if partial.passCount() != 1 {
		t.Fatalf("passes=%d, want a pass that filled no batch to wait for the interval", partial.passCount())
	}
}

func TestAFailedPassWaitsForTheInterval(t *testing.T) {
	store := &recordingStore{retired: 10, err: errors.New("database unavailable")}
	runner := newRetention(t, store, &fakeClock{now: time.Now()})
	runner.RunIfDue(context.Background())
	runner.RunIfDue(context.Background())
	if store.passCount() != 1 {
		t.Fatalf("passes=%d, want a failed pass not to be retried at once", store.passCount())
	}
}

func TestRequestsSkipThePassWhileAnotherRuns(t *testing.T) {
	store := &recordingStore{block: make(chan struct{}), entered: make(chan struct{}, 1)}
	runner := newRetention(t, store, &fakeClock{now: time.Now()})
	done := make(chan struct{})
	go func() {
		runner.RunIfDue(context.Background())
		close(done)
	}()
	<-store.entered
	skipped := make(chan struct{})
	go func() {
		runner.RunIfDue(context.Background())
		close(skipped)
	}()
	select {
	case <-skipped:
	case <-time.After(5 * time.Second):
		t.Fatal("a request waited for the running pass")
	}
	close(store.block)
	<-done
	if store.passCount() != 1 {
		t.Fatalf("passes=%d, want one", store.passCount())
	}
}

func TestAPassOutlivesItsCanceledRequest(t *testing.T) {
	store := &recordingStore{}
	runner := newRetention(t, store, &fakeClock{now: time.Now()})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner.RunIfDue(ctx)
	if store.passCount() != 1 || store.contextErrors[0] != nil {
		t.Fatalf("passes=%d context errors=%v, want one pass with a live context", store.passCount(), store.contextErrors)
	}
}
