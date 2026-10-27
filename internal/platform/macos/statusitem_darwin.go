//go:build darwin && cgo

package macos

/*
#include <stdint.h>
#include <stdlib.h>
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

import "unsafe"

// CreateStatusItem returns a retained NSStatusItem, or nil.
func CreateStatusItem() unsafe.Pointer { return C.quiCreateStatusItem() }

// StatusItemSetImageRGBA sets the icon from premultiplied RGBA pixels drawn
// at ptW x ptH points; a template image is tinted for the menu bar.
func StatusItemSetImageRGBA(item unsafe.Pointer, pixels []byte, pxW, pxH, ptW, ptH int, template bool) {
	if len(pixels) == 0 {
		return
	}
	C.quiStatusItemSetImageRGBA(item, (*C.uint8_t)(unsafe.Pointer(&pixels[0])),
		C.int(pxW), C.int(pxH), C.int(ptW), C.int(ptH), cBool(template))
}

// StatusItemClearImage removes the icon.
func StatusItemClearImage(item unsafe.Pointer) { C.quiStatusItemClearImage(item) }

// StatusItemSetTitle sets the text beside the icon.
func StatusItemSetTitle(item unsafe.Pointer, title string) {
	c, free := cStr(title)
	defer free()
	C.quiStatusItemSetTitle(item, c)
}

// StatusItemSetTooltip sets the hover tooltip.
func StatusItemSetTooltip(item unsafe.Pointer, tip string) {
	c, free := cStr(tip)
	defer free()
	C.quiStatusItemSetTooltip(item, c)
}

// StatusItemSetMenu attaches a menu from BeginSubmenu (consuming its
// retain); nil detaches the current one.
func StatusItemSetMenu(item, menu unsafe.Pointer) { C.quiStatusItemSetMenu(item, menu) }

// StatusItemSetClickAction routes clicks to Host.MenuAction(id); 0 clears.
func StatusItemSetClickAction(item unsafe.Pointer, id int) {
	C.quiStatusItemSetClickAction(item, C.int(id))
}

// StatusItemSetVisible shows or hides the item.
func StatusItemSetVisible(item unsafe.Pointer, visible bool) {
	C.quiStatusItemSetVisible(item, cBool(visible))
}

// StatusItemDestroy removes the item from the menu bar and releases it.
func StatusItemDestroy(item unsafe.Pointer) { C.quiStatusItemDestroy(item) }
