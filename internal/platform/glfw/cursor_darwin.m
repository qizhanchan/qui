//go:build darwin && cgo && !qui_nogl && qui_glfw

#import <Cocoa/Cocoa.h>

// quiGLFWCursorAdopt replaces the NSCursor inside a GLFWcursor with ours,
// so GLFW keeps doing all of its own cursor management (cursorUpdate:,
// focus changes, hidden mode) while showing a high-density image that
// glfwCreateCursor cannot express (it sizes images 1 pixel = 1 point).
//
// GLFW 3.3's private layout is struct _GLFWcursor { _GLFWcursor* next;
// _GLFWcursorNS { id object; } ns; }. The swap is refused unless the slot
// really holds an NSCursor, so a layout change degrades to the 1x image
// instead of corrupting memory. GLFW releases the object (non-ARC) on
// glfwDestroyCursor, which consumes the reference we hand it.
int quiGLFWCursorAdopt(void* glfwCursor, void* nsCursor) {
    if (glfwCursor == NULL || nsCursor == NULL) return 0;
    void** slots = (void**)glfwCursor;
    void* old = slots[1];
    if (old == NULL) return 0;
    id oldObj = (__bridge id)old;
    if (![oldObj isKindOfClass:[NSCursor class]]) return 0;
    CFRetain(nsCursor);
    slots[1] = nsCursor;
    CFRelease(old);
    return 1;
}
