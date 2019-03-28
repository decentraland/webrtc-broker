package logging

import (
	"github.com/sirupsen/logrus"
)

type Logger = logrus.Logger
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
