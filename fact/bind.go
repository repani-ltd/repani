package fact

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Bind materializes a validated fact set as native Go values, for
// direct consumption by text/template and similar consumers. No
// intermediate encoding is involved.
//
// Dotted key paths become nested maps: "current.temp" binds to
// m["current"].(map[string]any)["temp"]. An instance marker segment
// "kind:id" contributes two levels (kind, then id), so "day:d10.hi"
// binds to m["day"].(map[string]any)["d10"].(map[string]any)["hi"].
// The nesting is a view for value access; the fact set itself stays
// flat (SPEC §5).
//
// Values bind by declared type: bool → bool, int → int64,
// float → float64, str and enum symbols → string, none → nil,
// list(T) → []any. A ref value binds to the referenced instance's
// map, shared rather than copied — so an ordered relationship like
// "daily: list(ref(day))" binds to an ordered []any of row maps,
// ready for {{range}}.
//
// Bind expects a set that has passed Validate. Its own error classes
// are path collisions in the nested view: a key that is both a value
// and a namespace prefix of another key (e.g. "a.b" alongside
// "a.b.c"), and a singleton key inside a kind's instance namespace
// (e.g. "day.foo" alongside "day:d1.hi" — the flat set keeps them
// apart, the view cannot).
func Bind(facts []Fact) (map[string]any, error) {
	root := map[string]any{}
	reg := map[string]map[string]any{} // marker -> shared instance map
	kinds := map[string]bool{}         // key prefix through the kind ("api.day")
	markers := make([]string, len(facts))
	for i, f := range facts {
		marker, prefix := keyMarker(f.Key)
		markers[i] = marker
		if marker == "" {
			continue
		}
		if reg[marker] == nil {
			reg[marker] = map[string]any{}
		}
		kinds[strings.TrimSuffix(prefix, marker[strings.IndexByte(marker, ':'):])] = true
	}
	// Instance facts first, so their registry maps are wired into the
	// tree before any singleton key walks the same path.
	for _, pass := range [2]bool{true, false} {
		for i, f := range facts {
			marker := markers[i]
			if (marker != "") != pass {
				continue
			}
			if marker == "" {
				if k := kindPrefix(f.Key, kinds); k != "" {
					return nil, fmt.Errorf("bind %q: key collides with instance kind %q; rename the key or the kind", f.Key, k)
				}
			}
			val, err := bindValue(f.Type, f.Value, reg)
			if err != nil {
				return nil, fmt.Errorf("bind %q: %w", f.Key, err)
			}
			if err := bindPlace(root, reg, f.Key, val); err != nil {
				return nil, err
			}
		}
	}
	return root, nil
}

// kindPrefix returns the kind namespace a singleton key lies in (the
// key itself or one of its dotted prefixes being a kind path), or "".
func kindPrefix(key string, kinds map[string]bool) string {
	segs := strings.Split(key, ".")
	for i := range segs {
		if p := strings.Join(segs[:i+1], "."); kinds[p] {
			return p
		}
	}
	return ""
}

// bindPlace walks key's segments, creating nested maps as needed, and
// sets the final level to val. A marker segment expands to two levels
// with the id level backed by the instance's shared registry map.
func bindPlace(root map[string]any, reg map[string]map[string]any, key string, val any) error {
	type level struct {
		name string
		inst map[string]any // non-nil: this level is an instance id
	}
	var levels []level
	for _, seg := range strings.Split(key, ".") {
		if kind, id, isMarker := strings.Cut(seg, ":"); isMarker {
			levels = append(levels, level{name: kind}, level{name: id, inst: reg[seg]})
		} else {
			levels = append(levels, level{name: seg})
		}
	}
	cur := root
	for _, lv := range levels[:len(levels)-1] {
		next, exists := cur[lv.name]
		if !exists {
			m := lv.inst
			if m == nil {
				m = map[string]any{}
			}
			cur[lv.name] = m
			cur = m
			continue
		}
		m, ok := next.(map[string]any)
		if !ok {
			return fmt.Errorf("bind %q: segment %q is both a value and a namespace", key, lv.name)
		}
		cur = m
	}
	leaf := levels[len(levels)-1]
	if _, exists := cur[leaf.name]; exists {
		return fmt.Errorf("bind %q: segment %q is both a value and a namespace", key, leaf.name)
	}
	cur[leaf.name] = val
	return nil
}

func bindValue(t Type, v Value, reg map[string]map[string]any) (any, error) {
	if v.None {
		return nil, nil
	}
	if v.List {
		out := make([]any, len(v.Elems))
		for i, tok := range v.Elems {
			x, err := bindScalar(t, tok, reg)
			if err != nil {
				return nil, err
			}
			out[i] = x
		}
		return out, nil
	}
	return bindScalar(t, v.Elems[0], reg)
}

func bindScalar(t Type, tok string, reg map[string]map[string]any) (any, error) {
	switch t.Base {
	case Bool:
		return tok == "true", nil
	case Int:
		return strconv.ParseInt(tok, 10, 64)
	case Float:
		return strconv.ParseFloat(tok, 64)
	case Str:
		if plainString(tok) {
			return tok[1 : len(tok)-1], nil
		}
		var s string
		if err := json.Unmarshal([]byte(tok), &s); err != nil {
			return nil, err
		}
		return s, nil
	case Datetime, Enum:
		return tok, nil
	case Ref:
		m := reg[tok]
		if m == nil {
			return nil, fmt.Errorf("unresolved ref %q (run Validate before Bind)", tok)
		}
		return m, nil
	}
	return nil, fmt.Errorf("unknown base type for %q", tok)
}
