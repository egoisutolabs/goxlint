// Package antislop rejects low-evidence and low-signal Go patterns. It is a Go
// port of the ideas in https://github.com/dmmulroy/anti-slop.
package antislop

import (
	"go/types"

	"github.com/egoisutolabs/goxlint"
)

// Plugin registers every anti-slop rule.
var Plugin = goxlint.Plugin{
	Name: "anti-slop",
	Rules: []*goxlint.Rule{
		NoAnyMapValues,
		NoAnyParameters,
		NoAnyReturns,
		NoBlankError,
		NoDetachedContext,
		NoErrorStringMatching,
		NoGenericPackageNames,
		NoLogAndReturnError,
		NoMapAsStruct,
		NoPanicInLibrary,
		NoPlaceholderComments,
		NoPrintInLibrary,
		NoRedundantErrorPrefix,
		NoReflectDynamicAccess,
		NoRestatingComments,
		NoSleepInTests,
		NoSwallowedError,
		RequireSafetyCommentForTypeAssertion,
	},
}

// isEmptyInterface reports whether t is any, interface{}, or a named type or
// alias with an empty interface underneath. Type parameters are not, even when
// their constraint is any.
func isEmptyInterface(t types.Type) bool {
	if t == nil {
		return false
	}
	t = types.Unalias(t)
	if _, ok := t.(*types.TypeParam); ok {
		return false
	}
	iface, ok := t.Underlying().(*types.Interface)
	return ok && iface.Empty()
}
