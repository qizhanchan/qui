//go:build darwin && cgo
// ^ advisory; the _darwin.m suffix is what gates it.

#import <Cocoa/Cocoa.h>
#import <ApplicationServices/ApplicationServices.h>
#include <pthread.h>
#include <stdatomic.h>
#include <stdint.h>
#include <time.h>
#include <unistd.h>

#include "_cgo_export.h"

// System shortcut capture (qui.Window.SetSystemKeyCapture). A session
// CGEventTap on a dedicated thread with its own run loop: the tap sits in
// the path of EVERY keystroke in the login session, so it must never wait
// on our UI thread. The callback decides from atomics alone, hands
// swallowed keys to Go (which posts them to the UI thread in order), and
// returns at once.
//
// The tap is enabled only while a window's capture is active (key window +
// raw mode + enabled); otherwise it is disabled outright, costing nothing.

static CFMachPortRef gTap = NULL;
static _Atomic int gTapActive = 0;
static _Atomic uintptr_t gTapHandle = 0;
static _Atomic int gEscCode = -1;   // kVK of the raw escape chord, -1 none
static _Atomic int gEscMods = 0;    // its 4-bit modifier mask
static int gEscPassing = -1;        // tap thread only

// Stall guard: keys posted to Go vs keys the UI thread has processed.
static _Atomic uint64_t gPosted = 0;
static _Atomic uint64_t gProcessed = 0;
static _Atomic uint64_t gPendingSinceNs = 0;
static const uint64_t kStallNs = 1000000000ull; // 1 s

static uint64_t nowNs(void) { return clock_gettime_nsec_np(CLOCK_UPTIME_RAW); }

static int tapModsOf(CGEventFlags flags) {
    int mods = 0;
    if (flags & kCGEventFlagMaskShift)     mods |= 1;
    if (flags & kCGEventFlagMaskControl)   mods |= 2;
    if (flags & kCGEventFlagMaskAlternate) mods |= 4;
    if (flags & kCGEventFlagMaskCommand)   mods |= 8;
    return mods;
}

static CGEventRef quiTapCallback(CGEventTapProxy proxy, CGEventType type,
                                 CGEventRef event, void* info) {
    if (type == kCGEventTapDisabledByTimeout || type == kCGEventTapDisabledByUserInput) {
        if (atomic_load(&gTapActive) && gTap) CGEventTapEnable(gTap, true);
        return event;
    }
    if (!atomic_load(&gTapActive)) return event;
    uintptr_t handle = atomic_load(&gTapHandle);
    if (handle == 0) return event;
    // Only keys on their way to THIS process: if another app is frontmost
    // (focus moved and qui has not caught up yet), never touch its input.
    if ((pid_t)CGEventGetIntegerValueField(event, kCGEventTargetUnixProcessID) != getpid()) {
        return event;
    }

    // A hung UI thread must not hold the keyboard hostage.
    uint64_t since = atomic_load(&gPendingSinceNs);
    if (since != 0 && nowNs() - since > kStallNs) return event;

    int code = (int)CGEventGetIntegerValueField(event, kCGKeyboardEventKeycode);
    CGEventFlags flags = CGEventGetFlags(event);
    int mods = tapModsOf(flags);

    // The escape chord goes through to AppKit untouched (and so does the
    // release of its key).
    if (type == kCGEventKeyDown && code == atomic_load(&gEscCode) && mods == atomic_load(&gEscMods)) {
        gEscPassing = code;
        return event;
    }
    if (type == kCGEventKeyUp && code == gEscPassing) {
        gEscPassing = -1;
        return event;
    }

    int kind = type == kCGEventKeyDown ? 0 : type == kCGEventKeyUp ? 1 : 2;
    int repeat = (int)CGEventGetIntegerValueField(event, kCGKeyboardEventAutorepeat);
    char utf8[64] = {0};
    if (type == kCGEventKeyDown) {
        UniChar buf[16];
        UniCharCount n = 0;
        CGEventKeyboardGetUnicodeString(event, 16, &n, buf);
        if (n > 0) {
            CFStringRef s = CFStringCreateWithCharacters(kCFAllocatorDefault, buf, (CFIndex)n);
            if (s) {
                CFStringGetCString(s, utf8, sizeof utf8, kCFStringEncodingUTF8);
                CFRelease(s);
            }
        }
    }
    uint64_t seq = atomic_fetch_add(&gPosted, 1) + 1;
    if (seq - 1 == atomic_load(&gProcessed)) atomic_store(&gPendingSinceNs, nowNs());
    quiTapKeyEvent(handle, kind, code, repeat, (uint64_t)flags, mods, utf8, seq);
    return NULL; // swallowed: the system never sees it
}

static void* quiTapThread(void* arg) {
    CFRunLoopSourceRef src = CFMachPortCreateRunLoopSource(kCFAllocatorDefault, gTap, 0);
    CFRunLoopAddSource(CFRunLoopGetCurrent(), src, kCFRunLoopCommonModes);
    CFRelease(src);
    CFRunLoopRun();
    return NULL;
}

// quiTapPrepare creates the tap (disabled) and its thread once. Returns 1
// on success, 0 when the Accessibility permission is missing.
int quiTapPrepare(void) {
    if (gTap) return 1;
    if (!AXIsProcessTrusted()) return 0;
    CGEventMask mask = CGEventMaskBit(kCGEventKeyDown) | CGEventMaskBit(kCGEventKeyUp) |
                       CGEventMaskBit(kCGEventFlagsChanged);
    CFMachPortRef tap = CGEventTapCreate(kCGSessionEventTap, kCGHeadInsertEventTap,
                                         kCGEventTapOptionDefault, mask, quiTapCallback, NULL);
    if (tap == NULL) return 0;
    CGEventTapEnable(tap, false);
    gTap = tap;
    pthread_t th;
    if (pthread_create(&th, NULL, quiTapThread, NULL) != 0) {
        CFMachPortInvalidate(tap);
        CFRelease(tap);
        gTap = NULL;
        return 0;
    }
    pthread_detach(th);
    return 1;
}

void quiTapSetActive(uintptr_t handle, int active, int escCode, int escMods) {
    atomic_store(&gEscCode, escCode);
    atomic_store(&gEscMods, escMods);
    atomic_store(&gTapHandle, active ? handle : 0);
    // Reset the stall guard on every transition.
    atomic_store(&gProcessed, atomic_load(&gPosted));
    atomic_store(&gPendingSinceNs, 0);
    atomic_store(&gTapActive, active);
    if (gTap) CGEventTapEnable(gTap, active ? true : false);
}

void quiTapAck(uint64_t seq) {
    atomic_store(&gProcessed, seq);
    if (seq >= atomic_load(&gPosted)) atomic_store(&gPendingSinceNs, 0);
}
