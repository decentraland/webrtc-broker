package logging

import (
	pionlogging "github.com/pion/logging"
	"github.com/sirupsen/logrus"
)

type Logger = logrus.Logger
type Entry = logrus.Entry
type Fields = logrus.Fields

var globalLevel logrus.Level = logrus.DebugLevel
var globalSetReportCaller bool

func SetLevel(level string) error {
	lvl, err := logrus.ParseLevel(level)

	if err != nil {
		return err
	}

	globalLevel = lvl
	logrus.SetLevel(lvl)
	return nil
}

func SetReportCaller(reportCaller bool) {
	globalSetReportCaller = reportCaller
	logrus.SetReportCaller(reportCaller)
}

func New() *Logger {
	formatter := logrus.JSONFormatter{
		FieldMap: logrus.FieldMap{
			logrus.FieldKeyTime: "@timestamp",
		},
	}

	log := logrus.New()
	log.SetLevel(globalLevel)
	log.SetReportCaller(globalSetReportCaller)
	log.SetFormatter(&formatter)
	logrus.SetFormatter(&formatter)

	return log
}

func LogPanic() {
	if r := recover(); r != nil {
		err, ok := r.(error)
		if ok {
			logrus.WithError(err).Error("panic")
		}
	}
}

type LogrusLevelLogger struct {
	log       *Logger
	peerAlias uint64
}

func (lll *LogrusLevelLogger) Trace(msg string) { lll.log.WithField("peer", lll.peerAlias).Trace(msg) }
func (lll *LogrusLevelLogger) Error(msg string) { lll.log.WithField("peer", lll.peerAlias).Error(msg) }
func (lll *LogrusLevelLogger) Debug(msg string) { lll.log.WithField("peer", lll.peerAlias).Debug(msg) }
func (lll *LogrusLevelLogger) Info(msg string)  { lll.log.WithField("peer", lll.peerAlias).Info(msg) }
func (lll *LogrusLevelLogger) Warn(msg string)  { lll.log.WithField("peer", lll.peerAlias).Warn(msg) }

func (lll *LogrusLevelLogger) Tracef(format string, args ...interface{}) {
	lll.log.WithField("peer", lll.peerAlias).Tracef(format, args...)
}
func (lll *LogrusLevelLogger) Debugf(format string, args ...interface{}) {
	lll.log.WithField("peer", lll.peerAlias).Debugf(format, args...)
}
func (lll *LogrusLevelLogger) Infof(format string, args ...interface{}) {
	lll.log.WithField("peer", lll.peerAlias).Infof(format, args...)
}
func (lll *LogrusLevelLogger) Warnf(format string, args ...interface{}) {
	lll.log.WithField("peer", lll.peerAlias).Warnf(format, args...)
}
func (lll *LogrusLevelLogger) Errorf(format string, args ...interface{}) {
	lll.log.WithField("peer", lll.peerAlias).Errorf(format, args...)
}

type PionLoggingFactory struct {
	PeerAlias uint64
}

func (f *PionLoggingFactory) NewLogger(scope string) pionlogging.LeveledLogger {
	log := New()
	log.SetLevel(logrus.ErrorLevel)
	return &LogrusLevelLogger{log: log, peerAlias: f.PeerAlias}
}
