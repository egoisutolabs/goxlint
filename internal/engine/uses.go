package engine

import (
	"cmp"
	"go/ast"
	"go/types"
	"slices"
	"sync"

	"github.com/egoisutolabs/goxlint"
)

// useIndex is one file's uses. The first lookup builds it. After that it is
// read-only. Rules of one file share the index, and once publishes the map to
// callers that run at the same time.
type useIndex struct {
	once   sync.Once
	file   *ast.File
	info   *types.Info
	byObj  map[types.Object][]goxlint.Use
	builds int
}

func newUseIndex(file *ast.File, info *types.Info) *useIndex {
	return &useIndex{file: file, info: info}
}

func (u *useIndex) lookup(obj types.Object) []goxlint.Use {
	if u == nil || u.info == nil || obj == nil {
		return nil
	}
	u.once.Do(func() {
		u.builds++
		u.build()
	})
	return u.byObj[obj]
}

func (u *useIndex) build() {
	u.byObj = map[types.Object][]goxlint.Use{}
	if u.file == nil {
		return
	}
	var stack []ast.Node
	ast.Inspect(u.file, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		if id, ok := n.(*ast.Ident); ok {
			if obj := u.info.Uses[id]; obj != nil {
				expr, parent := useRef(id, stack)
				u.byObj[obj] = append(u.byObj[obj], goxlint.Use{Ident: id, Expr: expr, Parent: parent})
			}
		}
		stack = append(stack, n)
		return true
	})
	for obj, sites := range u.byObj {
		slices.SortStableFunc(sites, func(a, b goxlint.Use) int {
			return cmp.Compare(a.Ident.Pos(), b.Ident.Pos())
		})
		u.byObj[obj] = sites
	}
}

// useRef returns the expression that refers to id and the node that contains
// it. A selector's Sel refers to the selector expression. Parentheses around
// that expression are not the parent.
func useRef(id *ast.Ident, stack []ast.Node) (expr ast.Expr, parent ast.Node) {
	expr = id
	i := len(stack) - 1
	if i >= 0 {
		if sel, ok := stack[i].(*ast.SelectorExpr); ok && sel.Sel == id {
			expr = sel
			i--
		}
	}
	for i >= 0 {
		if _, ok := stack[i].(*ast.ParenExpr); ok {
			i--
			continue
		}
		return expr, stack[i]
	}
	return expr, nil
}
