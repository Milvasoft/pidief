// Package tmpl is a Handlebars/Mustache-compatible HTML template engine with Turkish (tr-TR)
// formatting helpers. Placeholders follow the Mustache style used by many HTML template editors
// ({{cari.unvan}}, {{#kalemler}}...{{/kalemler}}) plus helpers, conditions and loops.
//
// Syntax:
//
//	{{path.to.value}}           HTML-escaped value      {{{html}}}   raw value
//	{{#each list}}..{{else}}..{{/each}}   loop (@index, @number, @first, @last, this, ../parent)
//	{{#each list as |item|}}..{{/each}}  loop with a name for the item
//	{{#if cond}}..{{else if x}}..{{else}}..{{/if}}   {{#unless cond}}..{{/unless}}
//	{{#with obj}}..{{/with}}    {{#list}}..{{/list}} (Mustache section)  {{^list}}..{{/list}}
//	{{helper arg "literal" 2 (sub expr)}}  e.g. {{para tutar}}, {{tarih d "d MMMM yyyy"}}
//	{{! comment }}
package tmpl

import (
	"fmt"
	"strconv"
	"strings"
)

type nodeKind int

const (
	textNode nodeKind = iota
	exprNode
	blockNode
)

type node struct {
	kind    nodeKind
	text    string
	expr    *expr
	raw     bool
	name    string // block helper name: each, if, unless, with, section, inverted
	alias   string // each ... as |alias|
	body    []*node
	elseIfs []*elseIf
	inverse []*node
	line    int
}

type elseIf struct {
	cond *expr
	body []*node
}

// expr is a path, a literal, or a helper call.
type expr struct {
	helper string
	args   []*expr
	path   string
	lit    any
	isLit  bool
}

// Template is a parsed template.
type Template struct {
	nodes []*node
}

// ParseError points at the line of a syntax error.
type ParseError struct {
	Line int
	Msg  string
}

func (e *ParseError) Error() string { return fmt.Sprintf("template line %d: %s", e.Line, e.Msg) }

type tag struct {
	content string
	raw     bool
	line    int
	start   int
	end     int
}

// Parse parses src.
func Parse(src string) (*Template, error) {
	p := &parser{src: src}
	nodes, endTag, err := p.parseUntil("")
	if err != nil {
		return nil, err
	}
	if endTag != nil {
		return nil, &ParseError{endTag.line, "unexpected {{" + endTag.content + "}}"}
	}
	return &Template{nodes: nodes}, nil
}

type parser struct {
	src string
	pos int
}

func (p *parser) lineAt(i int) int { return strings.Count(p.src[:i], "\n") + 1 }

func (p *parser) next() (text string, t *tag, err error) {
	i := strings.Index(p.src[p.pos:], "{{")
	if i < 0 {
		text = p.src[p.pos:]
		p.pos = len(p.src)
		return text, nil, nil
	}
	text = p.src[p.pos : p.pos+i]
	start := p.pos + i
	raw := strings.HasPrefix(p.src[start:], "{{{")
	open, closer := 2, "}}"
	if raw {
		open, closer = 3, "}}}"
	}
	j := strings.Index(p.src[start+open:], closer)
	if j < 0 {
		return "", nil, &ParseError{p.lineAt(start), "unclosed {{"}
	}
	content := strings.TrimSpace(p.src[start+open : start+open+j])
	// Whitespace control: {{~ and ~}}.
	trimLeft, trimRight := strings.HasPrefix(content, "~"), strings.HasSuffix(content, "~")
	content = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(content, "~"), "~"))
	if trimLeft {
		text = strings.TrimRight(text, " \t\r\n")
	}
	p.pos = start + open + j + len(closer)
	if trimRight {
		for p.pos < len(p.src) && strings.ContainsRune(" \t\r\n", rune(p.src[p.pos])) {
			p.pos++
		}
	}
	return text, &tag{content: content, raw: raw, line: p.lineAt(start), start: start, end: p.pos}, nil
}

// parseUntil parses nodes until a closing/else tag; returns that tag.
func (p *parser) parseUntil(block string) ([]*node, *tag, error) {
	var nodes []*node
	for {
		text, t, err := p.next()
		if err != nil {
			return nil, nil, err
		}
		if text != "" {
			nodes = append(nodes, &node{kind: textNode, text: text})
		}
		if t == nil {
			if block != "" {
				return nil, nil, &ParseError{p.lineAt(len(p.src)), "missing {{/" + block + "}}"}
			}
			return nodes, nil, nil
		}
		c := t.content
		switch {
		case c == "":
			return nil, nil, &ParseError{t.line, "empty tag"}
		case strings.HasPrefix(c, "!"):
			continue
		case strings.HasPrefix(c, "/"), c == "else", strings.HasPrefix(c, "else "), c == "^" && !t.raw:
			return nodes, t, nil
		case strings.HasPrefix(c, "#"), strings.HasPrefix(c, "^"):
			n, err := p.parseBlock(t)
			if err != nil {
				return nil, nil, err
			}
			nodes = append(nodes, n)
		default:
			e, err := parseExpr(c, t.line)
			if err != nil {
				return nil, nil, err
			}
			nodes = append(nodes, &node{kind: exprNode, expr: e, raw: t.raw, line: t.line})
		}
	}
}

func (p *parser) parseBlock(t *tag) (*node, error) {
	inverted := strings.HasPrefix(t.content, "^")
	c := strings.TrimSpace(t.content[1:])
	n := &node{kind: blockNode, line: t.line}
	head := c
	if i := strings.Index(c, " as |"); i >= 0 {
		j := strings.LastIndex(c, "|")
		if j <= i+5 {
			return nil, &ParseError{t.line, "bad block parameter"}
		}
		n.alias = strings.TrimSpace(c[i+5 : j])
		head = strings.TrimSpace(c[:i])
	}
	name, rest, _ := strings.Cut(head, " ")
	rest = strings.TrimSpace(rest)
	closeName := name
	switch {
	case inverted:
		n.name = "inverted"
		e, err := parseExpr(head, t.line)
		if err != nil {
			return nil, err
		}
		n.expr = e
	case name == "each" || name == "if" || name == "unless" || name == "with":
		if rest == "" {
			return nil, &ParseError{t.line, "{{#" + name + "}} needs an argument"}
		}
		n.name = name
		e, err := parseExpr(rest, t.line)
		if err != nil {
			return nil, err
		}
		n.expr = e
	default:
		n.name = "section"
		e, err := parseExpr(head, t.line)
		if err != nil {
			return nil, err
		}
		n.expr = e
		closeName = head
	}
	body, end, err := p.parseUntil(closeName)
	if err != nil {
		return nil, err
	}
	n.body = body
	target := &n.body
	for end != nil && (end.content == "else" || strings.HasPrefix(end.content, "else ") || end.content == "^") {
		if strings.HasPrefix(end.content, "else if ") && n.name == "if" {
			cond, err := parseExpr(strings.TrimSpace(strings.TrimPrefix(end.content, "else if ")), end.line)
			if err != nil {
				return nil, err
			}
			ei := &elseIf{cond: cond}
			n.elseIfs = append(n.elseIfs, ei)
			b, e2, err := p.parseUntil(closeName)
			if err != nil {
				return nil, err
			}
			ei.body = b
			end = e2
			continue
		}
		b, e2, err := p.parseUntil(closeName)
		if err != nil {
			return nil, err
		}
		n.inverse = b
		target = &n.inverse
		end = e2
	}
	_ = target
	if end == nil {
		return nil, &ParseError{t.line, "missing {{/" + closeName + "}}"}
	}
	closing := strings.TrimSpace(strings.TrimPrefix(end.content, "/"))
	if closing != closeName {
		return nil, &ParseError{end.line, fmt.Sprintf("{{/%s}} does not close {{#%s}} (line %d)", closing, closeName, t.line)}
	}
	return n, nil
}

// parseExpr parses `path`, `literal`, or `helper arg1 arg2 (sub expr)`.
func parseExpr(s string, line int) (*expr, error) {
	toks, err := tokenize(s, line)
	if err != nil {
		return nil, err
	}
	e, rest, err := parseCall(toks, line)
	if err != nil {
		return nil, err
	}
	if len(rest) > 0 {
		return nil, &ParseError{line, "unexpected " + rest[0]}
	}
	return e, nil
}

func parseCall(toks []string, line int) (*expr, []string, error) {
	if len(toks) == 0 {
		return nil, nil, &ParseError{line, "empty expression"}
	}
	first, rest, err := parseArg(toks, line)
	if err != nil {
		return nil, nil, err
	}
	var args []*expr
	for len(rest) > 0 && rest[0] != ")" {
		var a *expr
		a, rest, err = parseArg(rest, line)
		if err != nil {
			return nil, nil, err
		}
		args = append(args, a)
	}
	if len(args) == 0 {
		return first, rest, nil
	}
	if first.isLit || first.helper != "" {
		return nil, nil, &ParseError{line, "helper name expected"}
	}
	return &expr{helper: first.path, args: args}, rest, nil
}

func parseArg(toks []string, line int) (*expr, []string, error) {
	t := toks[0]
	switch {
	case t == "(":
		e, rest, err := parseCall(toks[1:], line)
		if err != nil {
			return nil, nil, err
		}
		if len(rest) == 0 || rest[0] != ")" {
			return nil, nil, &ParseError{line, "missing )"}
		}
		if e.helper == "" && !e.isLit {
			e = &expr{helper: e.path}
		}
		return e, rest[1:], nil
	case strings.HasPrefix(t, `"`) || strings.HasPrefix(t, `'`):
		return &expr{lit: t[1 : len(t)-1], isLit: true}, toks[1:], nil
	case t == "true" || t == "false":
		return &expr{lit: t == "true", isLit: true}, toks[1:], nil
	case t == "null" || t == "undefined":
		return &expr{lit: nil, isLit: true}, toks[1:], nil
	}
	if f, err := strconv.ParseFloat(t, 64); err == nil && (t[0] == '-' || (t[0] >= '0' && t[0] <= '9')) {
		return &expr{lit: f, isLit: true}, toks[1:], nil
	}
	return &expr{path: t}, toks[1:], nil
}

func tokenize(s string, line int) ([]string, error) {
	var toks []string
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '(' || c == ')':
			toks = append(toks, string(c))
			i++
		case c == '"' || c == '\'':
			j := strings.IndexByte(s[i+1:], c)
			if j < 0 {
				return nil, &ParseError{line, "unclosed string"}
			}
			toks = append(toks, s[i:i+j+2])
			i += j + 2
		default:
			j := i
			for j < len(s) && !strings.ContainsRune(" \t\r\n()", rune(s[j])) {
				j++
			}
			toks = append(toks, s[i:j])
			i = j
		}
	}
	return toks, nil
}
