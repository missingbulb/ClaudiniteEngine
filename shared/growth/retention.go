package growth

import "math"

// DefaultRetentionDays is the window a repo that says nothing gets.
const DefaultRetentionDays = 10.0

// ResolveRetentionDays turns a declared retention_days into the window
// the prune runs on, nil for "prune nothing": absent (or null) is the
// default; a finite number is the repo's own call, a positive one the
// window and any other the capture-only opt-out; anything else is
// unknown, and an unknown value is never a licence to delete.
func ResolveRetentionDays(declared any, present bool) *float64 {
	if !present || declared == nil {
		d := DefaultRetentionDays
		return &d
	}
	v, ok := Number(declared)
	if !ok || v <= 0 {
		return nil
	}
	return &v
}

// Number is a declared value as a finite number.
func Number(v any) (float64, bool) {
	var f float64
	switch x := v.(type) {
	case float64:
		f = x
	case float32:
		f = float64(x)
	case int:
		f = float64(x)
	case int64:
		f = float64(x)
	case uint64:
		f = float64(x)
	default:
		return 0, false
	}
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return 0, false
	}
	return f, true
}
