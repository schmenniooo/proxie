// Package logging builds the slog logger from the logging config.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/schmenniooo/proxie/internal/config"
	"gopkg.in/natefinch/lumberjack.v2"
)

// New returns a logger that writes to stdout and, if cfg.File is set, also to
// a size-rotated log file. The returned closer closes the log file and must be
// called once logging is done.
func New(cfg config.Log, stdout io.Writer) (*slog.Logger, io.Closer, error) {
	out := stdout
	var closer io.Closer = nopCloser{}

	if cfg.File != "" {
		if err := checkWritable(cfg.File); err != nil {
			return nil, nil, fmt.Errorf("log file: %w", err)
		}
		file := &lumberjack.Logger{
			Filename:   cfg.File,
			MaxSize:    cfg.FileMaxMB,
			MaxBackups: cfg.FileMaxBackups,
			MaxAge:     cfg.FileMaxAgeDays,
		}
		out = io.MultiWriter(stdout, file)
		closer = file
	}

	opts := &slog.HandlerOptions{Level: cfg.Level}
	var handler slog.Handler
	if cfg.Format == "text" {
		handler = slog.NewTextHandler(out, opts)
	} else {
		handler = slog.NewJSONHandler(out, opts)
	}
	return slog.New(handler), closer, nil
}

// checkWritable fails early if the log file cannot be written, instead of
// failing on the first log line. The directory must already exist.
func checkWritable(path string) error {
	dir := filepath.Dir(path)
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("directory %s: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o640)
	if err != nil {
		return err
	}
	return f.Close()
}

type nopCloser struct{}

func (nopCloser) Close() error { return nil }
