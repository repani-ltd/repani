package row

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"repani.com/chrometest"
)

// TestJS runs row.js against the same vectors in headless Chrome.
// Skips when there is no Chrome.
func TestJS(t *testing.T) {
	if !chrometest.Available(chrometest.Options{}) {
		t.Skip("no Chrome; set CHROMETEST_CHROME to its binary to test row.js")
	}
	mux := http.NewServeMux()
	serve := func(path, ctype string, body func() []byte) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", ctype)
			w.Write(body())
		})
	}
	serve("/row.js", "text/javascript", func() []byte { return []byte(JS()) })
	mux.HandleFunc("/vectors.json", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "testdata/vectors.json")
	})
	mux.HandleFunc("/row_test.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		http.ServeFile(w, r, "testdata/row_test.js")
	})
	serve("/", "text/html", func() []byte { return []byte("<!doctype html><title>row.js</title>") })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	p := chrometest.Chrome(t, chrometest.Options{})
	p.Navigate(srv.URL + "/")
	var res struct {
		Fails []string
		Count int
	}
	if err := json.Unmarshal(p.Eval(`import("/row_test.js").then((m) => m.run())`), &res); err != nil {
		t.Fatal(err)
	}
	for _, f := range res.Fails {
		t.Error(f)
	}
	if res.Count == 0 {
		t.Fatal("row.js ran no vectors")
	}
	t.Logf("row.js: %d vectors, %d failures", res.Count, len(res.Fails))
}
