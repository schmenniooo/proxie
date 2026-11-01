package logging_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/schmenniooo/proxie/internal/config"
	"github.com/schmenniooo/proxie/internal/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewWritesJSONToStdout(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer

	logger, closer, err := logging.New(config.Log{Level: slog.LevelInfo, Format: "json"}, &stdout)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, closer.Close()) })
	logger.Info("hello", "n", 1)

	var line map[string]any
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &line))
	assert.Equal(t, "hello", line["msg"])
	assert.InDelta(t, 1, line["n"], 0)
}

func TestNewWritesText(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer

	logger, _, err := logging.New(config.Log{Level: slog.LevelInfo, Format: "text"}, &stdout)
	require.NoError(t, err)
	logger.Info("hello")

	assert.Contains(t, stdout.String(), "msg=hello")
}

func TestNewRespectsLevel(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer

	logger, _, err := logging.New(config.Log{Level: slog.LevelWarn, Format: "json"}, &stdout)
	require.NoError(t, err)
	logger.Info("dropped")
	logger.Warn("kept")

	assert.NotContains(t, stdout.String(), "dropped")
	assert.Contains(t, stdout.String(), "kept")
}

func TestNewWritesToFileAndStdout(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	path := filepath.Join(t.TempDir(), "proxie.log")

	logger, closer, err := logging.New(config.Log{
		Level:     slog.LevelInfo,
		Format:    "json",
		File:      path,
		FileMaxMB: 1,
	}, &stdout)
	require.NoError(t, err)
	logger.Info("both")
	require.NoError(t, closer.Close())

	file, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(file), `"msg":"both"`)
	assert.Contains(t, stdout.String(), `"msg":"both"`)
}

func TestNewFailsWhenLogDirIsMissing(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "missing", "proxie.log")

	_, _, err := logging.New(config.Log{Format: "json", File: path, FileMaxMB: 1}, &bytes.Buffer{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "log file")
}

func TestNewFailsWhenLogDirIsAFile(t *testing.T) {
	t.Parallel()
	notDir := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(notDir, nil, 0o600))

	_, _, err := logging.New(config.Log{Format: "json", File: filepath.Join(notDir, "proxie.log"), FileMaxMB: 1}, &bytes.Buffer{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}
