package harmonydb

import (
	"fmt"
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var Logger *zap.Logger

func InitLogger(port int) error {
	config := zap.NewProductionConfig()
	config.Level = zap.NewAtomicLevelAt(zap.DebugLevel)

	// Create file output
	logFile, err := os.OpenFile(fmt.Sprintf("/opt/homebrew/var/log/harmony_%d.log", port), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return err
	}

	// Create encoder config for structured logging
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "timestamp",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		FunctionKey:    zapcore.OmitKey,
		MessageKey:     "message",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	// Create console encoder for stdout
	consoleEncoder := zapcore.NewConsoleEncoder(encoderConfig)
	// Create JSON encoder for file
	fileEncoder := zapcore.NewJSONEncoder(encoderConfig)

	// Create multi-output core
	core := zapcore.NewTee(
		zapcore.NewCore(consoleEncoder, zapcore.AddSync(os.Stdout), zap.DebugLevel),
		zapcore.NewCore(fileEncoder, zapcore.AddSync(logFile), zap.ErrorLevel),
	)

	Logger = zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))

	return nil
}

func GetLogger() *zap.Logger {
	return Logger
}

func Sync() {
	if Logger != nil {
	}
}
