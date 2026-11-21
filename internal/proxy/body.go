// Package proxy forwards requests to a tenant's upstream and passes the
// response back. It holds no provider-specific code.
package proxy

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
)

// Body is the request body as read by ReadBody.
type Body struct {
	// Bytes is the whole body. It is nil when the body is Oversize.
	Bytes []byte
	// Oversize means the body is larger than the limit. It is still
	// forwarded unchanged, but must not be cached.
	Oversize bool
}

// ReadBody reads the body of r up to limit bytes and makes r.Body readable
// again from the start. A body within the limit is buffered in Bytes. A
// larger one is not: r.Body then yields the bytes read so far followed by
// the rest of the original body, so it is forwarded unchanged.
func ReadBody(r *http.Request, limit int64) (Body, error) {
	if r.Body == nil || r.Body == http.NoBody {
		r.Body, r.ContentLength = http.NoBody, 0
		return Body{Bytes: []byte{}}, nil
	}
	original := r.Body
	b, err := io.ReadAll(io.LimitReader(original, limit+1))
	if err != nil {
		return Body{}, fmt.Errorf("read request body: %w", err)
	}
	if int64(len(b)) > limit {
		r.Body = readCloser{Reader: io.MultiReader(bytes.NewReader(b), original), Closer: original}
		return Body{Oversize: true}, nil
	}
	_ = original.Close()
	if len(b) == 0 {
		r.Body, r.ContentLength = http.NoBody, 0
		return Body{Bytes: b}, nil
	}
	r.Body, r.ContentLength = io.NopCloser(bytes.NewReader(b)), int64(len(b))
	return Body{Bytes: b}, nil
}

type readCloser struct {
	io.Reader
	io.Closer
}
