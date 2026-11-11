package scene3d

import "github.com/qizhanchan/qui"

// InstancedCustomMaterial is a user WGSL material usable on an
// InstanceBatch. InstanceSource is compiled after Preamble and the
// instance preamble, so its vertex stage reads each copy's model matrix
// with mesh_model(instance_index); Source (optional) is the same shader
// for ordinary nodes, using obj.model.
type InstancedCustomMaterial struct {
	Source, InstanceSource string
	OnParams               func(scene *Scene) MaterialParams
}

func (m *InstancedCustomMaterial) WGSL() string         { return m.Source }
func (m *InstancedCustomMaterial) instanceWGSL() string { return m.InstanceSource }
func (m *InstancedCustomMaterial) Destroy()             {}
func (m *InstancedCustomMaterial) Params(s *Scene) MaterialParams {
	if m.OnParams == nil {
		return MaterialParams{Color: qui.Color{R: 1, G: 1, B: 1, A: 1}}
	}
	return m.OnParams(s)
}
