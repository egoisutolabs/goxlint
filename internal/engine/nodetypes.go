package engine

import (
	"go/ast"
	"reflect"
)

// concreteNodeTypes lists every pointer type in go/ast that implements ast.Node.
// Interface handlers expand to the entries that implement the interface, once
// per file, so the walk can keep a concrete type filter.
// Add an entry when a Go release adds a go/ast node type. The completeness
// test fails when a parsed file contains a node type that is missing here.
var concreteNodeTypes = []reflect.Type{
	reflect.TypeFor[*ast.ArrayType](),
	reflect.TypeFor[*ast.AssignStmt](),
	reflect.TypeFor[*ast.BadDecl](),
	reflect.TypeFor[*ast.BadExpr](),
	reflect.TypeFor[*ast.BadStmt](),
	reflect.TypeFor[*ast.BasicLit](),
	reflect.TypeFor[*ast.BinaryExpr](),
	reflect.TypeFor[*ast.BlockStmt](),
	reflect.TypeFor[*ast.BranchStmt](),
	reflect.TypeFor[*ast.CallExpr](),
	reflect.TypeFor[*ast.CaseClause](),
	reflect.TypeFor[*ast.ChanType](),
	reflect.TypeFor[*ast.CommClause](),
	reflect.TypeFor[*ast.Comment](),
	reflect.TypeFor[*ast.CommentGroup](),
	reflect.TypeFor[*ast.CompositeLit](),
	reflect.TypeFor[*ast.DeclStmt](),
	reflect.TypeFor[*ast.DeferStmt](),
	reflect.TypeFor[*ast.Directive](),
	reflect.TypeFor[*ast.Ellipsis](),
	reflect.TypeFor[*ast.EmptyStmt](),
	reflect.TypeFor[*ast.ExprStmt](),
	reflect.TypeFor[*ast.Field](),
	reflect.TypeFor[*ast.FieldList](),
	reflect.TypeFor[*ast.File](),
	reflect.TypeFor[*ast.ForStmt](),
	reflect.TypeFor[*ast.FuncDecl](),
	reflect.TypeFor[*ast.FuncLit](),
	reflect.TypeFor[*ast.FuncType](),
	reflect.TypeFor[*ast.GenDecl](),
	reflect.TypeFor[*ast.GoStmt](),
	reflect.TypeFor[*ast.Ident](),
	reflect.TypeFor[*ast.IfStmt](),
	reflect.TypeFor[*ast.ImportSpec](),
	reflect.TypeFor[*ast.IncDecStmt](),
	reflect.TypeFor[*ast.IndexExpr](),
	reflect.TypeFor[*ast.IndexListExpr](),
	reflect.TypeFor[*ast.InterfaceType](),
	reflect.TypeFor[*ast.KeyValueExpr](),
	reflect.TypeFor[*ast.LabeledStmt](),
	reflect.TypeFor[*ast.MapType](),
	reflect.TypeFor[*ast.Package](),
	reflect.TypeFor[*ast.ParenExpr](),
	reflect.TypeFor[*ast.RangeStmt](),
	reflect.TypeFor[*ast.ReturnStmt](),
	reflect.TypeFor[*ast.SelectStmt](),
	reflect.TypeFor[*ast.SelectorExpr](),
	reflect.TypeFor[*ast.SendStmt](),
	reflect.TypeFor[*ast.SliceExpr](),
	reflect.TypeFor[*ast.StarExpr](),
	reflect.TypeFor[*ast.StructType](),
	reflect.TypeFor[*ast.SwitchStmt](),
	reflect.TypeFor[*ast.TypeAssertExpr](),
	reflect.TypeFor[*ast.TypeSpec](),
	reflect.TypeFor[*ast.TypeSwitchStmt](),
	reflect.TypeFor[*ast.UnaryExpr](),
	reflect.TypeFor[*ast.ValueSpec](),
}
