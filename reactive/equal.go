package reactive

import (
	"reflect"
	"unsafe"
)

// valuesEqual is the equality used for signal values, hook dependencies,
// and context values. Function values intentionally follow Go's safe
// reflect.DeepEqual semantics: non-nil functions are never equal. Component
// memoization is separate and requires an explicit comparator
// (MemoComponent), so callback closures can never accidentally keep stale
// render-local state through a default bailout.
func valuesEqual(a, b any) bool {
	if a == nil || b == nil {
		return a == b
	}
	va, vb := reflect.ValueOf(a), reflect.ValueOf(b)
	if va.Type() != vb.Type() {
		return false
	}
	return deepValueEqual(va, vb, make(map[visitPair]bool))
}

// visitPair guards against cycles, mirroring reflect.DeepEqual's visited
// set (pointer pair + type).
type visitPair struct {
	a, b unsafe.Pointer
	typ  reflect.Type
}

func deepValueEqual(a, b reflect.Value, visited map[visitPair]bool) bool {
	if !a.IsValid() || !b.IsValid() {
		return a.IsValid() == b.IsValid()
	}
	if a.Type() != b.Type() {
		return false
	}

	// Cycle detection for reference kinds that can recurse.
	switch a.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice:
		if !a.IsNil() && !b.IsNil() {
			ap := unsafe.Pointer(a.Pointer())
			bp := unsafe.Pointer(b.Pointer())
			if uintptr(ap) > uintptr(bp) {
				ap, bp = bp, ap
			}
			pair := visitPair{a: ap, b: bp, typ: a.Type()}
			if visited[pair] {
				return true
			}
			visited[pair] = true
		}
	}

	switch a.Kind() {
	case reflect.Func:
		// Go function values do not expose closure identity. A code pointer
		// identifies the function body, not captured state.
		if a.IsNil() || b.IsNil() {
			return a.IsNil() == b.IsNil()
		}
		return false
	case reflect.Pointer:
		if a.Pointer() == b.Pointer() {
			return true
		}
		if a.IsNil() || b.IsNil() {
			return false
		}
		return deepValueEqual(a.Elem(), b.Elem(), visited)
	case reflect.Interface:
		if a.IsNil() || b.IsNil() {
			return a.IsNil() == b.IsNil()
		}
		return deepValueEqual(a.Elem(), b.Elem(), visited)
	case reflect.Struct:
		for i := 0; i < a.NumField(); i++ {
			if !deepValueEqual(a.Field(i), b.Field(i), visited) {
				return false
			}
		}
		return true
	case reflect.Slice:
		if a.IsNil() != b.IsNil() {
			return false
		}
		if a.Len() != b.Len() {
			return false
		}
		if a.Len() > 0 && a.Pointer() == b.Pointer() {
			return true
		}
		for i := 0; i < a.Len(); i++ {
			if !deepValueEqual(a.Index(i), b.Index(i), visited) {
				return false
			}
		}
		return true
	case reflect.Array:
		for i := 0; i < a.Len(); i++ {
			if !deepValueEqual(a.Index(i), b.Index(i), visited) {
				return false
			}
		}
		return true
	case reflect.Map:
		if a.IsNil() != b.IsNil() {
			return false
		}
		if a.Len() != b.Len() {
			return false
		}
		if a.Len() > 0 && a.Pointer() == b.Pointer() {
			return true
		}
		iter := a.MapRange()
		for iter.Next() {
			bv := b.MapIndex(iter.Key())
			if !bv.IsValid() || !deepValueEqual(iter.Value(), bv, visited) {
				return false
			}
		}
		return true
	case reflect.Chan, reflect.UnsafePointer:
		return a.Pointer() == b.Pointer()
	// Scalar kinds use the typed accessors — reflect.Value.Interface()
	// panics on values read from unexported struct fields, and props
	// structs routinely have unexported fields.
	case reflect.Bool:
		return a.Bool() == b.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return a.Int() == b.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return a.Uint() == b.Uint()
	case reflect.Float32, reflect.Float64:
		return a.Float() == b.Float()
	case reflect.Complex64, reflect.Complex128:
		return a.Complex() == b.Complex()
	case reflect.String:
		return a.String() == b.String()
	default:
		// Unreachable for well-formed values; be conservative.
		return false
	}
}
