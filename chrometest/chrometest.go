// Package chrometest drives headless Chrome over the DevTools protocol
// for Go tests of browser code: a Browser is one profile (one device,
// its own storage), and each Page a tab in it, so two pages of one
// Browser share IndexedDB and localStorage the way two tabs do.
//
//	b := chrometest.Start(t, chrometest.Options{Phone: true})
//	p := b.Page()
//	p.Navigate(srv.URL + "/")
//	p.WaitText("Get started")
//
// The Chrome binary is opts.Bin, else $CHROMETEST_CHROME, else
// Chrome's macOS path. Available reports whether it exists, so a test
// can skip on a machine without Chrome. CHROMETEST.t holds the
// package's decisions.
package chrometest

import (
	"cmp"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

// Options configure a Browser.
type Options struct {
	Bin   string // the Chrome binary
	Phone bool   // pages emulate a 390 x 844 phone
}

// Browser is one headless Chrome with its own profile.
type Browser struct {
	t     *testing.T
	port  string
	phone bool
	first bool // the tab Chrome opened is still unclaimed
}

// binary is the Chrome that Start runs.
func (opts Options) binary() string {
	return cmp.Or(opts.Bin, os.Getenv("CHROMETEST_CHROME"), "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome")
}

// Available reports whether the Chrome that Start would run exists.
func Available(opts Options) bool {
	_, err := os.Stat(opts.binary())
	return err == nil
}

// Start launches Chrome for the test; it is killed, and its profile
// removed, when the test ends.
func Start(t *testing.T, opts Options) *Browser {
	t.Helper()
	bin := opts.binary()
	// Not t.TempDir: Chrome's helper processes may still write to the
	// profile after the browser is killed, and a failed removal would
	// fail the test. The profile is removed on a best-effort basis.
	dir, err := os.MkdirTemp("", "chrometest-")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "--headless=new", "--remote-debugging-port=0", "--remote-allow-origins=*",
		"--user-data-dir="+dir, "--no-first-run", "--no-default-browser-check", "about:blank")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
		for range 20 {
			if os.RemoveAll(dir) == nil {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	})
	b := &Browser{t: t, phone: opts.Phone, first: true}
	for i := 0; b.port == "" && i < 100; i++ {
		raw, err := os.ReadFile(filepath.Join(dir, "DevToolsActivePort"))
		if err == nil && strings.Contains(string(raw), "\n") {
			b.port = strings.SplitN(string(raw), "\n", 2)[0]
		}
		time.Sleep(100 * time.Millisecond)
	}
	if b.port == "" {
		t.Fatal("chrome: no DevTools port")
	}
	return b
}

// Page returns a tab: the one Chrome opened, then a new one per call.
func (b *Browser) Page() *Page {
	b.t.Helper()
	var url string
	if b.first {
		b.first = false
		var targets []struct{ Type, WebSocketDebuggerURL string }
		for range 50 {
			resp, err := http.Get("http://127.0.0.1:" + b.port + "/json")
			if err == nil {
				json.NewDecoder(resp.Body).Decode(&targets)
				resp.Body.Close()
			}
			if len(targets) > 0 {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		for _, tg := range targets {
			if tg.Type == "page" {
				url = tg.WebSocketDebuggerURL
			}
		}
	} else {
		req, _ := http.NewRequest(http.MethodPut, "http://127.0.0.1:"+b.port+"/json/new?about:blank", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			b.t.Fatal(err)
		}
		var tg struct{ WebSocketDebuggerURL string }
		json.NewDecoder(resp.Body).Decode(&tg)
		resp.Body.Close()
		url = tg.WebSocketDebuggerURL
	}
	if url == "" {
		b.t.Fatal("chrome: no page target")
	}
	ws, err := websocket.Dial(url, "", "http://127.0.0.1")
	if err != nil {
		b.t.Fatal(err)
	}
	ws.MaxPayloadBytes = 64 << 20
	p := &Page{t: b.t, ws: ws, wait: map[int]chan json.RawMessage{}}
	go p.read()
	if b.phone {
		p.Call("Emulation.setDeviceMetricsOverride", map[string]any{"width": 390, "height": 844, "deviceScaleFactor": 2, "mobile": true})
	}
	return p
}

// Chrome starts a Browser and returns its first Page: one device.
func Chrome(t *testing.T, opts Options) *Page {
	t.Helper()
	return Start(t, opts).Page()
}

// Page is one tab, driven over the DevTools protocol.
type Page struct {
	t    *testing.T
	ws   *websocket.Conn
	mu   sync.Mutex
	next int
	wait map[int]chan json.RawMessage
}

func (p *Page) read() {
	for {
		var msg struct {
			ID     int
			Result json.RawMessage
			Error  json.RawMessage
		}
		if err := websocket.JSON.Receive(p.ws, &msg); err != nil {
			return
		}
		p.mu.Lock()
		ch := p.wait[msg.ID]
		delete(p.wait, msg.ID)
		p.mu.Unlock()
		if ch != nil {
			if msg.Error != nil {
				ch <- msg.Error
			} else {
				ch <- msg.Result
			}
		}
	}
}

// Call sends one DevTools command and returns its result.
func (p *Page) Call(method string, params any) json.RawMessage {
	p.t.Helper()
	p.mu.Lock()
	p.next++
	id := p.next
	ch := make(chan json.RawMessage, 1)
	p.wait[id] = ch
	p.mu.Unlock()
	if err := websocket.JSON.Send(p.ws, map[string]any{"id": id, "method": method, "params": params}); err != nil {
		p.t.Fatal(err)
	}
	select {
	case r := <-ch:
		return r
	case <-time.After(20 * time.Second):
		p.t.Fatalf("chrome: %s timed out", method)
		return nil
	}
}

// Eval evaluates a JavaScript expression, awaiting a promise, and
// returns its JSON value. An exception fails the test.
func (p *Page) Eval(expr string) json.RawMessage {
	p.t.Helper()
	var r struct {
		Result struct {
			Value json.RawMessage
		}
		ExceptionDetails *struct {
			Text      string
			Exception struct{ Description string }
		}
	}
	raw := p.Call("Runtime.evaluate", map[string]any{"expression": expr, "awaitPromise": true, "returnByValue": true})
	if err := json.Unmarshal(raw, &r); err != nil {
		p.t.Fatalf("eval %s: %s", expr, raw)
	}
	if r.ExceptionDetails != nil {
		p.t.Fatalf("eval %s: %s %s", expr, r.ExceptionDetails.Text, r.ExceptionDetails.Exception.Description)
	}
	return r.Result.Value
}

// Str evaluates an expression that yields a string.
func (p *Page) Str(expr string) string {
	p.t.Helper()
	var s string
	json.Unmarshal(p.Eval(expr), &s)
	return s
}

// Text is the page's visible text: #app's if there is one.
func (p *Page) Text() string {
	return p.Str(`document.getElementById('app')?.innerText ?? document.body.innerText`)
}

// Navigate loads url and waits for it. Confirms are accepted and
// alerts recorded in window.__alert, so no dialog stops the test.
func (p *Page) Navigate(url string) {
	p.t.Helper()
	p.Call("Page.navigate", map[string]any{"url": url})
	p.WaitFor(`document.readyState === 'complete' && !!document.body`)
	p.Eval(`window.confirm = () => true; window.alert = (m) => { window.__alert = m; }; true`)
}

// WaitFor polls a JavaScript condition until it holds, for up to 15 s.
// A condition that is a Promise is awaited and judged by what it
// resolves to, not by being an object.
func (p *Page) WaitFor(cond string) {
	p.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if string(p.Eval(`Promise.resolve(`+cond+`).then((v) => !!v)`)) == "true" {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	p.t.Fatalf("timed out waiting for %s\npage:\n%s\nalert: %s", cond, p.Text(), p.Eval(`window.__alert ?? null`))
}

// WaitText waits until the page shows every string.
func (p *Page) WaitText(want ...string) {
	p.t.Helper()
	conds := make([]string, len(want))
	for i, w := range want {
		conds[i] = fmt.Sprintf("document.body.textContent.includes(%s)", JS(w)) // innerText applies CSS uppercase
	}
	p.WaitFor(strings.Join(conds, " && "))
}

// Click clicks the button labelled label, once there is one.
func (p *Page) Click(label string) {
	p.t.Helper()
	q := JS(label)
	p.WaitFor(fmt.Sprintf(`[...document.querySelectorAll('button')].some((b) => b.innerText.trim() === %s)`, q))
	p.Eval(fmt.Sprintf(`[...document.querySelectorAll('button')].find((b) => b.innerText.trim() === %s).click()`, q))
}

// Set types a value into an input by id, as a person would.
func (p *Page) Set(id, value string) {
	p.t.Helper()
	p.WaitFor(fmt.Sprintf(`document.getElementById(%q)`, id))
	p.Eval(fmt.Sprintf(`(() => { const i = document.getElementById(%q); i.value = %s;
		i.dispatchEvent(new Event('input')); i.dispatchEvent(new Event('change')); })()`, id, JS(value)))
}

// Shot saves the whole page, light and dark, as NAME-light.png and
// NAME-dark.png in dir, when dir is set: screenshots for a person
// reviewing the UI, never compared by a test.
func (p *Page) Shot(dir, name string) {
	p.t.Helper()
	if dir == "" {
		return
	}
	for _, scheme := range []string{"light", "dark"} {
		p.Call("Emulation.setEmulatedMedia", map[string]any{"features": []map[string]string{{"name": "prefers-color-scheme", "value": scheme}}})
		time.Sleep(100 * time.Millisecond)
		var m struct {
			CSSContentSize    struct{ Width, Height float64 }
			CSSLayoutViewport struct{ ClientWidth, ClientHeight float64 }
		}
		json.Unmarshal(p.Call("Page.getLayoutMetrics", map[string]any{}), &m)
		var r struct{ Data []byte }
		json.Unmarshal(p.Call("Page.captureScreenshot", map[string]any{"format": "png", "captureBeyondViewport": true,
			"clip": map[string]any{"x": 0, "y": 0, "width": m.CSSLayoutViewport.ClientWidth,
				"height": max(m.CSSLayoutViewport.ClientHeight, m.CSSContentSize.Height), "scale": 1}}), &r)
		if err := os.WriteFile(filepath.Join(dir, name+"-"+scheme+".png"), r.Data, 0o644); err != nil {
			p.t.Fatal(err)
		}
	}
	p.Call("Emulation.setEmulatedMedia", map[string]any{"features": []map[string]string{{"name": "prefers-color-scheme", "value": "light"}}})
}

// JS quotes s as a JavaScript string literal.
func JS(s string) string {
	q, _ := json.Marshal(s)
	return string(q)
}
