package goxlinttest

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/tools/go/packages"

	"github.com/egoisutolabs/goxlint/internal/engine"
)

func init() {
	// packages.Load resolves the go command with exec.LookPath, which uses the
	// process PATH and ignores the command environment. Put the toolchain that
	// built this binary first so export data matches the go/types in the binary.
	bin, ok := toolchainBin()
	if !ok {
		return
	}
	path := os.Getenv("PATH")
	prefix := bin + string(os.PathListSeparator)
	if path == bin || strings.HasPrefix(path, prefix) {
		return
	}
	if err := os.Setenv("PATH", prefix+path); err != nil {
		return
	}
}

// startupDir is the working directory at process start. Loading uses it so a
// later Chdir, including one that adds a broken go.mod, does not change which
// packages are imported.
var startupDir = func() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	return dir
}()

// parseMu serializes additions to the shared file set. packages.Load can parse
// in parallel when export data is missing, and token.FileSet forbids that.
var parseMu sync.Mutex

type cachedImport struct {
	pkg *types.Package
	gen int
}

// exportImporter returns packages from one packages.Load. The type checker
// requires one identity for a type that two of those packages both name.
type exportImporter struct {
	byPath map[string]*types.Package
	errs   map[string]error
}

func (e *exportImporter) fail(path string, err error) {
	if e.errs == nil {
		e.errs = map[string]error{}
	}
	e.errs[path] = err
}

func (e exportImporter) Import(path string) (*types.Package, error) {
	if path == "unsafe" {
		return types.Unsafe, nil
	}
	if err := e.errs[path]; err != nil {
		return nil, err
	}
	pkg := e.byPath[path]
	if pkg == nil {
		return nil, fmt.Errorf("package %q is not loaded", path)
	}
	return pkg, nil
}

func importPaths(files []engine.File) []string {
	seen := make(map[string]bool)
	var paths []string
	for _, file := range files {
		for _, spec := range file.AST.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil || path == "" || path == "unsafe" || seen[path] {
				continue
			}
			seen[path] = true
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)
	return paths
}

// importerFor loads paths from export data. Caller holds mu.
// No paths still returns an importer, because package unsafe does not go
// through packages.Load.
//
// A miss loads the requested paths together with every package already in
// the cache. Packages from different calls to packages.Load do not share
// type identity, so one load replaces the cache and a later case can reuse
// any subset.
func importerFor(paths []string) (types.Importer, error) {
	if len(paths) == 0 {
		return exportImporter{}, nil
	}
	if imp, ok := cachedImporter(paths); ok {
		return imp, nil
	}
	want := make(map[string]bool, len(paths))
	all := make([]string, 0, len(paths)+len(importCache))
	for _, path := range paths {
		want[path] = true
		all = append(all, path)
	}
	for path := range importCache {
		if !want[path] {
			all = append(all, path)
		}
	}
	slices.Sort(all)
	loaded, err := loadExportPackages(all)
	if err != nil {
		return nil, err
	}
	importGen++
	next := make(map[string]cachedImport, len(all))
	imp := exportImporter{byPath: make(map[string]*types.Package, len(paths))}
	for _, path := range all {
		pkg, ok := loaded[path]
		var loadErr error
		if !ok {
			loadErr = fmt.Errorf("package %q was not returned by the go command", path)
		} else if err := exportError(pkg); err != nil {
			loadErr = err
		}
		if loadErr != nil {
			if want[path] {
				imp.fail(path, loadErr)
			}
			continue
		}
		next[path] = cachedImport{pkg: pkg.Types, gen: importGen}
		if want[path] {
			imp.byPath[path] = pkg.Types
		}
	}
	importCache = next
	return imp, nil
}

func cachedImporter(paths []string) (exportImporter, bool) {
	imp := exportImporter{byPath: make(map[string]*types.Package, len(paths))}
	var gen int
	for i, path := range paths {
		item, ok := importCache[path]
		if !ok || (i > 0 && item.gen != gen) {
			return exportImporter{}, false
		}
		if i == 0 {
			gen = item.gen
		}
		imp.byPath[path] = item.pkg
	}
	return imp, true
}

func loadExportPackages(paths []string) (map[string]*packages.Package, error) {
	patterns := make([]string, len(paths))
	for i, path := range paths {
		// pattern= stops the loader from reading an import path as a query.
		patterns[i] = "pattern=" + path
	}
	pkgs, err := packages.Load(&packages.Config{
		// NeedTypes without NeedDeps reads compiler export data.
		// NeedDeps type-checks every dependency from source.
		Mode:       packages.NeedName | packages.NeedTypes,
		Dir:        startupDir,
		Env:        goListEnv(),
		BuildFlags: []string{"-mod=readonly"},
		Fset:       fset,
		ParseFile:  parseForLoad,
	}, patterns...)
	if err != nil {
		return nil, err
	}
	byPath := make(map[string]*packages.Package, len(pkgs))
	for _, pkg := range pkgs {
		path := pkg.PkgPath
		if path == "" {
			path = pkg.ID
		}
		byPath[path] = pkg
	}
	return byPath, nil
}

func parseForLoad(fset *token.FileSet, filename string, src []byte) (*ast.File, error) {
	parseMu.Lock()
	defer parseMu.Unlock()
	return parser.ParseFile(fset, filename, src, parser.ParseComments|parser.SkipObjectResolution)
}

// goListEnv is the environment for the go command. The last value of each
// variable wins. GOPROXY and GOSUMDB stay off so the load does not use the
// network. GOPACKAGESDRIVER=off forces the go command when a driver is
// installed. GOTOOLCHAIN=local keeps the toolchain that built the test.
func goListEnv() []string {
	env := os.Environ()
	if bin, ok := toolchainBin(); ok {
		env = append(env,
			"GOROOT="+runtime.GOROOT(),
			"PATH="+bin+string(os.PathListSeparator)+envPath(env),
			"GOTOOLCHAIN=local",
		)
	}
	return append(env,
		"GOPROXY=off",
		"GOSUMDB=off",
		"GOPACKAGESDRIVER=off",
	)
}

func toolchainBin() (string, bool) {
	goroot := runtime.GOROOT()
	if goroot == "" {
		return "", false
	}
	name := "go"
	if runtime.GOOS == "windows" {
		name = "go.exe"
	}
	bin := filepath.Join(goroot, "bin")
	if _, err := os.Stat(filepath.Join(bin, name)); err != nil {
		return "", false
	}
	return bin, true
}

func envPath(env []string) string {
	var path string
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if key == "PATH" || (runtime.GOOS == "windows" && strings.EqualFold(key, "Path")) {
			path = value
		}
	}
	return path
}

func exportError(pkg *packages.Package) error {
	if len(pkg.Errors) > 0 {
		msgs := make([]string, len(pkg.Errors))
		for i, e := range pkg.Errors {
			msgs[i] = e.Msg
		}
		return errors.New(strings.Join(msgs, "; "))
	}
	if pkg.Types == nil || !pkg.Types.Complete() {
		path := pkg.PkgPath
		if path == "" {
			path = pkg.ID
		}
		return fmt.Errorf("package %q has no export data", path)
	}
	return nil
}
