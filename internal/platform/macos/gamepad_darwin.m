// Game controllers through GameController.framework (MFi, Xbox,
// PlayStation and Switch Pro pads on macOS 11+), polled once per frame.
#import <GameController/GameController.h>
#include "gamepad_darwin.h"

static float qv(GCControllerButtonInput *b) { return b ? b.value : 0; }
static int qp(GCControllerButtonInput *b) { return b && b.isPressed; }

int quiGamepads(QuiGamepad *out, int max) {
    @autoreleasepool {
        int n = 0;
        for (GCController *c in [GCController controllers]) {
            if (n >= max) break;
            GCExtendedGamepad *g = c.extendedGamepad;
            if (!g) continue;
            QuiGamepad *p = &out[n];
            memset(p, 0, sizeof *p);
            p->id = (int)(((uintptr_t)(__bridge void *)c) >> 4 & 0x7fffffff);
            const char *name = c.vendorName.UTF8String;
            if (name) strlcpy(p->name, name, sizeof p->name);
            p->axes[0] = g.leftThumbstick.xAxis.value;
            p->axes[1] = g.leftThumbstick.yAxis.value;
            p->axes[2] = g.rightThumbstick.xAxis.value;
            p->axes[3] = g.rightThumbstick.yAxis.value;
            p->axes[4] = qv(g.leftTrigger);
            p->axes[5] = qv(g.rightTrigger);
            int bits = 0;
            int i = 0;
            #define B(x) bits |= (qp(x) << i); i++;
            B(g.buttonA) B(g.buttonB) B(g.buttonX) B(g.buttonY)
            B(g.leftShoulder) B(g.rightShoulder)
            B(g.dpad.up) B(g.dpad.down) B(g.dpad.left) B(g.dpad.right)
            B(g.buttonMenu) B(g.buttonOptions)
            B(g.leftThumbstickButton) B(g.rightThumbstickButton)
            #undef B
            p->buttons = bits;
            n++;
        }
        return n;
    }
}
