package observability

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// The Lambda host's environment. Terraform sets all four, and refuses them in
// deploy/local/lambda.env, so they can't drift from the reserved concurrency.
const (
	ConcurrencyEnv       = "ISLAND_BATTLES_CONCURRENCY"
	LogLinesPerMinuteEnv = "ISLAND_BATTLES_LOG_LINES_PER_MINUTE"
	LogLineBytesEnv      = "ISLAND_BATTLES_LOG_LINE_BYTES"
	TraceIntervalEnv     = "ISLAND_BATTLES_TRACE_INTERVAL"
)

// LoadSettings reads the settings, with the defaults for any that are unset,
// and checks them against the budget.
func LoadSettings(lookup func(string) (string, bool)) (Settings, error) {
	settings := DefaultSettings()
	var err error
	if settings.Concurrency, err = readWholeNumber(lookup, ConcurrencyEnv, settings.Concurrency); err != nil {
		return Settings{}, err
	}
	if settings.LogLinesPerMinute, err = readWholeNumber(lookup, LogLinesPerMinuteEnv, settings.LogLinesPerMinute); err != nil {
		return Settings{}, err
	}
	if settings.LogLineBytes, err = readWholeNumber(lookup, LogLineBytesEnv, settings.LogLineBytes); err != nil {
		return Settings{}, err
	}
	if value := read(lookup, TraceIntervalEnv); value != "" {
		if settings.TraceInterval, err = time.ParseDuration(value); err != nil {
			return Settings{}, fmt.Errorf("%s must be a duration such as 180s", TraceIntervalEnv)
		}
	}
	if err := settings.Validate(); err != nil {
		return Settings{}, fmt.Errorf("%s, %s, %s and %s: %w", ConcurrencyEnv, LogLinesPerMinuteEnv, LogLineBytesEnv, TraceIntervalEnv, err)
	}
	return settings, nil
}

func read(lookup func(string) (string, bool), name string) string {
	value, _ := lookup(name)
	return strings.TrimSpace(value)
}

func readWholeNumber(lookup func(string) (string, bool), name string, fallback int) (int, error) {
	value := read(lookup, name)
	if value == "" {
		return fallback, nil
	}
	number, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a whole number", name)
	}
	return number, nil
}
