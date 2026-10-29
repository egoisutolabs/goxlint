package antislop

import (
	"testing"

	"github.com/egoisutolabs/goxlint/goxlinttest"
)

func TestNoMapAsStruct(t *testing.T) {
	file := func(code string) map[string]string { return map[string]string{"p.go": code} }
	local := goxlinttest.PackageError{MessageID: "local"}
	shape := goxlinttest.PackageError{MessageID: "shape"}
	returned := goxlinttest.PackageError{MessageID: "returned"}
	asserted := goxlinttest.PackageError{MessageID: "asserted"}

	goxlinttest.RunPackage(t, NoMapAsStruct, goxlinttest.PackageCases{
		Valid: []goxlinttest.PackageCase{
			{Name: "returned", Files: file(`
func f() map[string]any {
	m := map[string]any{"name": "a", "age": 1}
	return m
}`)},
			{Name: "passed to a call", Files: file(`
func use(map[string]int) {}

func f() {
	m := map[string]int{"a": 1, "b": 2}
	use(m)
}`)},
			{Name: "ranged", Files: file(`
func f() int {
	m := map[string]int{"a": 1, "b": 2}
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}`)},
			{Name: "variable key", Files: file(`
func f(k string) int {
	m := map[string]int{"a": 1, "b": 2}
	return m[k]
}`)},
			{Name: "len", Files: file(`
func f() int {
	m := map[string]int{"a": 1, "b": 2}
	return len(m) + m["a"]
}`)},
			{Name: "one key", Files: file(`
func f() int {
	m := map[string]int{"a": 1}
	return m["a"]
}`)},
			{Name: "int keys", Files: file(`
func f() int {
	m := map[int]int{1: 1, 2: 2}
	return m[1] + m[2]
}`)},
			{Name: "non-constant literal key", Files: file(`
func f(k string) int {
	m := map[string]int{k: 1, "b": 2}
	return m["b"]
}`)},
			{Name: "one literal shape", Files: file(`
func use(map[string]int) {}

func f() { use(map[string]int{"a": 1, "b": 2, "c": 3}) }`)},
			{Name: "shapes with two keys", Files: file(`
func use(map[string]int) {}

func f() {
	use(map[string]int{"a": 1, "b": 2})
	use(map[string]int{"a": 3, "b": 4})
}`)},
			{Name: "returned literals with different keys", Files: file(`
func f(ok bool) map[string]int {
	if ok {
		return map[string]int{"a": 1, "b": 2, "c": 3}
	}
	return map[string]int{"x": 1, "y": 2, "z": 3}
}`)},
			{Name: "returned variable", Files: file(`
func f(keys []string) map[string]int {
	m := map[string]int{}
	for _, k := range keys {
		m[k] = 1
	}
	return m
}`)},
			{Name: "assertion with variable key", Files: file(`
func f(m map[string]any, k string) string { return m[k].(string) }`)},
			{Name: "assertion on typed values", Files: file(`
type value interface{ String() string }

func f(m map[string]value) value { return m["a"].(value) }`)},
			{Name: "parameter ranged over", Files: file(`
func f(m map[string]int) int {
	n := m["a"] + m["b"]
	for range m {
		n++
	}
	return n
}`)},
			{Name: "parameter receives a shared map", Files: file(`
func pick(slots map[string]int, slot string) int { return slots[slot] }

func pair(slots map[string]int) int { return slots["a"] + slots["b"] }

func dispatch(slots map[string]int, kind string) int {
	if kind == "one" {
		return pick(slots, "a")
	}
	return pair(slots)
}`)},
			{Name: "exported field", Files: file(`
type Config struct{ Opts map[string]string }

func (c Config) addr() string { return c.Opts["host"] + ":" + c.Opts["port"] }`)},
			{Name: "field passed out", Files: file(`
type api struct{ opts map[string]string }

func use(map[string]string) {}

func (a api) f() string {
	use(a.opts)
	return a.opts["host"] + a.opts["port"]
}`)},
			{Name: "test file", Files: map[string]string{"p_test.go": `
func f() int {
	m := map[string]int{"a": 1, "b": 2}
	return m["a"] + m["b"]
}`}},
		},
		Invalid: []goxlinttest.PackageCase{
			{Name: "literal with fixed reads", Files: file(`
import "fmt"

func f(name string, age int) {
	user := map[string]any{"name": name, "age": age}
	fmt.Println(user["name"], user["age"])
}`), Errors: []goxlinttest.PackageError{{
				MessageID: "local", Line: 5, Column: 2,
				Message: "`user` is a map used as a struct: it only uses the fixed keys \"name\", \"age\".",
			}}},
			{Name: "make with fixed writes", Files: file(`
func f() int {
	counts := make(map[string]int)
	counts["errors"]++
	counts["warnings"] += 2
	return counts["errors"]
}`), Errors: []goxlinttest.PackageError{local}},
			{Name: "var declaration", Files: file(`
func f() string {
	var cfg = map[string]string{"host": "localhost"}
	cfg["port"] = "80"
	return cfg["host"] + cfg["port"]
}`), Errors: []goxlinttest.PackageError{local}},
			{Name: "constant keys", Files: file(`
const keyName, keyAge = "name", "age"

func f() any {
	m := map[string]any{}
	m[keyName] = "a"
	m[keyAge] = 1
	return m[keyName]
}`), Errors: []goxlinttest.PackageError{local}},
			{Name: "used in a closure", Files: file(`
func f() func() int {
	m := map[string]int{"a": 1, "b": 2}
	return func() int { return m["a"] + m["b"] }
}`), Errors: []goxlinttest.PackageError{local}},
			{Name: "function returns a record", Files: file(`
type order struct{ id, customer string; total float64 }

func toMap(o order) map[string]any {
	return map[string]any{"id": o.id, "customer": o.customer, "total": o.total}
}`), Errors: []goxlinttest.PackageError{{MessageID: "returned", Line: 4, Column: 6}}},
			{Name: "every return has the same keys", Files: file(`
func f(ok bool) map[string]int {
	if ok {
		return map[string]int{"a": 1, "b": 2, "c": 3}
	}
	return map[string]int{"c": 0, "b": 0, "a": 0}
}`), Errors: []goxlinttest.PackageError{returned}},
			{Name: "typed read", Files: file(`
func total(rows []map[string]any) float64 {
	var t float64
	for _, r := range rows {
		t += r["total"].(float64)
	}
	return t
}`), Errors: []goxlinttest.PackageError{{
				MessageID: "asserted", Line: 5, Column: 8,
				Message: "`r[\"total\"].(float64)` reads a field from a map and asserts its type. A struct field has a type already.",
			}}},
			{Name: "comma-ok typed read", Files: file(`
func name(m map[string]any) string {
	s, _ := m["name"].(string)
	return s
}`), Errors: []goxlinttest.PackageError{asserted}},
			{Name: "parameter with fixed keys", Files: file(`
func addr(cfg map[string]string) string { return cfg["host"] + ":" + cfg["port"] }`),
				Errors: []goxlinttest.PackageError{{MessageID: "local", Line: 2, Column: 11}}},
			{Name: "parameter read in a loop over constant keys", Files: file(`
func first(problems map[string]string) string {
	for _, k := range []string{"customer", "email"} {
		if msg, ok := problems[k]; ok {
			return msg
		}
	}
	return ""
}`), Errors: []goxlinttest.PackageError{local}},
			{Name: "unexported field across methods", Files: map[string]string{
				"a.go": `
type api struct{ opts map[string]string }

func newAPI() *api { return &api{opts: map[string]string{"currency": "USD"}} }

func (a *api) price() string { return a.opts["currency"] }`,
				"b.go": `
func logf(opts map[string]string) bool { return opts["format"] == "json" && opts["level"] != "" }

func (a *api) log() bool { return logf(a.opts) }`,
			}, Errors: []goxlinttest.PackageError{
				{Filename: "a.go", MessageID: "field", Line: 2, Message: "Field `opts` is a map used as a struct: it is only read with the fixed keys \"currency\", \"format\", \"level\"."},
				{Filename: "b.go", MessageID: "local", Line: 2},
			}},
			{Name: "repeated shape across files", Files: map[string]string{
				"a.go": `
func use(map[string]any) {}

func a() { use(map[string]any{"id": 1, "name": "a", "email": "e"}) }`,
				"b.go": `
func b() { use(map[string]any{"email": "f", "id": 2, "name": "b"}) }`,
			}, Errors: []goxlinttest.PackageError{
				{Filename: "a.go", MessageID: "shape", Line: 4, Related: []goxlinttest.RelatedError{{Filename: "b.go", Line: 2, Message: "same keys"}}},
				{Filename: "b.go", MessageID: "shape", Line: 2, Related: []goxlinttest.RelatedError{{Filename: "a.go", Line: 4, Message: "same keys"}}},
			}},
			{Name: "repeated shape in one function", Files: file(`
func use(map[string]int) {}

func f() {
	use(map[string]int{"a": 1, "b": 2, "c": 3})
	use(map[string]int{"c": 4, "b": 5, "a": 6})
}`), Errors: []goxlinttest.PackageError{shape, shape}},
		},
	})
}

func TestMapAsStructHelp(t *testing.T) {
	for _, tc := range []struct{ key, want string }{
		{"name", "Name"},
		{"user_id", "UserID"},
		{"created-at", "CreatedAt"},
		{"homepage url", "HomepageURL"},
		{"2fa", ""},
		{"", ""},
	} {
		if got := fieldName(tc.key); got != tc.want {
			t.Errorf("fieldName(%q) = %q, want %q", tc.key, got, tc.want)
		}
	}
}
