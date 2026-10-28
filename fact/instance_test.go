package fact

import (
	"reflect"
	"strings"
	"testing"
)

func TestInstances(t *testing.T) {
	facts, errs := Load([]byte(`title: str = "Users"
user:alice.name: str = "Alice \"Al\" Ng"
user:alice.roles: list(str) = ["ops", "treasury"]
user:bob.name: str = "Bob"
book:b1.payload.amount: int = 500
user:alice.active: bool = true
api.day:d1.hi: int = 9
`))
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	got, singletons := Instances(facts)
	if len(singletons) != 1 || singletons[0].Key != "title" || singletons[0].Text()[0] != "Users" {
		t.Fatalf("singletons %+v", singletons)
	}
	type inst struct {
		prefix, kind, id string
		line             int
		keys             []string
	}
	var view []inst
	for _, in := range got {
		v := inst{in.Prefix, in.Kind, in.ID, in.Line, nil}
		for _, f := range in.Fields {
			v.keys = append(v.keys, f.Key)
		}
		view = append(view, v)
	}
	want := []inst{
		{"", "user", "alice", 2, []string{"name", "roles", "active"}},
		{"", "user", "bob", 4, []string{"name"}},
		{"", "book", "b1", 5, []string{"payload.amount"}},
		{"api", "day", "d1", 7, []string{"hi"}},
	}
	if !reflect.DeepEqual(view, want) {
		t.Fatalf("\ngot  %+v\nwant %+v", view, want)
	}
	alice := got[0].Fields
	if s := alice[0].Text(); len(s) != 1 || s[0] != `Alice "Al" Ng` {
		t.Fatal(s)
	}
	if s := alice[1].Text(); !reflect.DeepEqual(s, []string{"ops", "treasury"}) {
		t.Fatal(s)
	}
	if s := alice[2].Text(); s[0] != "true" || alice[2].Line != 6 {
		t.Fatal(s, alice[2].Line)
	}
	if s := got[2].Fields[0].Text(); s[0] != "500" {
		t.Fatal(s)
	}
}

func TestLoadInstances(t *testing.T) {
	ins, err := LoadInstances([]byte("request:a.makers: list(str) = [\"ops\"]\nsee:ops.classes: list(str) = [\"kyc\"]\n"), "request", "see")
	if err != nil || len(ins) != 2 || ins[1].Kind != "see" {
		t.Fatal(ins, err)
	}
	if v, err := ins[0].Fields[0].One(); err != nil || v != "ops" {
		t.Fatal(v, err)
	}
	for src, want := range map[string]string{
		"user:a.name: str = \"A\"\nuser:a.name: str = \"B\"\n": "E00", // Load's own refusal, a duplicate key
		"title: str = \"x\"\n":                                 `line 1: key "title" is not user:ID.FIELD`,
		"role:r.name: str = \"x\"\n":                           "line 1: role:r is not of user",
		"api.user:a.name: str = \"x\"\n":                       "line 1: user:a is not of user, at the key's start",
	} {
		if _, err := LoadInstances([]byte(src), "user"); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want %q", src, err, want)
		}
	}
	ins, _ = LoadInstances([]byte("user:a.roles: list(str) = [\"x\", \"y\"]\n"), "user")
	if _, err := ins[0].Fields[0].One(); err == nil || err.Error() != "line 1: roles takes one value" {
		t.Fatal(err)
	}
}

func TestTextOfNone(t *testing.T) {
	facts, errs := Load([]byte("a: str? = none\n"))
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	if s := facts[0].Text(); s != nil {
		t.Fatal(s)
	}
}
