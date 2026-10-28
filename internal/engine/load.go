package engine

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"

	"golang.org/x/tools/go/packages"
)

// LoadOptions controls which files are loaded and how.
type LoadOptions struct {
	Dir      string
	Patterns []string // go-style patterns: "./...", "dir", "dir/...", "file.go"
	// Typed loads packages with go/packages and type-checks them. Otherwise
	// files are only parsed.
	Typed bool
	// Tags are build tags for typed loading, as in `go build -tags`. Syntax-only
	// loading reads every file regardless of build constraints.
	Tags []string
	// Ignore reports whether an absolute path should be skipped.
	Ignore func(path string, isDir bool) bool
	// Overlay replaces disk contents by absolute, cleaned filename. The caller
	// must not mutate it while Load is running.
	Overlay map[string][]byte
}

// Load returns the files matched by opts. Errors are non-fatal: files that
// load are returned alongside the problems encountered.
func Load(opts LoadOptions) ([]File, []error) {
	if len(opts.Patterns) == 0 {
		opts.Patterns = []string{"./..."}
	}
	if opts.Ignore == nil {
		opts.Ignore = func(string, bool) bool { return false }
	}
	if opts.Typed {
		return loadTyped(opts)
	}
	return loadSyntax(opts)
}

func loadSyntax(opts LoadOptions) ([]File, []error) {
	paths, errs, failedDirs := expandPatterns(opts)
	fset := token.NewFileSet()
	files := make([]File, len(paths))
	fileErrs := make([]error, len(paths))
	parallel(len(paths), func(i int) {
		src, err := readSource(paths[i], opts.Overlay)
		if err != nil {
			fileErrs[i] = err
			return
		}
		af, err := parser.ParseFile(fset, paths[i], src, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			fileErrs[i] = err
			return
		}
		files[i] = File{Fset: fset, AST: af, Filename: paths[i], Source: src}
	})
	var out []File
	for i, f := range files {
		if fileErrs[i] != nil {
			errs = append(errs, fileErrs[i])
			failedDirs[filepath.Dir(paths[i])] = true
			continue
		}
		if !ast.IsGenerated(f.AST) {
			out = append(out, f)
		}
	}
	// A package missing a file cannot support cross-file analysis or audits.
	for i := range out {
		out[i].Incomplete = failedDirs[filepath.Dir(out[i].Filename)]
	}
	return out, errs
}

// expandPatterns resolves patterns to absolute .go file paths. It also returns
// the directories whose files could not all be listed.
func expandPatterns(opts LoadOptions) ([]string, []error, map[string]bool) {
	var paths []string
	var errs []error
	failedDirs := map[string]bool{}
	seen := map[string]bool{}
	add := func(p string) {
		if !seen[p] && !opts.Ignore(p, false) {
			seen[p] = true
			paths = append(paths, p)
		}
	}
	for _, pat := range opts.Patterns {
		recursive := false
		if pat == "..." || strings.HasSuffix(pat, "/...") {
			recursive = true
			pat = strings.TrimSuffix(strings.TrimSuffix(pat, "..."), "/")
			if pat == "" {
				pat = "."
			}
		}
		root := pat
		if !filepath.IsAbs(root) {
			root = filepath.Join(opts.Dir, root)
		}
		if _, ok := opts.Overlay[root]; ok && !recursive {
			add(root)
			continue
		}
		info, err := os.Stat(root)
		if err != nil {
			errs = append(errs, err)
			if strings.HasSuffix(root, ".go") {
				failedDirs[filepath.Dir(root)] = true
			} else {
				failedDirs[root] = true
			}
			continue
		}
		if !info.IsDir() {
			add(root)
			continue
		}
		err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				failedDirs[p] = true
				failedDirs[filepath.Dir(p)] = true
				return err
			}
			if d.IsDir() {
				if p == root {
					return nil
				}
				if !recursive || skipDir(d.Name()) || opts.Ignore(p, true) {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(p, ".go") {
				add(p)
			}
			return nil
		})
		if err != nil {
			errs = append(errs, err)
		}
	}
	return paths, errs, failedDirs
}

func skipDir(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") ||
		name == "vendor" || name == "testdata" || name == "node_modules"
}

func loadTyped(opts LoadOptions) ([]File, []error) {
	patterns := make([]string, len(opts.Patterns))
	for i, p := range opts.Patterns {
		if strings.HasSuffix(p, ".go") {
			p = "file=" + p
		}
		patterns[i] = p
	}
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedImports |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedModule,
		Dir:        opts.Dir,
		Tests:      true,
		BuildFlags: buildFlags(opts.Tags),
		Overlay:    opts.Overlay,
		Fset:       token.NewFileSet(),
		ParseFile: func(fset *token.FileSet, filename string, src []byte) (*ast.File, error) {
			return parser.ParseFile(fset, filename, src, parser.ParseComments|parser.SkipObjectResolution)
		},
	}
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, []error{err}
	}

	// A package is incomplete when it or anything it imports failed to load:
	// its type information may be partial, so missing diagnostics prove nothing.
	broken := map[*packages.Package]bool{}
	packages.Visit(pkgs, nil, func(pkg *packages.Package) {
		broken[pkg] = len(pkg.Errors) > 0
		for _, dep := range pkg.Imports {
			broken[pkg] = broken[pkg] || broken[dep]
		}
	})

	var out []File
	var errs []error
	seen := map[string]bool{}
	seenErr := map[string]bool{}
	unattributed := false
	for _, pkg := range pkgs {
		for _, e := range packageErrors(pkg) {
			if msg := e.Error(); !seenErr[msg] {
				seenErr[msg] = true
				errs = append(errs, e)
			}
		}
		if strings.HasSuffix(pkg.ID, ".test") { // generated test main
			continue
		}
		if len(pkg.Errors) > 0 && len(pkg.Syntax) == 0 {
			unattributed = true // nothing tells us which package this error belongs to
		}
		goFiles := map[string]bool{}
		for _, f := range pkg.GoFiles {
			goFiles[f] = true
		}
		for _, af := range pkg.Syntax {
			name := cfg.Fset.File(af.FileStart).Name()
			if seen[name] || !goFiles[name] || opts.Ignore(name, false) || ast.IsGenerated(af) {
				continue
			}
			seen[name] = true
			src, err := readSource(name, opts.Overlay)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			out = append(out, File{Fset: cfg.Fset, AST: af, Filename: name, Source: src, Pkg: pkg.Types, Info: pkg.TypesInfo, Incomplete: broken[pkg]})
		}
	}
	if unattributed {
		for i := range out {
			out[i].Incomplete = true
		}
	}
	return out, errs
}

func readSource(name string, overlay map[string][]byte) ([]byte, error) {
	if src, ok := overlay[name]; ok {
		return src, nil
	}
	return os.ReadFile(name)
}

// packageErrors returns pkg's errors without the compiler output that go list
// repeats as a "# pkg" list error alongside the same type errors.
func packageErrors(pkg *packages.Package) []error {
	hasTypeErrors := slices.ContainsFunc(pkg.Errors, func(e packages.Error) bool { return e.Kind == packages.TypeError })
	var errs []error
	for _, e := range pkg.Errors {
		if hasTypeErrors && e.Kind == packages.ListError && strings.HasPrefix(e.Msg, "# ") {
			continue
		}
		errs = append(errs, fmt.Errorf("%s", e))
	}
	return errs
}

func buildFlags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	return []string{"-tags=" + strings.Join(tags, ",")}
}

// parallel calls fn(0..n-1) across GOMAXPROCS goroutines.
func parallel(n int, fn func(i int)) {
	workers := min(runtime.GOMAXPROCS(0), n)
	var wg sync.WaitGroup
	next := make(chan int)
	for range workers {
		wg.Go(func() {
			for i := range next {
				fn(i)
			}
		})
	}
	for i := range n {
		next <- i
	}
	close(next)
	wg.Wait()
}
