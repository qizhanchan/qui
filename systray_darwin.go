//go:build darwin && cgo

package qui

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa

#include <stdlib.h>
#include <stdint.h>

void* quiCreateStatusItem(void);
void quiStatusItemSetImageRGBA(void* item, const uint8_t* pixels, int pxW, int pxH, int ptW, int ptH, int isTemplate);
void quiStatusItemClearImage(void* item);
void quiStatusItemSetTitle(void* item, const char* title);
void quiStatusItemSetTooltip(void* item, const char* tip);
void quiStatusItemSetMenu(void* item, void* nsMenu);
void quiStatusItemSetClickAction(void* item, int actionID);
void quiStatusItemSetVisible(void* item, int visible);
void quiStatusItemDestroy(void* item);
*/
import "C"

import (
	"errors"
	"image"
	"image/draw"
	"unsafe"
)

// StatusItem is a macOS NSStatusItem handle backed by a retained
// Objective-C pointer. Construct via App.NewStatusItem; release via
// Destroy. All mutating methods must run on the Go main goroutine
// (the same one that called NewApp).
type StatusItem struct {
	handle  unsafe.Pointer // NSStatusItem*
	scope   actionScope
	app     *App
	onClick func()
	hasMenu bool
}

// NewStatusItem creates a macOS menu-bar status item. The returned
// StatusItem must be released with Destroy when no longer needed;
// App.Run also destroys all registered status items on shutdown
// (SIGINT, normal close, etc.) so well-behaved apps see the icon
// disappear in all teardown paths.
func (a *App) NewStatusItem(opts StatusItemOptions) (*StatusItem, error) {
	if a == nil {
		return nil, errors.New("qui: nil App")
	}
	handle := C.quiCreateStatusItem()
	if handle == nil {
		return nil, errors.New("qui: failed to create NSStatusItem")
	}
	s := &StatusItem{
		handle:  unsafe.Pointer(handle),
		scope:   allocStatusItemScope(),
		app:     a,
		onClick: opts.OnClick,
	}
	if opts.Title != "" {
		s.applyTitle(opts.Title)
	}
	if opts.Tooltip != "" {
		s.applyTooltip(opts.Tooltip)
	}
	if opts.Image != nil {
		s.applyImage(opts.Image)
	} else if opts.Icon != nil {
		s.applyIcon(opts.Icon)
	}
	if opts.Menu != nil {
		s.applyMenu(opts.Menu)
	}
	// Wire OnClick last — Menu attachment overrides click in macOS,
	// so wiring both is harmless but the menu path takes priority.
	s.applyClickAction()

	a.statusItems = append(a.statusItems, s)
	return s, nil
}

// SetIcon replaces the tray icon with a template image rasterized
// from a VectorSource (e.g. *svg.Document). macOS auto-tints it for
// the current menu bar appearance.
func (s *StatusItem) SetIcon(src VectorSource) {
	if s == nil || s.handle == nil || src == nil {
		return
	}
	s.applyIcon(src)
}

// SetImage replaces the tray icon with a pre-rasterized RGBA bitmap.
// Used as-is (NOT a template) so colors are preserved.
func (s *StatusItem) SetImage(img image.Image) {
	if s == nil || s.handle == nil || img == nil {
		return
	}
	s.applyImage(img)
}

// ClearImage removes the icon, leaving only the title text (if any).
func (s *StatusItem) ClearImage() {
	if s == nil || s.handle == nil {
		return
	}
	C.quiStatusItemClearImage(s.handle)
}

// SetTitle replaces the text shown next to (or instead of) the icon.
func (s *StatusItem) SetTitle(title string) {
	if s == nil || s.handle == nil {
		return
	}
	s.applyTitle(title)
}

// SetTooltip replaces the hover tooltip.
func (s *StatusItem) SetTooltip(tooltip string) {
	if s == nil || s.handle == nil {
		return
	}
	s.applyTooltip(tooltip)
}

// SetMenu replaces the popup menu. Passing nil removes the menu —
// the OnClick callback (if any) becomes active again instead.
func (s *StatusItem) SetMenu(menu *NativeMenu) {
	if s == nil || s.handle == nil {
		return
	}
	// Drop all previously registered action callbacks for this scope
	// (old menu items + old click handler), then rebuild from scratch.
	resetActionsForScope(s.scope)
	C.quiStatusItemSetMenu(s.handle, nil)
	s.hasMenu = false
	if menu != nil {
		s.applyMenu(menu)
	}
	// Re-arm the click action after the reset so OnClick survives a
	// SetMenu(nil) call.
	s.applyClickAction()
}

// SetVisible toggles whether the icon appears in the menu bar.
// Hidden items remain registered and reappear when toggled back.
func (s *StatusItem) SetVisible(visible bool) {
	if s == nil || s.handle == nil {
		return
	}
	v := C.int(0)
	if visible {
		v = 1
	}
	C.quiStatusItemSetVisible(s.handle, v)
}

// Destroy removes the icon from the menu bar and releases all
// associated callbacks. Safe to call multiple times.
func (s *StatusItem) Destroy() {
	if s == nil || s.handle == nil {
		return
	}
	C.quiStatusItemDestroy(s.handle)
	s.handle = nil
	resetActionsForScope(s.scope)
	if s.app != nil {
		for i, existing := range s.app.statusItems {
			if existing == s {
				s.app.statusItems = append(s.app.statusItems[:i], s.app.statusItems[i+1:]...)
				break
			}
		}
		s.app = nil
	}
}

func (s *StatusItem) applyTitle(title string) {
	cTitle, free := cStr(title)
	defer free()
	C.quiStatusItemSetTitle(s.handle, cTitle)
}

func (s *StatusItem) applyTooltip(tip string) {
	cTip, free := cStr(tip)
	defer free()
	C.quiStatusItemSetTooltip(s.handle, cTip)
}

func (s *StatusItem) applyIcon(src VectorSource) {
	// 22 pt logical × 2 for retina = 44 px. Single rep at @2x; NSImage
	// downsamples to 22 px on non-retina displays.
	const pt = 22
	const px = pt * 2
	img := src.Rasterize(px, px, Color{R: 255, G: 255, B: 255, A: 255})
	if img == nil {
		return
	}
	pixels, pxW, pxH := rgbaBytes(img)
	if pixels == nil {
		return
	}
	C.quiStatusItemSetImageRGBA(s.handle,
		(*C.uint8_t)(unsafe.Pointer(&pixels[0])),
		C.int(pxW), C.int(pxH),
		C.int(pt), C.int(pt),
		C.int(1))
}

func (s *StatusItem) applyImage(img image.Image) {
	pixels, pxW, pxH := rgbaBytes(img)
	if pixels == nil {
		return
	}
	// Assume caller provided a 2x image; logical point size is half
	// the pixel dimensions. Callers needing different ratios should
	// resize before handing the image in.
	ptW := pxW / 2
	ptH := pxH / 2
	if ptW < 1 {
		ptW = pxW
	}
	if ptH < 1 {
		ptH = pxH
	}
	C.quiStatusItemSetImageRGBA(s.handle,
		(*C.uint8_t)(unsafe.Pointer(&pixels[0])),
		C.int(pxW), C.int(pxH),
		C.int(ptW), C.int(ptH),
		C.int(0))
}

func (s *StatusItem) applyMenu(menu *NativeMenu) {
	menuPtr := buildSubmenu(menu, s.scope)
	C.quiStatusItemSetMenu(s.handle, menuPtr)
	s.hasMenu = true
}

func (s *StatusItem) applyClickAction() {
	if s.onClick == nil {
		C.quiStatusItemSetClickAction(s.handle, 0)
		return
	}
	id := registerAction(s.onClick, s.scope)
	C.quiStatusItemSetClickAction(s.handle, C.int(id))
}

// rgbaBytes returns a contiguous premultiplied RGBA byte slice for
// src plus its pixel dimensions. If src is already *image.RGBA with
// a tight stride the underlying buffer is returned directly; otherwise
// a fresh image.RGBA is allocated and src is drawn into it.
func rgbaBytes(src image.Image) ([]byte, int, int) {
	if src == nil {
		return nil, 0, 0
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return nil, 0, 0
	}
	if r, ok := src.(*image.RGBA); ok && r.Stride == w*4 && b.Min.X == 0 && b.Min.Y == 0 {
		return r.Pix, w, h
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)
	return dst.Pix, w, h
}
