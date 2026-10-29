package antislop

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/egoisutolabs/goxlint"
)

// NoMapAsStruct finds string-keyed maps that are used as records: local maps,
// parameters, and unexported struct fields that are only indexed with constant
// keys; functions that return such a map; typed reads such as m["n"].(int);
// and map literals that repeat the same key set across the package. A struct
// gives the fields names and types the compiler checks. A map that escapes
// (returned, passed to an unknown call, ranged over) is not reported, because
// its other uses are not visible.
var NoMapAsStruct = &goxlint.Rule{
	Meta: goxlint.Meta{
		Name:        "no-map-as-struct",
		Description: "Find maps with fixed string keys that should be structs.",
		Category:    goxlint.Pedantic,
		NeedsTypes:  true,
		Messages: map[string]string{
			"local":    "`{{name}}` is a map used as a struct: it only uses the fixed keys {{keys}}.",
			"field":    "Field `{{name}}` is a map used as a struct: it is only read with the fixed keys {{keys}}.",
			"shape":    "This map literal has the keys {{keys}}, like {{count}} other literal(s) in this package.",
			"returned": "`{{name}}` returns a map with the fixed keys {{keys}}. Return a struct.",
			"asserted": "`{{expr}}` reads a field from a map and asserts its type. A struct field has a type already.",
		},
	},
	Create: func(ctx *goxlint.Context) goxlint.Visitor {
		if ctx.IsTestFile() {
			return nil
		}
		return goxlint.Visit(
			goxlint.On(func(fn *ast.FuncDecl) {
				if fn.Body != nil {
					checkMapLocals(ctx, fn.Body)
					checkMapReturns(ctx, fn)
				}
			}),
			goxlint.On(func(ta *ast.TypeAssertExpr) {
				checkAssertedField(ctx, ta)
			}),
		)
	},
	AnalyzePackage: func(pkg *goxlint.PackageContext) {
		checkMapShapes(pkg)
		checkMapParamsAndFields(pkg)
	},
}

// mapRecord collects the fixed keys of one local map and the value types seen for each.
type mapRecord struct {
	ident   *ast.Ident
	keys    []string
	types   map[string][]types.Type
	escapes bool
}

func (r *mapRecord) add(key string, t types.Type) {
	if _, ok := r.types[key]; !ok {
		r.keys = append(r.keys, key)
		r.types[key] = nil
	}
	if t != nil {
		r.types[key] = append(r.types[key], t)
	}
}

func checkMapLocals(ctx *goxlint.Context, body *ast.BlockStmt) {
	records := map[types.Object]*mapRecord{}
	var order []types.Object
	declare := func(id *ast.Ident, init ast.Expr) {
		obj := ctx.TypesInfo.Defs[id]
		if obj == nil || !isStringKeyMap(obj.Type()) {
			return
		}
		r := &mapRecord{ident: id, types: map[string][]types.Type{}}
		if !mapInit(ctx, init, r) {
			return
		}
		records[obj] = r
		order = append(order, obj)
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.AssignStmt:
			if s.Tok == token.DEFINE && len(s.Lhs) == len(s.Rhs) {
				for i, lhs := range s.Lhs {
					if id, ok := lhs.(*ast.Ident); ok {
						declare(id, s.Rhs[i])
					}
				}
			}
		case *ast.ValueSpec:
			if len(s.Names) == len(s.Values) {
				for i, id := range s.Names {
					declare(id, s.Values[i])
				}
			}
		}
		return true
	})
	if len(records) == 0 {
		return
	}
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			for i, lhs := range x.Lhs {
				if r, key, ok := recordIndex(ctx, records, lhs); ok && len(x.Lhs) == len(x.Rhs) {
					r.add(key, ctx.TypeOf(x.Rhs[i]))
				}
			}
		case *ast.IndexExpr:
			if r, key, ok := recordIndex(ctx, records, x); ok {
				r.add(key, nil)
				return false
			}
		case *ast.Ident:
			if r := records[ctx.TypesInfo.Uses[x]]; r != nil {
				r.escapes = true
			}
		}
		return true
	})
	for _, obj := range order {
		r := records[obj]
		if r.escapes || len(r.keys) < 2 {
			continue
		}
		ctx.Report(goxlint.Diagnostic{
			Node:      r.ident,
			MessageID: "local",
			Data:      goxlint.Data{"name": r.ident.Name, "keys": quoteKeys(r.keys)},
			Help:      structHelp(ctx, obj.Type(), r),
		})
	}
}

// checkMapReturns reports a function with one map result when every return
// statement returns a literal with the same set of at least three constant keys.
func checkMapReturns(ctx *goxlint.Context, fn *ast.FuncDecl) {
	results := fn.Type.Results
	if results == nil || len(results.List) != 1 || len(results.List[0].Names) > 1 || !isStringKeyMap(ctx.TypeOf(results.List[0].Type)) {
		return
	}
	var keys []string
	ok := true
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			lit, isLit := returnedLiteral(s)
			got, isRecord := literalKeys(ctx, lit)
			if !isLit || !isRecord || len(got) < 3 {
				ok = false
				return false
			}
			sorted := slices.Sorted(slices.Values(got))
			if keys != nil && !slices.Equal(slices.Sorted(slices.Values(keys)), sorted) {
				ok = false
			}
			keys = got
		}
		return ok
	})
	if !ok || keys == nil {
		return
	}
	ctx.Report(goxlint.Diagnostic{
		Node:      fn.Name,
		MessageID: "returned",
		Data:      goxlint.Data{"name": fn.Name.Name, "keys": quoteKeys(keys)},
	})
}

func returnedLiteral(s *ast.ReturnStmt) (*ast.CompositeLit, bool) {
	if len(s.Results) != 1 {
		return nil, false
	}
	lit, ok := ast.Unparen(s.Results[0]).(*ast.CompositeLit)
	return lit, ok
}

// literalKeys returns the keys of a map literal whose keys are all constant strings.
func literalKeys(ctx *goxlint.Context, lit *ast.CompositeLit) ([]string, bool) {
	if lit == nil {
		return nil, false
	}
	keys := make([]string, 0, len(lit.Elts))
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			return nil, false
		}
		key, ok := constantString(ctx, kv.Key)
		if !ok {
			return nil, false
		}
		keys = append(keys, key)
	}
	return keys, true
}

// checkAssertedField reports m["key"].(T) on a string-keyed map with
// empty-interface values: the code knows the field and its type, so the map
// stands in for a struct.
func checkAssertedField(ctx *goxlint.Context, ta *ast.TypeAssertExpr) {
	if ta.Type == nil {
		return
	}
	ix, ok := ast.Unparen(ta.X).(*ast.IndexExpr)
	if !ok {
		return
	}
	t := ctx.TypeOf(ix.X)
	if !isStringKeyMap(t) {
		return
	}
	// SAFETY: isStringKeyMap checked that the underlying type is a map.
	if !isEmptyInterface(t.Underlying().(*types.Map).Elem()) {
		return
	}
	if _, ok := constantString(ctx, ix.Index); !ok {
		return
	}
	ctx.Report(goxlint.Diagnostic{
		Node:      ta,
		MessageID: "asserted",
		Data:      goxlint.Data{"expr": ctx.Snippet(ta)},
	})
}

// mapInit reports whether init creates a map that may be a record: make, or a
// literal whose keys are all constant strings. Literal keys are added to r.
func mapInit(ctx *goxlint.Context, init ast.Expr, r *mapRecord) bool {
	switch e := ast.Unparen(init).(type) {
	case *ast.CallExpr:
		id, ok := ast.Unparen(e.Fun).(*ast.Ident)
		if !ok {
			return false
		}
		_, builtin := ctx.TypesInfo.Uses[id].(*types.Builtin)
		return builtin && id.Name == "make"
	case *ast.CompositeLit:
		for _, elt := range e.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				return false
			}
			key, ok := constantString(ctx, kv.Key)
			if !ok {
				return false
			}
			r.add(key, ctx.TypeOf(kv.Value))
		}
		return true
	}
	return false
}

// recordIndex reports whether expr is m["key"] for a tracked map m and a constant key.
func recordIndex(ctx *goxlint.Context, records map[types.Object]*mapRecord, expr ast.Expr) (*mapRecord, string, bool) {
	ix, ok := ast.Unparen(expr).(*ast.IndexExpr)
	if !ok {
		return nil, "", false
	}
	id, ok := ast.Unparen(ix.X).(*ast.Ident)
	if !ok {
		return nil, "", false
	}
	r := records[ctx.TypesInfo.Uses[id]]
	if r == nil {
		return nil, "", false
	}
	key, ok := constantString(ctx, ix.Index)
	if !ok {
		return nil, "", false
	}
	return r, key, true
}

type mapLiteral struct {
	ctx *goxlint.Context
	lit *ast.CompositeLit
}

// checkMapShapes reports map literals in different places that use the same
// set of at least three constant keys.
func checkMapShapes(pkg *goxlint.PackageContext) {
	groups := map[string][]mapLiteral{}
	var order []string
	for _, ctx := range pkg.Files {
		if ctx.IsTestFile() {
			continue
		}
		// Literals returned directly are left to the "returned" check.
		returnedLits := map[*ast.CompositeLit]bool{}
		ast.Inspect(ctx.File, func(n ast.Node) bool {
			if ret, ok := n.(*ast.ReturnStmt); ok {
				if lit, ok := returnedLiteral(ret); ok {
					returnedLits[lit] = true
				}
			}
			lit, ok := n.(*ast.CompositeLit)
			if !ok || returnedLits[lit] || len(lit.Elts) < 3 || !isStringKeyMap(ctx.TypeOf(lit)) {
				return true
			}
			keys := make([]string, 0, len(lit.Elts))
			for _, elt := range lit.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					return true
				}
				key, ok := constantString(ctx, kv.Key)
				if !ok {
					return true
				}
				keys = append(keys, key)
			}
			slices.Sort(keys)
			id := strings.Join(keys, "\x00")
			if _, seen := groups[id]; !seen {
				order = append(order, id)
			}
			groups[id] = append(groups[id], mapLiteral{ctx, lit})
			return true
		})
	}
	for _, id := range order {
		group := groups[id]
		if len(group) < 2 {
			continue
		}
		keys := quoteKeys(strings.Split(id, "\x00"))
		for i, m := range group {
			var related []goxlint.Related
			for j, other := range group {
				// Related positions must use the reporting file's FileSet; the
				// engine loads one package with one FileSet.
				if j != i && other.ctx.Fset == m.ctx.Fset {
					related = append(related, goxlint.Related{Node: other.lit, Message: "same keys"})
				}
			}
			m.ctx.Report(goxlint.Diagnostic{
				Node:      m.lit,
				MessageID: "shape",
				Data:      goxlint.Data{"keys": keys, "count": strconv.Itoa(len(group) - 1)},
				Help:      "Declare a struct with these fields. If the map must stay open to other keys, build it in one helper function.",
				Related:   related,
			})
		}
	}
}

func isStringKeyMap(t types.Type) bool {
	if t == nil {
		return false
	}
	m, ok := t.Underlying().(*types.Map)
	if !ok {
		return false
	}
	b, ok := m.Key().Underlying().(*types.Basic)
	return ok && b.Info()&types.IsString != 0
}

func constantString(ctx *goxlint.Context, expr ast.Expr) (string, bool) {
	tv, ok := ctx.TypesInfo.Types[expr]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(tv.Value), true
}

func quoteKeys(keys []string) string {
	quoted := make([]string, len(keys))
	for i, k := range keys {
		quoted[i] = strconv.Quote(k)
	}
	return strings.Join(quoted, ", ")
}

// structHelp proposes a struct for r. Field types come from the map's value
// type, or from the values stored when the value type is an empty interface.
func structHelp(ctx *goxlint.Context, mapType types.Type, r *mapRecord) string {
	// SAFETY: records are only created for objects that pass isStringKeyMap.
	elem := mapType.Underlying().(*types.Map).Elem()
	qualifier := types.RelativeTo(ctx.Pkg)
	fields := make([]string, 0, len(r.keys))
	for _, key := range r.keys {
		name := fieldName(key)
		if name == "" {
			return "Use a struct with one field for each key."
		}
		t := elem
		if isEmptyInterface(elem) {
			t = commonType(r.types[key], elem)
		}
		fields = append(fields, name+" "+types.TypeString(types.Default(t), qualifier))
	}
	return "Use a struct: struct { " + strings.Join(fields, "; ") + " }"
}

// commonType returns the type of every value in ts, or fallback when they differ.
func commonType(ts []types.Type, fallback types.Type) types.Type {
	if len(ts) == 0 {
		return fallback
	}
	for _, t := range ts[1:] {
		if !types.Identical(types.Default(t), types.Default(ts[0])) {
			return fallback
		}
	}
	return ts[0]
}

// fieldName converts a key such as "user_id" or "created-at" to an exported
// Go identifier, or returns "" when that is not possible.
func fieldName(key string) string {
	var b strings.Builder
	for _, part := range strings.FieldsFunc(key, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if strings.EqualFold(part, "id") || strings.EqualFold(part, "url") {
			b.WriteString(strings.ToUpper(part))
			continue
		}
		runes := []rune(part)
		runes[0] = unicode.ToUpper(runes[0])
		b.WriteString(string(runes))
	}
	name := b.String()
	if !token.IsIdentifier(name) || !token.IsExported(name) {
		return ""
	}
	return name
}

// packageMap is a parameter or struct field tracked across the package.
type packageMap struct {
	ctx     *goxlint.Context
	ident   *ast.Ident
	field   bool
	keys    []string
	seen    map[string]bool
	escapes bool
}

func (m *packageMap) add(keys ...string) {
	for _, k := range keys {
		if !m.seen[k] {
			m.seen[k] = true
			m.keys = append(m.keys, k)
		}
	}
}

// checkMapParamsAndFields reports map parameters and unexported map fields
// that are only indexed with constant keys. A map passed to a parameter that
// is itself only indexed with constant keys counts as indexed with those keys.
func checkMapParamsAndFields(pkg *goxlint.PackageContext) {
	tracked := map[types.Object]*packageMap{}
	var order []types.Object
	params := map[*types.Func][]types.Object{}
	track := func(ctx *goxlint.Context, id *ast.Ident, field bool) types.Object {
		obj := ctx.TypesInfo.Defs[id]
		if obj == nil || !isStringKeyMap(obj.Type()) || (field && obj.Exported()) {
			return nil
		}
		tracked[obj] = &packageMap{ctx: ctx, ident: id, field: field, seen: map[string]bool{}}
		order = append(order, obj)
		return obj
	}
	var files []*goxlint.Context
	for _, ctx := range pkg.Files {
		if ctx.IsTestFile() || ctx.TypesInfo == nil {
			continue
		}
		files = append(files, ctx)
		ast.Inspect(ctx.File, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.FuncDecl:
				fn, _ := ctx.TypesInfo.Defs[x.Name].(*types.Func)
				if fn == nil || x.Body == nil {
					return true
				}
				var objs []types.Object
				for _, f := range x.Type.Params.List {
					if len(f.Names) == 0 {
						objs = append(objs, nil)
					}
					for _, id := range f.Names {
						objs = append(objs, track(ctx, id, false))
					}
				}
				params[fn] = objs
			case *ast.StructType:
				for _, f := range x.Fields.List {
					for _, id := range f.Names {
						track(ctx, id, true)
					}
				}
			}
			return true
		})
	}
	if len(tracked) == 0 {
		return
	}
	// Two rounds: the first finds the keys of each parameter with every call
	// treated as an escape; the second lets callers pass a map to a parameter
	// that did not escape. Deeper chains of helpers are not followed.
	type summary struct {
		keys    []string
		escapes bool
	}
	var summaries map[types.Object]summary
	paramKeys := func(ctx *goxlint.Context, call *ast.CallExpr, i int) ([]string, bool) {
		objs := params[ctx.CalleeFunc(call)]
		if i >= len(objs) || objs[i] == nil {
			return nil, false
		}
		s, ok := summaries[objs[i]]
		return s.keys, ok && !s.escapes
	}
	// A parameter that receives a map used with dynamic keys elsewhere is a
	// shared map, not a record, even if this function reads fixed keys only.
	dynamic := map[types.Object]bool{}
	for _, ctx := range files {
		collectDynamicMaps(ctx, dynamic)
	}
	// A map passed to a dynamic parameter is dynamic too, and a parameter that
	// receives a dynamic map is shared. Repeat until nothing changes.
	shared := map[types.Object]bool{}
	for changed := true; changed; {
		changed = false
		for _, ctx := range files {
			ast.Inspect(ctx.File, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				objs := params[ctx.CalleeFunc(call)]
				for i, arg := range call.Args {
					obj := mapObject(ctx, arg)
					if obj == nil || i >= len(objs) || objs[i] == nil {
						continue
					}
					if dynamic[objs[i]] && !dynamic[obj] {
						dynamic[obj], changed = true, true
					}
					// A shared parameter is dynamic too, so a helper that it is
					// forwarded to is shared as well.
					if dynamic[obj] && !shared[objs[i]] {
						shared[objs[i]], dynamic[objs[i]], changed = true, true, true
					}
				}
				return true
			})
		}
	}
	for range 2 {
		for obj, m := range tracked {
			m.keys, m.seen, m.escapes = nil, map[string]bool{}, shared[obj]
		}
		for _, ctx := range files {
			scanMapUses(ctx, tracked, paramKeys)
		}
		summaries = map[types.Object]summary{}
		for obj, m := range tracked {
			summaries[obj] = summary{m.keys, m.escapes}
		}
	}
	for _, obj := range order {
		m := tracked[obj]
		if m.escapes || len(m.keys) < 2 {
			continue
		}
		id, name := "local", m.ident.Name
		if m.field {
			id = "field"
		}
		m.ctx.Report(goxlint.Diagnostic{
			Node:      m.ident,
			MessageID: id,
			Data:      goxlint.Data{"name": name, "keys": quoteKeys(m.keys)},
			Help:      "Use a struct with one field for each key.",
		})
	}
}

// scanMapUses records how each tracked map is used in one file. An identifier
// that refers to a tracked map is an escape unless its parent node is an
// allowed use: an index with a constant key, an argument to a tracked
// parameter, a whole-value assignment, or a struct literal key.
func scanMapUses(ctx *goxlint.Context, tracked map[types.Object]*packageMap, paramKeys func(*goxlint.Context, *ast.CallExpr, int) ([]string, bool)) {
	handled := map[*ast.Ident]bool{}
	rangeKeys := map[types.Object][]string{}
	ref := func(e ast.Expr) (*ast.Ident, *packageMap) {
		var id *ast.Ident
		switch x := ast.Unparen(e).(type) {
		case *ast.Ident:
			id = x
		case *ast.SelectorExpr:
			id = x.Sel
		default:
			return nil, nil
		}
		return id, tracked[ctx.TypesInfo.Uses[id]]
	}
	ast.Inspect(ctx.File, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.RangeStmt:
			if v, ok := x.Value.(*ast.Ident); ok && rangesOverConstants(ctx, x) {
				// SAFETY: rangesOverConstants checked that x.X is a composite literal.
				keys, _ := constantStrings(ctx, ast.Unparen(x.X).(*ast.CompositeLit))
				rangeKeys[ctx.TypesInfo.Defs[v]] = keys
			}
		case *ast.IndexExpr:
			id, m := ref(x.X)
			if m == nil {
				return true
			}
			if key, ok := constantString(ctx, x.Index); ok {
				m.add(key)
				handled[id] = true
			} else if k, ok := x.Index.(*ast.Ident); ok && rangeKeys[ctx.TypesInfo.Uses[k]] != nil {
				m.add(rangeKeys[ctx.TypesInfo.Uses[k]]...)
				handled[id] = true
			}
		case *ast.CallExpr:
			for i, arg := range x.Args {
				if id, m := ref(arg); m != nil {
					if keys, ok := paramKeys(ctx, x, i); ok {
						m.add(keys...)
						handled[id] = true
					}
				}
			}
		case *ast.AssignStmt:
			for _, lhs := range x.Lhs {
				if id, m := ref(lhs); m != nil {
					handled[id] = true
				}
			}
		case *ast.KeyValueExpr:
			if id, m := ref(x.Key); m != nil && m.field {
				handled[id] = true
			}
		case *ast.Ident:
			if m := tracked[ctx.TypesInfo.Uses[x]]; m != nil && !handled[x] {
				m.escapes = true
			}
		}
		return true
	})
}

// collectDynamicMaps adds each string-keyed map variable that is ranged over
// or indexed with a key that is not constant.
// A loop variable over a literal of constant strings counts as constant.
func collectDynamicMaps(ctx *goxlint.Context, dynamic map[types.Object]bool) {
	fixed := map[types.Object]bool{}
	ast.Inspect(ctx.File, func(n ast.Node) bool {
		var target, index ast.Expr
		switch x := n.(type) {
		case *ast.IndexExpr:
			target, index = x.X, x.Index
			if k, ok := index.(*ast.Ident); ok && fixed[ctx.TypesInfo.Uses[k]] {
				return true
			}
		case *ast.RangeStmt:
			target = x.X
			if v, ok := x.Value.(*ast.Ident); ok && rangesOverConstants(ctx, x) {
				fixed[ctx.TypesInfo.Defs[v]] = true
			}
		default:
			return true
		}
		obj := mapObject(ctx, target)
		if obj == nil || !isStringKeyMap(obj.Type()) {
			return true
		}
		if index != nil {
			if _, constant := constantString(ctx, index); constant {
				return true
			}
		}
		dynamic[obj] = true
		return true
	})
}

// rangesOverConstants reports whether r is `for _, v := range` over a literal
// of constant strings, and the loop body does not modify v.
func rangesOverConstants(ctx *goxlint.Context, r *ast.RangeStmt) bool {
	lit, ok := ast.Unparen(r.X).(*ast.CompositeLit)
	v, isIdent := r.Value.(*ast.Ident)
	if !ok || !isIdent || r.Tok != token.DEFINE {
		return false
	}
	if _, ok = constantStrings(ctx, lit); !ok {
		return false
	}
	return !modifies(ctx, r.Body, ctx.TypesInfo.Defs[v])
}

// modifies reports whether body assigns to obj, increments it, or takes its address.
func modifies(ctx *goxlint.Context, body ast.Node, obj types.Object) bool {
	is := func(e ast.Expr) bool {
		id, ok := ast.Unparen(e).(*ast.Ident)
		return ok && obj != nil && ctx.TypesInfo.Uses[id] == obj
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			found = found || slices.ContainsFunc(x.Lhs, is)
		case *ast.IncDecStmt:
			found = found || is(x.X)
		case *ast.UnaryExpr:
			found = found || (x.Op == token.AND && is(x.X))
		}
		return !found
	})
	return found
}

// mapObject returns the variable or field that expr names, or nil.
func mapObject(ctx *goxlint.Context, expr ast.Expr) types.Object {
	switch x := ast.Unparen(expr).(type) {
	case *ast.Ident:
		return ctx.TypesInfo.Uses[x]
	case *ast.SelectorExpr:
		return ctx.TypesInfo.Uses[x.Sel]
	}
	return nil
}

// constantStrings returns the elements of a literal whose elements are all constant strings.
func constantStrings(ctx *goxlint.Context, lit *ast.CompositeLit) ([]string, bool) {
	keys := make([]string, 0, len(lit.Elts))
	for _, elt := range lit.Elts {
		key, ok := constantString(ctx, elt)
		if !ok {
			return nil, false
		}
		keys = append(keys, key)
	}
	return keys, len(keys) > 0
}
