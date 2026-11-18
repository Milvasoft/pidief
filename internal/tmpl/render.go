package tmpl

import (
	"encoding/json"
	"fmt"
	"html"
	"math/big"
	"reflect"
	"sort"
	"strings"
	"time"
)

// Options controls rendering.
type Options struct {
	Strict   bool           // a missing value is an error instead of an empty string
	Empty    string         // printed for a missing/empty {{value}} (default "")
	Location *time.Location // time zone for dates (default Europe/Istanbul)
}

type frame struct {
	data   any
	locals map[string]any // @index, @first, aliases
}

type renderer struct {
	opts  Options
	stack []frame
	out   strings.Builder
	depth int
}

// MissingError reports an unresolved value in strict mode.
type MissingError struct {
	Path string
	Line int
}

func (e *MissingError) Error() string {
	return fmt.Sprintf("template line %d: no value for %q", e.Line, e.Path)
}

// Render executes the template with data (decoded JSON: map[string]any, []any, json.Number...).
func (t *Template) Render(data any, o Options) (string, error) {
	if o.Location == nil {
		o.Location = istanbul()
	}
	r := &renderer{opts: o, stack: []frame{{data: data}}}
	if err := r.nodes(t.nodes); err != nil {
		return "", err
	}
	return r.out.String(), nil
}

func istanbul() *time.Location {
	if l, err := time.LoadLocation("Europe/Istanbul"); err == nil {
		return l
	}
	return time.FixedZone("TRT", 3*3600)
}

// DecodeJSON decodes data keeping numbers exact (json.Number).
func DecodeJSON(b []byte) (any, error) {
	if len(strings.TrimSpace(string(b))) == 0 {
		return map[string]any{}, nil
	}
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("data is not valid JSON: %w", err)
	}
	return v, nil
}

func (r *renderer) nodes(ns []*node) error {
	for _, n := range ns {
		if err := r.node(n); err != nil {
			return err
		}
	}
	return nil
}

func (r *renderer) push(data any, locals map[string]any) {
	r.stack = append(r.stack, frame{data, locals})
}
func (r *renderer) pop() { r.stack = r.stack[:len(r.stack)-1] }

func (r *renderer) node(n *node) error {
	switch n.kind {
	case textNode:
		r.out.WriteString(n.text)
		return nil
	case exprNode:
		v, err := r.eval(n.expr, n.line)
		if err != nil {
			return err
		}
		s := toString(v)
		if s == "" && r.opts.Empty != "" && n.expr.helper == "" {
			s = r.opts.Empty
		}
		if n.raw {
			r.out.WriteString(s)
		} else {
			r.out.WriteString(html.EscapeString(s))
		}
		return nil
	}
	r.depth++
	defer func() { r.depth-- }()
	if r.depth > 64 {
		return fmt.Errorf("template line %d: nesting too deep", n.line)
	}
	v, err := r.evalLenient(n.expr, n.line)
	if err != nil {
		return err
	}
	switch n.name {
	case "if", "unless":
		cond := truthy(v)
		if n.name == "unless" {
			cond = !cond
		}
		if cond {
			return r.nodes(n.body)
		}
		for _, ei := range n.elseIfs {
			c, err := r.evalLenient(ei.cond, n.line)
			if err != nil {
				return err
			}
			if truthy(c) {
				return r.nodes(ei.body)
			}
		}
		return r.nodes(n.inverse)
	case "with":
		if !truthy(v) {
			return r.nodes(n.inverse)
		}
		r.push(v, aliasMap(n.alias, v))
		defer r.pop()
		return r.nodes(n.body)
	case "inverted":
		if !truthy(v) {
			return r.nodes(n.body)
		}
		return r.nodes(n.inverse)
	case "each", "section":
		items, isList := list(v)
		if n.name == "section" && !isList {
			if !truthy(v) {
				return r.nodes(n.inverse)
			}
			if _, ok := v.(bool); ok {
				return r.nodes(n.body)
			}
			r.push(v, aliasMap(n.alias, v))
			defer r.pop()
			return r.nodes(n.body)
		}
		if len(items) == 0 {
			return r.nodes(n.inverse)
		}
		keys := []string(nil)
		if m, ok := v.(map[string]any); ok && n.name == "each" {
			keys = sortedKeys(m)
		}
		for i, it := range items {
			loc := map[string]any{
				"@index": i, "@number": i + 1, "@first": i == 0, "@last": i == len(items)-1,
			}
			if keys != nil {
				loc["@key"] = keys[i]
			}
			if n.alias != "" {
				loc[n.alias] = it
			}
			r.push(it, loc)
			err := r.nodes(n.body)
			r.pop()
			if err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("template line %d: unknown block %q", n.line, n.name)
}

func aliasMap(alias string, v any) map[string]any {
	if alias == "" {
		return nil
	}
	return map[string]any{alias: v}
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func list(v any) ([]any, bool) {
	switch t := v.(type) {
	case []any:
		return t, true
	case map[string]any:
		_ = t
		return nil, false
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array {
		out := make([]any, rv.Len())
		for i := range out {
			out[i] = rv.Index(i).Interface()
		}
		return out, true
	}
	return nil, false
}

// evalLenient never fails on a missing value (block conditions treat it as false).
func (r *renderer) evalLenient(e *expr, line int) (any, error) {
	strict := r.opts.Strict
	r.opts.Strict = false
	defer func() { r.opts.Strict = strict }()
	return r.eval(e, line)
}

func (r *renderer) eval(e *expr, line int) (any, error) {
	if e.isLit {
		return e.lit, nil
	}
	if e.helper != "" {
		h, ok := helpers[e.helper]
		if !ok {
			return nil, fmt.Errorf("template line %d: unknown helper %q", line, e.helper)
		}
		args := make([]any, len(e.args))
		for i, a := range e.args {
			v, err := r.evalLenient(a, line)
			if err != nil {
				return nil, err
			}
			args[i] = v
		}
		v, err := h(r, args)
		if err != nil {
			return nil, fmt.Errorf("template line %d: %s: %w", line, e.helper, err)
		}
		return v, nil
	}
	// A bare helper name with no arguments, e.g. {{bugun}}.
	if h, ok := helpers[e.path]; ok && zeroArgHelpers[e.path] {
		return h(r, nil)
	}
	v, ok := r.lookup(e.path)
	if !ok && r.opts.Strict {
		return nil, &MissingError{Path: e.path, Line: line}
	}
	return v, nil
}

func (r *renderer) lookup(path string) (any, bool) {
	level := len(r.stack) - 1
	for strings.HasPrefix(path, "../") {
		path = path[3:]
		level--
	}
	if level < 0 {
		return nil, false
	}
	if path == "this" || path == "." {
		return r.stack[level].data, true
	}
	path = strings.TrimPrefix(path, "this.")
	path = strings.TrimPrefix(path, "./")
	if strings.HasPrefix(path, "@root.") {
		return resolve(r.stack[0].data, strings.Split(path[6:], "."))
	}
	parts := strings.Split(path, ".")
	// Locals (@index, aliases) of the nearest frames first.
	for i := level; i >= 0; i-- {
		if loc := r.stack[i].locals; loc != nil {
			if v, ok := loc[parts[0]]; ok {
				return resolve(v, parts[1:])
			}
		}
		if strings.HasPrefix(parts[0], "@") {
			continue
		}
		if v, ok := resolve(r.stack[i].data, parts); ok {
			return v, true
		}
		if i == level && strings.Contains(path, "../") {
			break
		}
	}
	return nil, false
}

func resolve(v any, parts []string) (any, bool) {
	for _, p := range parts {
		if p == "" {
			continue
		}
		switch t := v.(type) {
		case map[string]any:
			nv, ok := t[p]
			if !ok {
				return nil, false
			}
			v = nv
		case []any:
			var idx int
			if _, err := fmt.Sscanf(p, "%d", &idx); err != nil || idx < 0 || idx >= len(t) {
				if p == "length" {
					return len(t), true
				}
				return nil, false
			}
			v = t[idx]
		default:
			return nil, false
		}
	}
	return v, true
}

func truthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		return t != ""
	case json.Number:
		f, _ := t.Float64()
		return f != 0
	case float64:
		return t != 0
	case int:
		return t != 0
	case []any:
		return len(t) > 0
	case map[string]any:
		return true
	case *big.Rat:
		return t.Sign() != 0
	}
	return true
}

func toString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case json.Number:
		return t.String()
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		return formatFloat(t)
	case *big.Rat:
		return t.FloatString(2)
	case map[string]any, []any:
		b, _ := json.Marshal(t)
		return string(b)
	}
	return fmt.Sprint(v)
}
