package runner

import "reflect"

// isNilLike recognizes an interface containing a typed nil implementation.
// Driver implementations are external code, so interface nil checks alone are
// not enough at the Runtime, Executor, and ResultStream trust boundaries.
func isNilLike(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
