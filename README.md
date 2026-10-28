var NoWallClock = &goxlint.Rule{
	Meta: goxlint.Meta{
		Name:       "no-wall-clock",
		Category:   goxlint.Restriction,
		NeedsTypes: true,
		Messages:   map[string]string{"now": "`time.Now` reads the wall clock. Tests cannot control it."},
	},
	Create: func(ctx *goxlint.Context) goxlint.Visitor {
		if ctx.Pkg.Name() == "clock" {
			return nil
		}
		return goxlint.Visit(goxlint.On(func(call *ast.CallExpr) {
			if ctx.IsPkgFunc(call, "time", "Now") {
				ctx.Report(goxlint.Diagnostic{Node: call, MessageID: "now", Help: "Take a clock.Clock and call its Now method."})
			}
		}))
	},
}
```

Your linter is a Go program that lists its plugins:

```go
func main() { cli.Main(goxlint.Plugin{Name: "acme", Rules: []*goxlint.Rule{NoWallClock}}) }
```

```text
  × acme(no-wall-clock): `time.Now` reads the wall clock. Tests cannot control it.
   ╭─[billing/invoice.go:

Fixed:
