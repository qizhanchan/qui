//go:build darwin && cgo && qui_glfw

package glfw

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework QuartzCore

#import <Cocoa/Cocoa.h>
#import <QuartzCore/QuartzCore.h>

// quiGLFWMetalLayer makes the window's content view host a CAMetalLayer
// (once) and returns it, unretained (the view keeps it).
static void* quiGLFWMetalLayer(void* nsWindow) {
    NSWindow* win = (__bridge NSWindow*)nsWindow;
    NSView* view = win.contentView;
    if (view == nil) return NULL;
    if ([view.layer isKindOfClass:[CAMetalLayer class]]) return (__bridge void*)view.layer;
    CAMetalLayer* layer = [CAMetalLayer layer];
    CGColorSpaceRef cs = CGColorSpaceCreateWithName(kCGColorSpaceSRGB);
    layer.colorspace = cs;
    CGColorSpaceRelease(cs);
    layer.contentsScale = win.backingScaleFactor;
    layer.opaque = win.isOpaque;
    [view setLayer:layer];
    [view setWantsLayer:YES];
    return (__bridge void*)layer;
}
*/
import "C"

import "github.com/qizhanchan/qui/internal/platform/contract"

// NativeSurfaceTarget implements contract.NativeSurface: a CAMetalLayer on
// the GLFW window's content view.
func (s glfwSurface) NativeSurfaceTarget() contract.SurfaceTarget {
	ns := glfwNativeWindow(s.win.handle)
	if ns == nil {
		return contract.SurfaceTarget{}
	}
	return contract.SurfaceTarget{MetalLayer: C.quiGLFWMetalLayer(ns)}
}
