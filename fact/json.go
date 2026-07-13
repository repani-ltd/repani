package fact

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
)

// jsonFact is one entry of the canonical JSON encoding (SPEC §10).
type jsonFact struct {
	Key   string          `json:"key"`
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value"`
}

// EncodeJSON renders facts as the canonical JSON encoding: an array of
// {key, type, value} objects sorted by key.
func EncodeJSON(facts []Fact) ([]byte, error) {
	sorted := append([]Fact(nil), facts...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Key < sorted[j].Key })
	arr := make([]jsonFact, len(sorted))
	for i, f := range sorted {
		arr[i] = jsonFact{Key: f.Key, Type: f.Type.Raw, Value: valueJSON(f)}
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(arr); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func valueJSON(f Fact) json.RawMessage {
	if f.Value.None {
		return json.RawMessage("null")
	}
	toks := make([]string, len(f.Value.Elems))
	for i, e := range f.Value.Elems {
		toks[i] = scalarJSON(f.Type.Base, e)
	}
	if f.Value.List {
		return json.RawMessage("[" + strings.Join(toks, ", ") + "]")
	}
	return json.RawMessage(toks[0])
}

// scalarJSON maps a canonical scalar token to its JSON encoding. Canonical
// bool/int/float/str tokens are already valid JSON; enum symbols and ref
// markers become JSON strings.
func scalarJSON(b BaseKind, tok string) string {
	if b == Enum || b == Ref {
		quoted, _ := json.Marshal(tok) // tokens are [a-z0-9_:]+, never escaped
		return string(quoted)
	}
	return tok
}

// DecodeJSON parses the canonical JSON encoding back into facts, applying
// the same line-local checks as Parse. Error line numbers refer to the
// 1-based position of the entry in the JSON array.
func DecodeJSON(data []byte) ([]Fact, []Error) {
	var arr []jsonFact
	if err := json.Unmarshal(data, &arr); err != nil {
		return nil, []Error{{1, "E001", "invalid JSON: " + err.Error()}}
	}
	var facts []Fact
	var errs []Error
	for i, jf := range arr {
		n := i + 1
		if err := checkKey(jf.Key, n); err != nil {
			errs = append(errs, *err)
			continue
		}
		t, terr := parseType(jf.Type, n)
		if terr != nil {
			errs = append(errs, *terr)
			continue
		}
		tok, ok := jsonValueToken(t, jf.Value)
		if !ok {
			errs = append(errs, Error{n, "E005", "value does not match the JSON encoding of " + t.Raw})
			continue
		}
		v, verr := checkValue(t, tok, n)
		if verr != nil {
			errs = append(errs, *verr)
			continue
		}
		facts = append(facts, Fact{Key: jf.Key, Type: t, Value: v, Line: n})
	}
	return facts, errs
}

// jsonValueToken converts a JSON-encoded value back into fact-line value
// syntax; checkValue then re-validates and canonicalizes it.
func jsonValueToken(t Type, raw json.RawMessage) (string, bool) {
	raw = bytes.TrimSpace(raw)
	if string(raw) == "null" {
		return "none", true
	}
	if len(raw) > 0 && raw[0] == '[' {
		var elems []json.RawMessage
		if err := json.Unmarshal(raw, &elems); err != nil {
			return "", false
		}
		toks := make([]string, len(elems))
		for i, e := range elems {
			tok, ok := jsonScalarToken(t.Base, e)
			if !ok {
				return "", false
			}
			toks[i] = tok
		}
		return "[" + strings.Join(toks, ", ") + "]", true
	}
	return jsonScalarToken(t.Base, raw)
}

func jsonScalarToken(b BaseKind, raw json.RawMessage) (string, bool) {
	raw = bytes.TrimSpace(raw)
	if b == Enum || b == Ref {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return "", false
		}
		return s, true
	}
	// bool/int/float/str: the JSON token and the fact-line token coincide.
	return string(raw), true
}
