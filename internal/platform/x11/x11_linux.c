//go:build linux && cgo

// Xlib side of the X11 backend. Everything here runs on the UI thread
// except quiX11Wake, which only writes to a pipe.
//
// Events are flattened into quiX11Event and handed to Go one at a time
// (quiX11Next), so the Go side owns all dispatch and no Go callbacks are
// exported. Selection requests (other clients asking for our clipboard)
// are answered here without involving Go.

#include "x11_linux.h"

#include <X11/Xatom.h>
#include <X11/Xresource.h>
#include <X11/cursorfont.h>
#include <X11/keysym.h>
#include <errno.h>
#include <fcntl.h>
#include <locale.h>
#include <poll.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>

static Display* dpy;
static int screen;
static Window root;
static Visual* visual;
static int depth;
static int wakePipe[2] = {-1, -1};
static XIM xim;
static Atom aWMProtocols, aWMDelete, aClipboard, aTargets, aUTF8, aText, aSelProp,
    aNetWMName, aNetWMState, aNetFullscreen, aNetMaxH, aNetMaxV, aNetWMStateHidden, aWMState;
static char* clipboardText; // what we serve while we own CLIPBOARD
static int bgrOrder = 1;    // visual stores pixels as B,G,R,X in memory

typedef struct {
    Window win;
    XIC xic;
    XImage* image;
    char* pixels;
    int fbW, fbH;
    int hidden; // cursor hidden
} quiWin;

#define MAX_WINS 64
static quiWin wins[MAX_WINS];

static quiWin* lookup(Window w) {
    for (int i = 0; i < MAX_WINS; i++)
        if (wins[i].win == w) return &wins[i];
    return NULL;
}

int quiX11Open(void) {
    setlocale(LC_CTYPE, "");
    XInitThreads();
    dpy = XOpenDisplay(NULL);
    if (!dpy) return 0;
    screen = DefaultScreen(dpy);
    root = RootWindow(dpy, screen);
    visual = DefaultVisual(dpy, screen);
    depth = DefaultDepth(dpy, screen);
    bgrOrder = visual->red_mask == 0xff0000;
    if (pipe(wakePipe) == 0) {
        fcntl(wakePipe[0], F_SETFL, O_NONBLOCK);
        fcntl(wakePipe[1], F_SETFL, O_NONBLOCK);
    }
    XSetLocaleModifiers("");
    xim = XOpenIM(dpy, NULL, NULL, NULL);
    aWMProtocols = XInternAtom(dpy, "WM_PROTOCOLS", False);
    aWMDelete = XInternAtom(dpy, "WM_DELETE_WINDOW", False);
    aClipboard = XInternAtom(dpy, "CLIPBOARD", False);
    aTargets = XInternAtom(dpy, "TARGETS", False);
    aUTF8 = XInternAtom(dpy, "UTF8_STRING", False);
    aText = XInternAtom(dpy, "TEXT", False);
    aSelProp = XInternAtom(dpy, "QUI_SELECTION", False);
    aNetWMName = XInternAtom(dpy, "_NET_WM_NAME", False);
    aNetWMState = XInternAtom(dpy, "_NET_WM_STATE", False);
    aNetFullscreen = XInternAtom(dpy, "_NET_WM_STATE_FULLSCREEN", False);
    aNetMaxH = XInternAtom(dpy, "_NET_WM_STATE_MAXIMIZED_HORZ", False);
    aNetMaxV = XInternAtom(dpy, "_NET_WM_STATE_MAXIMIZED_VERT", False);
    aNetWMStateHidden = XInternAtom(dpy, "_NET_WM_STATE_HIDDEN", False);
    aWMState = XInternAtom(dpy, "WM_STATE", False);
    return 1;
}

// quiX11DPI reads Xft.dpi, which desktop environments set to express the
// user's scale (96 = 1x); 0 when unset.
double quiX11DPI(void) {
    char* rms = XResourceManagerString(dpy);
    if (!rms) return 0;
    XrmInitialize();
    XrmDatabase db = XrmGetStringDatabase(rms);
    if (!db) return 0;
    double dpi = 0;
    char* type = NULL;
    XrmValue value;
    if (XrmGetResource(db, "Xft.dpi", "Xft.Dpi", &type, &value) && value.addr) dpi = atof(value.addr);
    XrmDestroyDatabase(db);
    return dpi;
}

void quiX11ScreenSize(int* w, int* h) {
    *w = DisplayWidth(dpy, screen);
    *h = DisplayHeight(dpy, screen);
}

unsigned long quiX11CreateWindow(int w, int h, const char* title) {
    int slot = -1;
    for (int i = 0; i < MAX_WINS; i++)
        if (wins[i].win == 0) { slot = i; break; }
    if (slot < 0) return 0;
    XSetWindowAttributes attrs;
    memset(&attrs, 0, sizeof attrs);
    attrs.background_pixel = WhitePixel(dpy, screen);
    attrs.event_mask = KeyPressMask | KeyReleaseMask | ButtonPressMask | ButtonReleaseMask |
                       PointerMotionMask | StructureNotifyMask | ExposureMask | FocusChangeMask |
                       PropertyChangeMask;
    Window win = XCreateWindow(dpy, root, 0, 0, w, h, 0, depth, InputOutput, visual,
                               CWBackPixel | CWEventMask, &attrs);
    if (!win) return 0;
    XSetWMProtocols(dpy, win, &aWMDelete, 1);
    quiX11SetTitle(win, title);
    quiWin* q = &wins[slot];
    memset(q, 0, sizeof *q);
    q->win = win;
    if (xim) {
        q->xic = XCreateIC(xim, XNInputStyle, XIMPreeditNothing | XIMStatusNothing, XNClientWindow,
                           win, XNFocusWindow, win, NULL);
    }
    XMapWindow(dpy, win);
    XFlush(dpy);
    return win;
}

void quiX11DestroyWindow(unsigned long w) {
    quiWin* q = lookup(w);
    if (!q) return;
    if (q->xic) XDestroyIC(q->xic);
    if (q->image) XDestroyImage(q->image); // frees pixels
    XDestroyWindow(dpy, w);
    XFlush(dpy);
    memset(q, 0, sizeof *q);
}

void quiX11SetTitle(unsigned long w, const char* title) {
    XStoreName(dpy, w, title);
    XChangeProperty(dpy, w, aNetWMName, aUTF8, 8, PropModeReplace, (const unsigned char*)title,
                    strlen(title));
    XFlush(dpy);
}

void quiX11Geometry(unsigned long w, int* x, int* y, int* width, int* height) {
    Window child;
    XWindowAttributes a;
    XGetWindowAttributes(dpy, w, &a);
    XTranslateCoordinates(dpy, w, root, 0, 0, x, y, &child);
    *width = a.width;
    *height = a.height;
}

void quiX11Move(unsigned long w, int x, int y) { XMoveWindow(dpy, w, x, y); XFlush(dpy); }
void quiX11Resize(unsigned long w, int width, int height) {
    XResizeWindow(dpy, w, width > 0 ? width : 1, height > 0 ? height : 1);
    XFlush(dpy);
}

void quiX11SizeLimits(unsigned long w, int minW, int minH, int maxW, int maxH) {
    XSizeHints* h = XAllocSizeHints();
    if (minW > 0 || minH > 0) {
        h->flags |= PMinSize;
        h->min_width = minW > 0 ? minW : 1;
        h->min_height = minH > 0 ? minH : 1;
    }
    if (maxW > 0 || maxH > 0) {
        h->flags |= PMaxSize;
        h->max_width = maxW > 0 ? maxW : 65535;
        h->max_height = maxH > 0 ? maxH : 65535;
    }
    XSetWMNormalHints(dpy, w, h);
    XFree(h);
    XFlush(dpy);
}

static void netWMState(Window w, int add, Atom a, Atom b) {
    XEvent e;
    memset(&e, 0, sizeof e);
    e.xclient.type = ClientMessage;
    e.xclient.window = w;
    e.xclient.message_type = aNetWMState;
    e.xclient.format = 32;
    e.xclient.data.l[0] = add ? 1 : 0;
    e.xclient.data.l[1] = a;
    e.xclient.data.l[2] = b;
    e.xclient.data.l[3] = 1;
    XSendEvent(dpy, root, False, SubstructureRedirectMask | SubstructureNotifyMask, &e);
    XFlush(dpy);
}

void quiX11Iconify(unsigned long w) { XIconifyWindow(dpy, w, screen); XFlush(dpy); }
void quiX11Maximize(unsigned long w, int on) { netWMState(w, on, aNetMaxH, aNetMaxV); }
void quiX11Fullscreen(unsigned long w, int on) { netWMState(w, on, aNetFullscreen, 0); }
void quiX11Focus(unsigned long w) {
    XRaiseWindow(dpy, w);
    XSetInputFocus(dpy, w, RevertToParent, CurrentTime);
    XFlush(dpy);
}
void quiX11SetVisible(unsigned long w, int visible) {
    if (visible) XMapRaised(dpy, w);
    else XUnmapWindow(dpy, w);
    XFlush(dpy);
}

int quiX11IsViewable(unsigned long w) {
    XWindowAttributes a;
    XGetWindowAttributes(dpy, w, &a);
    return a.map_state == IsViewable;
}

// quiX11IsIconified reads ICCCM WM_STATE (IconicState = 3).
int quiX11IsIconified(unsigned long w) {
    Atom type;
    int format;
    unsigned long n, after;
    unsigned char* data = NULL;
    int iconic = 0;
    if (XGetWindowProperty(dpy, w, aWMState, 0, 2, False, aWMState, &type, &format, &n, &after,
                           &data) == Success && data) {
        if (n > 0 && format == 32) iconic = ((long*)data)[0] == 3;
        XFree(data);
    }
    return iconic;
}

void quiX11Pointer(unsigned long w, int* x, int* y, unsigned int* mask) {
    Window r, c;
    int rx, ry;
    XQueryPointer(dpy, w, &r, &c, &rx, &ry, x, y, mask);
}

void quiX11SetCursor(unsigned long w, int shape) {
    static Cursor cache[16];
    static Cursor blank;
    Cursor cur;
    if (shape < 0) {
        if (!blank) {
            char zero = 0;
            Pixmap pm = XCreateBitmapFromData(dpy, w, &zero, 1, 1);
            XColor black;
            memset(&black, 0, sizeof black);
            blank = XCreatePixmapCursor(dpy, pm, pm, &black, &black, 0, 0);
            XFreePixmap(dpy, pm);
        }
        cur = blank;
    } else {
        if (shape >= 16) shape = 0;
        static const unsigned int fonts[] = {XC_left_ptr, XC_xterm, XC_crosshair, XC_hand2,
                                             XC_sb_h_double_arrow, XC_sb_v_double_arrow,
                                             XC_bottom_right_corner, XC_bottom_left_corner};
        unsigned int f = shape < (int)(sizeof fonts / sizeof fonts[0]) ? fonts[shape] : XC_left_ptr;
        if (!cache[shape]) cache[shape] = XCreateFontCursor(dpy, f);
        cur = cache[shape];
    }
    XDefineCursor(dpy, w, cur);
    XFlush(dpy);
}

// --- presentation ----------------------------------------------------

// quiX11Pixels returns the window's frame buffer, (re)allocating it at
// the given physical size. Pixels are stored in the visual's byte order;
// the Go side converts from RGBA while copying the damaged rows.
char* quiX11Pixels(unsigned long w, int width, int height, int* bgr) {
    quiWin* q = lookup(w);
    if (!q) return NULL;
    *bgr = bgrOrder;
    if (q->image && q->fbW == width && q->fbH == height) return q->pixels;
    if (q->image) XDestroyImage(q->image);
    q->pixels = calloc((size_t)width * height, 4);
    q->image = XCreateImage(dpy, visual, depth, ZPixmap, 0, q->pixels, width, height, 32, width * 4);
    if (!q->image) {
        free(q->pixels);
        q->pixels = NULL;
        return NULL;
    }
    q->fbW = width;
    q->fbH = height;
    return q->pixels;
}

void quiX11Put(unsigned long w, int x, int y, int width, int height) {
    quiWin* q = lookup(w);
    if (!q || !q->image) return;
    GC gc = DefaultGC(dpy, screen);
    XPutImage(dpy, w, gc, q->image, x, y, x, y, width, height);
    XFlush(dpy);
}

// --- clipboard -------------------------------------------------------

void quiX11SetClipboard(unsigned long w, const char* text) {
    free(clipboardText);
    clipboardText = strdup(text);
    XSetSelectionOwner(dpy, aClipboard, w, CurrentTime);
    XFlush(dpy);
}

static void answerSelection(XSelectionRequestEvent* r) {
    XSelectionEvent reply;
    memset(&reply, 0, sizeof reply);
    reply.type = SelectionNotify;
    reply.requestor = r->requestor;
    reply.selection = r->selection;
    reply.target = r->target;
    reply.time = r->time;
    reply.property = None;
    if (clipboardText && r->property != None) {
        if (r->target == aTargets) {
            Atom targets[] = {aTargets, aUTF8, XA_STRING, aText};
            XChangeProperty(dpy, r->requestor, r->property, XA_ATOM, 32, PropModeReplace,
                            (unsigned char*)targets, 4);
            reply.property = r->property;
        } else if (r->target == aUTF8 || r->target == XA_STRING || r->target == aText) {
            XChangeProperty(dpy, r->requestor, r->property, r->target, 8, PropModeReplace,
                            (unsigned char*)clipboardText, strlen(clipboardText));
            reply.property = r->property;
        }
    }
    XSendEvent(dpy, r->requestor, False, 0, (XEvent*)&reply);
    XFlush(dpy);
}

// quiX11GetClipboard returns a malloc'd copy of the CLIPBOARD text (the
// caller frees it), waiting up to 250ms for the owner to answer.
char* quiX11GetClipboard(unsigned long w) {
    Window owner = XGetSelectionOwner(dpy, aClipboard);
    if (owner == None) return NULL;
    if (owner == w || lookup(owner)) return clipboardText ? strdup(clipboardText) : NULL;
    XConvertSelection(dpy, aClipboard, aUTF8, aSelProp, w, CurrentTime);
    XFlush(dpy);
    struct timespec start, now;
    clock_gettime(CLOCK_MONOTONIC, &start);
    for (;;) {
        XEvent e;
        if (XCheckTypedWindowEvent(dpy, w, SelectionNotify, &e)) {
            if (e.xselection.property == None) return NULL;
            Atom type;
            int format;
            unsigned long n, after;
            unsigned char* data = NULL;
            char* out = NULL;
            if (XGetWindowProperty(dpy, w, aSelProp, 0, 1 << 24, True, AnyPropertyType, &type,
                                   &format, &n, &after, &data) == Success && data) {
                out = strndup((char*)data, n);
                XFree(data);
            }
            return out;
        }
        clock_gettime(CLOCK_MONOTONIC, &now);
        long ms = (now.tv_sec - start.tv_sec) * 1000 + (now.tv_nsec - start.tv_nsec) / 1000000;
        if (ms > 250) return NULL;
        struct pollfd p = {ConnectionNumber(dpy), POLLIN, 0};
        poll(&p, 1, 10);
    }
}

// --- event loop ------------------------------------------------------

void quiX11Wake(void) {
    if (wakePipe[1] >= 0) {
        char b = 1;
        ssize_t r = write(wakePipe[1], &b, 1);
        (void)r;
    }
}

// quiX11Wait blocks until the X connection or the wake pipe is readable,
// or timeoutMs elapses (-1 = forever).
void quiX11Wait(int timeoutMs) {
    XFlush(dpy);
    if (XPending(dpy)) return;
    struct pollfd p[2] = {{ConnectionNumber(dpy), POLLIN, 0}, {wakePipe[0], POLLIN, 0}};
    int n = poll(p, wakePipe[0] >= 0 ? 2 : 1, timeoutMs);
    if (n > 0 && (p[1].revents & POLLIN)) {
        char buf[64];
        while (read(wakePipe[0], buf, sizeof buf) > 0) {
        }
    }
}

static int modsOf(unsigned int state) {
    int m = 0;
    if (state & ShiftMask) m |= 1;
    if (state & ControlMask) m |= 2;
    if (state & Mod1Mask) m |= 4;
    if (state & Mod4Mask) m |= 8;
    return m;
}

unsigned int quiX11CurrentState(unsigned long w) {
    int x, y;
    unsigned int mask = 0;
    quiX11Pointer(w, &x, &y, &mask);
    return mask;
}

int quiX11ModsOf(unsigned int state) { return modsOf(state); }

// quiX11Next fills out with the next event worth dispatching and returns
// 1, or returns 0 when the queue is empty.
int quiX11Next(quiX11Event* out) {
    while (XPending(dpy)) {
        XEvent e;
        XNextEvent(dpy, &e);
        if (XFilterEvent(&e, None)) continue; // consumed by the input method
        memset(out, 0, sizeof *out);
        out->window = e.xany.window;
        switch (e.type) {
        case MotionNotify:
            out->type = QUI_X11_MOTION;
            out->x = e.xmotion.x;
            out->y = e.xmotion.y;
            out->mods = modsOf(e.xmotion.state);
            return 1;
        case ButtonPress:
        case ButtonRelease:
            out->type = e.type == ButtonPress ? QUI_X11_BUTTON_DOWN : QUI_X11_BUTTON_UP;
            out->button = e.xbutton.button;
            out->x = e.xbutton.x;
            out->y = e.xbutton.y;
            out->mods = modsOf(e.xbutton.state);
            return 1;
        case KeyPress:
        case KeyRelease: {
            out->type = e.type == KeyPress ? QUI_X11_KEY_DOWN : QUI_X11_KEY_UP;
            out->keycode = e.xkey.keycode;
            out->keysym = XLookupKeysym(&e.xkey, 0);
            out->mods = modsOf(e.xkey.state);
            out->capsLock = (e.xkey.state & LockMask) != 0;
            if (e.type == KeyRelease && XEventsQueued(dpy, QueuedAfterReading)) {
                // Auto-repeat arrives as release+press with the same time.
                XEvent next;
                XPeekEvent(dpy, &next);
                if (next.type == KeyPress && next.xkey.time == e.xkey.time &&
                    next.xkey.keycode == e.xkey.keycode) {
                    continue; // drop the synthetic release; the press is a repeat
                }
            }
            if (e.type == KeyPress) {
                quiWin* q = lookup(e.xkey.window);
                KeySym sym;
                Status st = 0;
                int n;
                if (q && q->xic) {
                    n = Xutf8LookupString(q->xic, &e.xkey, out->text, sizeof out->text - 1, &sym, &st);
                    if (st == XBufferOverflow) n = 0;
                } else {
                    n = XLookupString(&e.xkey, out->text, sizeof out->text - 1, &sym, NULL);
                }
                out->text[n > 0 ? n : 0] = 0;
            }
            return 1;
        }
        case FocusIn:
        case FocusOut: {
            if (e.xfocus.mode == NotifyGrab || e.xfocus.mode == NotifyUngrab) continue;
            quiWin* q = lookup(e.xfocus.window);
            if (q && q->xic) {
                if (e.type == FocusIn) XSetICFocus(q->xic);
                else XUnsetICFocus(q->xic);
            }
            out->type = e.type == FocusIn ? QUI_X11_FOCUS_IN : QUI_X11_FOCUS_OUT;
            return 1;
        }
        case ConfigureNotify:
            out->type = QUI_X11_CONFIGURE;
            out->width = e.xconfigure.width;
            out->height = e.xconfigure.height;
            return 1;
        case Expose:
            if (e.xexpose.count != 0) continue;
            out->type = QUI_X11_EXPOSE;
            return 1;
        case ClientMessage:
            if (e.xclient.message_type == aWMProtocols && (Atom)e.xclient.data.l[0] == aWMDelete) {
                out->type = QUI_X11_CLOSE;
                return 1;
            }
            continue;
        case SelectionRequest:
            answerSelection(&e.xselectionrequest);
            continue;
        case SelectionClear:
            if (e.xselectionclear.selection == aClipboard) {
                free(clipboardText);
                clipboardText = NULL;
            }
            continue;
        case MapNotify:
        case UnmapNotify:
            out->type = QUI_X11_STATE;
            return 1;
        default:
            continue;
        }
    }
    return 0;
}
