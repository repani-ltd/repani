package fact

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// Marshal serializes a Go struct as a canonical FACT document
// (SPEC §8): the struct is flattened to a fact set, validated, and
// emitted in canonical form — so equal values produce byte-identical
// documents.
//
// Field mapping, driven by `fact:"name,opts"` tags:
//
//   - The fact key is the tag name, or the snake_cased field name;
//     nested structs contribute dotted key prefixes ("motm.quote").
//     `fact:"-"` skips a field. Unexported fields are skipped.
//   - Scalars map to their base types: bool → bool, signed and
//     unsigned integers → int, floats → float, string → str,
//     time.Time → datetime (UTC, date-time form).
//   - A pointer to a scalar marshals as the optional type T?; nil is
//     the asserted "none" (SPEC §5, totality rule). A nil struct
//     pointer omits its subtree: a namespace has no none (SPEC §5,
//     existence rule), so a reader cannot tell a nil record from one
//     that was never set — a program that needs that distinction
//     asserts it with an explicit optional scalar.
//   - A slice of scalars maps to list(T).
//   - A slice of structs requires the `kind=K` tag option: each
//     element becomes an instance "K:id" with auto-assigned ids
//     (first letter of K + two-digit ordinal, counted per kind
//     across the document), and the field itself becomes one
//     list(ref(K)) fact carrying the order (SPEC §6.3). An element
//     that contributes no fact (no marshalled fields) is an error:
//     empty records cannot exist (SPEC §5).
//
// Maps, interfaces, enum and ref types are not supported — the
// vocabulary grows with its first real user, not before.
func Marshal(v any) ([]byte, error) {
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil, fmt.Errorf("fact: Marshal of nil pointer")
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil, fmt.Errorf("fact: Marshal of non-struct %s", rv.Kind())
	}
	m := &marshaller{ids: map[string]int{}}
	if err := m.walkStruct("", rv); err != nil {
		return nil, err
	}
	if errs := Validate(m.facts); len(errs) > 0 {
		return nil, fmt.Errorf("fact: marshalled set is invalid: %s", errs[0].Msg)
	}
	// A key that collides with an instance kind in Bind's nested view
	// (e.g. a field named like the kind) fails here, where the fix is
	// a rename, not at Unmarshal.
	if _, err := Bind(m.facts); err != nil {
		return nil, fmt.Errorf("fact: %w", err)
	}
	return Canonical(m.facts), nil
}

type marshaller struct {
	facts []Fact
	ids   map[string]int // kind -> instances assigned so far
}

// emit appends the fact "key: typeExpr = raw", where raw is the value in
// fact-line syntax. The value goes through checkValue exactly as a parsed
// line would, so the checker is the single authority on value syntax and
// canonical spelling: Marshal can never emit a fact Parse would reject
// (NaN, ±Inf, a datetime outside 0001-9999).
func (m *marshaller) emit(key, typeExpr, raw string) error {
	t, terr := parseType(typeExpr, 0)
	if terr != nil {
		return fmt.Errorf("fact: key %q: %s", key, terr.Msg)
	}
	if err := checkKey(key, 0); err != nil {
		return fmt.Errorf("fact: %s", err.Msg)
	}
	v, verr := checkValue(t, raw, 0)
	if verr != nil {
		return fmt.Errorf("fact: key %q: %s", key, verr.Msg)
	}
	m.facts = append(m.facts, Fact{Key: key, Type: t, Value: v})
	return nil
}

// list renders scalar tokens as a list value token.
func list(toks []string) string {
	return "[" + strings.Join(toks, ", ") + "]"
}

func (m *marshaller) walkStruct(prefix string, v reflect.Value) error {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, kind, skip := parseTag(f)
		if skip {
			continue
		}
		key := name
		if prefix != "" {
			key = prefix + "." + name
		}
		if err := m.walkField(key, kind, v.Field(i)); err != nil {
			return err
		}
	}
	return nil
}

func (m *marshaller) walkField(key, kind string, v reflect.Value) error {
	ft := v.Type()
	switch ft.Kind() {
	case reflect.Pointer:
		if ft.Elem().Kind() == reflect.Struct && ft.Elem() != reflect.TypeFor[time.Time]() {
			if v.IsNil() {
				return nil // omitted subtree
			}
			return m.walkStruct(key, v.Elem())
		}
		typeExpr, ok := scalarType(ft.Elem())
		if !ok {
			return fmt.Errorf("fact: key %q: unsupported pointer type %s", key, ft)
		}
		if v.IsNil() {
			return m.emit(key, typeExpr+"?", "none")
		}
		return m.emit(key, typeExpr+"?", scalarToken(v.Elem()))

	case reflect.Struct:
		if ft == reflect.TypeFor[time.Time]() {
			break // scalar; handled below
		}
		return m.walkStruct(key, v)

	case reflect.Slice:
		if ft.Elem().Kind() == reflect.Struct && ft.Elem() != reflect.TypeFor[time.Time]() {
			return m.walkInstances(key, kind, v)
		}
		typeExpr, ok := scalarType(ft.Elem())
		if !ok {
			return fmt.Errorf("fact: key %q: unsupported slice type %s", key, ft)
		}
		toks := make([]string, v.Len())
		for i := range toks {
			toks[i] = scalarToken(v.Index(i))
		}
		return m.emit(key, "list("+typeExpr+")", list(toks))
	}

	typeExpr, ok := scalarType(ft)
	if !ok {
		return fmt.Errorf("fact: key %q: unsupported type %s", key, ft)
	}
	return m.emit(key, typeExpr, scalarToken(v))
}

// walkInstances marshals a slice of structs as instances of kind plus
// one ordered list(ref(kind)) fact. Instance facts are rooted at the
// marker ("kind:id.field"), regardless of where the list lives — the
// list carries membership and order, the instances carry the data.
func (m *marshaller) walkInstances(key, kind string, v reflect.Value) error {
	if kind == "" {
		return fmt.Errorf("fact: key %q: slice of structs needs a `kind=` tag option", key)
	}
	if !IsSegment(kind) {
		return fmt.Errorf("fact: key %q: kind %q violates [a-zA-Z][a-zA-Z0-9_]*", key, kind)
	}
	toks := make([]string, v.Len())
	for i := range toks {
		m.ids[kind]++
		marker := fmt.Sprintf("%s:%c%02d", kind, kind[0], m.ids[kind])
		toks[i] = marker
		before := len(m.facts)
		if err := m.walkStruct(marker, v.Index(i)); err != nil {
			return err
		}
		if len(m.facts) == before {
			return fmt.Errorf("fact: key %q: element %d marshals to no fact; empty records cannot exist (SPEC §5)", key, i)
		}
	}
	return m.emit(key, "list(ref("+kind+"))", list(toks))
}

// scalarType maps a Go type to its FACT base type expression.
func scalarType(t reflect.Type) (string, bool) {
	if t == reflect.TypeFor[time.Time]() {
		return "datetime", true
	}
	switch t.Kind() {
	case reflect.Bool:
		return "bool", true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "int", true
	case reflect.Float32, reflect.Float64:
		return "float", true
	case reflect.String:
		return "str", true
	}
	return "", false
}

// scalarToken renders a scalar of a scalarType-supported type in
// fact-line value syntax. It is a renderer, not a checker: emit
// re-checks and canonicalizes the token (so a uint beyond int64, or a
// float32 whose shortest spelling differs from the float64 one, is the
// checker's business).
func scalarToken(v reflect.Value) string {
	if v.Type() == reflect.TypeFor[time.Time]() {
		return v.Interface().(time.Time).UTC().Format("2006-01-02T15:04:05Z")
	}
	switch v.Kind() {
	case reflect.Bool:
		return strconv.FormatBool(v.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(v.Uint(), 10)
	case reflect.Float32:
		return strconv.FormatFloat(v.Float(), 'g', -1, 32)
	case reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'g', -1, 64)
	case reflect.String:
		return Quote(v.String())
	}
	panic("fact: scalarToken of unsupported type " + v.Type().String())
}

// parseTag reads `fact:"name,opts"`: name (default snake_cased field
// name), the kind= option, and whether the field is skipped.
func parseTag(f reflect.StructField) (name, kind string, skip bool) {
	tag := f.Tag.Get("fact")
	if tag == "-" {
		return "", "", true
	}
	parts := strings.Split(tag, ",")
	name = parts[0]
	if name == "" {
		name = snake(f.Name)
	}
	for _, opt := range parts[1:] {
		if k, ok := strings.CutPrefix(opt, "kind="); ok {
			kind = k
		}
	}
	return name, kind, false
}

// snake lowercases a Go field name, inserting "_" at lower-to-upper
// transitions: TackleFromBehind -> tackle_from_behind, MOTM -> motm.
func snake(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			if i > 0 && s[i-1] >= 'a' && s[i-1] <= 'z' {
				b.WriteByte('_')
			}
			c += 'a' - 'A'
		}
		b.WriteByte(c)
	}
	return b.String()
}

// Unmarshal parses, validates, and binds a FACT document, then decodes
// the bound values into the struct pointed to by v, using the same
// field mapping as Marshal. Keys absent from the document leave their
// fields at zero values; an asserted "none" leaves a pointer field nil.
// Instance rows referenced through list(ref(kind)) decode into slices
// of structs, in list order.
func Unmarshal(data []byte, v any) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() || rv.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("fact: Unmarshal target must be a non-nil pointer to struct")
	}
	facts, errs := Load(data)
	if len(errs) > 0 {
		msgs := make([]string, len(errs))
		for i, e := range errs {
			msgs[i] = e.Error()
		}
		return fmt.Errorf("fact: %s", strings.Join(msgs, "; "))
	}
	bound, err := Bind(facts)
	if err != nil {
		return err
	}
	return decodeStruct(bound, rv.Elem())
}

func decodeStruct(m map[string]any, v reflect.Value) error {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, _, skip := parseTag(f)
		if skip {
			continue
		}
		raw, ok := m[name]
		if !ok {
			continue
		}
		if err := decodeValue(name, raw, v.Field(i)); err != nil {
			return err
		}
	}
	return nil
}

func decodeValue(key string, raw any, v reflect.Value) error {
	ft := v.Type()
	if ft == reflect.TypeFor[time.Time]() {
		s, ok := raw.(string)
		if !ok {
			return mismatch(key, raw, ft)
		}
		layout := "2006-01-02T15:04:05Z"
		if len(s) == 10 {
			layout = "2006-01-02"
		}
		tv, err := time.Parse(layout, s)
		if err != nil {
			return fmt.Errorf("fact: key %q: %v", key, err)
		}
		v.Set(reflect.ValueOf(tv))
		return nil
	}
	switch ft.Kind() {
	case reflect.Pointer:
		if raw == nil {
			return nil // asserted none
		}
		p := reflect.New(ft.Elem())
		if err := decodeValue(key, raw, p.Elem()); err != nil {
			return err
		}
		v.Set(p)
		return nil
	case reflect.Struct:
		mm, ok := raw.(map[string]any)
		if !ok {
			return mismatch(key, raw, ft)
		}
		return decodeStruct(mm, v)
	case reflect.Slice:
		arr, ok := raw.([]any)
		if !ok {
			return mismatch(key, raw, ft)
		}
		out := reflect.MakeSlice(ft, len(arr), len(arr))
		for i, e := range arr {
			if err := decodeValue(key, e, out.Index(i)); err != nil {
				return err
			}
		}
		v.Set(out)
		return nil
	case reflect.Bool:
		b, ok := raw.(bool)
		if !ok {
			return mismatch(key, raw, ft)
		}
		v.SetBool(b)
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, ok := raw.(int64)
		if !ok {
			return mismatch(key, raw, ft)
		}
		if v.OverflowInt(n) {
			return fmt.Errorf("fact: key %q: %d overflows %s", key, n, ft)
		}
		v.SetInt(n)
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, ok := raw.(int64)
		if !ok {
			return mismatch(key, raw, ft)
		}
		if n < 0 || v.OverflowUint(uint64(n)) {
			return fmt.Errorf("fact: key %q: %d overflows %s", key, n, ft)
		}
		v.SetUint(uint64(n))
		return nil
	case reflect.Float32, reflect.Float64:
		switch x := raw.(type) {
		case float64:
			v.SetFloat(x)
		case int64: // an int fact is welcome in a float field
			v.SetFloat(float64(x))
		default:
			return mismatch(key, raw, ft)
		}
		return nil
	case reflect.String:
		s, ok := raw.(string)
		if !ok {
			return mismatch(key, raw, ft)
		}
		v.SetString(s)
		return nil
	}
	return fmt.Errorf("fact: key %q: unsupported target type %s", key, ft)
}

func mismatch(key string, raw any, want reflect.Type) error {
	return fmt.Errorf("fact: key %q: cannot decode %T into %s", key, raw, want)
}
