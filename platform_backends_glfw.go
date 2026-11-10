//go:build cgo && !js && (windows || qui_glfw)

package qui

import _ "github.com/qizhanchan/qui/internal/platform/glfw"
