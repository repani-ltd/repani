// HTTPTransport: the standard HTTP implementation of Transport.
package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"repani.com/kv/wire"
)

// ErrNoBaseURL reports an HTTPTransport constructed without a server
// URL.
var ErrNoBaseURL = errors.New("client: transport has no base URL")

// ErrResponseTooLarge reports a response longer than wire.MaxFrameBytes.
var ErrResponseTooLarge = errors.New("client: response exceeds the frame cap")

// HTTPTransport POSTs each sealed frame to <Base>/v1/sync and returns
// the response bytes, capped at wire.MaxFrameBytes. It is the
// canonical Transport for HTTP servers; a server mounted under a
// path prefix (e.g. an almanac station's /kv) is reached by including the
// prefix in Base.
// A request is bounded by ctx and by Client's own Timeout.
type HTTPTransport struct {
	Base   string       // server base URL; a trailing slash is tolerated
	Client *http.Client // nil = http.DefaultClient
}

// Do implements Transport.
func (t *HTTPTransport) Do(ctx context.Context, frame []byte) ([]byte, error) {
	if t.Base == "" {
		return nil, ErrNoBaseURL
	}
	url := strings.TrimRight(t.Base, "/") + wire.SyncPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(frame))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	hc := t.Client
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	// One byte past the cap tells an oversized reply from one that
	// fits, so it is refused as what it is, not cut short and then
	// reported as a seal that does not open.
	body, err := io.ReadAll(io.LimitReader(resp.Body, wire.MaxFrameBytes+1))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("client: server returned %s", resp.Status)
	}
	if len(body) > wire.MaxFrameBytes {
		return nil, ErrResponseTooLarge
	}
	return body, nil
}
