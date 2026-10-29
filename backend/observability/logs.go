package observability

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"sync"
	"time"
	"unicode/utf8"
)

// TimestampKey replaces slog's "time" key. Lambda's log-level filtering
// reads "level" and an RFC 3339 "timestamp"; a line without a valid
// timestamp counts as INFO, which a WARN filter drops.
const TimestampKey = "timestamp"

// Keys that a cut line keeps.
const (
	lineBytesKey     = "line_bytes"
	truncatedLineKey = "truncated_line"
)

// cutMessageBytes bounds the message of a cut line.
const cutMessageBytes = 160

// LimitedHandler writes JSON lines for Lambda, within Settings' limits:
//   - It writes nothing below its level, WARN on Lambda.
//   - It writes at most N lines in each minute. Warnings may use N-1, so one
//     is left for an error. A line over the limit is dropped and counted.
//   - Once a minute with dropped lines has passed, the next line, or the next
//     call to Flush, first writes one line that counts them. It is at ERROR
//     when an error was dropped, so the error metric still sees it.
//   - A line over L bytes is cut: it keeps its timestamp, level, a shortened
//     message and its trace IDs, and the start of the full line as a string.
//
// One instance of the function has one LimitedHandler. Handlers made with
// WithAttrs and WithGroup share its limits.
type LimitedHandler struct {
	state *limiterState
	// encoder renders a record with this handler's attributes and groups.
	encoder slog.Handler
}

type limiterState struct {
	mutex        sync.Mutex
	output       io.Writer
	level        slog.Leveler
	linesPerMin  int
	lineBytes    int
	now          func() time.Time
	buffer       bytes.Buffer
	plain        slog.Handler
	windowStart  time.Time
	written      int
	droppedWarns int
	droppedErrs  int
}

// NewLimitedHandler writes to output. now is time.Now outside tests.
func NewLimitedHandler(output io.Writer, level slog.Leveler, settings Settings, now func() time.Time) *LimitedHandler {
	state := &limiterState{output: output, level: level, linesPerMin: settings.LogLinesPerMinute, lineBytes: settings.LogLineBytes, now: now}
	options := &slog.HandlerOptions{Level: slog.LevelDebug, ReplaceAttr: lambdaTimestamp}
	state.plain = slog.NewJSONHandler(&state.buffer, options)
	return &LimitedHandler{state: state, encoder: state.plain}
}

// NewLambdaLogger is the logger the Lambda host uses: WARN and above, within
// the settings' limits, with trace IDs on lines from sampled requests.
func NewLambdaLogger(output io.Writer, settings Settings) (*slog.Logger, *LimitedHandler) {
	handler := NewLimitedHandler(output, slog.LevelWarn, settings, time.Now)
	return slog.New(WithTraceIDs(handler)), handler
}

// lambdaTimestamp writes slog's time as "timestamp" in UTC RFC 3339.
func lambdaTimestamp(groups []string, attr slog.Attr) slog.Attr {
	if len(groups) == 0 && attr.Key == slog.TimeKey && attr.Value.Kind() == slog.KindTime {
		return slog.String(TimestampKey, attr.Value.Time().UTC().Format(time.RFC3339Nano))
	}
	return attr
}

func (handler *LimitedHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= handler.state.level.Level()
}

func (handler *LimitedHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &LimitedHandler{state: handler.state, encoder: handler.encoder.WithAttrs(attrs)}
}

func (handler *LimitedHandler) WithGroup(name string) slog.Handler {
	return &LimitedHandler{state: handler.state, encoder: handler.encoder.WithGroup(name)}
}

func (handler *LimitedHandler) Handle(ctx context.Context, record slog.Record) error {
	state := handler.state
	state.mutex.Lock()
	defer state.mutex.Unlock()
	if err := state.roll(ctx); err != nil {
		return err
	}
	limit := state.linesPerMin
	if record.Level < slog.LevelError {
		limit--
	}
	if state.written >= limit {
		if record.Level >= slog.LevelError {
			state.droppedErrs++
		} else {
			state.droppedWarns++
		}
		return nil
	}
	line, err := state.render(ctx, handler.encoder, record)
	if err != nil {
		return err
	}
	if len(line) > state.lineBytes {
		if line, err = state.cut(ctx, record, line); err != nil {
			return err
		}
	}
	state.written++
	_, err = state.output.Write(line)
	return err
}

// Flush writes the dropped-lines line if a minute with dropped lines has
// passed. The Lambda host calls it after every request, so the count doesn't
// wait for the next log line.
func (handler *LimitedHandler) Flush() error {
	state := handler.state
	state.mutex.Lock()
	defer state.mutex.Unlock()
	return state.roll(context.Background())
}

// roll starts a new window once a minute has passed since the last one
// started, and first reports what the last one dropped.
func (state *limiterState) roll(ctx context.Context) error {
	now := state.now()
	if !state.windowStart.IsZero() && now.Sub(state.windowStart) < time.Minute {
		return nil
	}
	dropped := state.droppedWarns + state.droppedErrs
	errorsDropped := state.droppedErrs
	state.windowStart, state.written, state.droppedWarns, state.droppedErrs = now, 0, 0, 0
	if dropped == 0 {
		return nil
	}
	level := slog.LevelWarn
	if errorsDropped > 0 {
		level = slog.LevelError
	}
	record := slog.NewRecord(now, level, "Dropped log lines over this instance's limit", 0)
	record.AddAttrs(slog.Int("dropped_lines", dropped), slog.Int("dropped_errors", errorsDropped), slog.Int("lines_per_minute", state.linesPerMin))
	line, err := state.render(ctx, state.plain, record)
	if err != nil {
		return err
	}
	_, err = state.output.Write(line)
	return err
}

// render encodes one record into a fresh copy of its line.
func (state *limiterState) render(ctx context.Context, encoder slog.Handler, record slog.Record) ([]byte, error) {
	state.buffer.Reset()
	if err := encoder.Handle(ctx, record); err != nil {
		return nil, err
	}
	return bytes.Clone(state.buffer.Bytes()), nil
}

// cut shortens a line to the limit. It keeps the record's trace IDs and as
// much of the full line as fits, quoted as a string; escaping makes the
// quoted part longer than the bytes it holds, so it shrinks until it fits.
func (state *limiterState) cut(ctx context.Context, record slog.Record, full []byte) ([]byte, error) {
	message := truncate(record.Message, cutMessageBytes)
	traceAttrs := traceAttrsOf(record)
	keep := state.lineBytes
	for range 8 {
		cut := slog.NewRecord(record.Time, record.Level, message, record.PC)
		cut.AddAttrs(traceAttrs...)
		cut.AddAttrs(slog.Int(lineBytesKey, len(full)), slog.String(truncatedLineKey, truncate(string(full), keep)))
		line, err := state.render(ctx, state.plain, cut)
		if err != nil || len(line) <= state.lineBytes {
			return line, err
		}
		keep -= len(line) - state.lineBytes + 16
		if keep <= 0 {
			break
		}
	}
	// Nothing of the line fits beside a message that escapes badly.
	cut := slog.NewRecord(record.Time, record.Level, "Log line over the size limit", record.PC)
	cut.AddAttrs(traceAttrs...)
	cut.AddAttrs(slog.Int(lineBytesKey, len(full)))
	return state.render(ctx, state.plain, cut)
}

func traceAttrsOf(record slog.Record) []slog.Attr {
	var attrs []slog.Attr
	record.Attrs(func(attr slog.Attr) bool {
		if attr.Key == TraceIDKey || attr.Key == XRayTraceIDKey {
			attrs = append(attrs, attr)
		}
		return true
	})
	return attrs
}

// truncate cuts text to at most limit bytes at a rune boundary.
func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	for limit > 0 && !utf8.RuneStart(text[limit]) {
		limit--
	}
	return text[:limit]
}
