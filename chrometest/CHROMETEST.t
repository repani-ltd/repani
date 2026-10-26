CHROMETEST
.date 2026-10-12
.by Pavlos Christoforou
.rights All rights reserved © repani.com
.rem The contract and decisions of repani.com/chrometest. Usage is
.rem the package documentation (go doc repani.com/chrometest).

Package chrometest lets a Go test drive headless Chrome: start a
browser with its own profile, open tabs, load pages, evaluate
JavaScript and read the answers. It exists so that browser code, a
web app or a JavaScript library, is tested from `go test` like
everything else, with no JavaScript tooling installed. Chrome itself
is the only requirement.

# Contract

.item A Browser is one Chrome process with a fresh profile, removed
when the test ends. Its Pages are tabs and share that profile's
storage, so two Pages are two tabs of one device and two Browsers
are two devices.
.item Every failure fails the test: a call that times out, an
exception thrown by evaluated JavaScript, a condition never met. No
method returns an error.
.item Eval awaits a promise and returns its value as JSON, so an
async JavaScript function is called as it would be awaited.
.item The binary is Options.Bin, else `$CHROMETEST_CHROME`, else
Chrome's macOS path. Available reports whether it exists, and a test
skips when it does not.
.item The package is for tests only. It is imported from `_test.go`
files and takes a `*testing.T` everywhere.

# Decisions

.term DevTools directly
The package speaks the DevTools protocol itself over one WebSocket
per tab: Runtime.evaluate, Page.navigate, two Emulation commands and
Page.captureScreenshot cover every test so far. A full client such
as chromedp would bring a dependency tree and an API larger than the
tests that use it.
.term golang.org/x/net/websocket
Admitted as the one dependency outside the standard library, for the
WebSocket the DevTools protocol runs on. The standard library has no
WebSocket client, and this package is the Go project's own.
.term Polling, not events
WaitFor evaluates a condition every 100 ms for up to 15 s. Tests wait
for what a person would see, and polling needs no event plumbing. A
condition may be a Promise, awaited and judged by its value: until
2026-10-26 it was judged as an object, always true, and three
conditions that read a vault (webapp's watch tests) passed without
waiting at all.
.term A phone by option
Options.Phone emulates a 390 x 844 phone, the device the apps are
used on; without it a page gets Chrome's default desktop viewport.
.width 48
.cols 2
.font sans
