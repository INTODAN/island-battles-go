// Package observability keeps the Lambda host's logs and traces inside AWS's
// free monthly allowances (#46), and gives both hosts their request logs,
// error lines and traces.
//
// The limits are per Lambda instance. With the function's concurrency capped
// at C instances, the worst case for a whole month follows from the settings
// alone, and the Lambda host refuses to serve with settings whose worst case
// is over the budget. deploy/README.md has the full budget, including the
// sources that these settings don't cover.
package observability

import (
	"fmt"
	"time"
)

// The monthly budget (#46). CloudWatch Logs' free 5 GB a month is shared by
// ingestion, storage and Logs Insights scans, so ingestion gets 1.5 GB of it.
// X-Ray records 100,000 traces a month for free, so traces get 80,000.
const (
	MonthlyLogBudgetBytes = 1_500_000_000
	MonthlyTraceBudget    = 80_000
)

// The worst case assumes a 31-day month, the longest there is.
const (
	MinutesPerMonth = 31 * 24 * 60
	SecondsPerMonth = MinutesPerMonth * 60
)

// LineOverheadBytes is what each log line may cost beyond its own bytes.
// CloudWatch counts 26 bytes per event in PutLogEvents' size, and the rest is
// room for anything Lambda adds to a line.
const LineOverheadBytes = 128

// DroppedLineBytes bounds the line that reports the lines a window dropped.
// The handler writes at most one of them a minute.
const DroppedLineBytes = 256

// MinLogLineBytes is the smallest line limit. A cut line keeps its time,
// level, message and trace IDs, which need some room.
const MinLogLineBytes = 512

// Settings are the limits behind the budget. The Lambda host reads them from
// its environment, and Terraform sets them, Concurrency from the same value
// as the function's reserved concurrency (deploy/terraform/lambda.tf).
type Settings struct {
	// Concurrency is C, the most instances of the function that run at once.
	Concurrency int
	// LogLinesPerMinute is N, the most warning and error lines one instance
	// writes a minute. Warnings may use N-1 of them, so one is left for an
	// error.
	LogLinesPerMinute int
	// LogLineBytes is L, the most bytes of one line. Longer lines are cut.
	LogLineBytes int
	// TraceInterval is T. One instance starts at most one new trace every T,
	// and its first one T after it starts.
	TraceInterval time.Duration
}

// DefaultSettings fit the budget: 5 instances writing 5 lines of 1 KB a minute
// come to at most 1.37 GB in a 31-day month, and a trace every 3 minutes on
// each comes to at most 74,400 traces.
func DefaultSettings() Settings {
	return Settings{Concurrency: 5, LogLinesPerMinute: 5, LogLineBytes: 1024, TraceInterval: 180 * time.Second}
}

// MonthlyLogBytes is the most the instances can write in a month: every
// instance writes its N lines of L bytes and one dropped-lines line every
// minute.
func (settings Settings) MonthlyLogBytes() int64 {
	perMinute := int64(settings.LogLinesPerMinute)*int64(settings.LogLineBytes+LineOverheadBytes) + DroppedLineBytes + LineOverheadBytes
	return int64(settings.Concurrency) * MinutesPerMonth * perMinute
}

// MonthlyTraces is the most traces the instances can start in a month,
// C × seconds per month / T, rounded up.
func (settings Settings) MonthlyTraces() int64 {
	if settings.TraceInterval <= 0 {
		return 0
	}
	month := int64(settings.Concurrency) * SecondsPerMonth * int64(time.Second)
	interval := int64(settings.TraceInterval)
	return (month + interval - 1) / interval
}

// Validate checks each setting and the budget.
func (settings Settings) Validate() error {
	switch {
	case settings.Concurrency < 1:
		return fmt.Errorf("the concurrency must be at least 1")
	case settings.LogLinesPerMinute < 2:
		return fmt.Errorf("the log lines a minute must be at least 2, so an error always has room")
	case settings.LogLineBytes < MinLogLineBytes:
		return fmt.Errorf("the log line limit must be at least %d bytes", MinLogLineBytes)
	case settings.TraceInterval < time.Second:
		return fmt.Errorf("the trace interval must be at least 1s")
	}
	if logs := settings.MonthlyLogBytes(); logs > MonthlyLogBudgetBytes {
		return fmt.Errorf("%d instances writing %d lines of %d bytes a minute come to %d bytes of logs a month, over the budget of %d; lower one of them",
			settings.Concurrency, settings.LogLinesPerMinute, settings.LogLineBytes, logs, MonthlyLogBudgetBytes)
	}
	if traces := settings.MonthlyTraces(); traces > MonthlyTraceBudget {
		return fmt.Errorf("%d instances starting a trace every %s come to %d traces a month, over the budget of %d; lower the concurrency or raise the interval",
			settings.Concurrency, settings.TraceInterval, traces, MonthlyTraceBudget)
	}
	return nil
}
