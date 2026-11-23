package pattern

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestMatchLanguage(t *testing.T) {
	tests := []struct {
		name   string
		pat    string
		code   string
		search bool
		want   bool
		// caps is nil when the test does not check captures.
		// An empty map checks that nothing was captured.
		caps map[string]int
	}{
		{name: "same call", pat: `f(1)`, code: `f(1)`, want: true},
		{name: "different arg", pat: `f(1)`, code: `f(2)`, want: false},
		{name: "ignore parens", pat: `f(1)`, code: `(f((1)))`, want: true},
		{name: "ignore comment", pat: `f(1, 2)`, code: `f(1 /* c */, 2)`, want: true},
		{name: "parens change meaning", pat: `$x + $y`, code: `(a + b) * c`, want: false},
		{name: "parens keep meaning", pat: `$x + $y`, code: `(a) + (b)`, want: true, caps: map[string]int{"x": 1, "y": 1}},

		{name: "any node", pat: `$x`, code: `1 + 2`, want: true, caps: map[string]int{"x": 1}},
		{name: "wild not captured", pat: `f($_, $_)`, code: `f(1, 2)`, want: true, caps: map[string]int{}},
		{name: "wild independent", pat: `f($_, $_)`, code: `f(1, 2)`, want: true},
		{name: "repeated equal", pat: `$x == nil || len($x) == 0`, code: `a == nil || len(a) == 0`, want: true, caps: map[string]int{"x": 1}},
		{name: "repeated equal with parens", pat: `$x == nil || len($x) == 0`, code: `(a == nil) || (len((a)) == 0)`, want: true},
		{name: "repeated not equal", pat: `$x == nil || len($x) == 0`, code: `a == nil || len(b) == 0`, want: false},
		{name: "repeated call args equal", pat: `f($x, $x)`, code: `f(1, 1)`, want: true},
		{name: "repeated call args differ", pat: `f($x, $x)`, code: `f(1, 2)`, want: false},

		{name: "variadic zero", pat: `f($*xs)`, code: `f()`, want: true, caps: map[string]int{"xs": 0}},
		{name: "variadic one", pat: `f($*xs)`, code: `f(1)`, want: true, caps: map[string]int{"xs": 1}},
		{name: "variadic many", pat: `f($*xs)`, code: `f(1, 2, 3)`, want: true, caps: map[string]int{"xs": 3}},
		{name: "variadic wild", pat: `fmt.Println($*_)`, code: `fmt.Println(1, 2)`, want: true, caps: map[string]int{}},
		{name: "variadic then one", pat: `f($*xs, $y)`, code: `f(1, 2, 3)`, want: true, caps: map[string]int{"xs": 2, "y": 1}},

		{name: "composite zero", pat: `[]int{$*xs}`, code: `[]int{}`, want: true, caps: map[string]int{"xs": 0}},
		{name: "composite one", pat: `[]int{$*xs}`, code: `[]int{1}`, want: true, caps: map[string]int{"xs": 1}},
		{name: "composite many", pat: `[]int{$*xs}`, code: `[]int{1, 2}`, want: true, caps: map[string]int{"xs": 2}},

		{name: "statement variadic zero", pat: `{ $*body }`, code: `{}`, want: true, caps: map[string]int{"body": 0}},
		{name: "statement variadic one", pat: `{ $*body }`, code: `{ f() }`, want: true, caps: map[string]int{"body": 1}},
		{name: "statement variadic many", pat: `{ $*body }`, code: `{ f(); g() }`, want: true, caps: map[string]int{"body": 2}},

		{
			name:   "if statement",
			pat:    `if $err != nil { return $err }`,
			code:   "package p\nfunc f(err error) error {\n\tif err != nil {\n\t\treturn err\n\t}\n\treturn nil\n}\n",
			search: true,
			want:   true,
			caps:   map[string]int{"err": 1},
		},
		{
			name:   "if statement not equal",
			pat:    `if $err != nil { return $err }`,
			code:   "package p\nfunc f(err error) error {\n\tif err != nil {\n\t\treturn nil\n\t}\n\treturn nil\n}\n",
			search: true,
			want:   false,
		},
		{name: "nested call", pat: `errors.New(fmt.Sprintf($*args))`, code: `errors.New(fmt.Sprintf("%d", 1))`, want: true, caps: map[string]int{"args": 2}},
		{name: "switch case", pat: `switch $x { default: return $x }`, code: `switch n { default: return n }`, want: true, caps: map[string]int{"x": 1}},
		{name: "string is not a variable", pat: `"$x"`, code: `"$x"`, want: true, caps: map[string]int{}},
		{name: "string is not that variable", pat: `"$x"`, code: `"y"`, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := Compile(tt.pat)
			if err != nil {
				t.Fatalf("compile %s: %v", tt.pat, err)
			}
			root := parseSnippet(t, tt.code)
			caps, ok := findMatch(p, root, tt.search, nil)
			if ok != tt.want {
				t.Fatalf("match = %v, want %v", ok, tt.want)
			}
			if tt.caps == nil || !ok {
				return
			}
			if len(caps) != len(tt.caps) {
				t.Fatalf("captures = %v, want lengths %v", captureLens(caps), tt.caps)
			}
			for name, n := range tt.caps {
				if got := len(caps[name]); got != n {
					t.Fatalf("capture %s len = %d, want %d", name, got, n)
				}
			}
		})
	}
}

func TestNodeTypes(t *testing.T) {
	call, err := Compile(`fmt.Println($*_)`)
	if err != nil {
		t.Fatal(err)
	}
	if call.NodeType() != reflect.TypeFor[*ast.CallExpr]() {
		t.Fatalf("call root = %v", call.NodeType())
	}
	ifs, err := Compile(`if $err != nil { return $err }`)
	if err != nil {
		t.Fatal(err)
	}
	if ifs.NodeType() != reflect.TypeFor[*ast.IfStmt]() {
		t.Fatalf("if root = %v", ifs.NodeType())
	}
	wild, err := Compile(`$x`)
	if err != nil {
		t.Fatal(err)
	}
	if wild.NodeType() != reflect.TypeFor[ast.Node]() {
		t.Fatalf("wildcard root = %v", wild.NodeType())
	}
}

func TestExcept(t *testing.T) {
	base, err := Compile(`$x == $y`)
	if err != nil {
		t.Fatal(err)
	}
	ex, err := Compile(`$x == $x`)
	if err != nil {
		t.Fatal(err)
	}
	filtered := base.WithExcept(ex)
	if _, ok := mustMatch(t, base, `a == a`); !ok {
		t.Fatal("base pattern should match equal operands")
	}
	if _, ok := mustMatch(t, filtered, `a == a`); ok {
		t.Fatal("exception removed a == a")
	}
	if _, ok := mustMatch(t, filtered, `a == b`); !ok {
		t.Fatal("unequal operands should remain")
	}
	if _, ok := mustMatch(t, base, `a == a`); !ok {
		t.Fatal("Except changed the original pattern")
	}

	localBase, err := Compile(`$_ == $_`)
	if err != nil {
		t.Fatal(err)
	}
	localEx, err := Compile(`$x == $x`)
	if err != nil {
		t.Fatal(err)
	}
	local := localBase.WithExcept(localEx)
	if _, ok := mustMatch(t, local, `a == a`); ok {
		t.Fatal("local $x == $x should remove equal operands")
	}
	if _, ok := mustMatch(t, local, `a == b`); !ok {
		t.Fatal("local exception should keep unequal operands")
	}
}

func TestImportPath(t *testing.T) {
	const src = `package p
import f "fmt"

type fmtT struct{}

func (fmtT) Println(v ...int) {}

func g() { f.Println(1) }

func h(fmt fmtT) { fmt.Println(2) }
`
	file, info := typeCheck(t, src)
	p, err := Compile(`fmt.Println($x)`)
	if err != nil {
		t.Fatal(err)
	}
	got := matchedLits(p, file, info)
	if len(got) != 1 || got[0] != "1" {
		t.Fatalf("with types got %v, want [1]", got)
	}
	got = matchedLits(p, file, nil)
	if len(got) != 1 || got[0] != "2" {
		t.Fatalf("without types got %v, want [2]", got)
	}
}

func TestCompileErrors(t *testing.T) {
	_, err := Compile("fmt.Println(")
	if err == nil || !strings.Contains(err.Error(), "pattern:1:") {
		t.Fatalf("invalid pattern: %v", err)
	}
	_, err = Compile("f($*)")
	if err == nil || !strings.Contains(err.Error(), "pattern:1:3:") || !strings.Contains(err.Error(), "missing name") {
		t.Fatalf("missing name: %v", err)
	}
	_, err = Compile("$*xs + 1")
	if err == nil || !strings.Contains(err.Error(), "pattern:1:1:") || !strings.Contains(err.Error(), "list") {
		t.Fatalf("bad variadic: %v", err)
	}
	_, err = Compile("$x + $*x")
	if err == nil || !strings.Contains(err.Error(), "one node and as a list") {
		t.Fatalf("mixed kind: %v", err)
	}
	if _, err := Compile(""); err == nil {
		t.Fatal("empty pattern")
	}
}

func TestConcurrentMatch(t *testing.T) {
	p, err := Compile(`f($x, $*ys)`)
	if err != nil {
		t.Fatal(err)
	}
	root := parseSnippet(t, `f(1, 2, 3)`)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 50 {
				caps, ok := p.Match(root, nil)
				if !ok || len(caps["x"]) != 1 || len(caps["ys"]) != 2 {
					t.Errorf("concurrent match = %v %v", ok, captureLens(caps))
					return
				}
			}
		})
	}
	wg.Wait()
}

func mustMatch(t *testing.T, p *Pattern, code string) (map[string][]ast.Node, bool) {
	t.Helper()
	return p.Match(parseSnippet(t, code), nil)
}

func findMatch(p *Pattern, root ast.Node, search bool, info *types.Info) (map[string][]ast.Node, bool) {
	if !search {
		return p.Match(root, info)
	}
	var caps map[string][]ast.Node
	var ok bool
	ast.Inspect(root, func(n ast.Node) bool {
		if n == nil || ok {
			return true
		}
		if c, matched := p.Match(n, info); matched {
			caps, ok = c, true
		}
		return true
	})
	return caps, ok
}

func matchedLits(p *Pattern, root ast.Node, info *types.Info) []string {
	var out []string
	ast.Inspect(root, func(n ast.Node) bool {
		caps, ok := p.Match(n, info)
		if !ok {
			return true
		}
		for _, node := range caps["x"] {
			if lit, ok := node.(*ast.BasicLit); ok {
				out = append(out, lit.Value)
			}
		}
		return true
	})
	return out
}

func captureLens(caps map[string][]ast.Node) map[string]int {
	out := map[string]int{}
	for k, v := range caps {
		out[k] = len(v)
	}
	return out
}

func parseSnippet(t *testing.T, src string) ast.Node {
	t.Helper()
	src = strings.TrimSpace(src)
	if strings.HasPrefix(src, "package ") {
		f, err := parser.ParseFile(token.NewFileSet(), "x.go", src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	if expr, err := parser.ParseExpr(src); err == nil {
		return expr
	}
	f, err := parser.ParseFile(token.NewFileSet(), "x.go", "package p\nfunc _() {\n"+src+"\n}\n", parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	body := f.Decls[0].(*ast.FuncDecl).Body
	if len(body.List) == 1 {
		return body.List[0]
	}
	return body
}

func typeCheck(t *testing.T, src string) (*ast.File, *types.Info) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{Uses: map[*ast.Ident]types.Object{}, Defs: map[*ast.Ident]types.Object{}}
	conf := types.Config{Importer: importer.ForCompiler(fset, "source", nil), Error: func(err error) {
		msg := err.Error()
		if strings.Contains(msg, "declared and not used") || strings.Contains(msg, "imported and not used") {
			return
		}
		t.Errorf("type error: %v", err)
	}}
	if _, err := conf.Check("p", fset, []*ast.File{f}, info); err != nil && t.Failed() {
		t.Fatal(err)
	}
	return f, info
}
