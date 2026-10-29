package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace"
)

type clock struct{ now time.Time }

func (clock *clock) Now() time.Time { return clock.now }

func newTestLogger(settings Settings) (*slog.Logger, *LimitedHandler, *bytes.Buffer, *clock) {
	var output bytes.Buffer
	now := &clock{now: time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)}
	handler := NewLimitedHandler(&output, slog.LevelWarn, settings, now.Now)
	return slog.New(WithTraceIDs(handler)), handler, &output, now
}

func lines(t *testing.T, output *bytes.Buffer) []map[string]any {
	t.Helper()
	var parsed []map[string]any
	for _, line := range strings.Split(strings.TrimSuffix(output.String(), "\n"), "\n") {
		if line == "" {
			continue
		}
		var fields map[string]any
		if err := json.Unmarshal([]byte(line), &fields); err != nil {
			t.Fatalf("line is not JSON: %q", line)
		}
		parsed = append(parsed, fields)
	}
	output.Reset()
	return parsed
}

// Each minute holds at most N lines, warnings at most N-1, and the lines over
// it come back as one line that counts them, at ERROR when an error was
// dropped.
func TestTheHandlerWritesAtMostNLinesAMinute(t *testing.T) {
	logger, handler, output, now := newTestLogger(Settings{LogLinesPerMinute: 3, LogLineBytes: 1024})
	logger.Info("Not written below WARN")
	for range 5 {
		logger.Warn("Slow query")
	}
	logger.Error("Request failed")
	logger.Error("Request failed again")
	logger.Error("Dropped error")
	written := lines(t, output)
	if len(written) != 3 || written[0]["level"] != "WARN" || written[1]["level"] != "WARN" || written[2]["msg"] != "Request failed" {
		t.Fatalf("first minute = %v", written)
	}
	// Flush within the minute writes nothing yet.
	if err := handler.Flush(); err != nil || output.Len() != 0 {
		t.Fatalf("Flush() within the minute wrote %q, %v", output.String(), err)
	}
	now.now = now.now.Add(time.Minute)
	if err := handler.Flush(); err != nil {
		t.Fatal(err)
	}
	written = lines(t, output)
	if len(written) != 1 || written[0]["level"] != "ERROR" || written[0]["dropped_lines"] != float64(5) || written[0]["dropped_errors"] != float64(2) {
		t.Fatalf("dropped line = %v", written)
	}
	// The new minute has room again, and only warnings dropped makes a WARN.
	for range 3 {
		logger.Warn("Slow query")
	}
	now.now = now.now.Add(time.Minute)
	logger.Error("Next minute")
	written = lines(t, output)
	if len(written) != 4 || written[2]["level"] != "WARN" || written[2]["dropped_lines"] != float64(1) || written[3]["msg"] != "Next minute" {
		t.Fatalf("second and third minutes = %v", written)
	}
}

// The dropped-lines line must fit the size the budget counts for it.
func TestTheDroppedLineFitsItsBudget(t *testing.T) {
	logger, handler, output, now := newTestLogger(Settings{LogLinesPerMinute: 2, LogLineBytes: 1024})
	for range 1_000_000 {
		logger.Error("Request failed")
	}
	output.Reset()
	now.now = now.now.Add(time.Minute + 999*time.Millisecond + 999*time.Microsecond)
	if err := handler.Flush(); err != nil {
		t.Fatal(err)
	}
	if output.Len() > DroppedLineBytes || !strings.Contains(output.String(), `"dropped_lines":999998`) {
		t.Fatalf("dropped line is %d bytes: %s", output.Len(), output.String())
	}
}

// A long line is cut to L bytes and stays JSON with its level, timestamp,
// message and trace IDs, so Lambda's filter and the error metric still read it.
func TestLongLinesAreCutAndStayJSON(t *testing.T) {
	logger, _, output, _ := newTestLogger(Settings{LogLinesPerMinute: 10, LogLineBytes: MinLogLineBytes})
	ctx := sampledContext()
	for name, value := range map[string]string{
		"plain text":       strings.Repeat("stack frame ", 1000),
		"escapes":          strings.Repeat("\"\x01<>&", 1000),
		"multi-byte runes": strings.Repeat("é世", 1000),
	} {
		logger.ErrorContext(ctx, strings.Repeat("panic: ", 100)+name, "stack", value)
		raw := output.String()
		written := lines(t, output)
		if len(raw) > MinLogLineBytes || len(written) != 1 {
			t.Fatalf("%s: cut line is %d bytes: %s", name, len(raw), raw)
		}
		line := written[0]
		if line["level"] != "ERROR" || line[TraceIDKey] != "0af7651916cd43dd8448eb211c80319c" || line[XRayTraceIDKey] != "1-0af76519-16cd43dd8448eb211c80319c" || line[lineBytesKey] == nil {
			t.Fatalf("%s: cut line lost its fields: %v", name, line)
		}
		if _, err := time.Parse(time.RFC3339Nano, line[TimestampKey].(string)); err != nil {
			t.Fatalf("%s: timestamp = %v", name, line[TimestampKey])
		}
	}
}

func TestLinesFromUnsampledSpansCarryNoTraceIDs(t *testing.T) {
	logger, _, output, _ := newTestLogger(DefaultSettings())
	unsampled := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{1}})
	logger.ErrorContext(trace.ContextWithSpanContext(context.Background(), unsampled), "Request failed")
	logger.ErrorContext(sampledContext(), "Request failed")
	written := lines(t, output)
	if written[0][TraceIDKey] != nil || written[1][TraceIDKey] == nil {
		t.Fatalf("lines = %v", written)
	}
}

func sampledContext() context.Context {
	traceID, _ := trace.TraceIDFromHex("0af7651916cd43dd8448eb211c80319c")
	spanID, _ := trace.SpanIDFromHex("b7ad6b7169203331")
	span := trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled})
	return trace.ContextWithSpanContext(context.Background(), span)
}
