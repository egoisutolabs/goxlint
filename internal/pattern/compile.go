// Package pattern compiles and matches Go code patterns.
// Patterns are immutable after compilation and safe for concurrent use.
package pattern

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"
)

// meta is one occurrence of a pattern variable, keyed by its placeholder ident.
type meta struct {
	name string // variable name without $ or *; empty when wild
	many bool
	wild bool
	call bool // placeholder was written as a call, not an ident
	off  int  // byte offset of '$' in the original pattern
}

// Pattern is a compiled expression or statement list.
type Pattern struct {
	root   ast.Node
	vars   map[string]meta
	except []*Pattern
}

// WithExcept returns a new pattern that rejects nodes matching any exception.
// The receiver is not modified.
func (p *Pattern) WithExcept(ex ...*Pattern) *Pattern {
	n := *p
	n.except = make([]*Pattern, 0, len(p.except)+len(ex))
	n.except = append(n.except, p.except...)
	n.except = append(n.except, ex...)
	return &n
}

// NodeType is the AST type OnMatch registers.
// A pattern whose root is a single wildcard matches every node.
func (p *Pattern) NodeType() reflect.Type {
	if id, ok := p.root.(*ast.Ident); ok {
		if m, ok := p.identMarker(id); ok && !m.many {
			return nodeIface
		}
	}
	if m, ok := p.callMarker(p.root); ok && !m.many {
		return nodeIface
	}
	return reflect.TypeOf(p.root)
}

const (
	stmtPrefix = "package p\nfunc _() {\n"
	stmtSuffix = "\n}\n"
)

type errKind int

const (
	errParse errKind = iota
	errRewrite
	errValidate
)

type kindError struct {
	error
	kind errKind
}

// Compile compiles src as an expression, or as a statement list when it is
// not an expression. $name is any one node. $*name is a list.
func Compile(src string) (*Pattern, error) {
	if strings.TrimSpace(src) == "" {
		return nil, kindError{fmt.Errorf("pattern:1:1: empty pattern"), errRewrite}
	}
	p, err := compileOnce(src, false)
	if err == nil || fatal(err) {
		return p, err
	}
	p2, err2 := compileOnce(src, true)
	if err2 == nil {
		return p2, nil
	}
	if fatal(err2) {
		return nil, err2
	}
	return nil, later(err, err2)
}

func fatal(err error) bool {
	var ke kindError
	if asKind(err, &ke) {
		return ke.kind != errParse
	}
	return false
}

func asKind(err error, ke *kindError) bool {
	if err == nil {
		return false
	}
	k, ok := err.(kindError)
	if !ok {
		return false
	}
	*ke = k
	return true
}

func later(a, b error) error {
	la, ca := errLineCol(a)
	lb, cb := errLineCol(b)
	if lb > la || (lb == la && cb > ca) {
		return b
	}
	return a
}

func errLineCol(err error) (int, int) {
	var line, col int
	fmt.Sscanf(err.Error(), "pattern:%d:%d:", &line, &col)
	return line, col
}

func compileOnce(src string, callSingle bool) (*Pattern, error) {
	rw, err := rewrite(src, callSingle)
	if err != nil {
		return nil, kindError{err, errRewrite}
	}
	node, err := parsePattern(src, rw)
	if err != nil {
		return nil, kindError{err, errParse}
	}
	if err := validate(node, rw.vars, src); err != nil {
		return nil, kindError{err, errValidate}
	}
	return &Pattern{root: node, vars: rw.vars}, nil
}

type rewritten struct {
	text   string
	origAt []int // rewritten byte -> original byte
	vars   map[string]meta
}

func (r rewritten) original(off int) int {
	if len(r.origAt) == 0 {
		return 0
	}
	if off < 0 {
		off = 0
	}
	if off >= len(r.origAt) {
		return r.origAt[len(r.origAt)-1]
	}
	return r.origAt[off]
}

func rewrite(src string, callSingle bool) (rewritten, error) {
	prefix := "goxlintMeta"
	for strings.Contains(src, prefix) {
		prefix += "Z"
	}
	var b strings.Builder
	b.Grow(len(src) + 16)
	out := rewritten{vars: map[string]meta{}}
	kinds := map[string]bool{}
	writeSrc := func(i, j int) {
		if j <= i {
			return
		}
		b.WriteString(src[i:j])
		for k := i; k < j; k++ {
			out.origAt = append(out.origAt, k)
		}
	}
	writeMapped := func(s string, orig int) {
		b.WriteString(s)
		for range len(s) {
			out.origAt = append(out.origAt, orig)
		}
	}
	n := 0
	for i := 0; i < len(src); {
		switch src[i] {
		case '"', '\'', '`':
			j := skipLiteral(src, i)
			writeSrc(i, j)
			i = j
			continue
		case '/':
			if i+1 < len(src) && (src[i+1] == '/' || src[i+1] == '*') {
				j := skipComment(src, i)
				writeSrc(i, j)
				i = j
				continue
			}
		}
		if src[i] != '$' {
			start := i
			for i < len(src) && src[i] != '$' && src[i] != '"' && src[i] != '\'' && src[i] != '`' && src[i] != '/' {
				i++
			}
			writeSrc(start, i)
			continue
		}
		start := i
		i++
		many := false
		if i < len(src) && src[i] == '*' {
			many = true
			i++
		}
		name, next, ok := scanIdent(src, i)
		if !ok {
			line, col := position(src, start)
			if many {
				return rewritten{}, fmt.Errorf("pattern:%d:%d: missing name after $*", line, col)
			}
			return rewritten{}, fmt.Errorf("pattern:%d:%d: missing name after $", line, col)
		}
		i = next
		wild := name == "_"
		if !wild {
			if prev, seen := kinds[name]; seen && prev != many {
				line, col := position(src, start)
				return rewritten{}, fmt.Errorf("pattern:%d:%d: $%s is used as one node and as a list", line, col, name)
			}
			kinds[name] = many
		}
		ph := fmt.Sprintf("%s%d", prefix, n)
		n++
		call := many || callSingle
		out.vars[ph] = meta{name: name, many: many, wild: wild, call: call, off: start}
		if r, ok := lastRune(b.String()); ok && isIdentRune(r) {
			writeMapped(" ", start)
		}
		if call {
			writeMapped(ph+"()", start)
		} else {
			writeMapped(ph, start)
			if i < len(src) {
				r, _ := utf8.DecodeRuneInString(src[i:])
				if r == '_' || unicode.IsLetter(r) {
					writeMapped(" ", start)
				}
			}
		}
	}
	out.text = b.String()
	return out, nil
}

func lastRune(s string) (rune, bool) {
	if s == "" {
		return 0, false
	}
	r, _ := utf8.DecodeLastRuneInString(s)
	return r, true
}

func isIdentRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func scanIdent(src string, i int) (string, int, bool) {
	if i >= len(src) {
		return "", i, false
	}
	r, size := utf8.DecodeRuneInString(src[i:])
	if r != '_' && !unicode.IsLetter(r) {
		return "", i, false
	}
	j := i + size
	for j < len(src) {
		r, size = utf8.DecodeRuneInString(src[j:])
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			break
		}
		j += size
	}
	return src[i:j], j, true
}

func skipLiteral(src string, i int) int {
	quote := src[i]
	i++
	if quote == '`' {
		for i < len(src) && src[i] != '`' {
			i++
		}
		if i < len(src) {
			i++
		}
		return i
	}
	for i < len(src) {
		c := src[i]
		i++
		if c == '\\' && i < len(src) {
			i++
			continue
		}
		if c == quote || c == '\n' {
			break
		}
	}
	return i
}

func skipComment(src string, i int) int {
	if src[i+1] == '/' {
		i += 2
		for i < len(src) && src[i] != '\n' {
			i++
		}
		return i
	}
	i += 2
	for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
		i++
	}
	if i+1 < len(src) {
		i += 2
	}
	return i
}

func position(src string, off int) (int, int) {
	if off < 0 {
		off = 0
	}
	if off > len(src) {
		off = len(src)
	}
	line, col := 1, 1
	for i := 0; i < off; i++ {
		if src[i] == '\n' {
			line++
			col = 1
			continue
		}
		col++
	}
	return line, col
}

func parsePattern(src string, rw rewritten) (ast.Node, error) {
	expr, exprErr := parser.ParseExprFrom(token.NewFileSet(), "pattern", rw.text, parser.SkipObjectResolution)
	if exprErr == nil {
		return unwrapParen(expr), nil
	}
	node, stmtErr := parseStmts(src, rw)
	if stmtErr == nil {
		return node, nil
	}
	return nil, later(remap(exprErr, rw, src, 0), stmtErr)
}

func parseStmts(src string, rw rewritten) (ast.Node, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "pattern", stmtPrefix+rw.text+stmtSuffix, parser.SkipObjectResolution)
	if err != nil {
		return nil, remap(err, rw, src, len(stmtPrefix))
	}
	if len(file.Decls) != 1 {
		return nil, fmt.Errorf("pattern:1:1: invalid statement pattern")
	}
	fn, ok := file.Decls[0].(*ast.FuncDecl)
	if !ok || fn.Body == nil {
		return nil, fmt.Errorf("pattern:1:1: invalid statement pattern")
	}
	switch len(fn.Body.List) {
	case 0:
		return nil, fmt.Errorf("pattern:1:1: empty pattern")
	case 1:
		return fn.Body.List[0], nil
	default:
		return fn.Body, nil
	}
}

func remap(err error, rw rewritten, src string, prefix int) error {
	pe := firstParseError(err)
	if pe == nil {
		return fmt.Errorf("pattern:1:1: %v", err)
	}
	off := pe.Pos.Offset - prefix
	var orig int
	switch {
	case off < 0:
		orig = 0
	case off >= len(rw.text):
		// The failure is at the end of the pattern, or in the statement wrapper after it.
		orig = len(src)
	default:
		orig = rw.original(off)
	}
	line, col := position(src, orig)
	return fmt.Errorf("pattern:%d:%d: %s", line, col, pe.Msg)
}

func firstParseError(err error) *scanner.Error {
	if err == nil {
		return nil
	}
	if list, ok := err.(scanner.ErrorList); ok && len(list) > 0 {
		return list[0]
	}
	if pe, ok := err.(*scanner.Error); ok {
		return pe
	}
	return nil
}

func unwrapParen(n ast.Node) ast.Node {
	for {
		p, ok := n.(*ast.ParenExpr)
		if !ok {
			return n
		}
		n = p.X
	}
}

func validate(root ast.Node, vars map[string]meta, src string) error {
	var stack []ast.Node
	var vErr error
	var walk func(ast.Node)
	walk = func(n ast.Node) {
		if n == nil || vErr != nil {
			return
		}
		stack = append(stack, n)
		if m, ok := callMarkerNode(n, vars); ok && m.many && !variadicOK(stack) {
			line, col := position(src, m.off)
			vErr = fmt.Errorf("pattern:%d:%d: $* must be a list of arguments, statements, or composite literal elements", line, col)
		}
		walkChildren(n, walk)
		stack = stack[:len(stack)-1]
	}
	walk(root)
	return vErr
}

func variadicOK(stack []ast.Node) bool {
	call := stack[len(stack)-1]
	if len(stack) < 2 {
		return false
	}
	parent := stack[len(stack)-2]
	switch p := parent.(type) {
	case *ast.CallExpr:
		for _, a := range p.Args {
			if a == call {
				return true
			}
		}
	case *ast.CompositeLit:
		for _, e := range p.Elts {
			if e == call {
				return true
			}
		}
	case *ast.ExprStmt:
		if p.X != call || len(stack) < 3 {
			return false
		}
		switch g := stack[len(stack)-3].(type) {
		case *ast.BlockStmt:
			return stmtIn(g.List, parent)
		case *ast.CaseClause:
			return stmtIn(g.Body, parent)
		case *ast.CommClause:
			return stmtIn(g.Body, parent)
		}
	}
	return false
}

func stmtIn(list []ast.Stmt, stmt ast.Node) bool {
	for _, s := range list {
		if s == stmt {
			return true
		}
	}
	return false
}

func callMarkerNode(n ast.Node, vars map[string]meta) (meta, bool) {
	call, ok := n.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 || call.Ellipsis.IsValid() {
		return meta{}, false
	}
	id, ok := call.Fun.(*ast.Ident)
	if !ok {
		return meta{}, false
	}
	m, ok := vars[id.Name]
	if !ok || !m.call {
		return meta{}, false
	}
	return m, true
}

func walkChildren(n ast.Node, walk func(ast.Node)) {
	v := reflect.ValueOf(n)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return
	}
	v = v.Elem()
	if v.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < v.NumField(); i++ {
		walkValue(v.Field(i), walk)
	}
}

func walkValue(v reflect.Value, walk func(ast.Node)) {
	if !v.IsValid() {
		return
	}
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer:
		if v.IsNil() || !v.Type().Implements(nodeIface) {
			return
		}
		n, ok := v.Interface().(ast.Node)
		if !ok {
			return
		}
		walk(n)
	case reflect.Slice:
		if !sliceIsNodes(v.Type()) {
			return
		}
		for i := 0; i < v.Len(); i++ {
			walkValue(v.Index(i), walk)
		}
	}
}

func sliceIsNodes(t reflect.Type) bool {
	e := t.Elem()
	return (e.Kind() == reflect.Interface || e.Kind() == reflect.Pointer) && e.Implements(nodeIface)
}
