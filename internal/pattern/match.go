package pattern

import (
	"go/ast"
	"go/token"
	"go/types"
	"reflect"
)

var (
	nodeIface = reflect.TypeFor[ast.Node]()
	posType   = reflect.TypeFor[token.Pos]()
)

// Match reports whether node matches p.
// info enables import-path matching of package selectors. A nil info compares
// selector names as they are written.
// Captures maps a variable name to the matched nodes. $_ and $*_ are omitted.
// A variadic name is present even when it matched no nodes.
func (p *Pattern) Match(node ast.Node, info *types.Info) (map[string][]ast.Node, bool) {
	caps := map[string][]ast.Node{}
	if !p.matchNode(p.root, node, caps, info) {
		return nil, false
	}
	for _, ex := range p.except {
		trial := snapshot(caps)
		if ex.matchNode(ex.root, node, trial, info) {
			return nil, false
		}
	}
	return caps, true
}

func (p *Pattern) matchNode(pat, node ast.Node, caps map[string][]ast.Node, info *types.Info) bool {
	// Unwrap the pattern so ($x) is still one capture. Keep the candidate's
	// parentheses on a capture so a fix can paste them back.
	pat = unwrapParen(pat)
	if m, ok := p.oneMarker(pat); ok {
		return bindOne(m, node, caps)
	}
	if es, ok := pat.(*ast.ExprStmt); ok {
		if m, ok := p.oneMarker(es.X); ok {
			return bindOne(m, node, caps)
		}
	}
	node = unwrapParen(node)
	if pat == nil || node == nil {
		return pat == nil && node == nil
	}
	if reflect.TypeOf(pat) != reflect.TypeOf(node) {
		return false
	}
	if ps, ok := pat.(*ast.SelectorExpr); ok {
		ns, ok := node.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		if matched, decided := p.tryPkg(ps, ns, caps, info); decided {
			return matched
		}
	}
	saved := snapshot(caps)
	ok := p.matchStruct(reflect.ValueOf(pat).Elem(), reflect.ValueOf(node).Elem(), caps, info)
	if !ok {
		restore(caps, saved)
	}
	return ok
}

// tryPkg matches pkg.Name by import path when info is set and pkg is a concrete name.
// decided is false when the pattern selector is not a package qualifier.
func (p *Pattern) tryPkg(ps, ns *ast.SelectorExpr, caps map[string][]ast.Node, info *types.Info) (bool, bool) {
	if info == nil {
		return false, false
	}
	x, ok := unwrapParen(ps.X).(*ast.Ident)
	if !ok || p.isIdentMarker(x) {
		return false, false
	}
	y, ok := unwrapParen(ns.X).(*ast.Ident)
	if !ok {
		return false, true
	}
	pn, ok := info.ObjectOf(y).(*types.PkgName)
	if !ok || pn.Imported() == nil || pn.Imported().Path() != x.Name {
		return false, true
	}
	return p.matchNode(ps.Sel, ns.Sel, caps, info), true
}

func (p *Pattern) matchStruct(pv, nv reflect.Value, caps map[string][]ast.Node, info *types.Info) bool {
	t := pv.Type()
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if !sf.IsExported() || skipField(sf) {
			continue
		}
		pf, nf := pv.Field(i), nv.Field(i)
		if sf.Type == posType {
			if !samePosPresence(pf, nf) {
				return false
			}
			continue
		}
		if !p.matchValue(pf, nf, caps, info) {
			return false
		}
	}
	return true
}

func (p *Pattern) matchValue(pv, nv reflect.Value, caps map[string][]ast.Node, info *types.Info) bool {
	if !pv.IsValid() || !nv.IsValid() {
		return pv.IsValid() == nv.IsValid()
	}
	switch pv.Kind() {
	case reflect.Interface, reflect.Pointer:
		if pv.IsNil() || nv.IsNil() {
			return pv.IsNil() && nv.IsNil()
		}
		if pv.Type() != nv.Type() || !pv.Type().Implements(nodeIface) {
			return false
		}
		pn, ok1 := pv.Interface().(ast.Node)
		nn, ok2 := nv.Interface().(ast.Node)
		if !ok1 || !ok2 {
			return false
		}
		return p.matchNode(pn, nn, caps, info)
	case reflect.Slice:
		return p.matchSlice(pv, nv, caps, info)
	case reflect.Struct:
		if pv.Type() != nv.Type() {
			return false
		}
		return p.matchStruct(pv, nv, caps, info)
	default:
		return pv.Interface() == nv.Interface()
	}
}

func (p *Pattern) matchSlice(pv, nv reflect.Value, caps map[string][]ast.Node, info *types.Info) bool {
	if !sliceIsNodes(pv.Type()) {
		if pv.Len() != nv.Len() {
			return false
		}
		for i := 0; i < pv.Len(); i++ {
			if pv.Index(i).Interface() != nv.Index(i).Interface() {
				return false
			}
		}
		return true
	}
	pats := valueNodes(pv)
	if p.hasVariadic(pats) {
		return p.matchList(pats, valueNodes(nv), caps, info)
	}
	if pv.Len() != nv.Len() {
		return false
	}
	for i := 0; i < pv.Len(); i++ {
		if !p.matchValue(pv.Index(i), nv.Index(i), caps, info) {
			return false
		}
	}
	return true
}

func (p *Pattern) hasVariadic(nodes []ast.Node) bool {
	for _, n := range nodes {
		if m, ok := p.listVariadic(n); ok && m.many {
			return true
		}
	}
	return false
}

func (p *Pattern) matchList(pats, nodes []ast.Node, caps map[string][]ast.Node, info *types.Info) bool {
	return p.matchListFrom(pats, nodes, 0, 0, caps, info)
}

func (p *Pattern) matchListFrom(pats, nodes []ast.Node, i, j int, caps map[string][]ast.Node, info *types.Info) bool {
	if i == len(pats) {
		return j == len(nodes)
	}
	if m, ok := p.listVariadic(pats[i]); ok && m.many {
		need := minFixed(p, pats[i+1:])
		maxTake := len(nodes) - j - need
		if maxTake < 0 {
			return false
		}
		for take := 0; take <= maxTake; take++ {
			saved := snapshot(caps)
			if bindMany(m, nodes[j:j+take], caps) && p.matchListFrom(pats, nodes, i+1, j+take, caps, info) {
				return true
			}
			restore(caps, saved)
		}
		return false
	}
	if j >= len(nodes) {
		return false
	}
	saved := snapshot(caps)
	if p.matchNode(pats[i], nodes[j], caps, info) && p.matchListFrom(pats, nodes, i+1, j+1, caps, info) {
		return true
	}
	restore(caps, saved)
	return false
}

func minFixed(p *Pattern, pats []ast.Node) int {
	n := 0
	for _, pat := range pats {
		if m, ok := p.listVariadic(pat); !ok || !m.many {
			n++
		}
	}
	return n
}

func valueNodes(v reflect.Value) []ast.Node {
	out := make([]ast.Node, v.Len())
	for i := 0; i < v.Len(); i++ {
		elem := v.Index(i)
		if (elem.Kind() == reflect.Interface || elem.Kind() == reflect.Pointer) && !elem.IsNil() {
			if n, ok := elem.Interface().(ast.Node); ok {
				out[i] = n
			}
		}
	}
	return out
}

func (p *Pattern) oneMarker(n ast.Node) (meta, bool) {
	if m, ok := p.callMarker(n); ok && !m.many {
		return m, true
	}
	if id, ok := n.(*ast.Ident); ok {
		if m, ok := p.identMarker(id); ok && !m.many {
			return m, true
		}
	}
	return meta{}, false
}

func (p *Pattern) listVariadic(n ast.Node) (meta, bool) {
	if m, ok := p.callMarker(n); ok && m.many {
		return m, true
	}
	es, ok := n.(*ast.ExprStmt)
	if !ok {
		return meta{}, false
	}
	m, ok := p.callMarker(es.X)
	if !ok || !m.many {
		return meta{}, false
	}
	return m, true
}

func (p *Pattern) callMarker(n ast.Node) (meta, bool) {
	return callMarkerNode(n, p.vars)
}

func (p *Pattern) identMarker(id *ast.Ident) (meta, bool) {
	m, ok := p.vars[id.Name]
	if !ok || m.call {
		return meta{}, false
	}
	return m, true
}

func (p *Pattern) isIdentMarker(id *ast.Ident) bool {
	_, ok := p.identMarker(id)
	return ok
}

func bindOne(m meta, node ast.Node, caps map[string][]ast.Node) bool {
	if node == nil || m.wild {
		return node != nil && m.wild
	}
	if prev, ok := caps[m.name]; ok {
		return len(prev) == 1 && equalNode(prev[0], node)
	}
	caps[m.name] = []ast.Node{node}
	return true
}

func bindMany(m meta, nodes []ast.Node, caps map[string][]ast.Node) bool {
	if m.wild {
		return true
	}
	if prev, ok := caps[m.name]; ok {
		if len(prev) != len(nodes) {
			return false
		}
		for i := range prev {
			if !equalNode(prev[i], nodes[i]) {
				return false
			}
		}
		return true
	}
	caps[m.name] = append([]ast.Node{}, nodes...)
	return true
}

func snapshot(caps map[string][]ast.Node) map[string][]ast.Node {
	out := make(map[string][]ast.Node, len(caps))
	for k, v := range caps {
		out[k] = append([]ast.Node(nil), v...)
	}
	return out
}

func restore(dst, src map[string][]ast.Node) {
	for k := range dst {
		delete(dst, k)
	}
	for k, v := range src {
		dst[k] = v
	}
}

func skipField(sf reflect.StructField) bool {
	switch sf.Name {
	case "Obj", "Scope", "Doc", "Comment", "Comments":
		return true
	}
	return sf.Type == posType && sf.Name != "Ellipsis"
}

func equalNode(a, b ast.Node) bool {
	a = unwrapParen(a)
	b = unwrapParen(b)
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if a == b {
		return true
	}
	if reflect.TypeOf(a) != reflect.TypeOf(b) {
		return false
	}
	return equalStruct(reflect.ValueOf(a).Elem(), reflect.ValueOf(b).Elem())
}

func samePosPresence(a, b reflect.Value) bool {
	ap, aok := a.Interface().(token.Pos)
	bp, bok := b.Interface().(token.Pos)
	return aok && bok && ap.IsValid() == bp.IsValid()
}

func equalStruct(av, bv reflect.Value) bool {
	t := av.Type()
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if !sf.IsExported() || skipField(sf) {
			continue
		}
		af, bf := av.Field(i), bv.Field(i)
		if sf.Type == posType {
			// Only CallExpr.Ellipsis reaches here. Its presence is part of the call.
			if !samePosPresence(af, bf) {
				return false
			}
			continue
		}
		if !equalValue(af, bf) {
			return false
		}
	}
	return true
}

func equalValue(av, bv reflect.Value) bool {
	if !av.IsValid() || !bv.IsValid() {
		return av.IsValid() == bv.IsValid()
	}
	switch av.Kind() {
	case reflect.Interface, reflect.Pointer:
		if av.IsNil() || bv.IsNil() {
			return av.IsNil() && bv.IsNil()
		}
		if av.Type() != bv.Type() || !av.Type().Implements(nodeIface) {
			return false
		}
		an, ok1 := av.Interface().(ast.Node)
		bn, ok2 := bv.Interface().(ast.Node)
		if !ok1 || !ok2 {
			return false
		}
		return equalNode(an, bn)
	case reflect.Slice:
		if av.Len() != bv.Len() {
			return false
		}
		for i := 0; i < av.Len(); i++ {
			if !equalValue(av.Index(i), bv.Index(i)) {
				return false
			}
		}
		return true
	case reflect.Struct:
		if av.Type() != bv.Type() {
			return false
		}
		return equalStruct(av, bv)
	default:
		return av.Interface() == bv.Interface()
	}
}
