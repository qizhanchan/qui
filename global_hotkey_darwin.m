//go:build darwin && cgo

// Global hotkeys through Carbon's RegisterEventHotKey: still the only
// public API that grabs a chord system-wide without an event tap (and so
// without the Accessibility permission). The handler runs on the main
// thread while the app pumps events, which qui's loop always does.

#import <Carbon/Carbon.h>
#include <stdint.h>

#include "_cgo_export.h"

static EventHandlerRef gQuiHotKeyHandler;

static OSStatus quiHotKeyEvent(EventHandlerCallRef next, EventRef event, void* data) {
    EventHotKeyID hk;
    if (GetEventParameter(event, kEventParamDirectObject, typeEventHotKeyID,
                          NULL, sizeof(hk), NULL, &hk) != noErr) {
        return eventNotHandledErr;
    }
    if (hk.signature != 'quih') return eventNotHandledErr;
    quiHotKeyFired((int)hk.id);
    return noErr;
}

// mods is qui's 4-bit mask: 1 shift, 2 control, 4 option, 8 command.
void* quiHotKeyRegister(int id, int code, int mods) {
    if (gQuiHotKeyHandler == NULL) {
        EventTypeSpec spec = {kEventClassKeyboard, kEventHotKeyPressed};
        if (InstallApplicationEventHandler(&quiHotKeyEvent, 1, &spec, NULL,
                                           &gQuiHotKeyHandler) != noErr) {
            return NULL;
        }
    }
    UInt32 cm = 0;
    if (mods & 1) cm |= shiftKey;
    if (mods & 2) cm |= controlKey;
    if (mods & 4) cm |= optionKey;
    if (mods & 8) cm |= cmdKey;
    EventHotKeyID hid = {'quih', (UInt32)id};
    EventHotKeyRef ref = NULL;
    if (RegisterEventHotKey((UInt32)code, cm, hid, GetApplicationEventTarget(),
                            0, &ref) != noErr) {
        return NULL;
    }
    return ref;
}

void quiHotKeyUnregister(void* ref) {
    if (ref != NULL) UnregisterEventHotKey((EventHotKeyRef)ref);
}
