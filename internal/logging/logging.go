package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Mode controls the handler style used when constructing a logger.
type Mode int

const (
	// ModeCLI renders log records in a terse text-oriented format.
	ModeCLI Mode = iota
	// ModeJSON renders log records as JSON.
	ModeJSON
)

// New constructs a logger targeting the provided writer using the requested mode.
// If level is nil, slog.LevelInfo is used.
func New(mode Mode, w io.Writer, level slog.Leveler) *slog.Logger {
	if w == nil {
		panic("logging: writer must not be nil")
	}
	if level == nil {
		level = slog.LevelInfo
	}

	switch mode {
	case ModeJSON:
		handler := slog.NewJSONHandler(w, &slog.HandlerOptions{
			Level: level,
		})
		return slog.New(handler)
	default:
		handler := newCLIHandler(w, level)
		return slog.New(handler)
	}
}

// NewCLI constructs a logger that emits human-readable records suitable for CLI use.
func NewCLI(w io.Writer, level slog.Leveler) *slog.Logger {
	return New(ModeCLI, w, level)
}

// NewJSON constructs a logger that emits structured JSON records.
func NewJSON(w io.Writer, level slog.Leveler) *slog.Logger {
	return New(ModeJSON, w, level)
}

// Ensure returns the provided logger or the process default if nil.
func Ensure(logger *slog.Logger) *slog.Logger {
	if logger != nil {
		return logger
	}
	return slog.Default()
}

type cliHandler struct {
	writer io.Writer
	level  slog.Leveler

	mu     sync.Mutex
	attrs  []slog.Attr
	groups []string
}

func newCLIHandler(w io.Writer, level slog.Leveler) slog.Handler {
	return &cliHandler{
		writer: w,
		level:  level,
	}
}

func (h *cliHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= currentLevel(h.level)
}

func (h *cliHandler) Handle(_ context.Context, record slog.Record) error {
	var builder strings.Builder
	levelLabel := strings.ToUpper(record.Level.String())
	timestamp := record.Time
	if timestamp.IsZero() {
		timestamp = time.Now()
	}

	builder.WriteString(levelLabel)
	builder.WriteByte(' ')
	builder.WriteString(timestamp.UTC().Format(time.RFC3339))
	builder.WriteString(" | ")
	builder.WriteString(record.Message)

	h.appendAttrs(&builder, h.groups, h.attrs)
	record.Attrs(func(attr slog.Attr) bool {
		h.appendAttr(&builder, h.groups, attr)
		return true
	})

	builder.WriteByte('\n')

	h.mu.Lock()
	defer h.mu.Unlock()

	_, err := io.WriteString(h.writer, builder.String())
	return err
}

func (h *cliHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	cloned := make([]slog.Attr, len(h.attrs))
	copy(cloned, h.attrs)
	cloned = append(cloned, attrs...)

	return &cliHandler{
		writer: h.writer,
		level:  h.level,
		attrs:  cloned,
		groups: append([]string(nil), h.groups...),
	}
}

func (h *cliHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &cliHandler{
		writer: h.writer,
		level:  h.level,
		attrs:  append([]slog.Attr(nil), h.attrs...),
		groups: append(append([]string(nil), h.groups...), name),
	}
}

func (h *cliHandler) appendAttrs(builder *strings.Builder, groups []string, attrs []slog.Attr) {
	for _, attr := range attrs {
		h.appendAttr(builder, groups, attr)
	}
}

func (h *cliHandler) appendAttr(builder *strings.Builder, groups []string, attr slog.Attr) {
	value := resolveValue(attr.Value)
	if value.Kind() == slog.KindGroup {
		nestedGroups := append(groups, attr.Key)
		for _, nested := range value.Group() {
			h.appendAttr(builder, nestedGroups, nested)
		}
		return
	}

	key := attr.Key
	if len(groups) > 0 {
		key = strings.Join(append(append([]string(nil), groups...), key), ".")
	}

	builder.WriteByte(' ')
	builder.WriteString(key)
	builder.WriteByte('=')
	builder.WriteString(formatValue(value))
}

func formatValue(value slog.Value) string {
	value = resolveValue(value)
	switch value.Kind() {
	case slog.KindString:
		return value.String()
	case slog.KindInt64:
		return strconv.FormatInt(value.Int64(), 10)
	case slog.KindUint64:
		return strconv.FormatUint(value.Uint64(), 10)
	case slog.KindFloat64:
		return strconv.FormatFloat(value.Float64(), 'f', -1, 64)
	case slog.KindBool:
		return strconv.FormatBool(value.Bool())
	case slog.KindDuration:
		return value.Duration().String()
	case slog.KindTime:
		return value.Time().UTC().Format(time.RFC3339)
	case slog.KindLogValuer:
		return "<logvaluer>"
	case slog.KindAny:
		if err, ok := value.Any().(error); ok && err != nil {
			return err.Error()
		}
		return fmt.Sprint(value.Any())
	default:
		return value.String()
	}
}

func currentLevel(level slog.Leveler) slog.Level {
	if level == nil {
		return slog.LevelInfo
	}
	return level.Level()
}

func resolveValue(value slog.Value) slog.Value {
	for i := 0; i < 4; i++ {
		if value.Kind() != slog.KindLogValuer {
			return value
		}
		value = value.Resolve()
	}
	return value
}
