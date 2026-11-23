package antislop

import (
	"testing"

	"github.com/egoisutolabs/goxlint/goxlinttest"
)

func TestNoDetachedContext(t *testing.T) {
	goxlinttest.Run(t, NoDetachedContext, goxlinttest.Cases{
		Valid: []goxlinttest.Case{
			{Name: "background at root", Code: `
import "context"

func Run() { serve(context.Background()) }

func serve(ctx context.Context) {}`},
			{Name: "passes ctx", Code: `
import "context"

func f(ctx context.Context) { g(ctx) }

func g(ctx context.Context) {}`},
			{Name: "todo in test", Filename: "x_test.go", Code: `
import "context"

var ctx = context.TODO()`},
			{Name: "unnamed ctx param", Code: `
import "context"

func f(_ context.Context) context.Context { return context.Background() }`},
		},
		Invalid: []goxlinttest.Case{
			{Name: "todo", Code: `
import "context"

func f() { g(context.TODO()) }

func g(ctx context.Context) {}`, Errors: []goxlinttest.Error{{MessageID: "todo", Line: 4}}},
			{
				Name: "background with ctx",
				Code: `
import "context"

func f(ctx context.Context) { g(context.Background()) }

func g(ctx context.Context) {}`,
				Errors: []goxlinttest.Error{{Message: "`context.Background()` ignores `ctx`, losing its cancellation and deadline."}},
				Output: `
import "context"

func f(ctx context.Context) { g(ctx) }

func g(ctx context.Context) {}`,
			},
			{
				Name: "closure inherits ctx",
				Code: `
import "context"

func f(reqCtx context.Context) {
	go func() { g(context.Background()) }()
}

func g(ctx context.Context) {}`,
				Errors: []goxlinttest.Error{{MessageID: "background"}},
				Output: `
import "context"

func f(reqCtx context.Context) {
	go func() { g(reqCtx) }()
}

func g(ctx context.Context) {}`,
			},
		},
	})
}

func TestNoSleepInTests(t *testing.T) {
	goxlinttest.Run(t, NoSleepInTests, goxlinttest.Cases{
		Valid: []goxlinttest.Case{
			{Name: "not a test file", Code: `
import "time"

func backoff() { time.Sleep(time.Second) }`},
			{Name: "synctest bubble", Filename: "x_test.go", Code: `
import (
	"testing"
	"testing/synctest"
	"time"
)

func TestX(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		time.Sleep(time.Second)
	})
}`},
			{Name: "local time value", Filename: "x_test.go", Code: `
import "testing"

type clock struct{}

func (clock) Sleep(n int) {}

func TestX(t *testing.T) {
	var time clock
	time.Sleep(1)
}`},
		},
		Invalid: []goxlinttest.Case{
			{Name: "renamed import", Filename: "x_test.go", Code: `
import (
	"testing"
	tme "time"
)

func TestX(t *testing.T) { tme.Sleep(tme.Second) }`, Errors: []goxlinttest.Error{{MessageID: "sleep"}}},
			{Name: "sleep in test", Filename: "x_test.go", Code: `
import (
	"testing"
	"time"
)

func TestX(t *testing.T) {
	go func() {}()
	time.Sleep(100 * time.Millisecond)
}`, Errors: []goxlinttest.Error{{MessageID: "sleep", Line: 9}}},
		},
	})
}

func TestNoPanicInLibrary(t *testing.T) {
	panicErr := goxlinttest.Error{MessageID: "panic"}

	goxlinttest.Run(t, NoPanicInLibrary, goxlinttest.Cases{
		Valid: []goxlinttest.Case{
			{Name: "main package", Code: `
package main

func main() { panic("boom") }`},
			{Name: "test file", Filename: "x_test.go", Code: `func helper() { panic("boom") }`},
			{Name: "Must function", Code: `
import "strconv"

func MustAtoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		panic(err)
	}
	return n
}`},
			{Name: "init", Code: `func init() { panic("bad build") }`},
			{Name: "unreachable", Code: `
func f(n int) int {
	switch n {
	case 0:
		return 1
	}
	panic("unreachable: n is validated by the caller")
}`},
		},
		Invalid: []goxlinttest.Case{
			{Name: "panic err", Code: `
import "strconv"

func Atoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		panic(err)
	}
	return n
}`, Errors: []goxlinttest.Error{{Message: "`panic` in library code crashes the caller's program.", Line: 7}}},
			{Name: "init method is not init", Code: `
type T struct{}

func (T) init() { panic("x") }`, Errors: []goxlinttest.Error{panicErr}},
		},
	})
}

func TestNoPrintInLibrary(t *testing.T) {
	goxlinttest.Run(t, NoPrintInLibrary, goxlinttest.Cases{
		Valid: []goxlinttest.Case{
			{Name: "main package", Code: `
package main

import "fmt"

func main() { fmt.Println("hi") }`},
			{Name: "writer", Code: `
import (
	"fmt"
	"io"
)

func f(w io.Writer) { fmt.Fprintln(w, "hi") }`},
			{Name: "sprintf", Code: `
import "fmt"

var s = fmt.Sprintf("%d", 1)`},
			{Name: "shadowed print", Code: `
func print(s string) {}

func f() { print("x") }`},
		},
		Invalid: []goxlinttest.Case{
			{Name: "fmt.Println", Code: `
import "fmt"

func f(n int) { fmt.Println("debug", n) }`, Errors: []goxlinttest.Error{{Message: "`fmt.Println` writes to stdout from library code."}}},
			{Name: "fmt.Printf", Code: `
import "fmt"

func f(n int) { fmt.Printf("%d\n", n) }`, Errors: []goxlinttest.Error{{MessageID: "print"}}},
			{Name: "builtin println", Code: `func f() { println("here") }`, Errors: []goxlinttest.Error{{Message: "`println` writes to stdout from library code."}}},
		},
	})
}
