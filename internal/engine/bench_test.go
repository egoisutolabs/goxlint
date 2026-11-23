package engine_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/egoisutolabs/goxlint"
	"github.com/egoisutolabs/goxlint/internal/engine"
)

// These benchmarks measure engine time as the rule count grows.
// Type-checking, when a sub-benchmark needs it, finishes before the timer.
// BenchmarkLintFile and BenchmarkLintFileSeparateWalks use one generated file,
// the same rule bodies, and the same rule counts.
// The separate-walk benchmark is the baseline: each rule walks the file with
// ast.Inspect. The repository benchmark stays in timing_bench_test.go.

const (
	// benchFileFuncs is the number of function pairs in the single-file input.
	// Each pair is one caller and one helper.
	benchFileFuncs = 200
	// benchPackageFiles is the number of files in the LintAll input.
	// benchPackageFuncs is the number of function pairs in each of those files.
	benchPackageFiles = 200
	benchPackageFuncs = 4
	// benchMod is the filter used by every file rule. A rule keeps a node when
	// the first byte of its name, modulo benchMod, equals the rule index modulo
	// benchMod. The same test runs in the engine walk and in ast.Inspect.
	benchMod = 7
)

var benchRuleCounts = []int{1, 10, 50, 200}

// benchSink keeps rule work observable. LintAll runs files in parallel.
var benchSink atomic.Int64

type benchSource struct {
	name string
	src  string
}

func BenchmarkLintFile(b *testing.B) {
	syntaxFile, typedFile := largeFile(b)
	benchModes(b, func(b *testing.B, typed bool) {
		file := syntaxFile
		if typed {
			file = typedFile
		}
		for _, n := range benchRuleCounts {
			b.Run(fmt.Sprintf("rules=%d", n), func(b *testing.B) {
				rules := fileRules(n, typed)
				var diags []engine.Diagnostic
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					diags = engine.LintFile(file, rules, engine.LintOptions{})
				}
				benchSink.Add(int64(len(diags)))
			})
		}
	})
}

func BenchmarkLintFileSeparateWalks(b *testing.B) {
	syntaxFile, typedFile := largeFile(b)
	benchModes(b, func(b *testing.B, typed bool) {
		file := syntaxFile
		var info *types.Info
		if typed {
			file = typedFile
			info = file.Info
		}
		for _, n := range benchRuleCounts {
			b.Run(fmt.Sprintf("rules=%d", n), func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					separateWalks(file.AST, info, n)
				}
			})
		}
	})
}

func BenchmarkLintAll(b *testing.B) {
	syntaxFiles, typedFiles := packageCorpus(b)
	benchModes(b, func(b *testing.B, typed bool) {
		files := syntaxFiles
		if typed {
			files = typedFiles
		}
		for _, n := range benchRuleCounts {
			// The count is the file-rule count. Each sub-benchmark also runs
			// one package rule, then the per-file finish pass.
			b.Run(fmt.Sprintf("rules=%d", n), func(b *testing.B) {
				rules := lintAllRules(n, typed)
				rulesFor := func(string) []engine.ConfiguredRule { return rules }
				var diags [][]engine.Diagnostic
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					diags = engine.LintAll(files, rulesFor, engine.LintOptions{})
				}
				benchSink.Add(int64(len(diags)))
			})
		}
	})
}

func benchModes(b *testing.B, fn func(b *testing.B, typed bool)) {
	b.Helper()
	for _, typed := range []bool{false, true} {
		name := "syntax"
		if typed {
			name = "typed"
		}
		b.Run(name, func(b *testing.B) { fn(b, typed) })
	}
}

func fileRules(n int, typed bool) []engine.ConfiguredRule {
	rules := make([]engine.ConfiguredRule, n)
	for i := range n {
		rule := i
		rules[i] = engine.ConfiguredRule{
			ID:       fmt.Sprintf("bench/rule-%d", rule),
			Plugin:   "bench",
			Severity: engine.Warn,
			Rule: &goxlint.Rule{
				Meta: goxlint.Meta{Name: fmt.Sprintf("rule-%d", rule), NeedsTypes: typed},
				Create: func(ctx *goxlint.Context) goxlint.Visitor {
					return goxlint.Visit(
						goxlint.On(func(n *ast.CallExpr) { applyCall(ctx.TypesInfo, n, rule) }),
						goxlint.On(func(n *ast.Ident) { applyIdent(ctx.TypesInfo, n, rule) }),
						goxlint.On(func(n *ast.FuncDecl) { applyFunc(ctx.TypesInfo, n, rule) }),
					)
				},
			},
		}
	}
	return rules
}

func packageRule(typed bool) engine.ConfiguredRule {
	return engine.ConfiguredRule{
		ID:       "bench/package-funcs",
		Plugin:   "bench",
		Severity: engine.Warn,
		Rule: &goxlint.Rule{
			Meta:           goxlint.Meta{Name: "package-funcs", NeedsTypes: typed},
			AnalyzePackage: applyPackage,
		},
	}
}

func lintAllRules(n int, typed bool) []engine.ConfiguredRule {
	return append(fileRules(n, typed), packageRule(typed))
}

func separateWalks(file *ast.File, info *types.Info, rules int) {
	for rule := range rules {
		ast.Inspect(file, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CallExpr:
				applyCall(info, n, rule)
			case *ast.Ident:
				applyIdent(info, n, rule)
			case *ast.FuncDecl:
				applyFunc(info, n, rule)
			}
			return true
		})
	}
}

func keepName(name string, rule int) bool {
	return name != "" && int(name[0])%benchMod == rule%benchMod
}

func applyCall(info *types.Info, n *ast.CallExpr, rule int) {
	id, _ := n.Fun.(*ast.Ident)
	if id == nil || !keepName(id.Name, rule) {
		return
	}
	benchSink.Add(int64(len(id.Name) + rule))
	if info == nil {
		return
	}
	if tv, ok := info.Types[n]; ok && tv.Type != nil {
		benchSink.Add(1)
	}
}

func applyIdent(info *types.Info, n *ast.Ident, rule int) {
	if !keepName(n.Name, rule) {
		return
	}
	benchSink.Add(int64(len(n.Name) + rule))
	if info == nil {
		return
	}
	if obj := info.ObjectOf(n); obj != nil {
		benchSink.Add(1)
	}
}

func applyFunc(info *types.Info, n *ast.FuncDecl, rule int) {
	if n.Name == nil || !keepName(n.Name.Name, rule) {
		return
	}
	benchSink.Add(int64(len(n.Name.Name) + rule))
	if info == nil {
		return
	}
	if obj := info.ObjectOf(n.Name); obj != nil {
		benchSink.Add(1)
	}
}

func applyPackage(pkg *goxlint.PackageContext) {
	var total int64
	for _, file := range pkg.Files {
		for _, decl := range file.File.Decls {
			fn, _ := decl.(*ast.FuncDecl)
			if fn == nil || fn.Name == nil {
				continue
			}
			total += int64(len(fn.Name.Name))
			if file.TypesInfo != nil {
				if obj := file.ObjectOf(fn.Name); obj != nil {
					total++
				}
			}
		}
	}
	benchSink.Add(total)
}

func sourceWithPairs(prefix string, pairs int) string {
	var b strings.Builder
	b.Grow(pairs*160 + 16)
	b.WriteString("package bench\n\n")
	writePairs(&b, prefix, pairs)
	return b.String()
}

func writePairs(b *strings.Builder, prefix string, pairs int) {
	for i := range pairs {
		fmt.Fprintf(b, "func %sf%d(a int, b int) int {\n", prefix, i)
		fmt.Fprintf(b, "\tx := %sh%d(a)\n", prefix, i)
		fmt.Fprintf(b, "\ty := %sh%d(b)\n", prefix, i)
		fmt.Fprintf(b, "\treturn %sh%d(x + y)\n}\n\n", prefix, i)
		fmt.Fprintf(b, "func %sh%d(n int) int {\n\treturn n + %d\n}\n\n", prefix, i, i%10)
	}
}

func largeFileSource() string { return sourceWithPairs("", benchFileFuncs) }

func packageSources() []benchSource {
	out := make([]benchSource, benchPackageFiles)
	for i := range out {
		out[i] = benchSource{
			name: fmt.Sprintf("file_%03d.go", i),
			src:  sourceWithPairs(fmt.Sprintf("p%d_", i), benchPackageFuncs),
		}
	}
	return out
}

var (
	largeOnce          sync.Once
	largeSyntax        engine.File
	largeTyped         engine.File
	largeErr           error
	packageOnce        sync.Once
	packageSyntaxFiles []engine.File
	packageTypedFiles  []engine.File
	packageErr         error
)

func largeFile(tb testing.TB) (syntax, typed engine.File) {
	tb.Helper()
	largeOnce.Do(func() {
		var syntaxFiles, typedFiles []engine.File
		syntaxFiles, typedFiles, largeErr = loadSources([]benchSource{{name: "large.go", src: largeFileSource()}})
		if largeErr == nil {
			largeSyntax, largeTyped = syntaxFiles[0], typedFiles[0]
		}
	})
	if largeErr != nil {
		tb.Fatal(largeErr)
	}
	return largeSyntax, largeTyped
}

func packageCorpus(tb testing.TB) (syntax, typed []engine.File) {
	tb.Helper()
	packageOnce.Do(func() {
		packageSyntaxFiles, packageTypedFiles, packageErr = loadSources(packageSources())
	})
	if packageErr != nil {
		tb.Fatal(packageErr)
	}
	return packageSyntaxFiles, packageTypedFiles
}

func loadSources(sources []benchSource) (syntax, typed []engine.File, err error) {
	fset := token.NewFileSet()
	syntax = make([]engine.File, len(sources))
	for i, src := range sources {
		file, err := parser.ParseFile(fset, src.name, src.src, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return nil, nil, err
		}
		syntax[i] = engine.File{Fset: fset, AST: file, Filename: src.name, Source: []byte(src.src)}
	}
	typed = make([]engine.File, len(syntax))
	copy(typed, syntax)
	syntaxTrees := make([]*ast.File, len(typed))
	for i := range typed {
		syntaxTrees[i] = typed[i].AST
	}
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
		Defs:  make(map[*ast.Ident]types.Object),
		Uses:  make(map[*ast.Ident]types.Object),
	}
	pkg, err := new(types.Config).Check("example.com/bench", fset, syntaxTrees, info)
	if err != nil {
		return nil, nil, err
	}
	for i := range typed {
		typed[i].Pkg = pkg
		typed[i].Info = info
	}
	return syntax, typed, nil
}

func TestBenchFixtureShape(t *testing.T) {
	seen := map[string]bool{}
	count := func(src string) int {
		n := 0
		for _, line := range strings.Split(src, "\n") {
			if !strings.HasPrefix(line, "func ") {
				continue
			}
			name := strings.Split(strings.Fields(line)[1], "(")[0]
			if seen[name] {
				t.Fatalf("duplicate function %s", name)
			}
			seen[name] = true
			n++
		}
		return n
	}
	if got := count(largeFileSource()); got != benchFileFuncs*2 {
		t.Fatalf("large file has %d functions, want %d", got, benchFileFuncs*2)
	}
	files := packageSources()
	if len(files) != benchPackageFiles {
		t.Fatalf("package has %d files, want %d", len(files), benchPackageFiles)
	}
	for _, file := range files {
		if got := count(file.src); got != benchPackageFuncs*2 {
			t.Fatalf("%s has %d functions, want %d", file.name, got, benchPackageFuncs*2)
		}
	}
	if _, typed := largeFile(t); typed.Info == nil || typed.Pkg == nil {
		t.Fatal("large file has no type information")
	}
	if _, typed := packageCorpus(t); len(typed) != benchPackageFiles || typed[0].Info == nil {
		t.Fatal("package corpus has no type information")
	}
}

func TestBenchRuleMetadata(t *testing.T) {
	typed := fileRules(10, true)
	syntax := fileRules(10, false)
	if len(typed) != 10 || len(syntax) != 10 {
		t.Fatalf("rule count typed %d syntax %d", len(typed), len(syntax))
	}
	for i := range typed {
		if !typed[i].Rule.Meta.NeedsTypes || typed[i].Rule.Create == nil {
			t.Fatalf("typed rule %d shape", i)
		}
		if syntax[i].Rule.Meta.NeedsTypes || syntax[i].Rule.Create == nil {
			t.Fatalf("syntax rule %d shape", i)
		}
		if typed[i].ID != fmt.Sprintf("bench/rule-%d", i) {
			t.Fatalf("rule id %s", typed[i].ID)
		}
	}
	pkg := packageRule(true)
	if pkg.Rule.AnalyzePackage == nil || pkg.Rule.Create != nil || !pkg.Rule.Meta.NeedsTypes {
		t.Fatal("package rule shape")
	}
	all := lintAllRules(3, false)
	if len(all) != 4 || all[3].ID != "bench/package-funcs" || all[3].Rule.Meta.NeedsTypes {
		t.Fatalf("lint-all rules: %+v", all[3])
	}
}

func TestBenchWalksMatch(t *testing.T) {
	for _, typed := range []bool{false, true} {
		name := "syntax"
		if typed {
			name = "typed"
		}
		t.Run(name, func(t *testing.T) {
			syntax, checked, err := loadSources([]benchSource{{name: "small.go", src: sourceWithPairs("", 3)}})
			if err != nil {
				t.Fatal(err)
			}
			file := syntax[0]
			var info *types.Info
			if typed {
				file = checked[0]
				info = file.Info
			}
			const rules = 4
			before := benchSink.Load()
			separateWalks(file.AST, info, rules)
			inspected := benchSink.Load() - before
			if inspected == 0 {
				t.Fatal("inspect did no work")
			}
			before = benchSink.Load()
			diags := engine.LintFile(file, fileRules(rules, typed), engine.LintOptions{})
			if linted := benchSink.Load() - before; linted != inspected {
				t.Fatalf("inspect sink %d, lint sink %d", inspected, linted)
			}
			for _, d := range diags {
				if d.IsInternalError() {
					t.Fatalf("internal error: %s", d.Message)
				}
			}
		})
	}
}

func TestBenchLintAllPackage(t *testing.T) {
	syntax, checked, err := loadSources([]benchSource{
		{name: "a.go", src: sourceWithPairs("a_", 1)},
		{name: "b.go", src: sourceWithPairs("b_", 1)},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, typed := range []bool{false, true} {
		files := syntax
		if typed {
			files = checked
		}
		before := benchSink.Load()
		out := engine.LintAll(files, func(string) []engine.ConfiguredRule {
			return lintAllRules(2, typed)
		}, engine.LintOptions{})
		if benchSink.Load() == before {
			t.Fatal("LintAll did no rule work")
		}
		if len(out) != len(files) {
			t.Fatalf("got %d file results, want %d", len(out), len(files))
		}
		for _, diags := range out {
			for _, d := range diags {
				if d.IsInternalError() {
					t.Fatalf("internal error: %s", d.Message)
				}
			}
		}
	}
}
