package ascon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"repani.com/chrometest"
)

// TestJS runs ascon.js against the known-answer vectors the Go package
// is tested with, in headless Chrome: XOF, CXOF, and AEAD sealed,
// opened and refused when forged. Skips when there is no Chrome.
func TestJS(t *testing.T) {
	if !chrometest.Available(chrometest.Options{}) {
		t.Skip("no Chrome; set CHROMETEST_CHROME to its binary to test ascon.js")
	}
	mux := http.NewServeMux()
	mux.Handle("/ascon.js", http.FileServerFS(JS))
	mux.Handle("/kat/", http.StripPrefix("/kat/", http.FileServer(http.Dir("testdata"))))
	mux.HandleFunc("/ascon_test.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		http.ServeFile(w, r, "testdata/ascon_test.js")
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<!doctype html><title>ascon.js</title>"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p := chrometest.Chrome(t, chrometest.Options{})
	p.Navigate(srv.URL + "/")
	var res struct {
		Fails []string
		Count int
	}
	if err := json.Unmarshal(p.Eval(`import("/ascon_test.js").then((m) => m.run())`), &res); err != nil {
		t.Fatal(err)
	}
	for _, f := range res.Fails {
		t.Error(f)
	}
	// 1025 XOF and 1089 CXOF vectors; 1089 AEAD vectors, each sealed,
	// opened and forged (every ciphertext carries its 16-byte tag):
	// 1025 + 1089 + 3 x 1089 = 5381.
	if res.Count != 5381 {
		t.Fatalf("ascon.js ran %d checks, want 5381", res.Count)
	}
	t.Logf("ascon.js: %d checks, %d failures", res.Count, len(res.Fails))
}
