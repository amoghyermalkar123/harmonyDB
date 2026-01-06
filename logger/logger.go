package logger

import (
	"io"
	"log/slog"
	"os"
)

var Log *slog.Logger

func init() {
	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	Log = slog.New(handler)
}

func initLogger(output io.Writer, level slog.Level, useJSON bool) {
	if output == nil {
		output = os.Stdout
	}

	var handler slog.Handler
	if useJSON {
		handler = slog.NewJSONHandler(output, &slog.HandlerOptions{
			Level: level,
		})
	} else {
		handler = slog.NewTextHandler(output, &slog.HandlerOptions{
			Level: level,
		})
	}

	Log = slog.New(handler)
}

func InitWithFile(filepath string, level slog.Level, useJSON bool) error {
	file, err := os.OpenFile(filepath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return err
	}

	initLogger(file, level, useJSON)
	return nil
}
