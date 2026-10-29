package observability

import (
	"strings"
	"testing"
	"time"
)

// The defaults must fit the budget with room to spare, and match the issue's
// example: 5 instances, 5 lines of 1 KB a minute, a trace every 3 minutes.
func TestTheDefaultsFitTheBudget(t *testing.T) {
	settings := DefaultSettings()
	if settings != (Settings{Concurrency: 5, LogLinesPerMinute: 5, LogLineBytes: 1024, TraceInterval: 180 * time.Second}) {
		t.Fatalf("defaults = %+v", settings)
	}
	if err := settings.Validate(); err != nil {
		t.Fatal(err)
	}
	// 5 × 44,640 minutes × (5 × (1,024 + 128) + 256 + 128) bytes.
	if logs := settings.MonthlyLogBytes(); logs != 1_371_340_800 || logs > MonthlyLogBudgetBytes {
		t.Fatalf("monthly logs = %d", logs)
	}
	// 5 × 2,678,400 seconds / 180 seconds.
	if traces := settings.MonthlyTraces(); traces != 74_400 || traces > MonthlyTraceBudget {
		t.Fatalf("monthly traces = %d", traces)
	}
	loaded, err := LoadSettings(func(string) (string, bool) { return "", false })
	if err != nil || loaded != settings {
		t.Fatalf("LoadSettings() without settings = %+v, %v", loaded, err)
	}
}

func TestSettingsOverTheBudgetAreRefused(t *testing.T) {
	for name, test := range map[string]struct {
		settings Settings
		want     string
	}{
		"logs":                  {Settings{Concurrency: 5, LogLinesPerMinute: 6, LogLineBytes: 1024, TraceInterval: 180 * time.Second}, "bytes of logs a month"},
		"traces":                {Settings{Concurrency: 5, LogLinesPerMinute: 5, LogLineBytes: 1024, TraceInterval: 160 * time.Second}, "traces a month"},
		"concurrency":           {Settings{Concurrency: 10, LogLinesPerMinute: 5, LogLineBytes: 1024, TraceInterval: 180 * time.Second}, "over the budget"},
		"one line a minute":     {Settings{Concurrency: 1, LogLinesPerMinute: 1, LogLineBytes: 1024, TraceInterval: time.Hour}, "at least 2"},
		"short lines":           {Settings{Concurrency: 1, LogLinesPerMinute: 2, LogLineBytes: 100, TraceInterval: time.Hour}, "at least 512"},
		"no concurrency":        {Settings{Concurrency: 0, LogLinesPerMinute: 2, LogLineBytes: 1024, TraceInterval: time.Hour}, "at least 1"},
		"interval under second": {Settings{Concurrency: 1, LogLinesPerMinute: 2, LogLineBytes: 1024, TraceInterval: time.Millisecond}, "at least 1s"},
	} {
		if err := test.settings.Validate(); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: Validate() = %v, want %q", name, err, test.want)
		}
	}
	// A 10-instance account limit fits with fewer lines and a longer interval.
	fitting := Settings{Concurrency: 10, LogLinesPerMinute: 2, LogLineBytes: 1024, TraceInterval: 360 * time.Second}
	if err := fitting.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadSettingsReadsTheEnvironment(t *testing.T) {
	values := map[string]string{ConcurrencyEnv: "10", LogLinesPerMinuteEnv: "2", LogLineBytesEnv: "1024", TraceIntervalEnv: "6m"}
	lookup := func(name string) (string, bool) { value, ok := values[name]; return value, ok }
	settings, err := LoadSettings(lookup)
	if err != nil || settings != (Settings{Concurrency: 10, LogLinesPerMinute: 2, LogLineBytes: 1024, TraceInterval: 6 * time.Minute}) {
		t.Fatalf("LoadSettings() = %+v, %v", settings, err)
	}
	values[TraceIntervalEnv] = "360"
	if _, err := LoadSettings(lookup); err == nil || !strings.Contains(err.Error(), TraceIntervalEnv) {
		t.Fatalf("LoadSettings() with an interval without a unit = %v", err)
	}
	values[TraceIntervalEnv], values[ConcurrencyEnv] = "6m", "ten"
	if _, err := LoadSettings(lookup); err == nil || !strings.Contains(err.Error(), ConcurrencyEnv) {
		t.Fatalf("LoadSettings() with a concurrency that isn't a number = %v", err)
	}
}
