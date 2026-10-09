//go:build windows

package build

// alive reads every holder as alive, so a lock goes stale by its age only.
func alive(int) bool { return true }
