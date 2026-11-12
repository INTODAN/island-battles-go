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
	// err fails retiring, and purgeErr fails purging.
	err      error
	purgeErr error
	purges   int
	// packages is what deleting bot packages returns, packagesErr fails it,
	// and packageBefores and packageLimits record its calls.
	packages       int
	packagesErr    error
	packageBefores []time.Time
	packageLimits  []int
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

func (store *recordingStore) PurgeRetiredGames(_ context.Context, generationLimit, gameLimit int) (dal.GamePurge, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.limits = append(store.limits, generationLimit, gameLimit)
	store.purges++
	return store.purge, store.purgeErr
}

func (store *recordingStore) DeleteExpiredBotPackages(_ context.Context, before time.Time, limit int) (int, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.packageBefores = append(store.packageBefores, before)
	store.packageLimits = append(store.packageLimits, limit)
	return store.packages, store.packagesErr
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
	if store.limits[0] != 10 || store.limits[1] != 10 || store.limits[2] != retention.MaxPurgedGames {
		t.Fatalf("limits = %v, want the batch size for both steps, from at most %d games", store.limits, retention.MaxPurgedGames)
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
		"deleted from the most games":     {purge: dal.GamePurge{Generations: 3, Games: retention.MaxPurgedGames}},
		"deleted a full package batch":    {packages: 10},
	} {
		clock := &fakeClock{now: time.Now()}
		runner := newRetention(t, store, clock)
		runner.RunIfDue(context.Background())
		runner.RunIfDue(context.Background())
		if store.passCount() != 2 {
			t.Errorf("%s: passes=%d, want the second request to continue the backlog", name, store.passCount())
		}
	}
	partial := &recordingStore{retired: 9, purge: dal.GamePurge{Generations: 9, Games: retention.MaxPurgedGames - 1}, packages: 9}
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

func TestAFailingStepDoesNotStopTheOther(t *testing.T) {
	retireFails := &recordingStore{err: errors.New("retire failed"), purge: dal.GamePurge{Generations: 4, Games: 1}}
	runner := newRetention(t, retireFails, &fakeClock{now: time.Now()})
	result, err := runner.Pass(context.Background(), time.Now())
	if err == nil || retireFails.purges != 1 || result.Generations != 4 || result.Games != 1 {
		t.Fatalf("result=%+v error=%v purges=%d, want retired games purged although retiring failed", result, err, retireFails.purges)
	}
	purgeFails := &recordingStore{retired: 3, purge: dal.GamePurge{Generations: 2}, purgeErr: errors.New("purge failed for one game")}
	runner = newRetention(t, purgeFails, &fakeClock{now: time.Now()})
	result, err = runner.Pass(context.Background(), time.Now())
	if err == nil || result.RetiredGames != 3 || result.Generations != 2 {
		t.Fatalf("result=%+v error=%v, want the retired games and the other games' purge counted", result, err)
	}
	packagesFail := &recordingStore{retired: 2, purge: dal.GamePurge{Generations: 1}, packagesErr: errors.New("delete failed")}
	runner = newRetention(t, packagesFail, &fakeClock{now: time.Now()})
	result, err = runner.Pass(context.Background(), time.Now())
	if err == nil || result.RetiredGames != 2 || result.Generations != 1 || result.BotPackages != 0 {
		t.Fatalf("result=%+v error=%v, want the games counted although deleting packages failed", result, err)
	}
	gamesFail := &recordingStore{err: errors.New("retire failed"), purgeErr: errors.New("purge failed"), packages: 4}
	runner = newRetention(t, gamesFail, &fakeClock{now: time.Now()})
	result, err = runner.Pass(context.Background(), time.Now())
	if err == nil || result.BotPackages != 4 {
		t.Fatalf("result=%+v error=%v, want packages deleted although the games' steps failed", result, err)
	}
}

func TestBotPackagesAreDeletedEightDaysAfterTheirUpload(t *testing.T) {
	store := &recordingStore{}
	clock := &fakeClock{now: time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)}
	newRetention(t, store, clock).RunIfDue(context.Background())
	if len(store.packageBefores) != 1 || !store.packageBefores[0].Equal(clock.now.Add(-8*24*time.Hour)) || store.packageLimits[0] != 10 {
		t.Fatalf("befores=%v limits=%v, want one batch of 10 older than 8 days", store.packageBefores, store.packageLimits)
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
