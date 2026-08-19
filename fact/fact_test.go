package fact

import (
	"bytes"
	"strings"
	"testing"
)

// The complete example from SPEC §12, non-canonical form.
const specInput = `# ── server ──────────────────────────────────
server.host: str = "0.0.0.0"
server.port: int = 8443
server.tls.enabled: bool = true
server.tls.cert: str = "/etc/certs/server.pem"
server.timeout_read_ms: int = 5000

# ── routes ──────────────────────────────────
route:health.path: str = "/healthz"
route:health.method: enum(get|post|put|delete) = get
route:health.auth: ref(policy)? = none

route:transfer.path: str = "/api/v1/transfers"
route:transfer.method: enum(get|post|put|delete) = post
route:transfer.auth: ref(policy)? = policy:maker
route:transfer.pipeline: ref(pipeline) = pipeline:two_eyes

# ── policies ────────────────────────────────
policy:maker.mechanism: enum(jwt|mtls) = jwt
policy:maker.roles: list(str) = ["maker"]

# ── approval pipeline ───────────────────────
pipeline:two_eyes.steps: list(ref(step)) = [step:make, step:check]
pipeline:two_eyes.on_reject: enum(halt|rollback) = rollback

step:make.role: str = "maker"
step:make.timeout_h: int = 24
step:check.role: str = "checker"
step:check.not_same_user_as: ref(step) = step:make
step:check.timeout_h: int = 48
`

// The same file in canonical form, per SPEC §12.
const specCanonical = `pipeline:two_eyes.on_reject: enum(halt|rollback) = rollback
pipeline:two_eyes.steps: list(ref(step)) = [step:make, step:check]
policy:maker.mechanism: enum(jwt|mtls) = jwt
policy:maker.roles: list(str) = ["maker"]
route:health.auth: ref(policy)? = none
route:health.method: enum(get|post|put|delete) = get
route:health.path: str = "/healthz"
route:transfer.auth: ref(policy)? = policy:maker
route:transfer.method: enum(get|post|put|delete) = post
route:transfer.path: str = "/api/v1/transfers"
route:transfer.pipeline: ref(pipeline) = pipeline:two_eyes
server.host: str = "0.0.0.0"
server.port: int = 8443
server.timeout_read_ms: int = 5000
server.tls.cert: str = "/etc/certs/server.pem"
server.tls.enabled: bool = true
step:check.not_same_user_as: ref(step) = step:make
step:check.role: str = "checker"
step:check.timeout_h: int = 48
step:make.role: str = "maker"
step:make.timeout_h: int = 24
`

func mustParse(t *testing.T, src string) []Fact {
	t.Helper()
	facts, errs := Parse([]byte(src))
	errs = append(errs, Validate(facts)...)
	for _, e := range errs {
		t.Errorf("unexpected error: %s", e.Error())
	}
	if t.Failed() {
		t.FailNow()
	}
	return facts
}

func TestSpecExampleCanonical(t *testing.T) {
	facts := mustParse(t, specInput)
	if got := string(Canonical(facts)); got != specCanonical {
		t.Errorf("canonical form mismatch:\ngot:\n%s\nwant:\n%s", got, specCanonical)
	}
}

func TestCanonicalIsIdempotent(t *testing.T) {
	facts := mustParse(t, specCanonical)
	if got := string(Canonical(facts)); got != specCanonical {
		t.Errorf("canonical form is not a fixed point:\n%s", got)
	}
}

func TestJSONRoundTrip(t *testing.T) {
	facts := mustParse(t, specInput)
	enc, err := EncodeJSON(facts)
	if err != nil {
		t.Fatal(err)
	}
	decoded, errs := DecodeJSON(enc)
	errs = append(errs, Validate(decoded)...)
	for _, e := range errs {
		t.Errorf("decode error: %s", e.Error())
	}
	if !bytes.Equal(Canonical(decoded), Canonical(facts)) {
		t.Errorf("decode(encode(F)) != canonical(F):\ngot:\n%s\nwant:\n%s",
			Canonical(decoded), Canonical(facts))
	}
}

func TestNonCanonicalSpacingAccepted(t *testing.T) {
	facts := mustParse(t, "server.port :int=8443\n")
	if got := facts[0].String(); got != "server.port: int = 8443" {
		t.Errorf("canonical line = %q", got)
	}
}

func TestValueCanonicalization(t *testing.T) {
	cases := []struct{ in, want string }{
		{`x.f: float = 1.5e-3`, `x.f: float = 0.0015`},
		{`x.f: float = 15.0`, `x.f: float = 15`},
		{`x.s: str = "A"`, `x.s: str = "A"`},
		{`x.l: list(int) = [1,2 , 3]`, `x.l: list(int) = [1, 2, 3]`},
		{`x.l: list(str) = []`, `x.l: list(str) = []`},
		{`x.o: int? = none`, `x.o: int? = none`},
		{`x.d: datetime = 2026-07-20`, `x.d: datetime = 2026-07-20`},
		{`x.d: datetime = 2026-09-01T09:30:00Z`, `x.d: datetime = 2026-09-01T09:30:00Z`},
		{`x.d: datetime = 2028-02-29T23:59:59Z`, `x.d: datetime = 2028-02-29T23:59:59Z`},
		{`x.d: datetime? = none`, `x.d: datetime? = none`},
		{`x.d: list(datetime) = [2026-01-01, 2026-04-01T00:00:00Z]`, `x.d: list(datetime) = [2026-01-01, 2026-04-01T00:00:00Z]`},
	}
	for _, c := range cases {
		facts, errs := Parse([]byte(c.in + "\n"))
		if len(errs) > 0 {
			t.Errorf("%s: %s", c.in, errs[0].Error())
			continue
		}
		if got := facts[0].String(); got != c.want {
			t.Errorf("canonical(%s) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestErrors(t *testing.T) {
	cases := []struct {
		name, src, code string
	}{
		{"E001 not a fact line", "just some words\n", "E001"},
		{"E002 digit-first segment", `server.9lives.on: bool = true` + "\n", "E002"},
		{"E002 illegal character", `server.tls-mode.on: bool = true` + "\n", "E002"},
		{"E003 two markers", `route:a.step:b.x: int = 1` + "\n", "E003"},
		{"E004 nested list", `x.a: list(list(int)) = [[1]]` + "\n", "E004"},
		{"E004 optional list", `x.a: list(int)? = none` + "\n", "E004"},
		{"E004 unknown type", `x.a: uint32 = 1` + "\n", "E004"},
		{"E004 empty enum", `x.a: enum() = a` + "\n", "E004"},
		{"E005 enum symbol not listed", `x.a: enum(get|post) = put` + "\n", "E005"},
		{"E005 int overflow", `x.a: int = 9223372036854775808` + "\n", "E005"},
		{"E005 leading zero int", `x.a: int = 007` + "\n", "E005"},
		{"E005 inline comment", `x.a: int = 1 # nope` + "\n", "E005"},
		{"E005 trailing comma", `x.a: list(int) = [1, 2,]` + "\n", "E005"},
		{"E005 unquoted string", `x.a: str = hello` + "\n", "E005"},
		{"E005 datetime offset", `x.a: datetime = 2026-09-01T09:30:00+02:00` + "\n", "E005"},
		{"E005 datetime zero offset", `x.a: datetime = 2026-09-01T09:30:00+00:00` + "\n", "E005"},
		{"E005 datetime missing Z", `x.a: datetime = 2026-09-01T09:30:00` + "\n", "E005"},
		{"E005 datetime lowercase z", `x.a: datetime = 2026-09-01T09:30:00z` + "\n", "E005"},
		{"E005 datetime fractional seconds", `x.a: datetime = 2026-09-01T09:30:00.5Z` + "\n", "E005"},
		{"E005 datetime Feb 30", `x.a: datetime = 2026-02-30` + "\n", "E005"},
		{"E005 datetime non-leap Feb 29", `x.a: datetime = 2027-02-29` + "\n", "E005"},
		{"E005 datetime month 13", `x.a: datetime = 2026-13-01` + "\n", "E005"},
		{"E005 datetime hour 24", `x.a: datetime = 2026-09-01T24:00:00Z` + "\n", "E005"},
		{"E005 datetime leap second", `x.a: datetime = 2026-06-30T23:59:60Z` + "\n", "E005"},
		{"E005 datetime year 0000", `x.a: datetime = 0000-01-01` + "\n", "E005"},
		{"E005 datetime unpadded", `x.a: datetime = 2026-7-20` + "\n", "E005"},
		{"E005 datetime quoted", `x.a: datetime = "2026-07-20"` + "\n", "E005"},
		{"E006 none on required", `x.a: int = none` + "\n", "E006"},
		{"E007 duplicate key", "x.a: int = 1\nx.a: int = 2\n", "E007"},
		{"E008 unresolved ref", `x.a: ref(policy) = policy:ghost` + "\n", "E008"},
		{"E009 ref kind mismatch", "step:make.role: str = \"m\"\nx.a: ref(policy) = step:make\n", "E009"},
		{"E010 marker moved", "route:a.x: int = 1\napi.route:a.y: int = 2\n", "E010"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			facts, errs := Parse([]byte(c.src))
			errs = append(errs, Validate(facts)...)
			for _, e := range errs {
				if e.Code == c.code {
					return
				}
			}
			var got []string
			for _, e := range errs {
				got = append(got, e.Error())
			}
			t.Errorf("want %s, got: %s", c.code, strings.Join(got, "; "))
		})
	}
}

// v0.2: segments are case-sensitive [a-zA-Z0-9_]; projected identifiers
// (Go) keep their source casing, and case distinguishes keys.
func TestCaseSensitiveKeys(t *testing.T) {
	src := `imports: list(str) = ["context", "errors"]
type:Poster.kind: enum(struct|iface|basic) = iface
type:Poster.method_Post_sig: str = "func(amount int) error"
type:MemLedger.implements: list(ref(type)) = [type:Poster]
func:Submit.exported: bool = true
func:submit.exported: bool = false
`
	facts := mustParse(t, src)
	if len(facts) != 6 {
		t.Fatalf("want 6 facts (Submit and submit are distinct keys), got %d", len(facts))
	}
	enc, err := EncodeJSON(facts)
	if err != nil {
		t.Fatal(err)
	}
	decoded, errs := DecodeJSON(enc)
	if len(errs) > 0 {
		t.Fatalf("decode: %s", errs[0].Error())
	}
	if !bytes.Equal(Canonical(decoded), Canonical(facts)) {
		t.Error("projection-profile facts do not round-trip through JSON")
	}
}

// v0.3: datetime values round-trip through JSON as strings, and the zone
// mistake gets its specific normative message (SPEC §14 E005).
func TestDatetime(t *testing.T) {
	src := `cert.expires: datetime = 2026-09-01T09:30:00Z
release.date: datetime = 2026-07-20
audit.windows: list(datetime) = [2026-01-01, 2026-04-01, 2026-07-01]
build.deployed: datetime? = none
`
	facts := mustParse(t, src)
	enc, err := EncodeJSON(facts)
	if err != nil {
		t.Fatal(err)
	}
	decoded, errs := DecodeJSON(enc)
	if len(errs) > 0 {
		t.Fatalf("decode: %s", errs[0].Error())
	}
	if !bytes.Equal(Canonical(decoded), Canonical(facts)) {
		t.Error("datetime facts do not round-trip through JSON")
	}

	_, perrs := Parse([]byte("x.a: datetime = 2026-09-01T09:30:00+02:00\n"))
	if len(perrs) != 1 || !strings.Contains(perrs[0].Msg, "datetime is UTC only (write Z)") {
		t.Errorf("offset value should get the UTC-only message, got %+v", perrs)
	}

	// Day precision and midnight are distinct values, so both survive in
	// one set and sort chronologically (bytewise = chronological).
	both := mustParse(t, "x.a: datetime = 2026-07-20\nx.b: datetime = 2026-07-20T00:00:00Z\n")
	if len(both) != 2 {
		t.Errorf("date and midnight forms must be distinct values, got %d facts", len(both))
	}
}

func TestErrorsDoNotCascade(t *testing.T) {
	src := "not a fact line\nserver.port: int = 8443\n???\n"
	facts, errs := Parse([]byte(src))
	if len(facts) != 1 || facts[0].Key != "server.port" {
		t.Errorf("good line should survive bad neighbors: %+v", facts)
	}
	if len(errs) != 2 {
		t.Errorf("want 2 errors, got %d", len(errs))
	}
}

func TestMergeByConcatenation(t *testing.T) {
	a := "server.port: int = 1\n"
	b := "server.host: str = \"h\"\n"
	facts := mustParse(t, a+b)
	if len(facts) != 2 {
		t.Errorf("concatenated disjoint files should validate, got %d facts", len(facts))
	}
}
