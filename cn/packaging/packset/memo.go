package packset

import (
	"strconv"
	"sync"
)

// memo keeps derived answers for the rest of a process that asked for it:
// a per-call hook derives the same packs and triggers in two capabilities
// within one short run, over a tree that does not change under it.
var memo struct {
	sync.Mutex
	on     bool
	values map[string]any
}

// Memoize makes Load and Remember keep every answer until the process
// exits. Only a short-lived process over a tree it does not write calls
// it.
func Memoize() {
	memo.Lock()
	defer memo.Unlock()
	memo.on = true
	if memo.values == nil {
		memo.values = map[string]any{}
	}
}

// Forget turns memoizing off and drops what it kept, for tests.
func Forget() {
	memo.Lock()
	defer memo.Unlock()
	memo.on, memo.values = false, nil
}

// Remember returns f's value for key, computed once when the process
// memoizes and on every call otherwise.
func Remember(key string, f func() any) any {
	memo.Lock()
	if !memo.on {
		memo.Unlock()
		return f()
	}
	if v, ok := memo.values[key]; ok {
		memo.Unlock()
		return v
	}
	memo.Unlock()
	v := f()
	memo.Lock()
	memo.values[key] = v
	memo.Unlock()
	return v
}

type loaded struct {
	set Set
	err error
}

func loadKey(repo, engine string, session bool) string {
	return "packset.Load\x00" + repo + "\x00" + engine + "\x00" + strconv.FormatBool(session)
}
