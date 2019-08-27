package logging

import (
	"os"
	"runtime/debug"

	pionlogging "github.com/pion/logging"
	pionzerolog "github.com/pion/logging/thirdparty/zerolog"
	"github.com/rs/zerolog"
)

// Logger ...
type Logger = zerolog.Logger

// New creates a new Logger
func New() Logger {
	return zerolog.New(os.Stdout)
}

// LogPanic will catch a panic and log it
func LogPanic(log Logger) {
	if r := recover(); r != nil {
		err, ok := r.(error)
		if ok {
			debug.PrintStack()
			log.Error().Err(err).Msg("panic")
		}
	}
}

// PionLoggingFactory is the log factory expected by pion
type PionLoggingFactory struct {
	PeerAlias uint64
}

// NewLogger creates a new logger for the given scope
func (f *PionLoggingFactory) NewLogger(scope string) pionlogging.LeveledLogger {
	log := New().Level(zerolog.ErrorLevel).With().Uint64("peer", f.PeerAlias).Logger().Level(zerolog.DebugLevel)
	return &pionlogging.Logger{
		FormatterFactory: func(lvl pionlogging.LogLevel) pionlogging.Formatter {
			return pionzerolog.NewZerologFormatter(log, lvl)
		},
		Lvl: pionlogging.LogLevelTrace,
	}
}
