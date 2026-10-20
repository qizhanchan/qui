//go:build cgo && !js && !qui_nogl && (windows || qui_glfw)

package qui

import _ "github.com/qizhanchan/qui/internal/platform/glfw"
