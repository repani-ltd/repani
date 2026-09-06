package kiosk

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestBuildString(t *testing.T) {
	for _, tc := range []struct {
		name string
		b    Build
		want string
	}{
		{"full", Build{Program: "kvserve", Revision: "8fa83a6c653e3d97", Time: "2026-09-06T20:51:14Z", Go: "go1.26.6"},
			"kvserve 8fa83a6 2026-09-06T20:51:14Z go1.26.6"},
		{"dirty", Build{Program: "kvserve", Revision: "8fa83a6c653e3d97", Modified: true, Go: "go1.26.6"},
			"kvserve 8fa83a6+dirty go1.26.6"},
		{"short revision kept whole", Build{Program: "p", Revision: "abc123", Go: "go1.26.6"},
			"p abc123 go1.26.6"},
		{"no vcs at all", Build{Program: "p", Go: "go1.26.6"},
			"p norev go1.26.6"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.b.String(); got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestReadBuild checks the stamp is really there under test, which is
// the same mechanism a release binary relies on.
func TestReadBuild(t *testing.T) {
	b := ReadBuild()
	if b.Go == "" {
		t.Error("no Go version in the build info")
	}
	if b.Program == "" {
		t.Error("no program name")
	}
}

func TestVersionHandler(t *testing.T) {
	h := wrapped(t, Config{Handler: versionMux()})
	resp := get(t, h, "GET", VersionPath, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	body, _ := io.ReadAll(resp.Body)
	if strings.TrimSpace(string(body)) != ReadBuild().String() {
		t.Errorf("body = %q, want %q", body, ReadBuild().String())
	}
	// Like Health, it is an ordinary route: the mux refuses the rest.
	if resp := get(t, h, "POST", VersionPath, nil); resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST -> %d, want 405", resp.StatusCode)
	}
}

func versionMux() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET "+VersionPath, Version())
	return mux
}
