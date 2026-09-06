package kiosk

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var body = []byte("<!doctype html>\n<title>page</title>\n")

func get(t *testing.T, h http.Handler, method, target string, header http.Header) *http.Response {
	t.Helper()
	r := httptest.NewRequest(method, target, nil)
	for k, vs := range header {
		r.Header[k] = vs
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Result()
}

func TestStaticServesBodyAndHeaders(t *testing.T) {
	h := Static(body, "text/html; charset=utf-8", Revalidate)
	resp := get(t, h, "GET", "/", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q", got)
	}
	if got := resp.Header.Get("ETag"); got != ETag(body) {
		t.Errorf("ETag = %q, want %q", got, ETag(body))
	}
	if got := resp.ContentLength; got != int64(len(body)) {
		t.Errorf("Content-Length = %d, want %d", got, len(body))
	}
}

func TestImmutablePolicy(t *testing.T) {
	h := Static(body, "font/woff2", Immutable)
	resp := get(t, h, "GET", "/f.woff2", nil)
	// The header the review found missing entirely on every font the
	// live station serves.
	if got := resp.Header.Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("Cache-Control = %q", got)
	}
	if resp.Header.Get("ETag") == "" {
		t.Error("no ETag on an immutable resource")
	}
}

// TestConditionalGET is the regression for the bug this package was
// written to stop: If-None-Match is a list, and comparing it to one
// tag with == returns a full body to any client that keeps two
// candidate versions.
func TestConditionalGET(t *testing.T) {
	h := Static(body, "text/html; charset=utf-8", Revalidate)
	tag := ETag(body)

	for _, tc := range []struct {
		name        string
		ifNoneMatch string
		want        int
	}{
		{"exact", tag, http.StatusNotModified},
		{"list, ours last", `"other", ` + tag, http.StatusNotModified},
		{"list, ours first", tag + `, "other"`, http.StatusNotModified},
		{"weak tag", "W/" + tag, http.StatusNotModified},
		{"star", "*", http.StatusNotModified},
		{"no match", `"nothing"`, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := get(t, h, "GET", "/", http.Header{"If-None-Match": {tc.ifNoneMatch}})
			if resp.StatusCode != tc.want {
				t.Fatalf("If-None-Match: %s -> %d, want %d", tc.ifNoneMatch, resp.StatusCode, tc.want)
			}
			if tc.want != http.StatusNotModified {
				return
			}
			// A 304 still carries the validator and the caching rule,
			// or the client has nothing to store the freshness against.
			if got := resp.Header.Get("ETag"); got != tag {
				t.Errorf("304 without ETag: %q", got)
			}
			if got := resp.Header.Get("Cache-Control"); got != "no-cache" {
				t.Errorf("304 without Cache-Control: %q", got)
			}
		})
	}
}

func TestHeadHasNoBody(t *testing.T) {
	h := Static(body, "text/html; charset=utf-8", Revalidate)
	resp := get(t, h, "HEAD", "/", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if resp.ContentLength != int64(len(body)) {
		t.Errorf("Content-Length = %d, want %d", resp.ContentLength, len(body))
	}
	buf := make([]byte, 1)
	if n, _ := resp.Body.Read(buf); n != 0 {
		t.Error("HEAD returned a body")
	}
}

func TestRange(t *testing.T) {
	h := Static(body, "text/html; charset=utf-8", Immutable)
	resp := get(t, h, "GET", "/", http.Header{"Range": {"bytes=0-4"}})
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", resp.StatusCode)
	}
	if got := resp.ContentLength; got != 5 {
		t.Errorf("Content-Length = %d, want 5", got)
	}
}

func TestStaticNeedsContentType(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Static accepted an empty content type")
		}
	}()
	Static(body, "", Revalidate)
}

func TestETagIsStrongAndQuoted(t *testing.T) {
	tag := ETag(body)
	if !strings.HasPrefix(tag, `"`) || !strings.HasSuffix(tag, `"`) {
		t.Fatalf("ETag %q is not quoted", tag)
	}
	if len(tag) != 18 { // 16 hex digits plus two quotes
		t.Fatalf("ETag %q has unexpected length %d", tag, len(tag))
	}
	if ETag([]byte("different")) == tag {
		t.Fatal("different bodies share a tag")
	}
}
