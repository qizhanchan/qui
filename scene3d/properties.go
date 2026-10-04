package scene3d

import (
	"fmt"
	"strconv"

	"github.com/qizhanchan/qui"
	"github.com/qizhanchan/qui/widgets"
)

// Vec3Field edits a (X, Y, Z) triple via three side-by-side text fields.
type Vec3Field struct {
	Title string
	Get   func() Vec3
	Set   func(Vec3)
}

func (f Vec3Field) Label() string { return f.Title }

func (f Vec3Field) Build(onChange func()) qui.Widget {
	cur := f.Get()
	field := func(initial float32, apply func(float32)) *widgets.Input {
		tf := widgets.NewInput("")
		tf.Text = fmt.Sprintf("%g", initial)
		tf.OnSubmit = func(text string) {
			v, err := strconv.ParseFloat(text, 32)
			if err != nil {
				tf.SetText(fmt.Sprintf("%g", initial))
				return
			}
			apply(float32(v))
			if onChange != nil {
				onChange()
			}
		}
		tf.SetFlex(1)
		return tf
	}
	x := field(cur.X, func(v float32) { c := f.Get(); c.X = v; f.Set(c) })
	y := field(cur.Y, func(v float32) { c := f.Get(); c.Y = v; f.Set(c) })
	z := field(cur.Z, func(v float32) { c := f.Get(); c.Z = v; f.Set(c) })
	return qui.NewContainer(qui.FlexLayout{Direction: qui.Horizontal, Gap: 4}, x, y, z)
}
