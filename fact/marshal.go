package fact

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// Marker is a reference value ("kind:id") for struct fields marshalled as
// ref(kind). The kind parameter comes from the field's `kind=` tag
// option — the tag names the domain, the value names the inhabitant
// (SPEC §6.1).
type Marker string

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
//     pointer omits that subtree entirely.
//   - A slice of scalars maps to list(T).
//   - A slice of structs requires the `kind=K` tag option: each
//     element becomes an instance "K:id" with auto-assigned ids
//     (first letter of K + two-digit ordinal, counted per kind
//     across the document), and the field itself becomes one
//     list(ref(K)) fact carrying the order (SPEC §6.3).
//   - A Marker (or slice of Marker) requires `kind=K` and marshals as
//     ref(K); values must carry the "K:" prefix.
//
// Maps, interfaces, and enum types are not supported — the vocabulary
// grows with its first real user, not before.
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
	// A root-level key equal to an instance kind collides in Bind's
	// nested view (root[kind] is the kind's instance namespace) —
	// fail here, where the fix is a rename, not at Unmarshal.
	for _, f := range m.facts {
		if !strings.ContainsAny(f.Key, ".:") && m.ids[f.Key] > 0 {
			return nil, fmt.Errorf("fact: field key %q collides with instance kind %q under Bind; rename the field or the kind", f.Key, f.Key)
		}
	}
	if errs := Validate(m.facts); len(errs) > 0 {
		return nil, fmt.Errorf("fact: marshalled set is invalid: %s", errs[0].Msg)
	}
	return Canonical(m.facts), nil
}

type marshaller struct {
	facts []Fact
	ids   map[string]int // kind -> instances assigned so far
}

func (m *marshaller) emit(key, typeExpr string, v Value) error {
	t, terr := parseType(typeExpr, 0)
	if terr != nil {
		return fmt.Errorf("fact: key %q: %s", key, terr.Msg)
	}
	if err := checkKey(key, 0); err != nil {
		return fmt.Errorf("fact: %s", err.Msg)
	}
	// The value is re-checked as its line token so that Marshal can
	// never emit a fact Parse would reject (NaN, ±Inf, a datetime
	// outside 0001-9999): the checker is the single authority on
	// value syntax.
	if _, err := checkValue(t, v.token(), 0); err != nil {
		return fmt.Errorf("fact: key %q: %s", key, err.Msg)
	}
	m.facts = append(m.facts, Fact{Key: key, Type: t, Value: v})
	return nil
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

	// Ref and []Ref: the tag supplies the domain kind.
	if ft == reflect.TypeFor[Marker]() {
		tok, err := refToken(key, kind, v.String())
		if err != nil {
			return err
		}
		return m.emit(key, "ref("+kind+")", Value{Elems: []string{tok}})
	}
	if ft.Kind() == reflect.Slice && ft.Elem() == reflect.TypeFor[Marker]() {
		toks := make([]string, v.Len())
		for i := range toks {
			tok, err := refToken(key, kind, v.Index(i).String())
			if err != nil {
				return err
			}
			toks[i] = tok
		}
		return m.emit(key, "list(ref("+kind+"))", Value{List: true, Elems: toks})
	}

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
			return m.emit(key, typeExpr+"?", Value{None: true})
		}
		tok, err := scalarToken(key, v.Elem())
		if err != nil {
			return err
		}
		return m.emit(key, typeExpr+"?", Value{Elems: []string{tok}})

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
			tok, err := scalarToken(key, v.Index(i))
			if err != nil {
				return err
			}
			toks[i] = tok
		}
		return m.emit(key, "list("+typeExpr+")", Value{List: true, Elems: toks})
	}

	typeExpr, ok := scalarType(ft)
	if !ok {
		return fmt.Errorf("fact: key %q: unsupported type %s", key, ft)
	}
	tok, err := scalarToken(key, v)
	if err != nil {
		return err
	}
	return m.emit(key, typeExpr, Value{Elems: []string{tok}})
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
		if err := m.walkStruct(marker, v.Index(i)); err != nil {
			return err
		}
	}
	return m.emit(key, "list(ref("+kind+"))", Value{List: true, Elems: toks})
}

func refToken(key, kind, val string) (string, error) {
	if kind == "" {
		return "", fmt.Errorf("fact: key %q: Marker field needs a `kind=` tag option", key)
	}
	if !strings.HasPrefix(val, kind+":") {
		return "", fmt.Errorf("fact: key %q: ref value %q is not of kind %q", key, val, kind)
	}
	return val, nil
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

// scalarToken renders one scalar as its canonical value token.
func scalarToken(key string, v reflect.Value) (string, error) {
	if v.Type() == reflect.TypeFor[time.Time]() {
		return v.Interface().(time.Time).UTC().Format("2006-01-02T15:04:05Z"), nil
	}
	switch v.Kind() {
	case reflect.Bool:
		return strconv.FormatBool(v.Bool()), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if v.Uint() > 1<<63-1 {
			return "", fmt.Errorf("fact: key %q: %d overflows 64-bit signed integer", key, v.Uint())
		}
		return strconv.FormatUint(v.Uint(), 10), nil
	case reflect.Float32:
		return strconv.FormatFloat(v.Float(), 'g', -1, 32), nil
	case reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'g', -1, 64), nil
	case reflect.String:
		return Quote(v.String()), nil
	}
	return "", fmt.Errorf("fact: key %q: unsupported type %s", key, v.Type())
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
// of structs, in list order. Marker fields cannot be decoded (Bind
// resolves refs to their instances — receive them as struct fields).
func Unmarshal(data []byte, v any) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() || rv.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("fact: Unmarshal target must be a non-nil pointer to struct")
	}
	facts, errs := Parse(data)
	errs = append(errs, Validate(facts)...)
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
	if ft == reflect.TypeFor[Marker]() {
		return fmt.Errorf("fact: key %q: refs bind to their instances; decode into a struct field", key)
	}
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
