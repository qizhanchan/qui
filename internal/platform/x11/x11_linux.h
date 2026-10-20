#ifndef QUI_X11_LINUX_H
#define QUI_X11_LINUX_H

#include <X11/Xlib.h>
#include <X11/Xutil.h>

enum {
    QUI_X11_MOTION = 1,
    QUI_X11_BUTTON_DOWN,
    QUI_X11_BUTTON_UP,
    QUI_X11_KEY_DOWN,
    QUI_X11_KEY_UP,
    QUI_X11_FOCUS_IN,
    QUI_X11_FOCUS_OUT,
    QUI_X11_CONFIGURE,
    QUI_X11_EXPOSE,
    QUI_X11_CLOSE,
    QUI_X11_STATE,
};

typedef struct {
    int type;
    unsigned long window;
    int x, y;
    int button;
    int mods;
    int capsLock;
    unsigned int keycode;
    unsigned long keysym;
    int width, height;
    char text[64];
} quiX11Event;

int quiX11Open(void);
double quiX11DPI(void);
void quiX11ScreenSize(int* w, int* h);
unsigned long quiX11CreateWindow(int w, int h, const char* title);
void quiX11DestroyWindow(unsigned long w);
void quiX11SetTitle(unsigned long w, const char* title);
void quiX11Geometry(unsigned long w, int* x, int* y, int* width, int* height);
void quiX11Move(unsigned long w, int x, int y);
void quiX11Resize(unsigned long w, int width, int height);
void quiX11SizeLimits(unsigned long w, int minW, int minH, int maxW, int maxH);
void quiX11Iconify(unsigned long w);
void quiX11Maximize(unsigned long w, int on);
void quiX11Fullscreen(unsigned long w, int on);
void quiX11Focus(unsigned long w);
void quiX11SetVisible(unsigned long w, int visible);
int quiX11IsViewable(unsigned long w);
int quiX11IsIconified(unsigned long w);
void quiX11Pointer(unsigned long w, int* x, int* y, unsigned int* mask);
void quiX11SetCursor(unsigned long w, int shape);
char* quiX11Pixels(unsigned long w, int width, int height, int* bgr);
void quiX11Put(unsigned long w, int x, int y, int width, int height);
void quiX11SetClipboard(unsigned long w, const char* text);
char* quiX11GetClipboard(unsigned long w);
void quiX11Wake(void);
void quiX11Wait(int timeoutMs);
unsigned int quiX11CurrentState(unsigned long w);
int quiX11ModsOf(unsigned int state);
int quiX11Next(quiX11Event* out);

#endif
