package engine

import (
	"fmt"
	"go/ast"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOverlay(t *testing.T) {
	for _, typed := range []bool{false, true} {
		t.Run(fmt.Sprint(typed), func(t *testing.T) {
			root := t.TempDir()
			name := filepath.Join(root, "buffer.go")
			for path, src := range map[string]string{
				"go.mod":    "module test.local/overlay\n\ngo 1.26.0\n",
				"buffer.go": "package p\nvar value = 1\n",
			} {
				if err := os.WriteFile(filepath.Join(root, path), []byte(src), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			source := []byte("package p\nvar value = \"overlay\"\n")
			overlay := map[string][]byte{name: source}
			files, errs := Load(LoadOptions{Dir: root, Patterns: []string{name}, Typed: typed, Overlay: overlay})
			if len(errs) != 0 || len(files) != 1 {
				t.Fatalf("files=%d, errors=%v", len(files), errs)
			}
			f := files[0]
			value := f.AST.Decls[0].(*ast.GenDecl).Specs[0].(*ast.ValueSpec).Values[0]
			if f.Filename != name || string(f.Source) != string(source) || value.(*ast.BasicLit).Value != `"overlay"` {
				t.Fatalf("inconsistent source and AST: %+v", f)
			}
			if typed && f.Info.TypeOf(value).String() != "string" {
				t.Fatal("type information used disk contents")
			}
			if len(overlay) != 1 || string(overlay[name]) != string(source) {
				t.Fatal("Load changed the overlay")
			}
			contents, err := os.ReadFile(name)
			if err != nil || string(contents) != "package p\nvar value = 1\n" {
				t.Fatalf("Load changed disk contents: %q: %v", contents, err)
			}
		})
	}
}

func TestSyntaxOverlayMissingAndEmptyFiles(t *testing.T) {
	root := t.TempDir()
	name := filepath.Join(root, "new", "buffer.go")
	files, errs := Load(LoadOptions{Dir: root, Patterns: []string{name}, Overlay: map[string][]byte{name: []byte("package p\n")}})
	if len(errs) != 0 || len(files) != 1 {
		t.Fatalf("new buffer: files=%d errors=%v", len(files), errs)
	}
	files, errs = Load(LoadOptions{Dir: root, Patterns: []string{name}, Overlay: map[string][]byte{name: nil}})
	if len(errs) != 1 || len(files) != 0 {
		t.Fatalf("empty buffer was not parsed: files=%d errors=%v", len(files), errs)
	}
	files, errs = Load(LoadOptions{Dir: root, Patterns: []string{name}, Overlay: map[string][]byte{name: nil}, Ignore: func(path string, _ bool) bool { return path == name }})
	if len(errs) != 0 || len(files) != 0 {
		t.Fatalf("ignored buffer was loaded: files=%d errors=%v", len(files), errs)
	}
}
