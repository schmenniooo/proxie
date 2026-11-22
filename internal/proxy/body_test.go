package proxy_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/schmenniooo/proxie/internal/proxy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newBodyRequest(t *testing.T, body io.Reader) *http.Request {
	t.Helper()
	return httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/x", body)
}

func TestReadBodyWithinLimit(t *testing.T) {
	r := newBodyRequest(t, strings.NewReader("hello"))

	b, err := proxy.ReadBody(r, 10)

	require.NoError(t, err)
	assert.Equal(t, proxy.Body{Bytes: []byte("hello")}, b)
	assert.EqualValues(t, 5, r.ContentLength)
	again, err := io.ReadAll(r.Body)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(again))
}

func TestReadBodyAtLimit(t *testing.T) {
	r := newBodyRequest(t, strings.NewReader(strings.Repeat("a", 10)))

	b, err := proxy.ReadBody(r, 10)

	require.NoError(t, err)
	assert.False(t, b.Oversize)
	assert.Len(t, b.Bytes, 10)
}

func TestReadBodyOverLimit(t *testing.T) {
	body := strings.Repeat("a", 10) + "b"
	r := newBodyRequest(t, strings.NewReader(body))

	b, err := proxy.ReadBody(r, 10)

	require.NoError(t, err)
	assert.True(t, b.Oversize)
	assert.Nil(t, b.Bytes)
	all, err := io.ReadAll(r.Body)
	require.NoError(t, err)
	assert.Equal(t, body, string(all))
}

func TestReadBodyEmpty(t *testing.T) {
	r := newBodyRequest(t, nil)

	b, err := proxy.ReadBody(r, 10)

	require.NoError(t, err)
	assert.Empty(t, b.Bytes)
	assert.False(t, b.Oversize)
	assert.Equal(t, http.NoBody, r.Body)
	assert.Zero(t, r.ContentLength)
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

func TestReadBodyReadError(t *testing.T) {
	r := newBodyRequest(t, failingReader{})

	_, err := proxy.ReadBody(r, 10)

	assert.Error(t, err)
}
