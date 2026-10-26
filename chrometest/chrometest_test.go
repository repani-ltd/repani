package chrometest

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

const page = `<!doctype html>
<meta name="viewport" content="width=device-width">
<title>t</title>
<div id="app"><p id="out">ready</p>
<input id="who"><button onclick="const v = document.getElementById('who').value; out.textContent = 'hi ' + v; localStorage.setItem('seen', v)">Greet</button></div>`

// TestDrive: a page loads, takes typing and a click, answers Eval with
// awaited JSON, and a second tab of the same Browser sees its storage.
func TestDrive(t *testing.T) {
	if !Available(Options{}) {
		t.Skip("no Chrome; set CHROMETEST_CHROME to its binary")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(page))
	}))
	defer srv.Close()

	b := Start(t, Options{Phone: true})
	p := b.Page()
	p.Navigate(srv.URL)
	p.WaitText("ready")
	p.Set("who", "ann")
	p.Click("Greet")
	p.WaitText("hi ann")
	if got := p.Str(`new Promise((r) => setTimeout(() => r(String(innerWidth)), 10))`); got != "390" {
		t.Errorf("awaited eval, phone width: %q, want 390", got)
	}
	// A Promise condition is judged by what it resolves to: this one is
	// false for half a second, so WaitFor must not return before then.
	p.Eval(`window.__later = false; setTimeout(() => { window.__later = true; }, 500); true`)
	p.WaitFor(`new Promise((r) => setTimeout(() => r(window.__later), 10))`)
	if got := p.Str(`String(window.__later)`); got != "true" {
		t.Errorf("WaitFor returned on a Promise of false (__later = %s)", got)
	}

	q := b.Page()
	q.Navigate(srv.URL)
	if got := q.Str(`localStorage.getItem('seen')`); got != "ann" {
		t.Errorf("second tab's storage: %q, want ann", got)
	}
	if got := JS(`a"b`); got != `"a\"b"` {
		t.Errorf("JS quoting: %s", got)
	}
}
