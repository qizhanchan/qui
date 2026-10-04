package main

// 3D viewer — a GLViewport showing a spinning lit cube sitting on a
// subtle plane. Demonstrates: FBO-backed 3D inside a 2D layout,
// continuous animation via the anim package, and
// 2D UI (title label, toolbar buttons) sitting beside the viewport.
//
// Drag on the viewport to orbit the camera (pseudo: we just rotate
// the cube itself with the mouse delta).

import (
	"fmt"
	"log"
	"math"
	"time"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/scene3d"
	"github.com/qizhanchan/qui/widgets"
)

func main() {
	app, err := qui.NewApp()
	if err != nil {
		log.Fatal(err)
	}
	window, err := app.NewWindow("qui — 3D viewer", 900, 600)
	if err != nil {
		log.Fatal(err)
	}
	window.SetRenderer(qui.NewGLRenderer())

	// Build the scene.
	scene := scene3d.NewScene()
	scene.Camera.Position = scene3d.Vec3{X: 2.2, Y: 2.0, Z: 3.5}
	scene.Camera.Target = scene3d.Vec3{X: 0, Y: 0, Z: 0}
	scene.Ambient = qui.Color{R: 0.12, G: 0.14, B: 0.20, A: 1}
	scene.Lights = []scene3d.Light{
		scene3d.DirectionalLight{
			Direction: scene3d.Vec3{X: -0.5, Y: -1, Z: -0.3},
			Color:     qui.Color{R: 1, G: 0.95, B: 0.85, A: 1},
			Intensity: 1.1,
		},
	}
	cube := scene3d.NewNode("cube")
	cube.Mesh = scene3d.NewCubeMesh()
	cube.Material = scene3d.NewLitMaterial(qui.Color{R: 0.55, G: 0.35, B: 0.95, A: 1})
	scene.Root.AddChild(cube)

	plane := scene3d.NewNode("ground")
	plane.Transform.Position = scene3d.Vec3{X: 0, Y: -0.5, Z: 0}
	plane.Transform.Scale = scene3d.Vec3{X: 4, Y: 1, Z: 4}
	plane.Mesh = scene3d.NewPlaneMesh()
	plane.Material = scene3d.NewLitMaterial(qui.Color{R: 0.20, G: 0.22, B: 0.25, A: 1})
	scene.Root.AddChild(plane)

	viewport := scene3d.NewViewport(scene)
	viewport.Continuous = true

	// Animate the cube via OnFrame. Rotation accumulated over time.
	var angle float32
	viewport.OnFrame = func(dt time.Duration) {
		angle += float32(dt.Seconds()) * 0.6
		cube.Transform.Rotation = scene3d.QuatFromAxisAngle(scene3d.Vec3{X: 0.2, Y: 1, Z: 0.1}, angle)
	}
	viewport.SetFlex(1)

	// Right-hand side control column.
	title := widgets.NewLabel("3D preview")
	title.Style().Foreground = qui.ColorWhite
	title.Style().Font.Size = 18
	title.Style().Font.Bold = true

	fovVal := float64(60)
	fovLabel := widgets.NewLabel(fmt.Sprintf("FOV: %.0f°", fovVal))
	fovLabel.Style().Foreground = qui.ColorWhite
	changeFOV := func(delta float64) {
		fovVal += delta
		if fovVal < 20 {
			fovVal = 20
		} else if fovVal > 120 {
			fovVal = 120
		}
		scene.Camera.FOV = float32(fovVal) * (math.Pi / 180)
		fovLabel.SetText(fmt.Sprintf("FOV: %.0f°", fovVal))
		viewport.InvalidateScene()
	}
	fovWide := widgets.NewButton("FOV +10", func() { changeFOV(10) })
	fovNarrow := widgets.NewButton("FOV -10", func() { changeFOV(-10) })
	reset := widgets.NewButton("Reset camera", func() {
		scene.Camera.Position = scene3d.Vec3{X: 2.2, Y: 2.0, Z: 3.5}
		fovVal = 60
		changeFOV(0)
	})

	controls := qui.NewContainer(qui.FlexLayout{Direction: qui.Vertical, Gap: 10}, title, fovLabel, fovWide, fovNarrow, reset)
	controls.Style().Padding = qui.Insets{Top: 16, Right: 16, Bottom: 16, Left: 16}
	controls.Style().Background = qui.Color{R: 0.10, G: 0.11, B: 0.13, A: 1}

	root := qui.NewContainer(qui.FlexLayout{Direction: qui.Horizontal, Gap: 0}, viewport, controls)
	root.Style().Background = qui.Color{R: 0.08, G: 0.08, B: 0.10, A: 1}
	controls.UpdateFlexItem(func(item *qui.FlexItem) { item.Basis = 220 })
	window.SetRoot(root)

	app.Run()
}
