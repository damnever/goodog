package frontend

import (
	"os"
	"strings"

	"github.com/mattn/go-isatty"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	logLevels = map[string]zapcore.Level{
		"debug": zapcore.DebugLevel,
		"info":  zapcore.InfoLevel,
		"warn":  zapcore.WarnLevel,
		"error": zapcore.ErrorLevel,
		"panic": zapcore.PanicLevel,
		"fatal": zapcore.FatalLevel,
	}

	DefaultLogger *zap.Logger
)

func init() {
	fd := os.Stdout.Fd()
	var err error
	if isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd) {
		DefaultLogger, err = zap.NewDevelopment()
	} else {
		DefaultLogger, err = zap.NewProduction()
	}
	if err != nil {
		panic(err)
	}
}

func setDefaultLogLevel(level string) {
	DefaultLogger.Core().Enabled(LogLevel(level))
}

func LogLevel(name string) zapcore.Level {
	if lvl, ok := logLevels[strings.ToLower(name)]; ok {
		return lvl
	}
	return zapcore.InfoLevel
}

/*
	defaultConfig = zap.Config{
		Level:             zap.NewAtomicLevelAt(zapcore.InfoLevel),
		DisableCaller:     true,
		DisableStacktrace: false,
		Sampling: &zap.SamplingConfig{
			Initial:    100,
			Thereafter: 100,
		},
		Encoding: "json",
		EncoderConfig: zapcore.EncoderConfig{
			TimeKey:        "ts",
			LevelKey:       "level",
			NameKey:        "logger",
			CallerKey:      "caller",
			MessageKey:     "msg",
			StacktraceKey:  "stacktrace",
			LineEnding:     zapcore.DefaultLineEnding,
			EncodeLevel:    zapcore.LowercaseLevelEncoder,
			EncodeTime:     zapcore.EpochTimeEncoder,
			EncodeDuration: zapcore.SecondsDurationEncoder,
			EncodeCaller:   zapcore.ShortCallerEncoder,
		},
		OutputPaths:      []string{"stdout"},
		ErrorOutputPaths: []string{"stdout"},
	}
*/
