package ops

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Field describes one parameter, derived from struct tags:
//
//	json:"marginTop"  help:"..."  default:"10mm"  enum:"print,screen"
//
// The same description drives CLI flags (--margin-top), multipart form fields, JSON bodies
// and the OpenAPI schema, which keeps the CLI and the API identical.
type Field struct {
	Name    string // JSON / form name
	Flag    string // CLI flag name
	Help    string
	Default string
	Enum    []string
	Kind    reflect.Kind
	Elem    reflect.Kind // for slices and maps
	index   []int
}

// Fields lists the parameters of a params struct (pointer), including embedded structs.
func Fields(p any) []Field {
	var out []Field
	collect(reflect.TypeOf(p).Elem(), nil, &out)
	return out
}

func collect(t reflect.Type, prefix []int, out *[]Field) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		idx := append(append([]int{}, prefix...), i)
		if f.Anonymous && f.Type.Kind() == reflect.Struct {
			collect(f.Type, idx, out)
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "" || name == "-" || !f.IsExported() {
			continue
		}
		fd := Field{Name: name, Flag: kebab(name), Help: f.Tag.Get("help"), Default: f.Tag.Get("default"), Kind: f.Type.Kind(), index: idx}
		if e := f.Tag.Get("enum"); e != "" {
			fd.Enum = strings.Split(e, ",")
		}
		if fd.Kind == reflect.Slice || fd.Kind == reflect.Map {
			fd.Elem = f.Type.Elem().Kind()
		}
		*out = append(*out, fd)
	}
}

func kebab(s string) string {
	var b strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(unicode.ToLower(r))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// FieldValue returns the reflect.Value of a field in p.
func FieldValue(p any, f Field) reflect.Value {
	return reflect.ValueOf(p).Elem().FieldByIndex(f.index)
}

// ApplyDefaults sets every field to its `default` tag.
func ApplyDefaults(p any) error {
	for _, f := range Fields(p) {
		if f.Default == "" {
			continue
		}
		if err := setString(FieldValue(p, f), f, f.Default, false); err != nil {
			return fmt.Errorf("default of %s: %w", f.Name, err)
		}
	}
	return nil
}

// SetString sets a field from its textual form (form fields, CLI); slices append when add is true.
func setString(v reflect.Value, f Field, s string, add bool) error {
	switch f.Kind {
	case reflect.String:
		v.SetString(s)
	case reflect.Bool:
		b, err := strconv.ParseBool(strings.TrimSpace(s))
		if err != nil {
			return fmt.Errorf("%s: %q is not a boolean", f.Name, s)
		}
		v.SetBool(b)
	case reflect.Int:
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			return fmt.Errorf("%s: %q is not an integer", f.Name, s)
		}
		v.SetInt(int64(n))
	case reflect.Float64:
		n, err := strconv.ParseFloat(strings.TrimSpace(strings.Replace(s, ",", ".", 1)), 64)
		if err != nil {
			return fmt.Errorf("%s: %q is not a number", f.Name, s)
		}
		v.SetFloat(n)
	case reflect.Slice:
		var items []string
		trimmed := strings.TrimSpace(s)
		if strings.HasPrefix(trimmed, "[") {
			if err := json.Unmarshal([]byte(trimmed), &items); err != nil {
				var nums []json.Number
				if err2 := json.Unmarshal([]byte(trimmed), &nums); err2 != nil {
					return fmt.Errorf("%s: %v", f.Name, err)
				}
				for _, n := range nums {
					items = append(items, n.String())
				}
			}
		} else if f.Elem == reflect.Int {
			for _, part := range strings.Split(trimmed, ",") {
				if part = strings.TrimSpace(part); part != "" {
					items = append(items, part)
				}
			}
		} else if trimmed != "" {
			items = []string{s}
		}
		if !add {
			v.Set(reflect.MakeSlice(v.Type(), 0, len(items)))
		}
		for _, it := range items {
			ev := reflect.New(v.Type().Elem()).Elem()
			if err := setString(ev, Field{Name: f.Name, Kind: f.Elem}, it, false); err != nil {
				return err
			}
			v.Set(reflect.Append(v, ev))
		}
	case reflect.Map:
		if v.IsNil() || !add {
			v.Set(reflect.MakeMap(v.Type()))
		}
		trimmed := strings.TrimSpace(s)
		if strings.HasPrefix(trimmed, "{") {
			m := map[string]string{}
			if err := json.Unmarshal([]byte(trimmed), &m); err != nil {
				return fmt.Errorf("%s: %v", f.Name, err)
			}
			for k, val := range m {
				v.SetMapIndex(reflect.ValueOf(k), reflect.ValueOf(val))
			}
			return nil
		}
		k, val, ok := strings.Cut(s, "=")
		if !ok {
			return fmt.Errorf("%s: expected key=value, got %q", f.Name, s)
		}
		v.SetMapIndex(reflect.ValueOf(strings.TrimSpace(k)), reflect.ValueOf(val))
	default:
		return fmt.Errorf("%s: unsupported kind %s", f.Name, f.Kind)
	}
	if len(f.Enum) > 0 && f.Kind == reflect.String {
		ok := false
		for _, e := range f.Enum {
			if strings.EqualFold(e, v.String()) {
				v.SetString(e)
				ok = true
			}
		}
		if !ok {
			return fmt.Errorf("%s: %q is not one of %s", f.Name, v.String(), strings.Join(f.Enum, ", "))
		}
	}
	return nil
}

// BindForm sets fields from multipart form values (name -> values). Unknown names are errors.
func BindForm(p any, values map[string][]string) error {
	byName := map[string]Field{}
	for _, f := range Fields(p) {
		byName[strings.ToLower(f.Name)] = f
		byName[f.Flag] = f
	}
	names := make([]string, 0, len(values))
	for k := range values {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		if k == "options" {
			for _, js := range values[k] {
				if err := BindJSON(p, []byte(js)); err != nil {
					return err
				}
			}
			continue
		}
		f, ok := byName[strings.ToLower(k)]
		if !ok {
			return fmt.Errorf("unknown parameter %q", k)
		}
		for i, s := range values[k] {
			if err := setString(FieldValue(p, f), f, s, i > 0 || f.Kind == reflect.Map); err != nil {
				return err
			}
		}
	}
	return nil
}

// BindJSON decodes a JSON object into p, rejecting unknown fields.
func BindJSON(p any, js []byte) error {
	if len(bytes.TrimSpace(js)) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(js))
	dec.DisallowUnknownFields()
	if err := dec.Decode(p); err != nil {
		return fmt.Errorf("parameters: %v", err)
	}
	for _, f := range Fields(p) {
		if len(f.Enum) > 0 {
			v := FieldValue(p, f)
			if err := setString(v, f, v.String(), false); err != nil {
				return err
			}
		}
	}
	return nil
}
