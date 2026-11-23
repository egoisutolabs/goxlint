package goxlinttest_test

import (
	"go/ast"
	"os"
	"testing"

	"github.com/egoisutolabs/goxlint"
	"github.com/egoisutolabs/goxlint/goxlinttest"
)

func TestStdlibImportTypes(t *testing.T) {
	t.Parallel()
	goxlinttest.Run(t, recorderRule(t), goxlinttest.Cases{
		Valid: []goxlinttest.Case{
			{Name: "fixture", Code: "func f() int { return 1 }"},
			{Name: "unsafe", Code: "import \"unsafe\"\nvar n = unsafe.Sizeof(0)"},
			{Name: "dot", Code: "import . \"fmt\"\nfunc f() string { return Sprintf(\"%d\", 1) }"},
			{Name: "io writer", Code: "import (\n\t\"io\"\n\t\"os\"\n)\nvar w io.Writer = os.Stdout"},
		},
		Invalid: []goxlinttest.Case{
			{
				Name:   "httptest",
				Code:   "import \"net/http/httptest\"\nfunc f() { httptest.NewRecorder() }",
				Errors: []goxlinttest.Error{{MessageID: "recorder", Line: 2, Column: 12}},
			},
			{
				Name:   "httptest renamed",
				Code:   "import ht \"net/http/httptest\"\nfunc f() { ht.NewRecorder() }",
				Errors: []goxlinttest.Error{{MessageID: "recorder", Line: 2, Column: 12}},
			},
			{
				Name:   "httptest and writer",
				Code:   "import (\n\t\"io\"\n\t\"net/http/httptest\"\n\t\"os\"\n)\n\nvar w io.Writer = os.Stdout\n\nfunc f() { httptest.NewRecorder() }",
				Errors: []goxlinttest.Error{{MessageID: "recorder", Line: 9, Column: 12}},
			},
		},
	})
	goxlinttest.RunPackage(t, recorderRule(t), goxlinttest.PackageCases{
		Invalid: []goxlinttest.PackageCase{{
			Name: "two files",
			Files: map[string]string{
				"a.go": "import \"net/http/httptest\"\nfunc f() { httptest.NewRecorder() }",
				"b.go": "func g() { f() }",
			},
			Errors: []goxlinttest.PackageError{{
				Filename: "a.go", MessageID: "recorder", Line: 2, Column: 12,
			}},
		}},
	})
}

func TestStdlibImportIgnoresWorkingDirectory(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("go.mod", []byte("this is not a go.mod\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	goxlinttest.Run(t, recorderRule(t), goxlinttest.Cases{
		Invalid: []goxlinttest.Case{{
			Code:   "import \"net/http/httptest\"\nfunc f() { httptest.NewRecorder() }",
			Errors: []goxlinttest.Error{{MessageID: "recorder", Line: 2, Column: 12}},
		}},
	})
}

func recorderRule(t *testing.T) *goxlint.Rule {
	t.Helper()
	return &goxlint.Rule{
		Meta: goxlint.Meta{
			NeedsTypes: true,
			Messages:   map[string]string{"recorder": "recorder"},
		},
		Create: func(ctx *goxlint.Context) goxlint.Visitor {
			return goxlint.Visit(goxlint.On(func(call *ast.CallExpr) {
				if !ctx.IsPkgFunc(call, "net/http/httptest", "NewRecorder") {
					return
				}
				typ := ctx.TypeOf(call)
				if typ == nil || typ.String() != "*net/http/httptest.ResponseRecorder" {
					t.Errorf("NewRecorder type = %v", typ)
				}
				ctx.Report(goxlint.Diagnostic{Node: call, MessageID: "recorder"})
			}))
		},
	}
}
