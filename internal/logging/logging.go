package logging

import (
	"runtime/debug"

	pionlogging "github.com/pion/logging"
	"github.com/sirupsen/logrus"
)

// Logger is an alias for logrus.Logger
type Logger = logrus.Logger

// Entry is an alias for logrus.Entry
type Entry = logrus.Entry

// Fields is an alias for logrus.Fields
type Fields = logrus.Fields

// LogPanic will catch a panic and log it using logrus default config
func LogPanic() {
	if r := recover(); r != nil {
		err, ok := r.(error)
		if ok {
			debug.PrintStack()
			logrus.WithError(err).Error("panic")
		}
	}
}

type logrusLevelLogger struct {
	log       *Logger
	peerAlias uint64
}

func (lll *logrusLevelLogger) Trace(msg string) { lll.log.WithField("peer", lll.peerAlias).Trace(msg) }
func (lll *logrusLevelLogger) Error(msg string) { lll.log.WithField("peer", lll.peerAlias).Error(msg) }
func (lll *logrusLevelLogger) Debug(msg string) { lll.log.WithField("peer", lll.peerAlias).Debug(msg) }
func (lll *logrusLevelLogger) Info(msg string)  { lll.log.WithField("peer", lll.peerAlias).Info(msg) }
func (lll *logrusLevelLogger) Warn(msg string)  { lll.log.WithField("peer", lll.peerAlias).Warn(msg) }

func (lll *logrusLevelLogger) Tracef(format string, args ...interface{}) {
	lll.log.WithField("peer", lll.peerAlias).Tracef(format, args...)
}
func (lll *logrusLevelLogger) Debugf(format string, args ...interface{}) {
	lll.log.WithField("peer", lll.peerAlias).Debugf(format, args...)
}
func (lll *logrusLevelLogger) Infof(format string, args ...interface{}) {
	lll.log.WithField("peer", lll.peerAlias).Infof(format, args...)
}
func (lll *logrusLevelLogger) Warnf(format string, args ...interface{}) {
	lll.log.WithField("peer", lll.peerAlias).Warnf(format, args...)
}
func (lll *logrusLevelLogger) Errorf(format string, args ...interface{}) {
	lll.log.WithField("peer", lll.peerAlias).Errorf(format, args...)
}

// PionLoggingFactory is the log factory expected by pion
type PionLoggingFactory struct {
	PeerAlias uint64
}

// NewLogger creates a new logger for the given scope
func (f *PionLoggingFactory) NewLogger(scope string) pionlogging.LeveledLogger {
	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)
	return &logrusLevelLogger{log: log, peerAlias: f.PeerAlias}
}
