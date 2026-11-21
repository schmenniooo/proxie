package proxy

import (
	"fmt"
	"io"
	"maps"
	"net/http"
)

// WriteResponse copies the status, headers (without hop-by-hop headers) and
// body of resp to w, and closes resp.Body. An error means the status was
// already sent and the body is incomplete.
func WriteResponse(w http.ResponseWriter, resp *http.Response) error {
	defer func() { _ = resp.Body.Close() }()

	h := resp.Header.Clone()
	RemoveHopHeaders(h)
	maps.Copy(w.Header(), h)
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		return fmt.Errorf("copy upstream response: %w", err)
	}
	return nil
}
