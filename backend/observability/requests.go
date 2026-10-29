package observability

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Path parameters that name what a request is about, and the keys their
// values get in its error line.
var pathIDKeys = map[string]string{
	"lobby_id": "lobby_id",
	"game_key": "game_key",
	// GET /game/{id}/graph
	"id": "game_id",
}

type requestKey struct{}

// request collects what a failed request's error line reports. Handlers add
// to it through Annotate and RecordError.
type request struct {
	mutex     sync.Mutex
	operation string
	attrs     []slog.Attr
	errors    []string
}

// Annotate adds IDs, such as the player, game or generation, to the error line
// that the request writes if it fails. Outside a request it does nothing.
func Annotate(ctx context.Context, attrs ...slog.Attr) {
	if current, ok := ctx.Value(requestKey{}).(*request); ok {
		current.mutex.Lock()
		defer current.mutex.Unlock()
		current.attrs = append(current.attrs, attrs...)
	}
}

// RecordError adds an error, with its whole chain, to the error line that the
// request writes if it fails. Handlers call it before turning an error into a
// response that drops the cause.
func RecordError(ctx context.Context, err error) {
	if err == nil {
		return
	}
	if current, ok := ctx.Value(requestKey{}).(*request); ok {
		current.addError(errorChain(err))
	}
}

// errorChain writes an error with everything it wraps. fmt.Errorf's messages
// already hold what they wrap, but errors such as the lobby's keep a safe
// message and hide their cause from it.
func errorChain(err error) string {
	var parts []string
	for current := err; current != nil; current = errors.Unwrap(current) {
		text := current.Error()
		if text == "" || len(parts) > 0 && strings.Contains(parts[len(parts)-1], text) {
			continue
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, ": ")
}

func (current *request) addError(message string) {
	current.mutex.Lock()
	defer current.mutex.Unlock()
	for _, recorded := range current.errors {
		if recorded == message {
			return
		}
	}
	current.errors = append(current.errors, message)
}

// NameOperations is a Huma API middleware that names each request's
// operation on its error line. Register it on the API before any operation,
// so that it runs before group middlewares such as authentication, which may
// answer without calling the handler.
func NameOperations(ctx huma.Context, next func(huma.Context)) {
	if current, ok := ctx.Context().Value(requestKey{}).(*request); ok {
		current.mutex.Lock()
		current.operation = ctx.Operation().OperationID
		current.mutex.Unlock()
	}
	next(ctx)
}

// RecordProblems is a Huma transformer. It records each 5xx problem with its
// cause, which a response may leave out. Middlewares that write their own
// problems, such as the lobby's authentication, skip it; their callers record
// the cause with RecordError.
func RecordProblems(ctx huma.Context, status string, value any) (any, error) {
	current, ok := ctx.Context().Value(requestKey{}).(*request)
	if !ok {
		return value, nil
	}
	code, err := strconv.Atoi(status)
	problem, isError := value.(error)
	if err != nil || code < http.StatusInternalServerError || !isError {
		return value, nil
	}
	current.addError(describeProblem(problem))
	return value, nil
}

// describeProblem writes a problem with what it wraps: Huma's error details
// hold the messages of the errors a handler passed, and other problems, such
// as the lobby's, unwrap to their cause.
func describeProblem(problem error) string {
	description := errorChain(problem)
	var model *huma.ErrorModel
	if errors.As(problem, &model) {
		for _, detail := range model.Errors {
			if detail != nil && detail.Message != "" {
				description += ": " + detail.Message
			}
		}
	}
	return description
}

// Requests logs every request once, after it is answered, and recovers its
// panics. A request that answers 5xx or panics gets one ERROR line with its
// operation, route, error chain and IDs, plus the panic and its stack. Other
// requests get an INFO line, which the Lambda host doesn't write. Client
// errors are INFO too: they are the client's to fix, and at WARN any caller
// could fill an instance's log limit with them.
func Requests(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(response http.ResponseWriter, httpRequest *http.Request) {
			current := &request{}
			ctx := context.WithValue(httpRequest.Context(), requestKey{}, current)
			httpRequest = httpRequest.WithContext(ctx)
			wrapped := middleware.NewWrapResponseWriter(response, httpRequest.ProtoMajor)
			start := time.Now()
			defer func() {
				recovered := recover()
				if errors.Is(asError(recovered), http.ErrAbortHandler) {
					// net/http's signal to abort the response quietly.
					panic(recovered)
				}
				if recovered != nil && wrapped.Status() == 0 {
					http.Error(wrapped, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				}
				logRequest(ctx, logger, httpRequest, wrapped, current, recovered, time.Since(start))
			}()
			next.ServeHTTP(wrapped, httpRequest)
		})
	}
}

func asError(value any) error {
	err, _ := value.(error)
	return err
}

func logRequest(ctx context.Context, logger *slog.Logger, httpRequest *http.Request, response middleware.WrapResponseWriter, current *request, recovered any, duration time.Duration) {
	status := response.Status()
	if status == 0 {
		status = http.StatusOK
	}
	route := ""
	var pathAttrs []slog.Attr
	if routing := chi.RouteContext(ctx); routing != nil {
		route = routing.RoutePattern()
		for index, key := range routing.URLParams.Keys {
			if name, ok := pathIDKeys[key]; ok && index < len(routing.URLParams.Values) {
				pathAttrs = append(pathAttrs, slog.String(name, routing.URLParams.Values[index]))
			}
		}
	}
	span := trace.SpanFromContext(ctx)
	if route != "" {
		span.SetName(httpRequest.Method + " " + route)
		span.SetAttributes(attribute.String("http.route", route))
	}
	attrs := []slog.Attr{
		slog.String("method", httpRequest.Method),
		slog.String("route", route),
		slog.String("path", httpRequest.URL.Path),
		slog.Int("status", status),
		slog.Int64("duration_ms", duration.Milliseconds()),
	}
	current.mutex.Lock()
	defer current.mutex.Unlock()
	failed := status >= http.StatusInternalServerError || recovered != nil
	if !failed {
		level := slog.LevelInfo
		if httpRequest.Method == http.MethodOptions {
			level = slog.LevelDebug
		}
		if len(current.errors) > 0 {
			attrs = append(attrs, slog.Any("errors", current.errors))
		}
		logger.LogAttrs(ctx, level, "Request served", attrs...)
		return
	}
	if current.operation != "" {
		attrs = append(attrs, slog.String("operation", current.operation))
	}
	attrs = append(attrs, pathAttrs...)
	attrs = append(attrs, current.attrs...)
	if len(current.errors) > 0 {
		attrs = append(attrs, slog.Any("errors", current.errors))
	}
	message := "Request failed"
	if recovered != nil {
		message = "Request panicked"
		panicText := fmt.Sprint(recovered)
		attrs = append(attrs, slog.String("panic", panicText), slog.String("stack", string(debug.Stack())))
		span.SetStatus(codes.Error, "panic: "+panicText)
	}
	logger.LogAttrs(ctx, slog.LevelError, message, attrs...)
}
