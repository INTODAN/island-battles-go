package services_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"islandBattles/backend/internal/testutils"
	"islandBattles/backend/models"
	"islandBattles/backend/services"
)

// Months begin at midnight UTC on the first, whatever the clock's zone.
func TestMonthStartIsTheFirstOfTheMonthInUTC(t *testing.T) {
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct {
		now  time.Time
		want string
	}{
		{time.Date(2026, time.November, 30, 23, 59, 59, 0, time.UTC), "2026-11-01T00:00:00Z"},
		{time.Date(2026, time.December, 1, 0, 0, 0, 0, time.UTC), "2026-12-01T00:00:00Z"},
		// 30 November in New York is already 1 December in UTC.
		{time.Date(2026, time.November, 30, 20, 0, 0, 0, newYork), "2026-12-01T00:00:00Z"},
		{time.Date(2026, time.December, 31, 23, 59, 59, 999, time.UTC), "2026-12-01T00:00:00Z"},
		{time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC), "2027-01-01T00:00:00Z"},
	} {
		if got := services.MonthStart(check.now).Format(time.RFC3339); got != check.want {
			t.Errorf("MonthStart(%v) = %s, want %s", check.now, got, check.want)
		}
	}
}

func TestTheSpentMessageNamesTheFirstOfTheNextMonth(t *testing.T) {
	for month, want := range map[string]string{
		"2026-11": "Uploaded bots have used this month's free compute. They're back on 1 December.",
		"2026-12": "Uploaded bots have used this month's free compute. They're back on 1 January.",
	} {
		if got := services.BotComputeSpentMessage(month); got != want {
			t.Errorf("month %s: %q, want %q", month, got, want)
		}
	}
}

// countingMeter reads used GB-seconds and counts its reads.
type countingMeter struct {
	used   float64
	err    error
	reads  int
	months []time.Time
}

func (meter *countingMeter) MonthToDate(_ context.Context, month, _ time.Time) (float64, error) {
	meter.reads++
	meter.months = append(meter.months, month)
	return meter.used, meter.err
}

func TestTheGuardComparesThisMonthsComputeWithTheBudget(t *testing.T) {
	now := time.Date(2026, time.November, 13, 10, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	for _, check := range []struct {
		used      float64
		available bool
	}{{0, true}, {299_999.99, true}, {300_000, false}, {1_000_000, false}} {
		guard := &services.BotComputeGuard{Meter: services.FixedBotCompute(check.used), BudgetGBSeconds: 300_000, Now: clock}
		usage, err := guard.Usage(context.Background())
		want := models.BotComputeView{Month: "2026-11", UsedGBSeconds: check.used, BudgetGBSeconds: 300_000, Available: check.available}
		if err != nil || usage != want {
			t.Errorf("used %v: %+v, %v, want %+v", check.used, usage, err, want)
		}
	}
	// Without a meter, as on a local server, the guard is off.
	off, err := (&services.BotComputeGuard{BudgetGBSeconds: 0, Now: clock}).Usage(context.Background())
	if err != nil || !off.Available || off.UsedGBSeconds != 0 {
		t.Errorf("without a meter: %+v, %v", off, err)
	}
	// A failed read is an error, not a verdict.
	failing := &services.BotComputeGuard{Meter: &countingMeter{err: errors.New("throttled")}, BudgetGBSeconds: 300_000, Now: clock}
	if _, err := failing.Usage(context.Background()); err == nil {
		t.Error("a failed read was a verdict")
	}
}

// The cache reads the meter at most once per 10 minutes, for every instance
// that shares the database, and again in a new month, keeping one row.
func TestTheComputeIsReadAtMostOncePerTenMinutes(t *testing.T) {
	database := testutils.InitTestDB(t)
	meter := &countingMeter{used: 1_200}
	now := time.Date(2026, time.November, 30, 23, 45, 0, 0, time.UTC)
	guard := func() *services.BotComputeGuard {
		// A new guard for each read, as each Lambda instance has its own.
		return &services.BotComputeGuard{Meter: services.CachedBotCompute{Database: database, Meter: meter}, BudgetGBSeconds: 300_000, Now: func() time.Time { return now }}
	}
	read := func(want float64, reads int) {
		t.Helper()
		usage, err := guard().Usage(context.Background())
		if err != nil || usage.UsedGBSeconds != want || meter.reads != reads {
			t.Fatalf("at %v: %+v, %v after %d reads, want %v after %d", now, usage, err, meter.reads, want, reads)
		}
	}
	read(1_200, 1)
	meter.used = 2_400
	now = now.Add(9*time.Minute + 59*time.Second)
	read(1_200, 1)
	now = now.Add(time.Second)
	read(2_400, 2)
	// 23:59: still cached from 23:55.
	now = now.Add(4 * time.Minute)
	read(2_400, 2)
	// The new month reads again at once, from its first day.
	meter.used = 3
	now = time.Date(2026, time.December, 1, 0, 0, 1, 0, time.UTC)
	read(3, 3)
	if got := meter.months[2]; !got.Equal(time.Date(2026, time.December, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("read the month of %v", got)
	}
	var rows []models.BotComputeUsage
	if err := database.Find(&rows).Error; err != nil || len(rows) != 1 || rows[0].Month.Format("2006-01-02") != "2026-12-01" || rows[0].GBSeconds != 3 {
		t.Fatalf("cache rows %+v, %v", rows, err)
	}
	// A failed read keeps nothing, so the next request reads again.
	meter.err = errors.New("throttled")
	now = now.Add(10 * time.Minute)
	if _, err := guard().Usage(context.Background()); err == nil || meter.reads != 4 {
		t.Fatalf("a failed read: %v after %d reads", err, meter.reads)
	}
	meter.err = nil
	read(3, 5)
}
