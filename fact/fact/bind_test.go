package fact

import (
	"strings"
	"testing"
	"text/template"
)

func mustBind(t *testing.T, src string) map[string]any {
	t.Helper()
	facts := mustParse(t, src)
	m, err := Bind(facts)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestBindScalars(t *testing.T) {
	m := mustBind(t, `issue: int = 187
temp: float = 26.437
on: bool = true
name: str = "hello\nworld"
mode: enum(fast|slow) = fast
extra: str? = none
tags: list(str) = ["a", "b"]
`)
	if m["issue"] != int64(187) || m["temp"] != 26.437 || m["on"] != true {
		t.Errorf("scalar binding wrong: %+v", m)
	}
	if m["name"] != "hello\nworld" || m["mode"] != "fast" || m["extra"] != nil {
		t.Errorf("string/enum/none binding wrong: %+v", m)
	}
	tags, ok := m["tags"].([]any)
	if !ok || len(tags) != 2 || tags[0] != "a" {
		t.Errorf("list binding wrong: %#v", m["tags"])
	}
}

func TestBindNestedKeys(t *testing.T) {
	m := mustBind(t, `current.wind.kt: float = 11.6
current.observed: str = "05:30"
`)
	cur := m["current"].(map[string]any)
	if cur["observed"] != "05:30" || cur["wind"].(map[string]any)["kt"] != 11.6 {
		t.Errorf("nested binding wrong: %+v", m)
	}
}

func TestBindInstancesAndOrderedRefs(t *testing.T) {
	m := mustBind(t, `daily: list(ref(day)) = [day:d1, day:d0]
day:d0.hi: float = 31.2
day:d0.summary: str = "sunny"
day:d1.hi: float = 30.4
day:d1.summary: str = "cloudy"
`)
	rows, ok := m["daily"].([]any)
	if !ok || len(rows) != 2 {
		t.Fatalf("daily should be 2 rows, got %#v", m["daily"])
	}
	// Order comes from the ref list (d1 first), not from id sorting.
	first := rows[0].(map[string]any)
	if first["summary"] != "cloudy" || first["hi"] != 30.4 {
		t.Errorf("row order/content wrong: %+v", first)
	}
	// Instances are also reachable by kind/id path, sharing the same map.
	byPath := m["day"].(map[string]any)["d1"].(map[string]any)
	if byPath["summary"] != "cloudy" {
		t.Errorf("instance path access wrong: %+v", byPath)
	}
}

func TestBindScalarRefResolvesToInstance(t *testing.T) {
	m := mustBind(t, `route.auth: ref(policy) = policy:maker
policy:maker.mechanism: enum(jwt|mtls) = jwt
`)
	auth := m["route"].(map[string]any)["auth"].(map[string]any)
	if auth["mechanism"] != "jwt" {
		t.Errorf("ref should resolve to instance map: %+v", auth)
	}
}

func TestBindPathCollision(t *testing.T) {
	facts := mustParse(t, "a.b: int = 1\na.b.c: int = 2\n")
	if _, err := Bind(facts); err == nil {
		t.Error("want collision error for a.b vs a.b.c")
	}
}

func TestBindTemplateExecution(t *testing.T) {
	m := mustBind(t, `title: str = "Weather"
current.temp: float = 26.437
daily: list(ref(day)) = [day:d0, day:d1]
day:d0.date: str = "Mon"
day:d0.hi: float = 31.2
day:d1.date: str = "Tue"
day:d1.hi: float = 30.4
sources: list(str) = ["https://a", "https://b"]
`)
	tmpl := template.Must(template.New("t").Parse(
		`{{.title}} {{.current.temp}}{{range .daily}} {{.date}}={{.hi}}{{end}}{{range .sources}} {{.}}{{end}}`))
	var b strings.Builder
	if err := tmpl.Execute(&b, m); err != nil {
		t.Fatal(err)
	}
	want := "Weather 26.437 Mon=31.2 Tue=30.4 https://a https://b"
	if b.String() != want {
		t.Errorf("template output:\n got %q\nwant %q", b.String(), want)
	}
}

func TestQuote(t *testing.T) {
	facts, errs := Parse([]byte("body: str = " + Quote("line one\nline \"two\" <b>") + "\n"))
	if len(errs) > 0 {
		t.Fatalf("Quote output must parse: %v", errs[0])
	}
	m, err := Bind(facts)
	if err != nil {
		t.Fatal(err)
	}
	if m["body"] != "line one\nline \"two\" <b>" {
		t.Errorf("Quote round trip wrong: %q", m["body"])
	}
}

func TestBindDatetime(t *testing.T) {
	facts, errs := Parse([]byte("when: datetime = 2026-07-20T09:30:00Z\n"))
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	m, err := Bind(facts)
	if err != nil {
		t.Fatalf("bind datetime: %v", err)
	}
	if m["when"] != "2026-07-20T09:30:00Z" {
		t.Errorf("bound datetime = %#v", m["when"])
	}
}
