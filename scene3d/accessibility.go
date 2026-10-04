package scene3d

import "github.com/qizhanchan/qui"

// Accessibility hooks for the 3D viewport. The agent layer treats
// Viewport as an opaque leaf — there's no DOM for the rendered
// scene, just an identifier so an agent can click on / focus the
// viewport for camera control.

func (v *Viewport) Role() string { return qui.RoleViewport3d }
