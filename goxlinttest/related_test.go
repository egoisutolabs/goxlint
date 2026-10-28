package goxlinttest_test

import (
	"go/ast"
	"path/filepath"
	"testing"

	"github.com/egoisutolabs/goxlint"
	"github.com/egoisutolabs/goxlint/goxlinttest"
)

func relatedFileRule() *goxlint.Rule {
	return &goxlint.Rule{Create: func(ctx *goxlint.Context) goxlint.Visitor {
		return goxlint.Visit(goxlint.On(func(fn *ast.FuncDecl) {
			ctx.Report(goxlint.Diagnostic{Node: fn.Name, Message: "found", Related: []goxlint.Related{{Node: fn.Name, Message: "declaration"}}})
		}))
	}}
}

func TestRunRelated(t *testing.T) {
	t.Parallel()
	goxlinttest.Run(t, relatedFileRule(), goxlinttest.Cases{Invalid: []goxlinttest.Case{
		{
			Name: "added package", Filename: filepath.Join("nested", "a.go"),
			Code: "// a function\nfunc f() {}",
			Errors: []goxlinttest.Error{{Message: "found", Related: []goxlinttest.RelatedError{
				{Filename: "nested/a.go", Message: "declaration", Line: 2, Column: 6},
			}}},
		},
		{
			Name: "explicit package", Filename: "nested/a.go",
			Code: "package p\n//line virtual.go:99\nfunc f() {}",
			Errors: []goxlinttest.Error{{Related: []goxlinttest.RelatedError{
				{Filename: filepath.Join("nested", "a.go"), Message: "declaration", Line: 3, Column: 6},
			}}},
		},
		{Name: "unchecked", Code: "func f() {}", Errors: []goxlinttest.Error{{}}},
		{Name: "zero fields", Code: "func f() {}", Errors: []goxlinttest.Error{{Related: []goxlinttest.RelatedError{{}}}}},
	}})
}

func relatedPackageRule() *goxlint.Rule {
	return &goxlint.Rule{AnalyzePackage: func(pkg *goxlint.PackageContext) {
		reference, primary := pkg.Files[0], pkg.Files[1]
		node := primary.File.Decls[0].(*ast.FuncDecl).Name
		primary.Report(goxlint.Diagnostic{Node: node, Message: "found", Related: []goxlint.Related{
			{Node: node, Message: "primary declaration"},
			{Node: reference.File.Decls[0].(*ast.FuncDecl).Name, Message: "original declaration"},
		}})
	}}
}

func relatedPackageCase() goxlinttest.PackageCase {
	return goxlinttest.PackageCase{
		Files: map[string]string{
			filepath.Join("a", "original.go"): "// original\n\nfunc original() {}",
			"b.go":                            "package p\nfunc copied() {}",
		},
		Errors: []goxlinttest.PackageError{{Filename: "b.go", Message: "found", Line: 2, Column: 6, Related: []goxlinttest.RelatedError{
			{Filename: "b.go", Message: "primary declaration", Line: 2, Column: 6},
			{Filename: "a/original.go", Message: "original declaration", Line: 3, Column: 6},
		}}},
	}
}

func TestRunPackageRelated(t *testing.T) {
	t.Parallel()
	checked := relatedPackageCase()
	checked.Name = "per-file prefixes and report order"
	unchecked := relatedPackageCase()
	unchecked.Name = "unchecked"
	unchecked.Errors[0].Related = nil
	zeroFields := relatedPackageCase()
	zeroFields.Name = "zero fields"
	zeroFields.Errors[0].Related = []goxlinttest.RelatedError{{}, {}}
	goxlinttest.RunPackage(t, relatedPackageRule(), goxlinttest.PackageCases{Invalid: []goxlinttest.PackageCase{checked, unchecked, zeroFields}})
}

func TestEmptyRelatedExpectation(t *testing.T) {
	t.Parallel()
	rule := &goxlint.Rule{Create: func(ctx *goxlint.Context) goxlint.Visitor {
		return goxlint.Visit(goxlint.On(func(fn *ast.FuncDecl) { ctx.Report(goxlint.Diagnostic{Node: fn.Name, Message: "found"}) }))
	}}
	goxlinttest.Run(t, rule, goxlinttest.Cases{Invalid: []goxlinttest.Case{{
		Code: "func f() {}", Errors: []goxlinttest.Error{{Related: []goxlinttest.RelatedError{}}},
	}}})
	goxlinttest.RunPackage(t, rule, goxlinttest.PackageCases{Invalid: []goxlinttest.PackageCase{{
		Files:  map[string]string{"a.go": "func f() {}"},
		Errors: []goxlinttest.PackageError{{Related: []goxlinttest.RelatedError{}}},
	}}})
}
