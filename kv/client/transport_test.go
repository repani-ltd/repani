package client

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"repani.com/kv/wire"
)

// TestHTTPTransport_FrameCap: a reply of exactly MaxFrameBytes is
// returned whole, and one byte more is ErrResponseTooLarge rather than
// a reply cut short.
func TestHTTPTransport_FrameCap(t *testing.T) {
	for _, tc := range []struct {
		size    int
		wantErr error
	}{
		{wire.MaxFrameBytes, nil},
		{wire.MaxFrameBytes + 1, ErrResponseTooLarge},
	} {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Write(bytes.Repeat([]byte{0xAA}, tc.size))
		}))
		body, err := (&HTTPTransport{Base: ts.URL}).Do(context.Background(), []byte{1})
		ts.Close()
		if !errors.Is(err, tc.wantErr) {
			t.Fatalf("reply of %d bytes: err %v, want %v", tc.size, err, tc.wantErr)
		}
		if err == nil && len(body) != tc.size {
			t.Fatalf("reply of %d bytes: got %d", tc.size, len(body))
		}
	}
}
