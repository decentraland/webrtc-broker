// Package logging ...
package logging

import (
	"os"
	"runtime/debug"

	pionlogging "github.com/pion/logging"
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

type levelLogger struct {
	log Logger
}

func (ll *levelLogger) Trace(msg string) { ll.log.Debug().Msg(msg) }
func (ll *levelLogger) Error(msg string) { ll.log.Error().Msg(msg) }
func (ll *levelLogger) Debug(msg string) { ll.log.Debug().Msg(msg) }
func (ll *levelLogger) Info(msg string)  { ll.log.Info().Msg(msg) }
func (ll *levelLogger) Warn(msg string)  { ll.log.Warn().Msg(msg) }

func (ll *levelLogger) Tracef(format string, args ...interface{}) {
	ll.log.Debug().Msgf(format, args...)
}
func (ll *levelLogger) Debugf(format string, args ...interface{}) {
	ll.log.Debug().Msgf(format, args...)
}
func (ll *levelLogger) Infof(format string, args ...interface{}) {
	ll.log.Info().Msgf(format, args...)
}
func (ll *levelLogger) Warnf(format string, args ...interface{}) {
	ll.log.Warn().Msgf(format, args...)
}
func (ll *levelLogger) Errorf(format string, args ...interface{}) {
	ll.log.Error().Msgf(format, args...)
}

// PionLoggingFactory is the log factory expected by pion
type PionLoggingFactory struct {
	PeerAlias       uint64
	DefaultLogLevel zerolog.Level
}

// NewLogger creates a new logger for the given scope
func (f *PionLoggingFactory) NewLogger(scope string) pionlogging.LeveledLogger {
	log := New().Level(f.DefaultLogLevel).With().Uint64("peer", f.PeerAlias).Logger()
	return &levelLogger{log: log}
}
