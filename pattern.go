package goxlint

import (
	"fmt"
	"go/ast"
	"reflect"
	"unicode"
	"unicode/utf8"

	"github.com/egoisutolabs/goxlint/internal/pattern"
)

// Pattern is a compiled Go expression or statement list with pattern variables.
// A Pattern is immutable. It is safe for concurrent use.
// Except and CompileExcept return a new Pattern.
type Pattern struct {
	impl     *pattern.Pattern
	nodeType reflect.Type
}

// Match is one place where a pattern matches a node.
type Match struct {
	// Node is the matched node.
	Node ast.Node
	// Captures maps a variable name to the matched nodes.
	// The name does not include the $ prefix.
	// $x maps to one node. $*xs maps to zero or more nodes.
	// $_ and $*_ are not keys.
	Captures map[string][]ast.Node
}

// CompilePattern compiles src.
// src is a Go expression, or a statement list when it is not an expression.
// An invalid pattern returns an error. The error text contains the position
// in src, in the form pattern:line:column.
func CompilePattern(src string) (*Pattern, error) {
	impl, err := pattern.Compile(src)
	if err != nil {
		return nil, err
	}
	return newPattern(impl), nil
}

// MustPattern compiles src for a package-level variable.
// It panics when src is not a valid pattern.
func MustPattern(src string) *Pattern {
	p, err := CompilePattern(src)
	if err != nil {
		panic(err)
	}
	return p
}

// Except returns a pattern that matches p and matches none of the exceptions.
// A node is tested against each exception with the captures from p.
// A $name in an exception that p captured must match that capture.
// A $name that p did not capture is local to the exception.
// Except panics when an exception is not valid. The panic value is the error
// from CompilePattern, with the position in that exception.
func (p *Pattern) Except(srcs ...string) *Pattern {
	return mustExcept(p, srcs)
}

func mustExcept(p *Pattern, srcs []string) *Pattern {
	np, err := p.CompileExcept(srcs...)
	if err != nil {
		panic(err)
	}
	return np
}

// CompileExcept returns a pattern that matches p and matches none of the
// exceptions. An invalid exception returns an error with its position.
// p is not modified.
func (p *Pattern) CompileExcept(srcs ...string) (*Pattern, error) {
	if p == nil {
		return nil, fmt.Errorf("pattern:1:1: nil pattern")
	}
	ex := make([]*pattern.Pattern, 0, len(srcs))
	for _, src := range srcs {
		ep, err := pattern.Compile(src)
		if err != nil {
			return nil, fmt.Errorf("exception: %w", err)
		}
		ex = append(ex, ep)
	}
	return newPattern(p.impl.WithExcept(ex...)), nil
}

func newPattern(impl *pattern.Pattern) *Pattern {
	return &Pattern{impl: impl, nodeType: impl.NodeType()}
}

// OnMatch registers fn for each node that matches p.
// The handler uses the node type of the pattern root, in the same walk as On.
// Call OnMatch from Create. A rule with NeedsTypes then matches a package
// selector by import path. The qualifier is the path or its last element, so
// json.Marshal matches encoding/json, including a renamed import. Without
// type information, the selector matches the written name.
func OnMatch(p *Pattern, fn func(Match)) Handler {
	if p == nil || fn == nil {
		//goxlint:ignore anti-slop/no-panic-in-library a broken rule is a programming error
		panic("goxlint: OnMatch requires a pattern and a function")
	}
	impl := p.impl
	info := pattern.CurrentInfo()
	return Handler{
		NodeType: p.nodeType,
		Fn: func(n ast.Node) {
			caps, ok := impl.Match(n, info)
			if !ok {
				return
			}
			fn(Match{Node: n, Captures: caps})
		},
	}
}

// ReplaceMatch returns an edit that replaces m.Node with template.
// Each $name or $*name in template is replaced by the source text of that
// capture. A list capture uses the text from its first node through its last
// node, including the separators between them. An empty list is empty text.
// A $ that does not start a name is copied. $$ therefore writes one $.
// ReplaceMatch panics when template names a capture that m does not have.
func (c *Context) ReplaceMatch(m Match, template string) Edit {
	return ReplaceNode(m.Node, expandTemplate(c, m, template))
}

func expandTemplate(ctx *Context, m Match, template string) string {
	var b []byte
	for i := 0; i < len(template); {
		if template[i] != '$' {
			b = append(b, template[i])
			i++
			continue
		}
		if i+1 < len(template) && template[i+1] == '$' {
			b = append(b, '$')
			i += 2
			continue
		}
		j := i + 1
		if j < len(template) && template[j] == '*' {
			j++
		}
		name, next, ok := templateName(template, j)
		if !ok {
			b = append(b, '$')
			i++
			continue
		}
		nodes, found := m.Captures[name]
		if !found {
			//goxlint:ignore anti-slop/no-panic-in-library a broken rule is a programming error
			panic(fmt.Errorf("goxlint: ReplaceMatch: template refers to unknown capture %q", name))
		}
		b = append(b, captureText(ctx, nodes)...)
		i = next
	}
	return string(b)
}

func templateName(s string, i int) (string, int, bool) {
	if i >= len(s) {
		return "", i, false
	}
	r, size := utf8.DecodeRuneInString(s[i:])
	if r != '_' && !unicode.IsLetter(r) {
		return "", i, false
	}
	j := i + size
	for j < len(s) {
		r, size = utf8.DecodeRuneInString(s[j:])
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			break
		}
		j += size
	}
	return s[i:j], j, true
}

func captureText(ctx *Context, nodes []ast.Node) string {
	if len(nodes) == 0 {
		return ""
	}
	start, end := nodes[0].Pos(), nodes[0].End()
	for _, n := range nodes[1:] {
		if n == nil || !n.Pos().IsValid() || !n.End().IsValid() {
			continue
		}
		if !start.IsValid() || n.Pos() < start {
			start = n.Pos()
		}
		if n.End() > end {
			end = n.End()
		}
	}
	if !start.IsValid() || !end.IsValid() || end < start {
		return ""
	}
	f := ctx.Fset.File(start)
	return string(ctx.Source[f.Offset(start):f.Offset(end)])
}
