package logging

import (
	"os"
	"runtime"
	"testing"

	"github.com/sirupsen/logrus"
)

var Logger *logrus.Logger = makeLogger()

func makeLogger() *logrus.Logger {
	// TODO should this someday be configurable?
	level := logrus.InfoLevel
	if testing.Testing() {
		level = logrus.DebugLevel
	}
	return &logrus.Logger{
		Out: os.Stdout,
		Formatter: &logrus.TextFormatter{
			DisableLevelTruncation: true,
			PadLevelText:           true,
			FullTimestamp:          true,
		},
		Hooks: make(logrus.LevelHooks),
		Level: level,
	}
}

func Trace() runtime.Frame {
	pc := make([]uintptr, 15)
	n := runtime.Callers(2, pc)
	frames := runtime.CallersFrames(pc[:n])
	frame, _ := frames.Next()
	return frame
}
