// Package goxlint is the rule-authoring API for goxlint.
//
// A rule declares metadata and a Create function, an AnalyzePackage function,
// or both. Create runs once per file and
// returns a Visitor: a set of node handlers that the engine calls during a single
// shared AST walk. Rules report problems through the Context.
//
//	var NoFoo = &goxlint.Rule{
//		Meta: goxlint.Meta{
//			Name:     "no-foo",
//			Category: goxlint.Suspicious,
//			Messages: map[string]string{"foo": "Avoid `{{name}}`."},
//		},
//		Create: func(ctx *goxlint.Context) goxlint.Visitor {
//			return goxlint.Visit(
//				goxlint.On(func(n *ast.Ident) {
//					if n.Name == "foo" {
//						ctx.Report(goxlint.Diagnostic{Node: n, MessageID: "foo", Data: goxlint.Data{"name": n.Name}})
//					}
//				}),
//			)
//		},
//	}
package goxlint

import (
	"go/ast"
	"go/types"
	"reflect"
)

// Category groups rules by intent, mirroring oxlint's categories.
type Category string

const (
	// Correctness rules catch code that is wrong or useless. Enabled by default.
	Correctness Category = "correctness"
	// Suspicious rules catch code that is most likely wrong.
	Suspicious Category = "suspicious"
	// Pedantic rules are strict and may have false positives.
	Pedantic Category = "pedantic"
	// Style rules enforce idiomatic or readable code.
	Style Category = "style"
	// Restriction rules ban patterns that are valid but unwanted by policy.
	Restriction Category = "restriction"
	// Nursery rules are under development.
	Nursery Category = "nursery"
)

// Categories lists every category in display order.
var Categories = []Category{Correctness, Suspicious, Pedantic, Style, Restriction, Nursery}

// FixKind describes how safe a rule's fixes are to apply automatically.
type FixKind int

const (
	// FixNone means the rule provides no fixes.
	FixNone FixKind = iota
	// FixSafe fixes never change program behavior. Applied by --fix.
	FixSafe
	// FixSuggestion fixes may change behavior and need review. Applied by --fix-suggestions.
	FixSuggestion
	// FixDangerous fixes can break code. Applied by --fix-dangerously.
	FixDangerous
)

func (k FixKind) String() string {
	switch k {
	case FixSafe:
		return "safe"
	case FixSuggestion:
		return "suggestion"
	case FixDangerous:
		return "dangerous"
	default:
		return "none"
	}
}

// Meta describes a rule.
type Meta struct {
	// Name is the rule name without the plugin prefix, for example "no-any-parameters".
	Name string
	// Description is a one-line summary shown by `goxlint rules`.
	Description string
	Category    Category
	// Messages maps message IDs to templates. Templates interpolate Data with {{key}}.
	Messages map[string]string
	// Fixable is the most dangerous kind of fix the rule can produce.
	Fixable FixKind
	// NeedsTypes requests type information. When no enabled rule needs types,
	// goxlint parses files directly without loading packages, which is much faster.
	NeedsTypes bool
}

// Rule is a lint rule.
type Rule struct {
	Meta Meta
	// Create runs once per file and returns the handlers to call during the walk.
	// It may be nil for a package-only rule.
	Create func(ctx *Context) Visitor
	// AnalyzePackage runs once per package after file visitors, before suppressions.
	// A rule may provide either or both callbacks. Package state must be local
	// to this invocation: different packages can be analyzed concurrently.
	AnalyzePackage func(ctx *PackageContext)
}

// PackageContext provides the loaded files in one directory and Go package.
// Files are ordered by filename, include files where this rule is disabled,
// and exclude ignored and generated files. A partial selection (such as an
// explicit file pattern in syntax mode) includes only the loaded files.
// Treat syntax trees and type information as read-only.
type PackageContext struct {
	Name  string
	Files []*Context
}

// UsesOf returns the uses of obj in all loaded files of the package,
// ordered by filename, then position. Files where the rule is disabled are
// included. Use.Ctx.Enabled reports which of those files run the rule.
// It returns nil when obj is nil or obj has no uses.
func (p *PackageContext) UsesOf(obj types.Object) []Use {
	if obj == nil {
		return nil
	}
	var out []Use
	for _, file := range p.Files {
		out = append(out, file.UsesOf(obj)...)
	}
	return out
}

// Plugin is a named set of rules. Rules are configured as "<plugin>/<rule>".
type Plugin struct {
	Name  string
	Rules []*Rule
}

// Visitor is the set of node handlers a rule registers for one file.
type Visitor []Handler

// Handler is a callback for one AST node type, on entry or exit.
type Handler struct {
	NodeType reflect.Type
	Exit     bool
	Fn       func(ast.Node)
}

// Visit collects handlers into a Visitor.
func Visit(handlers ...Handler) Visitor { return handlers }

// On registers fn for every node of type N, called before the node's children.
func On[N ast.Node](fn func(N)) Handler {
	// SAFETY: the engine only calls Fn with nodes whose dynamic type is NodeType.
	return Handler{NodeType: reflect.TypeFor[N](), Fn: func(n ast.Node) { fn(n.(N)) }}
}

// OnExit registers fn for every node of type N, called after the node's children.
func OnExit[N ast.Node](fn func(N)) Handler {
	// SAFETY: the engine only calls Fn with nodes whose dynamic type is NodeType.
	return Handler{NodeType: reflect.TypeFor[N](), Exit: true, Fn: func(n ast.Node) { fn(n.(N)) }}
}
