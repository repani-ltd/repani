// Static resources: validators, caching, and conditional requests.
package kiosk

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"net/http"
	"time"
)

// A Policy says how a client may reuse a body it already has.
type Policy int

const (
	// Revalidate stores the body but asks every time, and usually
	// gets a 304 back. For the one file in a publication that changes
	// in place, and for any document whose URL outlives its bytes.
	Revalidate Policy = iota

	// Immutable promises this URL will never carry different bytes,
	// so a client that has it need never ask again for a year. Only
	// for a file named by its content.
	Immutable

	// NoStore keeps the body out of every cache. For errors and for
	// anything a shared cache must not hold.
	NoStore
)

// immutableMaxAge is a year, the longest value RFC 9111 recommends
// anyone send.
const immutableMaxAge = 31536000

func (p Policy) header() string {
	switch p {
	case Immutable:
		return fmt.Sprintf("public, max-age=%d, immutable", immutableMaxAge)
	case NoStore:
		return "no-store"
	default:
		return "no-cache"
	}
}

// String names the policy for logs and errors.
func (p Policy) String() string {
	switch p {
	case Immutable:
		return "immutable"
	case NoStore:
		return "no-store"
	default:
		return "revalidate"
	}
}

// Static returns a handler that serves body as ctype under policy.
// The body is held as given and never re-read, so a caller may build
// one at startup from an embedded file, a decoded page, or bytes it
// generated itself; kiosk has no opinion about where it came from and
// assumes nothing about what is in it.
//
// ctype must be a full Content-Type including a charset where one
// applies ("text/html; charset=utf-8"). It is required rather than
// sniffed: content sniffing is what X-Content-Type-Options exists to
// switch off, and a resource whose type nobody could name is a
// resource nobody should serve. An empty ctype panics, at
// construction, which is startup.
func Static(body []byte, ctype string, policy Policy) http.Handler {
	if ctype == "" {
		panic("kiosk: Static needs a content type")
	}
	return &static{
		body:  body,
		ctype: ctype,
		cache: policy.header(),
		etag:  ETag(body),
	}
}

// ETag returns the strong entity tag kiosk uses: the first eight
// bytes of the body's SHA-256, hex, quoted. Two bodies with the same
// tag are the same body for every practical purpose, and a caller
// that serves content-named files can compare tags without reading
// them.
func ETag(body []byte) string {
	sum := sha256.Sum256(body)
	return fmt.Sprintf("%q", fmt.Sprintf("%x", sum[:8]))
}

type static struct {
	body  []byte
	ctype string
	cache string
	etag  string
}

// ServeHTTP sets the validator and the caching rule and then hands
// the body to http.ServeContent.
//
// Routing through ServeContent is the substance of this type, not a
// convenience. If-None-Match is a LIST (RFC 9110 section 13.1.2): a
// client that keeps two candidate versions, or any shared cache
// revalidating several stored variants, sends more than one tag, and
// the weak comparison function is the correct one for GET. Comparing
// the header to a single tag with == answers all of that with a full
// body, quietly, and looks right in every hand test with one browser.
// ServeContent also handles If-Match, If-Range, Range and HEAD, none
// of which is worth re-deriving here.
//
// The zero modtime is deliberate: the ETag is the only validator, and
// a Last-Modified computed from a build time would make byte-identical
// bodies look different across a rebuild.
func (s *static) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Type", s.ctype)
	h.Set("Cache-Control", s.cache)
	h.Set("ETag", s.etag)
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(s.body))
}
