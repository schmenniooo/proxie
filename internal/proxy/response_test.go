package proxy_test

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/schmenniooo/proxie/internal/proxy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// trackingBody records whether it was closed.
type trackingBody struct {
	io.Reader

	closed bool
}

func (b *trackingBody) Close() error {
	b.closed = true
	return nil
}

// writeResponse runs WriteResponse on an upstream response built from status,
// header and body, and returns the body to check that it was closed.
func writeResponse(w http.ResponseWriter, status int, header http.Header, body io.Reader) (*trackingBody, error) {
	b := &trackingBody{Reader: body}
	return b, proxy.WriteResponse(w, &http.Response{StatusCode: status, Header: header, Body: b})
}

func TestWriteResponseCopiesStatusHeadersBody(t *testing.T) {
	w := httptest.NewRecorder()

	body, err := writeResponse(w, http.StatusCreated, http.Header{
		"Content-Type": {"application/json"},
		"X-Request-Id": {"r1"},
	}, strings.NewReader(`{"ok":true}`))

	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	assert.Equal(t, "r1", w.Header().Get("X-Request-Id"))
	assert.JSONEq(t, `{"ok":true}`, w.Body.String())
	assert.True(t, body.closed)
}

func TestWriteResponseDropsHopHeaders(t *testing.T) {
	w := httptest.NewRecorder()

	_, err := writeResponse(w, http.StatusOK, http.Header{
		"Connection":        {"close"},
		"Keep-Alive":        {"timeout=5"},
		"Transfer-Encoding": {"chunked"},
	}, strings.NewReader("ok"))

	require.NoError(t, err)

	for _, name := range []string{"Connection", "Keep-Alive", "Transfer-Encoding"} {
		assert.Empty(t, w.Header().Values(name), name)
	}
}

func TestWriteResponseKeepsContentEncoding(t *testing.T) {
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	_, err := zw.Write([]byte("compressed"))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	w := httptest.NewRecorder()

	_, err = writeResponse(w, http.StatusOK, http.Header{"Content-Encoding": {"gzip"}}, bytes.NewReader(gz.Bytes()))

	require.NoError(t, err)

	assert.Equal(t, "gzip", w.Header().Get("Content-Encoding"))
	assert.Equal(t, gz.Bytes(), w.Body.Bytes())
}

func TestWriteResponseReportsBodyError(t *testing.T) {
	w := httptest.NewRecorder()

	body, err := writeResponse(w, http.StatusOK, http.Header{},
		io.MultiReader(strings.NewReader("abc"), failingReader{}))

	require.Error(t, err)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "abc", w.Body.String())
	assert.True(t, body.closed)
}
