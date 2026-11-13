package services

import (
	"context"
	"fmt"
	"time"

	"islandBattles/backend/dal"
	"islandBattles/backend/models"

	"gorm.io/gorm"
)

// BotComputeProblemType is the lobby's game_requirement problem type for a
// start whose roster has an uploaded bot, a member's or a bot seat's, once
// the bot runners have used the month's compute budget.
const BotComputeProblemType = "urn:island-battles:problem:bot-compute-spent"

// DefaultBotComputeBudgetGBSeconds is the bot runners' monthly budget unless
// ISLAND_BATTLES_BOT_COMPUTE_BUDGET_GB_SECONDS sets another: 300,000 of the
// 400,000 GB-s of Lambda's free tier, which the backend function shares
// (#226).
const DefaultBotComputeBudgetGBSeconds = 300_000

// BotComputeCachePeriod is how long the runners' compute read from
// CloudWatch is kept before it is read again.
const BotComputeCachePeriod = 10 * time.Minute

// botComputeReadTimeout bounds a read of the meter, during which the cache's
// lock is held.
const botComputeReadTimeout = 5 * time.Second

// BotComputeMeter reads the GB-seconds that the bot runners have used from
// month, the first of a month at midnight UTC, until now, such as
// botcompute.CloudWatch.
type BotComputeMeter interface {
	MonthToDate(ctx context.Context, month, now time.Time) (float64, error)
}

// FixedBotCompute is a meter that always reads the same GB-seconds, for local
// development and tests (ISLAND_BATTLES_BOT_COMPUTE_FAKE_GB_SECONDS).
type FixedBotCompute float64

func (fixed FixedBotCompute) MonthToDate(context.Context, time.Time, time.Time) (float64, error) {
	return float64(fixed), nil
}

// CachedBotCompute reads a meter at most once per BotComputeCachePeriod,
// keeping what it read in the database's island_bot_compute_usage for every
// instance of the backend. A refresh holds an advisory lock while it reads
// the meter, so instances that find the cache stale at once read it once
// between them.
type CachedBotCompute struct {
	Database *gorm.DB
	Meter    BotComputeMeter
}

func (cached CachedBotCompute) MonthToDate(ctx context.Context, month, now time.Time) (float64, error) {
	database := cached.Database.WithContext(ctx)
	if usage, err := dal.FindBotComputeUsage(database, month); err != nil {
		return 0, err
	} else if fresh(usage, now) {
		return usage.GBSeconds, nil
	}
	var used float64
	err := database.Transaction(func(transaction *gorm.DB) error {
		if err := dal.LockBotComputeUsage(transaction); err != nil {
			return err
		}
		// Another instance may have refreshed it while this one waited.
		usage, err := dal.FindBotComputeUsage(transaction, month)
		if err != nil {
			return err
		}
		if fresh(usage, now) {
			used = usage.GBSeconds
			return nil
		}
		// The lock is held while the meter is read, so the read is bounded.
		readCtx, cancel := context.WithTimeout(ctx, botComputeReadTimeout)
		defer cancel()
		if used, err = cached.Meter.MonthToDate(readCtx, month, now); err != nil {
			return err
		}
		return dal.SaveBotComputeUsage(transaction, models.BotComputeUsage{Month: month, GBSeconds: used, ReadAt: now})
	})
	return used, err
}

// fresh reports whether a cached usage was read less than the cache period
// before now.
func fresh(usage *models.BotComputeUsage, now time.Time) bool {
	return usage != nil && now.Sub(usage.ReadAt) < BotComputeCachePeriod && !usage.ReadAt.After(now)
}

// BotComputeGuard keeps uploaded bots inside the bot runners' monthly
// compute budget (#226): once the runners have used BudgetGBSeconds this
// month (UTC), no new game with an uploaded bot may start until the next.
// Without a Meter, as on a local server without a fake usage, the guard is
// off and every month is available.
type BotComputeGuard struct {
	Meter           BotComputeMeter
	BudgetGBSeconds float64
	// Now is the clock, time.Now when nil.
	Now func() time.Time
}

// Usage reads this month's compute against the budget. A failed read is
// returned as an error.
func (guard *BotComputeGuard) Usage(ctx context.Context) (models.BotComputeView, error) {
	now := time.Now()
	if guard != nil && guard.Now != nil {
		now = guard.Now()
	}
	month := MonthStart(now)
	view := models.BotComputeView{Month: month.Format("2006-01"), Available: true}
	if guard == nil {
		view.BudgetGBSeconds = DefaultBotComputeBudgetGBSeconds
		return view, nil
	}
	view.BudgetGBSeconds = guard.BudgetGBSeconds
	if guard.Meter == nil {
		return view, nil
	}
	used, err := guard.Meter.MonthToDate(ctx, month, now)
	if err != nil {
		return models.BotComputeView{}, err
	}
	view.UsedGBSeconds = used
	view.Available = used < guard.BudgetGBSeconds
	return view, nil
}

// MonthStart is the first of now's month, at midnight UTC.
func MonthStart(now time.Time) time.Time {
	year, month, _ := now.UTC().Date()
	return time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
}

// BotComputeSpentMessage tells players that uploaded bots have used the
// budget of month, YYYY-MM, and when they are back: the first of the next
// month. Both bot pickers show the same sentence.
func BotComputeSpentMessage(month string) string {
	start, err := time.Parse("2006-01", month)
	if err != nil {
		return "Uploaded bots have used this month's free compute. They're back next month."
	}
	return fmt.Sprintf("Uploaded bots have used this month's free compute. They're back on 1 %s.", start.AddDate(0, 1, 0).Month())
}
