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
	log := logrus.New()
	log.SetLevel(globalLevel)
	log.SetReportCaller(globalSetReportCaller)
	return log
}
