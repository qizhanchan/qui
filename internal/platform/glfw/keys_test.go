//go:build cgo && !js && !qui_nogl && (windows || qui_glfw)

package glfw

import (
	"testing"

	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/qizhanchan/qui/internal/platform/contract"
)

func TestGLFWMapsModifierAndExtendedKeys(t *testing.T) {
	cases := map[glfw.Key]contract.Key{
		glfw.KeyLeftShift: contract.KeyLeftShift, glfw.KeyRightShift: contract.KeyRightShift,
		glfw.KeyLeftControl: contract.KeyLeftControl, glfw.KeyRightControl: contract.KeyRightControl,
		glfw.KeyLeftAlt: contract.KeyLeftAlt, glfw.KeyRightAlt: contract.KeyRightAlt,
		glfw.KeyLeftSuper: contract.KeyLeftSuper, glfw.KeyRightSuper: contract.KeyRightSuper,
		glfw.KeyCapsLock: contract.KeyCapsLock, glfw.KeyGraveAccent: contract.KeyGrave,
		glfw.KeyF13: contract.KeyF13, glfw.KeyF20: contract.KeyF20,
		glfw.KeyKP0: contract.KeyKP0, glfw.KeyKP9: contract.KeyKP9, glfw.KeyKPAdd: contract.KeyKPAdd,
	}
	for in, want := range cases {
		if got := mapKey(in); got != want {
			t.Errorf("mapKey(%v) = %v, want %v", in, got, want)
		}
	}
}
