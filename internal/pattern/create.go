package pattern

import (
	"go/types"
	"runtime"
	"strconv"
	"sync"
)

// createInfo holds the type information of the rule whose Create is running
// on this goroutine. OnMatch reads it while Create builds handlers, so package
// selectors can use import paths without a second walk.
var createInfo sync.Map // goroutine id -> *types.Info

// BindCreate records info for OnMatch calls on this goroutine.
// A nil info selects name matching. The engine clears the record when Create returns.
func BindCreate(info *types.Info) {
	createInfo.Store(goid(), info)
}

// CurrentInfo returns the type information bound for this goroutine, or nil.
func CurrentInfo() *types.Info {
	v, ok := createInfo.Load(goid())
	if !ok || v == nil {
		return nil
	}
	info, _ := v.(*types.Info)
	return info
}

// UnbindCreate drops the record for this goroutine.
func UnbindCreate() {
	createInfo.Delete(goid())
}

func goid() int64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	// The first line is "goroutine <id> [status]:".
	const prefix = "goroutine "
	if n < len(prefix) {
		return 0
	}
	i := len(prefix)
	j := i
	for j < n && buf[j] >= '0' && buf[j] <= '9' {
		j++
	}
	id, err := strconv.ParseInt(string(buf[i:j]), 10, 64)
	if err != nil {
		return 0
	}
	return id
}
