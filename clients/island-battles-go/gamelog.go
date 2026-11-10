package islandbattles

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// SDKLogger is the logger name of the lines the SDK itself writes, such as
// the error of a failed turn or the game log limit.
const SDKLogger = "island_battles.sdk"

// Logger is the bot's logger for the turn that ctx belongs to, a standard
// log/slog logger named PlayerLogger. While the bot plays a turn under run
// or serve, every line it logs at or above the log level goes to the game
// log, with the server's limits applied, and to the console and log file.
// The lines in Result.Logs follow the lines the bot logged.
//
// Levels map onto the game log's: slog.LevelDebug is debug, slog.LevelInfo
// info, slog.LevelWarn warning and slog.LevelError error. Attributes are
// written after the message as key=value.
//
// Outside a turn, for example in a goroutine that outlives it, lines go to
// the console and file only; without a turn at all, as under check, Logger
// returns slog.Default().
func Logger(ctx context.Context) *slog.Logger {
	if turn, found := ctx.Value(turnLogKey{}).(*turnLog); found {
		return turn.logger
	}
	return slog.Default()
}

type turnLogKey struct{}

// gameLog is one generation's game log lines that have not been sent yet.
//
// Lines are sent as they come until the player has 99 stored or pending,
// counting the lines an earlier run already stored. The next line is held
// back until the turn's last request: it is sent if no more lines came, and
// otherwise replaced by an error that counts every dropped line, so the
// player never passes the server's limit. With 100 lines already stored,
// nothing more is sent.
type gameLog struct {
	mutex        sync.Mutex
	generationID uint
	pending      []LogLine
	accepted     int
	held         *LogLine
	dropped      int
	sealed       bool
}

func newGameLog(generationID uint, stored int) *gameLog {
	return &gameLog{generationID: generationID, accepted: stored}
}

// add keeps a line for the next request, and returns true when it is the
// first line dropped.
func (log *gameLog) add(line LogLine) bool {
	log.mutex.Lock()
	defer log.mutex.Unlock()
	if log.sealed {
		return false
	}
	if log.accepted < MaxLogLines-1 {
		log.pending = append(log.pending, line)
		log.accepted++
		return false
	}
	if log.accepted == MaxLogLines-1 && log.held == nil && log.dropped == 0 {
		log.held = &line
		return false
	}
	first := log.dropped == 0
	// The held line is dropped with the first line past the limit.
	if log.held != nil {
		log.dropped += 2
	} else {
		log.dropped++
	}
	log.held = nil
	return first
}

// batch returns the lines for the next request, and how many pending lines
// they include. The turn's last request also carries the held line, or the
// limit error in its place when there is room for it. Nothing is removed
// until commit, so a rejected request leaves the lines for the next one.
func (log *gameLog) batch(last bool) ([]LogLine, int) {
	log.mutex.Lock()
	defer log.mutex.Unlock()
	if log.sealed {
		return nil, 0
	}
	lines := slices.Clone(log.pending)
	count := len(lines)
	switch {
	case last && log.held != nil:
		lines = append(lines, *log.held)
	case last && log.dropped > 0 && log.accepted < MaxLogLines:
		lines = append(lines, LogLine{Level: LogError, Logger: SDKLogger, Message: limitError(log.generationID, log.dropped)})
	}
	return lines, count
}

// commit forgets the lines a request sent; after the last request, the log
// takes no more.
func (log *gameLog) commit(count int, last bool) {
	log.mutex.Lock()
	defer log.mutex.Unlock()
	log.pending = log.pending[min(count, len(log.pending)):]
	if last {
		log.sealLocked()
	}
}

// seal sends nothing more for this generation.
func (log *gameLog) seal() {
	log.mutex.Lock()
	defer log.mutex.Unlock()
	log.sealLocked()
}

func (log *gameLog) sealLocked() {
	log.sealed = true
	log.pending = nil
	log.held = nil
	log.dropped = 0
}

// limitError is the game log limit's error, with the final count of dropped
// lines once it is known (dropped >= 0).
func limitError(generationID uint, dropped int) string {
	rest := "the rest of this generation's lines are not stored"
	if dropped >= 0 {
		rest = fmt.Sprintf("%d lines were not stored", dropped)
	}
	return fmt.Sprintf("game log limit reached for generation %d: %d lines per generation; %s", generationID, MaxLogLines, rest)
}

// gameLogLevel maps a slog level onto a game log level.
func gameLogLevel(level slog.Level) LogLevel {
	switch {
	case level >= slog.LevelError:
		return LogError
	case level >= slog.LevelWarn:
		return LogWarning
	case level >= slog.LevelInfo:
		return LogInfo
	}
	return LogDebug
}

// logOutputs are where log records go besides the game log: the console and
// a log file, each optional, at or above a level.
type logOutputs struct {
	mutex   sync.Mutex
	level   slog.Level
	writers []io.Writer
}

// write writes one record as "2006-01-02 15:04:05,000 INFO name: message".
func (outputs *logOutputs) write(at time.Time, level slog.Level, name, message string) {
	if level < outputs.level {
		return
	}
	levelName := strings.ToUpper(string(gameLogLevel(level)))
	line := fmt.Sprintf("%s,%03d %s %s: %s\n", at.Format(time.DateTime), at.Nanosecond()/int(time.Millisecond), levelName, name, message)
	outputs.mutex.Lock()
	defer outputs.mutex.Unlock()
	for _, writer := range outputs.writers {
		_, _ = io.WriteString(writer, line)
	}
}

// turnLog is the logging of one turn: the bot's logger, which writes to the
// outputs and, while the turn is open, to the generation's game log.
type turnLog struct {
	gameLog *gameLog
	outputs *logOutputs
	logger  *slog.Logger
	open    atomic.Bool
}

func newTurnLog(log *gameLog, outputs *logOutputs) *turnLog {
	turn := &turnLog{gameLog: log, outputs: outputs}
	turn.logger = slog.New(&logHandler{outputs: outputs, turn: turn, name: PlayerLogger})
	turn.open.Store(true)
	return turn
}

// context returns ctx carrying the turn's logger, for Logger.
func (turn *turnLog) context(ctx context.Context) context.Context {
	return context.WithValue(ctx, turnLogKey{}, turn)
}

// close stops filing the bot's lines in the game log.
func (turn *turnLog) close() { turn.open.Store(false) }

// file adds a logged line to the game log, cut to the limit and made
// storable. The first dropped line writes the limit error to the outputs,
// never to the game log itself.
func (turn *turnLog) file(level slog.Level, name, message string) {
	if level < turn.outputs.level {
		return
	}
	message = cutMessage(cleanText(message))
	if message == "" {
		return
	}
	if utf8.RuneCountInString(name) > MaxLoggerLength {
		name = string([]rune(name)[:MaxLoggerLength])
	}
	line := LogLine{Level: gameLogLevel(level), Logger: cleanText(name), Message: message}
	if turn.gameLog.add(line) {
		turn.outputs.write(time.Now(), slog.LevelError, SDKLogger, limitError(turn.gameLog.generationID, -1))
	}
}

// sdkLog writes one of the SDK's own lines to the outputs and the game log,
// also after the bot returned.
func (turn *turnLog) sdkLog(level slog.Level, message string) {
	turn.outputs.write(time.Now(), level, SDKLogger, message)
	turn.file(level, SDKLogger, message)
}

// addResult files the lines a result returns after those the bot logged.
func (turn *turnLog) addResult(result Result) {
	for _, line := range result.Logs {
		turn.gameLog.add(line)
	}
}

// logHandler is the slog handler of the bot's logger and the SDK's.
type logHandler struct {
	outputs *logOutputs
	// turn is nil for the SDK's own logger, which writes to the outputs only.
	turn *turnLog
	name string
	// attributes are the logger's attributes, formatted, and group its
	// open group's prefix.
	attributes string
	group      string
}

func (handler *logHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= handler.outputs.level
}

func (handler *logHandler) Handle(_ context.Context, record slog.Record) error {
	var message strings.Builder
	message.WriteString(record.Message)
	message.WriteString(handler.attributes)
	record.Attrs(func(attribute slog.Attr) bool {
		writeAttribute(&message, handler.group, attribute)
		return true
	})
	at := record.Time
	if at.IsZero() {
		at = time.Now()
	}
	handler.outputs.write(at, record.Level, handler.name, message.String())
	if handler.turn != nil && handler.turn.open.Load() {
		handler.turn.file(record.Level, handler.name, message.String())
	}
	return nil
}

func (handler *logHandler) WithAttrs(attributes []slog.Attr) slog.Handler {
	with := *handler
	var formatted strings.Builder
	formatted.WriteString(handler.attributes)
	for _, attribute := range attributes {
		writeAttribute(&formatted, handler.group, attribute)
	}
	with.attributes = formatted.String()
	return &with
}

func (handler *logHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return handler
	}
	with := *handler
	with.group = handler.group + name + "."
	return &with
}

// writeAttribute writes " key=value", quoting a value with spaces.
func writeAttribute(builder *strings.Builder, group string, attribute slog.Attr) {
	value := attribute.Value.Resolve()
	if attribute.Equal(slog.Attr{}) {
		return
	}
	if value.Kind() == slog.KindGroup {
		prefix := group
		if attribute.Key != "" {
			prefix += attribute.Key + "."
		}
		for _, member := range value.Group() {
			writeAttribute(builder, prefix, member)
		}
		return
	}
	text := value.String()
	if text == "" || strings.ContainsAny(text, " \t\n\"=") {
		text = strconv.Quote(text)
	}
	builder.WriteString(" " + group + attribute.Key + "=" + text)
}
