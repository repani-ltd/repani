package fact

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Instance is the facts of one instance (SPEC §3, the instance marker)
// read as one record, the way an import file is read: the lines
// user:alice.name and user:alice.roles are one Instance, of Kind
// "user" and ID "alice". Prefix holds the key's segments before the
// marker, empty when the marker is the key's first segment. Fields are
// the instance's facts in source order, each Key the path after the
// marker ("name", "payload.amount"), empty for a fact keyed by the
// marker itself. Line is the line of its first fact.
//
// The grouping is a view; the set stays flat (SPEC §5).
type Instance struct {
	Prefix   string
	Kind, ID string
	Line     int
	Fields   []Fact
}

// Instances groups a set's facts by instance, in the order each
// instance first appears, and returns the singleton facts, those with
// no marker, apart and in order. Instances of one kind and id under
// two prefixes are two instances; Validate refuses that set.
func Instances(facts []Fact) (instances []Instance, singletons []Fact) {
	at := map[string]int{} // prefix through the marker -> index
	for _, f := range facts {
		marker, through := keyMarker(f.Key)
		if marker == "" {
			singletons = append(singletons, f)
			continue
		}
		i, seen := at[through]
		if !seen {
			kind, id, _ := strings.Cut(marker, ":")
			prefix := strings.TrimSuffix(strings.TrimSuffix(through, marker), ".")
			i = len(instances)
			at[through] = i
			instances = append(instances, Instance{Prefix: prefix, Kind: kind, ID: id, Line: f.Line})
		}
		field := f
		field.Key = strings.TrimPrefix(strings.TrimPrefix(f.Key, through), ".")
		instances[i].Fields = append(instances[i].Fields, field)
	}
	return instances, singletons
}

// LoadInstances loads and validates src as an import file of records:
// every fact belongs to an instance of one of kinds, keyed from its
// first segment (kind:id.field). It refuses the first error Load
// finds, a singleton fact, an instance of another kind and one under a
// prefix, naming the line.
func LoadInstances(src []byte, kinds ...string) ([]Instance, error) {
	facts, errs := Load(src)
	if len(errs) > 0 {
		return nil, errs[0]
	}
	instances, singletons := Instances(facts)
	if len(singletons) > 0 {
		f := singletons[0]
		return nil, fmt.Errorf("line %d: key %q is not %s:ID.FIELD", f.Line, f.Key, strings.Join(kinds, "|"))
	}
	for _, in := range instances {
		if in.Prefix != "" || !slices.Contains(kinds, in.Kind) {
			return nil, fmt.Errorf("line %d: %s:%s is not of %s, at the key's start", in.Line, in.Kind, in.ID, strings.Join(kinds, " or "))
		}
	}
	return instances, nil
}

// One is the fact's one value as text (Text), refusing none or a list
// of other than one element, naming the line.
func (f Fact) One() (string, error) {
	t := f.Text()
	if len(t) != 1 {
		return "", fmt.Errorf("line %d: %s takes one value", f.Line, f.Key)
	}
	return t[0], nil
}

// Text is the fact's value as text, element by element: a str
// unquoted, any other token as written (an int, a bool, a float, a
// datetime, an enum symbol, a ref's marker). A scalar is one element,
// a list its elements, none no element.
func (f Fact) Text() []string {
	if f.Value.None {
		return nil
	}
	out := make([]string, len(f.Value.Elems))
	for i, tok := range f.Value.Elems {
		out[i] = tok
		if f.Type.Base == Str {
			out[i] = unquote(tok)
		}
	}
	return out
}

// unquote reads a canonical str token, which checkValue has proven a
// JSON string.
func unquote(tok string) string {
	if plainString(tok) {
		return tok[1 : len(tok)-1]
	}
	var s string
	if err := json.Unmarshal([]byte(tok), &s); err != nil {
		return tok
	}
	return s
}
