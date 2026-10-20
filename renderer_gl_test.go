//go:build cgo && !js && !qui_nogl

package qui

import (
	"image"
	"testing"
)

func TestTextureUploadIsScopedToDamage(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 200, 100))
	r := &GLRenderer{}

	// Nothing mirrored yet: the first frame uploads everything.
	r.damage = image.Rect(10, 10, 20, 20)
	if got := r.textureUploadRect(img); got != img.Rect {
		t.Fatalf("first frame uploads %v, want the whole image", got)
	}

	r.texSrc = img
	if got, want := r.textureUploadRect(img), image.Rect(9, 9, 21, 21); got != want {
		t.Fatalf("damaged frame uploads %v, want %v (damage + 1px guard)", got, want)
	}
	r.damage = image.Rect(195, 95, 260, 140)
	if got, want := r.textureUploadRect(img), image.Rect(194, 94, 200, 100); got != want {
		t.Fatalf("damage past the edge uploads %v, want %v", got, want)
	}
	// Unknown damage (a renderer driven outside Window.Step) is everything.
	r.damage = image.Rectangle{}
	if got := r.textureUploadRect(img); got != img.Rect {
		t.Fatalf("unknown damage uploads %v, want the whole image", got)
	}
	// A different image (resize reallocated it) is not what the texture holds.
	r.damage = image.Rect(10, 10, 20, 20)
	if got := r.textureUploadRect(image.NewRGBA(img.Rect)); got != img.Rect {
		t.Fatalf("new image uploads %v, want the whole image", got)
	}
}

func TestTextureUploadRectDamageNone(t *testing.T) {
	r := &GLRenderer{}
	img := image.NewRGBA(image.Rect(0, 0, 200, 100))
	r.texSrc = img
	r.damageNone = true
	if got := r.textureUploadRect(img); !got.Empty() {
		t.Fatalf("GL-only frame must upload nothing, got %v", got)
	}
	// A different image (resize) still uploads everything.
	if got := r.textureUploadRect(image.NewRGBA(img.Rect)); got != img.Rect {
		t.Fatalf("new image must upload fully, got %v", got)
	}
}
