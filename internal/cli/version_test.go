package cli_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVersion(t *testing.T) {
	// version needs no config.
	setEnv(t, nil)

	code, stdout, _ := run(t.Context(), "version")

	assert.Equal(t, 0, code)
	assert.Regexp(t, `^proxie dev \(commit \S+\)\n$`, stdout)
}
