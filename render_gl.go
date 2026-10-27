//go:build cgo && !js && !qui_nogl

package qui

import "github.com/qizhanchan/qui/internal/render"

// CompileShader compiles a single GL shader stage and returns its ID.
// Returns an error on compile failure with the info log inlined. Must
// be called on a goroutine with the target GL context current.
func CompileShader(source string, shaderType uint32) (uint32, error) {
	return render.CompileShader(source, shaderType)
}
