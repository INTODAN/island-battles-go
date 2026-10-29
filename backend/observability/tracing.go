package observability

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
)

// EndpointEnv turns tracing on. It is OpenTelemetry's standard variable, and
// the exporter reads the other standard OTEL_EXPORTER_OTLP_* variables too.
// On Lambda, Terraform points it at the collector layer on localhost. Locally,
// tracing stays off unless it is set.
const EndpointEnv = "OTEL_EXPORTER_OTLP_ENDPOINT"

// Log keys for the trace a line belongs to. X-Ray shows OpenTelemetry's trace
// IDs in its own format, which xray_trace_id holds for searching.
const (
	TraceIDKey     = "trace_id"
	XRayTraceIDKey = "xray_trace_id"
)

// exportTimeout bounds one export to the collector.
const exportTimeout = 2 * time.Second

// Tracing sends spans to an OTLP endpoint. The zero value, or nil, is off.
type Tracing struct {
	provider *sdktrace.TracerProvider
	spans    *requestSpans
}

// StartTracing sets the global tracer provider when EndpointEnv is set. Spans
// that the sampler keeps wait in memory until Flush exports them, inline.
// Nothing runs between requests: no batch processor goroutine, no timer, and
// no kept-alive connection to the collector, because a Lambda instance may
// freeze or end once a request returns. Nothing reads trace headers from
// requests or adds them to outgoing calls, so callers on the internet can't
// make the backend record a trace.
func StartTracing(ctx context.Context, lookup func(string) (string, bool), service string, sampler sdktrace.Sampler) (*Tracing, error) {
	if read(lookup, EndpointEnv) == "" {
		return &Tracing{}, nil
	}
	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithHTTPClient(&http.Client{Transport: &http.Transport{DisableKeepAlives: true}, Timeout: exportTimeout}),
		// One attempt: a retry would wait with backoff inside a request.
		otlptracehttp.WithRetry(otlptracehttp.RetryConfig{Enabled: false}),
	)
	if err != nil {
		return nil, err
	}
	spans := &requestSpans{exporter: exporter}
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sampler),
		sdktrace.WithSpanProcessor(spans),
		sdktrace.WithResource(resource.NewSchemaless(semconv.ServiceName(service))),
	)
	otel.SetTracerProvider(provider)
	// An empty composite reads and writes no headers.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator())
	return &Tracing{provider: provider, spans: spans}, nil
}

// Enabled reports whether spans are exported.
func (tracing *Tracing) Enabled() bool { return tracing != nil && tracing.provider != nil }

// Handler gives every request a span, named after its route once the router
// has matched it. After a request that recorded spans, it exports them before
// returning, because a Lambda instance may freeze or end once the request
// returns.
func (tracing *Tracing) Handler(next http.Handler) http.Handler {
	if !tracing.Enabled() {
		return next
	}
	traced := otelhttp.NewHandler(next, "HTTP request", otelhttp.WithTracerProvider(tracing.provider))
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		traced.ServeHTTP(response, request)
		tracing.Flush(request.Context())
	})
}

// Flush exports the spans recorded since the last flush, if there are any,
// before it returns.
func (tracing *Tracing) Flush(ctx context.Context) {
	if !tracing.Enabled() {
		return
	}
	flushContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), exportTimeout)
	defer cancel()
	if err := tracing.spans.ForceFlush(flushContext); err != nil {
		slog.WarnContext(ctx, "Spans could not be exported", "error", err)
	}
}

// Shutdown exports what is left and stops the exporter.
func (tracing *Tracing) Shutdown(ctx context.Context) error {
	if !tracing.Enabled() {
		return nil
	}
	return tracing.provider.Shutdown(ctx)
}

// HTTPTransport traces each call made through base, such as Centrifugo's API.
// With tracing off, the global provider records nothing.
func HTTPTransport(base http.RoundTripper, name string) http.RoundTripper {
	return otelhttp.NewTransport(base, otelhttp.WithSpanNameFormatter(func(_ string, request *http.Request) string {
		return name + " " + request.Method
	}))
}

// maxPendingSpans bounds the spans that wait for a flush. A trace with more,
// such as a request with thousands of queries, loses the rest.
const maxPendingSpans = 2048

// requestSpans keeps the sampled spans that ended and exports them, in the
// caller's goroutine, when the request flushes. Requests without any skip the
// export.
type requestSpans struct {
	exporter sdktrace.SpanExporter
	mutex    sync.Mutex
	ended    []sdktrace.ReadOnlySpan
}

func (spans *requestSpans) OnStart(context.Context, sdktrace.ReadWriteSpan) {}

func (spans *requestSpans) OnEnd(span sdktrace.ReadOnlySpan) {
	if !span.SpanContext().IsSampled() {
		return
	}
	spans.mutex.Lock()
	defer spans.mutex.Unlock()
	if len(spans.ended) < maxPendingSpans {
		spans.ended = append(spans.ended, span)
	}
}

func (spans *requestSpans) ForceFlush(ctx context.Context) error {
	spans.mutex.Lock()
	ended := spans.ended
	spans.ended = nil
	spans.mutex.Unlock()
	if len(ended) == 0 {
		return nil
	}
	return spans.exporter.ExportSpans(ctx, ended)
}

func (spans *requestSpans) Shutdown(ctx context.Context) error {
	return errors.Join(spans.ForceFlush(ctx), spans.exporter.Shutdown(ctx))
}

// IntervalSampler starts at most one new trace every interval, and the first
// one an interval after it is made. Spans inside a trace follow their local
// parent's decision. A remote parent is ignored, so only this instance
// decides what it records.
type IntervalSampler struct {
	mutex    sync.Mutex
	interval time.Duration
	next     time.Time
	now      func() time.Time
}

// NewIntervalSampler makes the Lambda host's sampler. now is time.Now outside
// tests.
func NewIntervalSampler(interval time.Duration, now func() time.Time) *IntervalSampler {
	return &IntervalSampler{interval: interval, next: now().Add(interval), now: now}
}

func (sampler *IntervalSampler) ShouldSample(parameters sdktrace.SamplingParameters) sdktrace.SamplingResult {
	parent := trace.SpanContextFromContext(parameters.ParentContext)
	if parent.IsValid() && !parent.IsRemote() {
		decision := sdktrace.Drop
		if parent.IsSampled() {
			decision = sdktrace.RecordAndSample
		}
		return sdktrace.SamplingResult{Decision: decision, Tracestate: parent.TraceState()}
	}
	sampler.mutex.Lock()
	defer sampler.mutex.Unlock()
	now := sampler.now()
	if now.Before(sampler.next) {
		return sdktrace.SamplingResult{Decision: sdktrace.Drop}
	}
	sampler.next = now.Add(sampler.interval)
	return sdktrace.SamplingResult{Decision: sdktrace.RecordAndSample}
}

func (sampler *IntervalSampler) Description() string {
	return "IntervalSampler{" + sampler.interval.String() + "}"
}

// WithTraceIDs adds the trace IDs to lines logged inside a sampled span.
// Unsampled requests have no trace to find, so their lines stay shorter.
func WithTraceIDs(handler slog.Handler) slog.Handler { return traceIDHandler{handler} }

type traceIDHandler struct{ slog.Handler }

func (handler traceIDHandler) Handle(ctx context.Context, record slog.Record) error {
	if span := trace.SpanContextFromContext(ctx); span.IsValid() && span.IsSampled() {
		record = record.Clone()
		id := span.TraceID().String()
		record.AddAttrs(slog.String(TraceIDKey, id), slog.String(XRayTraceIDKey, XRayTraceID(span.TraceID())))
	}
	return handler.Handler.Handle(ctx, record)
}

func (handler traceIDHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return traceIDHandler{handler.Handler.WithAttrs(attrs)}
}

func (handler traceIDHandler) WithGroup(name string) slog.Handler {
	return traceIDHandler{handler.Handler.WithGroup(name)}
}

// XRayTraceID writes a trace ID the way X-Ray shows it: 1-, the first 8 hex
// digits, -, and the other 24.
func XRayTraceID(id trace.TraceID) string {
	hex := id.String()
	return "1-" + hex[:8] + "-" + hex[8:]
}
