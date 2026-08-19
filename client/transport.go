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
	"time"

	"repani.com/kv/wire"
)

// ErrNoBaseURL reports an HTTPTransport constructed without a server
// URL.
var ErrNoBaseURL = errors.New("client: transport has no base URL")

// HTTPTransport POSTs each sealed frame to <Base>/v1/sync and returns
// the response bytes, capped at wire.MaxFrameBytes. It is the
// canonical Transport for HTTP servers; a server mounted under a
// path prefix (e.g. Quietcasting's /kv) is reached by including the
// prefix in Base.
type HTTPTransport struct {
	Base    string        // server base URL; a trailing slash is tolerated
	Timeout time.Duration // per-request cap on top of ctx; 0 = ctx only
	Client  *http.Client  // nil = http.DefaultClient
}

// Do implements Transport.
func (t *HTTPTransport) Do(ctx context.Context, frame []byte) ([]byte, error) {
	if t.Base == "" {
		return nil, ErrNoBaseURL
	}
	if t.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, t.Timeout)
		defer cancel()
	}
	url := strings.TrimRight(t.Base, "/") + "/v1/sync"
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
	body, err := io.ReadAll(io.LimitReader(resp.Body, wire.MaxFrameBytes))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("client: server returned %s", resp.Status)
	}
	return body, nil
}
