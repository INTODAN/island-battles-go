package observability

import (
	"context"
	"testing"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func sample(sampler sdktrace.Sampler, parent context.Context) sdktrace.SamplingDecision {
	return sampler.ShouldSample(sdktrace.SamplingParameters{ParentContext: parent, Name: "request"}).Decision
}

// An instance starts its first trace one interval after it starts, and at
// most one every interval after that, so C instances start at most
// C × seconds per month / T traces whatever the traffic.
func TestTheSamplerStartsAtMostOneTraceAnInterval(t *testing.T) {
	now := &clock{now: time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)}
	sampler := NewIntervalSampler(3*time.Minute, now.Now)
	if sample(sampler, context.Background()) != sdktrace.Drop {
		t.Fatal("sampled a trace right after starting")
	}
	now.now = now.now.Add(3 * time.Minute)
	if sample(sampler, context.Background()) != sdktrace.RecordAndSample {
		t.Fatal("didn't sample once the interval passed")
	}
	sampled := 0
	for range 1000 {
		now.now = now.now.Add(time.Second)
		if sample(sampler, context.Background()) == sdktrace.RecordAndSample {
			sampled++
		}
	}
	// 1,000 seconds hold 5 full intervals of 180 seconds.
	if sampled != 5 {
		t.Fatalf("sampled %d traces in 1,000 seconds", sampled)
	}
}

// Spans inside a trace follow it, and a caller's trace header can't make the
// instance record a trace: a remote parent is ignored.
func TestTheSamplerFollowsLocalParentsAndIgnoresRemoteOnes(t *testing.T) {
	now := &clock{now: time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)}
	sampler := NewIntervalSampler(time.Hour, now.Now)
	local := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{1}, TraceFlags: trace.FlagsSampled})
	if sample(sampler, trace.ContextWithSpanContext(context.Background(), local)) != sdktrace.RecordAndSample {
		t.Fatal("dropped a span inside a sampled trace")
	}
	if sample(sampler, trace.ContextWithSpanContext(context.Background(), local.WithTraceFlags(0))) != sdktrace.Drop {
		t.Fatal("sampled a span inside an unsampled trace")
	}
	if sample(sampler, trace.ContextWithRemoteSpanContext(context.Background(), local)) != sdktrace.Drop {
		t.Fatal("a sampled remote parent started a trace")
	}
}

func TestTracingIsOffWithoutAnEndpoint(t *testing.T) {
	tracing, err := StartTracing(context.Background(), func(string) (string, bool) { return "", false }, "test", sdktrace.AlwaysSample())
	if err != nil || tracing.Enabled() {
		t.Fatalf("StartTracing() without %s = %v, %v", EndpointEnv, tracing.Enabled(), err)
	}
}
