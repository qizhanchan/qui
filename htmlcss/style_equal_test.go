package htmlcss

import (
	"reflect"
	"testing"
)

func TestStyleEqualityCoversComputedFields(t *testing.T) {
	var base ComputedStyle
	eng := &StyleEngine{}
	var nonzero func(reflect.Value) bool
	nonzero = func(v reflect.Value) bool {
		switch v.Kind() {
		case reflect.Bool:
			v.SetBool(true)
		case reflect.String:
			v.SetString("changed")
		case reflect.Int, reflect.Int8, reflect.Int32, reflect.Int64:
			v.SetInt(1)
		case reflect.Uint, reflect.Uint8, reflect.Uint32, reflect.Uint64:
			v.SetUint(1)
		case reflect.Float32, reflect.Float64:
			v.SetFloat(1)
		case reflect.Pointer:
			v.Set(reflect.New(v.Type().Elem()))
		case reflect.Slice:
			v.Set(reflect.MakeSlice(v.Type(), 1, 1))
		case reflect.Array:
			return nonzero(v.Index(0))
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				if v.Field(i).CanSet() && nonzero(v.Field(i)) {
					return true
				}
			}
			return false
		default:
			return false
		}
		return true
	}
	typ := reflect.TypeOf(base)
	if typ.NumField() != styleEqualFieldCount {
		t.Fatal("ComputedStyle changed: update style equality fields and coverage")
	}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.PkgPath != "" {
			continue
		} // private maps/ids have separate table checks
		t.Run(field.Name, func(t *testing.T) {
			changed := base
			if !nonzero(reflect.ValueOf(&changed).Elem().Field(i)) {
				return
			}
			if eng.stylesEqual(&base, &changed) {
				t.Fatalf("style equality omitted %s", field.Name)
			}
		})
	}
}

func TestStyleEqualityComparesSharedVariableTables(t *testing.T) {
	eng := &StyleEngine{}
	a := &ComputedStyle{customProps: map[string]string{"--theme": "red"}, customPropsID: new(byte)}
	b := &ComputedStyle{customProps: map[string]string{"--theme": "red"}, customPropsID: new(byte)}
	if !eng.stylesEqual(a, b) || len(eng.customEqual) != 1 {
		t.Fatal("equal theme tables were not memoized")
	}
	c := &ComputedStyle{customProps: map[string]string{"--theme": "blue"}, customPropsID: new(byte)}
	if eng.stylesEqual(a, c) {
		t.Fatal("changed CSS variable ignored")
	}
}
