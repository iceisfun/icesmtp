package icesmtp

import (
	"context"
	"io"
	"log"
)

// Logger defines the logging interface for icesmtp.
// Implementations may wrap slog, zap, zerolog, or any logging framework.
type Logger interface {
	// Debug logs a debug message with optional attributes.
	Debug(ctx context.Context, msg string, attrs ...LogAttr)

	// Info logs an informational message.
	Info(ctx context.Context, msg string, attrs ...LogAttr)

	// Warn logs a warning message.
	Warn(ctx context.Context, msg string, attrs ...LogAttr)

	// Error logs an error message.
	Error(ctx context.Context, msg string, attrs ...LogAttr)

	// WithAttrs returns a new Logger with the given attributes added.
	WithAttrs(attrs ...LogAttr) Logger

	// WithSession returns a new Logger with session context.
	WithSession(sessionID SessionID) Logger
}

// LogAttr is a key-value pair for structured logging.
type LogAttr struct {
	Key   LogAttrKey
	Value LogAttrValue
}

// LogAttrKey is the key of a log attribute.
type LogAttrKey = string

// LogAttrValue is the value of a log attribute.
type LogAttrValue = any

// Attr creates a log attribute.
func Attr(key LogAttrKey, value LogAttrValue) LogAttr {
	return LogAttr{Key: key, Value: value}
}

// Common attribute keys.
const (
	AttrSessionID    LogAttrKey = "session_id"
	AttrClientIP     LogAttrKey = "client_ip"
	AttrCommand      LogAttrKey = "command"
	AttrState        LogAttrKey = "state"
	AttrError        LogAttrKey = "error"
	AttrReplyCode    LogAttrKey = "reply_code"
	AttrMailFrom     LogAttrKey = "mail_from"
	AttrRcptTo       LogAttrKey = "rcpt_to"
	AttrMessageSize  LogAttrKey = "message_size"
	AttrRecipients   LogAttrKey = "recipients"
	AttrTLSVersion   LogAttrKey = "tls_version"
	AttrCipherSuite  LogAttrKey = "cipher_suite"
	AttrDuration     LogAttrKey = "duration_ms"
	AttrEnvelopeID   LogAttrKey = "envelope_id"
)

// LogLevel represents a logging level.
type LogLevel int

const (
	// LogLevelDebug is the debug level.
	LogLevelDebug LogLevel = iota

	// LogLevelInfo is the info level.
	LogLevelInfo

	// LogLevelWarn is the warning level.
	LogLevelWarn

	// LogLevelError is the error level.
	LogLevelError
)

// String returns the level name.
func (l LogLevel) String() string {
	switch l {
	case LogLevelDebug:
		return "DEBUG"
	case LogLevelInfo:
		return "INFO"
	case LogLevelWarn:
		return "WARN"
	case LogLevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// NullLogger is a Logger that discards all messages.
type NullLogger struct{}

func (NullLogger) Debug(_ context.Context, _ string, _ ...LogAttr) {}
func (NullLogger) Info(_ context.Context, _ string, _ ...LogAttr)  {}
func (NullLogger) Warn(_ context.Context, _ string, _ ...LogAttr)  {}
func (NullLogger) Error(_ context.Context, _ string, _ ...LogAttr) {}
func (n NullLogger) WithAttrs(_ ...LogAttr) Logger                 { return n }
func (n NullLogger) WithSession(_ SessionID) Logger                { return n }

// StdLogger wraps the standard library logger.
type StdLogger struct {
	logger *log.Logger
	level  LogLevel
	attrs  []LogAttr
}

// NewStdLogger creates a StdLogger writing to the given writer.
func NewStdLogger(w io.Writer, level LogLevel) *StdLogger {
	return &StdLogger{
		logger: log.New(w, "", log.LstdFlags),
		level:  level,
	}
}

// Debug logs a debug message.
func (l *StdLogger) Debug(ctx context.Context, msg string, attrs ...LogAttr) {
	if l.level <= LogLevelDebug {
		l.log("DEBUG", msg, attrs)
	}
}

// Info logs an info message.
func (l *StdLogger) Info(ctx context.Context, msg string, attrs ...LogAttr) {
	if l.level <= LogLevelInfo {
		l.log("INFO", msg, attrs)
	}
}

// Warn logs a warning message.
func (l *StdLogger) Warn(ctx context.Context, msg string, attrs ...LogAttr) {
	if l.level <= LogLevelWarn {
		l.log("WARN", msg, attrs)
	}
}

// Error logs an error message.
func (l *StdLogger) Error(ctx context.Context, msg string, attrs ...LogAttr) {
	if l.level <= LogLevelError {
		l.log("ERROR", msg, attrs)
	}
}

func (l *StdLogger) log(level, msg string, attrs []LogAttr) {
	allAttrs := append(l.attrs, attrs...)
	if len(allAttrs) == 0 {
		l.logger.Printf("[%s] %s", level, msg)
		return
	}

	// Build attribute string
	attrStr := ""
	for _, attr := range allAttrs {
		attrStr += " " + attr.Key + "="
		switch v := attr.Value.(type) {
		case string:
			attrStr += v
		case error:
			attrStr += v.Error()
		default:
			attrStr += formatValue(v)
		}
	}
	l.logger.Printf("[%s] %s%s", level, msg, attrStr)
}

func formatValue(v any) string {
	switch val := v.(type) {
	case nil:
		return "<nil>"
	case string:
		return val
	case int:
		return intToStr(int64(val))
	case int64:
		return intToStr(val)
	case bool:
		if val {
			return "true"
		}
		return "false"
	default:
		return "<value>"
	}
}

func intToStr(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// WithAttrs returns a new logger with added attributes.
func (l *StdLogger) WithAttrs(attrs ...LogAttr) Logger {
	newLogger := &StdLogger{
		logger: l.logger,
		level:  l.level,
		attrs:  make([]LogAttr, len(l.attrs)+len(attrs)),
	}
	copy(newLogger.attrs, l.attrs)
	copy(newLogger.attrs[len(l.attrs):], attrs)
	return newLogger
}

// WithSession returns a new logger with session context.
func (l *StdLogger) WithSession(sessionID SessionID) Logger {
	return l.WithAttrs(Attr(AttrSessionID, sessionID))
}

// TranscriptLogger logs the raw SMTP conversation.
// This is useful for debugging and testing.
type TranscriptLogger interface {
	// LogInput logs input from the client.
	LogInput(data []byte)

	// LogOutput logs output to the client.
	LogOutput(data []byte)
}

// WriterTranscriptLogger writes transcripts to an io.Writer.
type WriterTranscriptLogger struct {
	Writer io.Writer
}

// LogInput logs client input.
func (l *WriterTranscriptLogger) LogInput(data []byte) {
	l.Writer.Write([]byte("C: "))
	l.Writer.Write(data)
}

// LogOutput logs server output.
func (l *WriterTranscriptLogger) LogOutput(data []byte) {
	l.Writer.Write([]byte("S: "))
	l.Writer.Write(data)
}
