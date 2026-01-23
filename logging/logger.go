package logging

import (
	"os"
	"runtime"

	"github.com/sirupsen/logrus"
)

var Logger = &logrus.Logger{
	Out: os.Stdout,
	Formatter: &logrus.TextFormatter{
		DisableLevelTruncation: true,
		PadLevelText:           true,
		FullTimestamp:          true,
	},
	Hooks: make(logrus.LevelHooks),
	Level: logrus.InfoLevel,
}

func Trace() runtime.Frame {
	pc := make([]uintptr, 15)
	n := runtime.Callers(2, pc)
	frames := runtime.CallersFrames(pc[:n])
	frame, _ := frames.Next()
	return frame
}
