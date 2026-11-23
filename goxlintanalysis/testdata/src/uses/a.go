package uses

func f() {
	x := 1 // want "uses 2"
	_ = x
	_ = (x)
}
