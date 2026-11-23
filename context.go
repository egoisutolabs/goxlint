package goxlint

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"
)

// Data supplies values for {{key}} placeholders in message templates.
type Data map[string]string

// Diagnostic is a problem reported by a rule.
type Diagnostic struct {
	// Node locates the problem. Pos and End may be set instead.
	Node     ast.Node
	Pos, End token.Pos
	// MessageID selects a template from Meta.Messages. Message may be set instead.
	MessageID string
	Message   string
	Data      Data
	// Help is an optional hint printed under the diagnostic.
	Help string
	// Related contains supporting locations in loaded files of the same package.
	// They do not change the primary location used for suppressions and fixes.
	Related []Related
	// Fix is an optional fix. Its kind must not exceed Meta.Fixable.
	Fix *Fix
}

// Related is a supporting location for a diagnostic. Positions use the
// reporting context's FileSet. Node takes precedence over Pos and End.
// An omitted End denotes a point at Pos. Message labels the location.
type Related struct {
	Node     ast.Node
	Pos, End token.Pos
	Message  string
}

// Fix is a set of non-overlapping text edits.
type Fix struct {
	Kind    FixKind
	Message string
	Edits   []Edit
}

// Edit replaces the source between Pos and End with NewText.
type Edit struct {
	Pos, End token.Pos
	NewText  string
}

// ReplaceNode returns an edit that replaces node with text.
func ReplaceNode(node ast.Node, text string) Edit {
	return Edit{Pos: node.Pos(), End: node.End(), NewText: text}
}

// InsertBefore returns an edit that inserts text at pos.
func InsertBefore(pos token.Pos, text string) Edit {
	return Edit{Pos: pos, End: pos, NewText: text}
}

// Context is a rule's view of the file being linted.
type Context struct {
	// Enabled is false for package-analysis evidence files where the rule is
	// disabled. Reports from those contexts are ignored by the engine.
	Enabled  bool
	Rule     *Rule
	RuleID   string // "<plugin>/<rule>"
	Fset     *token.FileSet
	File     *ast.File
	Filename string
	Source   []byte
	// Pkg and TypesInfo are nil unless the rule's Meta.NeedsTypes is set.
	Pkg       *types.Package
	TypesInfo *types.Info

	options json.RawMessage
	stack   *[]ast.Node
	report  func(Diagnostic)
	// uses is the shared per-file index. The engine passes one function to
	// every rule of a file. Nil when the file has no index.
	uses func(obj types.Object) []Use
}

// Use is one place where an object is referenced.
type Use struct {
	Ident *ast.Ident
	// Expr is the expression that refers to the object: Ident, or the
	// *ast.SelectorExpr for a field or a qualified name.
	Expr ast.Expr
	// Parent is the node that contains Expr (parentheses are skipped).
	Parent ast.Node
	// Ctx is the file that contains the use.
	Ctx *Context
}

// NewContext is used by the engine. Rules receive a ready Context from Create.
// uses reports the uses of an object in this file. Pass the same function for
// every rule of a file so they share one index. It may be nil.
func NewContext(rule *Rule, ruleID string, fset *token.FileSet, file *ast.File, filename string, src []byte,
	pkg *types.Package, info *types.Info, options json.RawMessage, stack *[]ast.Node, report func(Diagnostic), uses func(types.Object) []Use) *Context {
	return &Context{
		Enabled: true,
		Rule:    rule, RuleID: ruleID, Fset: fset, File: file, Filename: filename, Source: src,
		Pkg: pkg, TypesInfo: info, options: options, stack: stack, report: report, uses: uses,
	}
}

// Report records a diagnostic. It panics when the rule itself is broken (an
// unknown message ID, or a fix more dangerous than Meta.Fixable); the engine
// recovers and reports the rule as failed.
func (c *Context) Report(d Diagnostic) {
	if !c.Enabled {
		return
	}
	if d.Message == "" {
		tmpl, ok := c.Rule.Meta.Messages[d.MessageID]
		if !ok {
			//goxlint:ignore anti-slop/no-panic-in-library a broken rule is a programming error
			panic(fmt.Sprintf("%s: unknown message ID %q", c.RuleID, d.MessageID))
		}
		d.Message = Interpolate(tmpl, d.Data)
	}
	if d.Fix != nil && d.Fix.Kind > c.Rule.Meta.Fixable {
		//goxlint:ignore anti-slop/no-panic-in-library a broken rule is a programming error
		panic(fmt.Sprintf("%s: reported a %s fix but Meta.Fixable is %s", c.RuleID, d.Fix.Kind, c.Rule.Meta.Fixable))
	}
	c.report(d)
}

// Interpolate replaces {{key}} placeholders in tmpl with values from data.
func Interpolate(tmpl string, data Data) string {
	for k, v := range data {
		tmpl = strings.ReplaceAll(tmpl, "{{"+k+"}}", v)
	}
	return tmpl
}

// Options decodes the rule's configured options into v. It leaves v unchanged
// when no options are configured, so callers set defaults before calling.
// Invalid options are reported as an error diagnostic and Options returns
// false; the rule should then return a nil Visitor.
func (c *Context) Options(v any) bool {
	if len(c.options) == 0 {
		return true
	}
	dec := json.NewDecoder(bytes.NewReader(c.options))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		c.report(Diagnostic{Pos: c.File.Package, Message: fmt.Sprintf("invalid options for %s: %v", c.RuleID, err)})
		return false
	}
	return true
}

// Parent returns the parent of the node currently being visited, or nil at the root.
func (c *Context) Parent() ast.Node {
	s := *c.stack
	if len(s) < 2 {
		return nil
	}
	return s[len(s)-2]
}

// Ancestors returns the ancestors of the current node, nearest first.
func (c *Context) Ancestors() []ast.Node {
	s := *c.stack
	out := make([]ast.Node, 0, len(s))
	for i := len(s) - 2; i >= 0; i-- {
		out = append(out, s[i])
	}
	return out
}

// Text returns the source text of node.
func (c *Context) Text(node ast.Node) string {
	f := c.Fset.File(node.Pos())
	return string(c.Source[f.Offset(node.Pos()):f.Offset(node.End())])
}

// maxSnippet is the longest snippet Snippet returns, in runes.
const maxSnippet = 60

// Snippet returns node's source collapsed to one line and shortened, for
// quoting code in messages.
func (c *Context) Snippet(node ast.Node) string {
	text := strings.Join(strings.Fields(c.Text(node)), " ")
	text = strings.ReplaceAll(text, "( ", "(")
	if r := []rune(text); len(r) > maxSnippet {
		return string(r[:maxSnippet-1]) + "…"
	}
	return text
}

// OwnLine reports whether only whitespace precedes pos on its line.
func (c *Context) OwnLine(pos token.Pos) bool {
	f := c.Fset.File(pos)
	off := f.Offset(pos)
	start := f.Offset(f.LineStart(f.Line(pos)))
	return strings.TrimSpace(string(c.Source[start:off])) == ""
}

// Line returns the 1-based line of pos.
func (c *Context) Line(pos token.Pos) int { return c.Fset.PositionFor(pos, false).Line }

// IsTestFile reports whether the file is a _test.go file.
func (c *Context) IsTestFile() bool { return strings.HasSuffix(c.Filename, "_test.go") }

// TypeOf returns the type of expr, or nil without type information.
func (c *Context) TypeOf(expr ast.Expr) types.Type {
	if c.TypesInfo == nil {
		return nil
	}
	return c.TypesInfo.TypeOf(expr)
}

// ObjectOf returns the object id denotes, or nil without type information.
func (c *Context) ObjectOf(id *ast.Ident) types.Object {
	if c.TypesInfo == nil {
		return nil
	}
	return c.TypesInfo.ObjectOf(id)
}

// ObjectOfExpr returns the object that expr names.
// An identifier names its definition or the object it uses.
// A selector x.f names the field, method, or qualified object f.
// Any other expression returns nil, as does a rule without type information.
func (c *Context) ObjectOfExpr(expr ast.Expr) types.Object {
	if c.TypesInfo == nil || expr == nil {
		return nil
	}
	switch x := ast.Unparen(expr).(type) {
	case *ast.Ident:
		if obj := c.TypesInfo.Uses[x]; obj != nil {
			return obj
		}
		return c.TypesInfo.Defs[x]
	case *ast.SelectorExpr:
		return c.TypesInfo.Uses[x.Sel]
	default:
		return nil
	}
}

// UsesOf returns the uses of obj in this file, in source order.
// It returns nil when the rule does not set Meta.NeedsTypes, when obj is nil,
// or when obj has no uses in this file.
func (c *Context) UsesOf(obj types.Object) []Use {
	if c.TypesInfo == nil || c.uses == nil || obj == nil {
		return nil
	}
	found := c.uses(obj)
	if len(found) == 0 {
		return nil
	}
	out := make([]Use, len(found))
	for i := range found {
		out[i] = found[i]
		out[i].Ctx = c
	}
	return out
}

// CalleeFunc returns the function or method a call statically invokes, or nil.
// An explicit instantiation (f[T](x), f[T, U](x)) and a method expression
// (T.M(v), (*T).M(v)) resolve to that function. A method of an instantiated
// generic type is the instance go/types records; Origin returns the declaration.
func (c *Context) CalleeFunc(call *ast.CallExpr) *types.Func {
	if call == nil {
		return nil
	}
	fun := ast.Unparen(call.Fun)
	switch x := fun.(type) {
	case *ast.IndexExpr:
		fun = ast.Unparen(x.X)
	case *ast.IndexListExpr:
		fun = ast.Unparen(x.X)
	}
	fn, _ := c.ObjectOfExpr(fun).(*types.Func)
	return fn
}

// IsPkgFunc reports whether call invokes the package-level function pkgPath.name.
func (c *Context) IsPkgFunc(call *ast.CallExpr, pkgPath string, names ...string) bool {
	fn := c.CalleeFunc(call)
	if fn == nil || fn.Pkg() == nil || fn.Pkg().Path() != pkgPath {
		return false
	}
	if sig, ok := fn.Type().(*types.Signature); !ok || sig.Recv() != nil {
		return false
	}
	for _, name := range names {
		if fn.Name() == name {
			return true
		}
	}
	return false
}

// CommentsBefore returns the comment group that ends on the line directly above
// pos, or nil. A blank line between the comment and pos detaches it.
func (c *Context) CommentsBefore(pos token.Pos) *ast.CommentGroup {
	line := c.Line(pos)
	for _, cg := range c.File.Comments {
		if c.Line(cg.End()) == line-1 {
			return cg
		}
	}
	return nil
}

// TrailingComment returns the comment group that starts on the same line as
// pos, after it, or nil.
func (c *Context) TrailingComment(pos token.Pos) *ast.CommentGroup {
	line := c.Line(pos)
	for _, cg := range c.File.Comments {
		if cg.Pos() > pos && c.Line(cg.Pos()) == line {
			return cg
		}
	}
	return nil
}
