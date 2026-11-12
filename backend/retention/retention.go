// Package retention deletes Island Battles games some time after their last
// turn. Nothing runs between requests, so the game's own requests run the
// passes as they end, in small batches. When nobody plays, no game data is
// added, so none needs deleting. The lobby deletes its own data separately,
// and this package never reads lobby tables. The same passes delete the bot
// packages uploaded to lobbies 8 days after their upload.
package retention

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"islandBattles/backend/dal"

	"gorm.io/gorm"
)

// Policy says when games are deleted and how much work one pass may do.
type Policy struct {
	// Period runs from the creation of a game's latest generation, or of the
	// game itself before its first one. Finished and abandoned games are
	// treated alike, since an abandoned game never gets an outcome.
	Period time.Duration
	// BatchSize bounds both the games one pass retires and the generations
	// it deletes, from at most MaxPurgedGames games. A generation is deleted
	// with its map, castles, soldier
	// groups, commands, player inputs and requests, so this bounds the rows
	// one transaction deletes by BatchSize generations' worth.
	BatchSize int
	// PassInterval is the least time between two passes in one process while
	// the previous pass found no backlog.
	PassInterval time.Duration
}

// DefaultPeriod keeps a game for 7 days after its last turn.
const DefaultPeriod = 7 * 24 * time.Hour

// MaxBatchSize bounds BatchSize, so one pass stays short.
const MaxBatchSize = 100

// DefaultPolicy keeps games for DefaultPeriod. A pass retires at most 10
// games and deletes at most 10 generations from at most MaxPurgedGames games,
// at most once a minute per process unless the previous pass found more to
// delete.
func DefaultPolicy() Policy {
	return Policy{Period: DefaultPeriod, BatchSize: 10, PassInterval: time.Minute}
}

// ErrInvalidPolicy rejects a policy with a non-positive duration or a batch
// size outside 1 to MaxBatchSize.
var ErrInvalidPolicy = errors.New("invalid game retention policy")

func (policy Policy) Validate() error {
	if policy.Period <= 0 || policy.PassInterval <= 0 || policy.BatchSize < 1 || policy.BatchSize > MaxBatchSize {
		return ErrInvalidPolicy
	}
	return nil
}

// BotPackagePeriod keeps an uploaded bot package's row for 8 days after it
// was created, as long as the bot package bucket's lifecycle rule keeps its
// zip (deploy/terraform/bot_packages.tf). It doesn't follow Period.
const BotPackagePeriod = 8 * 24 * time.Hour

// Result counts what one pass did.
type Result struct {
	// RetiredGames were hidden from every request. Later passes delete them.
	RetiredGames int
	dal.GamePurge
	// BotPackages counts the bot packages deleted.
	BotPackages int
}

// Store retires and deletes games, and deletes old bot packages.
// DatabaseStore is the real one.
type Store interface {
	// RetireExpiredGames hides up to limit games with no generation since
	// before, skipping games that a request has locked.
	RetireExpiredGames(ctx context.Context, before time.Time, limit int) (int, error)
	// PurgeRetiredGames deletes up to generationLimit generations of at most
	// gameLimit retired games, and the retired games left with none.
	PurgeRetiredGames(ctx context.Context, generationLimit, gameLimit int) (dal.GamePurge, error)
	// DeleteExpiredBotPackages deletes up to limit bot packages created
	// before before.
	DeleteExpiredBotPackages(ctx context.Context, before time.Time, limit int) (int, error)
}

// DatabaseStore runs the game's retention queries in dal.
type DatabaseStore struct {
	Database *gorm.DB
}

func (store DatabaseStore) RetireExpiredGames(ctx context.Context, before time.Time, limit int) (int, error) {
	return dal.RetireExpiredGames(store.Database.WithContext(ctx), before, limit)
}

func (store DatabaseStore) PurgeRetiredGames(ctx context.Context, generationLimit, gameLimit int) (dal.GamePurge, error) {
	return dal.PurgeRetiredGames(store.Database.WithContext(ctx), generationLimit, gameLimit)
}

func (store DatabaseStore) DeleteExpiredBotPackages(ctx context.Context, before time.Time, limit int) (int, error) {
	return dal.DeleteExpiredBotPackages(store.Database.WithContext(ctx), before, limit)
}

// MaxPurgedGames bounds the retired games one pass deletes from. Each takes a
// transaction of its own, of about 15 statements, and on Lambda every
// statement is a round trip to the database.
const MaxPurgedGames = 3

// passTimeout bounds one pass, so a slow database delays the request that
// runs it by at most this much.
const passTimeout = 5 * time.Second

// Retention runs passes at the end of game requests. A process runs at most
// one pass at a time, and at most one per PassInterval while passes find no
// backlog. When a pass fills a batch, the next request runs another, so a
// backlog drains at the pace of requests without one request doing all of
// it. A turn adds one generation and a pass deletes up to BatchSize, so
// deleting keeps ahead of playing.
type Retention struct {
	store  Store
	policy Policy
	now    func() time.Time
	logger *slog.Logger

	// running is held for a whole pass; requests that find it held skip.
	running sync.Mutex
	// nextPass is guarded by running.
	nextPass time.Time
}

// New makes a Retention whose clock is now.
func New(store Store, policy Policy, now func() time.Time, logger *slog.Logger) (*Retention, error) {
	if store == nil || now == nil || logger == nil {
		return nil, errors.New("game retention needs a store, a clock and a logger")
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	return &Retention{store: store, policy: policy, now: now, logger: logger}, nil
}

// RunIfDue runs a pass when one is due and none is running in this process.
// It outlives a canceled request, within its own timeout, and logs failures
// instead of returning them, because the request itself has succeeded.
func (retention *Retention) RunIfDue(ctx context.Context) {
	if !retention.running.TryLock() {
		return
	}
	defer retention.running.Unlock()
	now := retention.now()
	if now.Before(retention.nextPass) {
		return
	}
	retention.nextPass = now.Add(retention.policy.PassInterval)
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), passTimeout)
	defer cancel()
	result, err := retention.Pass(ctx, now)
	if err != nil {
		// The work that succeeded still counts. A failing pass waits for the
		// interval, so it isn't retried on every request.
		retention.logger.ErrorContext(ctx, "Game retention pass failed", "error", err)
	} else if retention.backlog(result) {
		retention.nextPass = now
	}
	if result != (Result{}) {
		retention.logger.InfoContext(ctx, "Game retention pass deleted expired games",
			"retired_games", result.RetiredGames, "generations", result.Generations, "games", result.Games, "bot_packages", result.BotPackages)
	}
}

// Pass retires the games that expired by now, deletes one batch of retired
// games' rows and one batch of bot packages older than BotPackagePeriod,
// whether or not a pass is due. Each step runs even when another fails, so
// games retired earlier are still deleted while retiring fails, and the
// reverse. The result counts what succeeded, and the error joins every
// failure.
func (retention *Retention) Pass(ctx context.Context, now time.Time) (Result, error) {
	var result Result
	retired, retireErr := retention.store.RetireExpiredGames(ctx, now.Add(-retention.policy.Period), retention.policy.BatchSize)
	if retireErr != nil {
		retireErr = fmt.Errorf("retire expired games: %w", retireErr)
	} else {
		result.RetiredGames = retired
	}
	purge, purgeErr := retention.store.PurgeRetiredGames(ctx, retention.policy.BatchSize, retention.purgedGames())
	if purgeErr != nil {
		purgeErr = fmt.Errorf("delete retired games: %w", purgeErr)
	}
	// A purge that fails for some games still reports the others.
	result.GamePurge = purge
	packages, packagesErr := retention.store.DeleteExpiredBotPackages(ctx, now.Add(-BotPackagePeriod), retention.policy.BatchSize)
	if packagesErr != nil {
		packagesErr = fmt.Errorf("delete expired bot packages: %w", packagesErr)
	} else {
		result.BotPackages = packages
	}
	return result, errors.Join(retireErr, purgeErr, packagesErr)
}

// purgedGames is the number of retired games one pass deletes from.
func (retention *Retention) purgedGames() int {
	return min(MaxPurgedGames, retention.policy.BatchSize)
}

// backlog reports a pass that filled a batch, which may have left more.
func (retention *Retention) backlog(result Result) bool {
	batchSize := retention.policy.BatchSize
	return result.RetiredGames >= batchSize || result.Generations >= batchSize || result.Games >= retention.purgedGames() || result.BotPackages >= batchSize
}
