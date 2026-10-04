//go:build darwin && cgo && webview_cef

#include <Cocoa/Cocoa.h>
#include <crt_externs.h>
#include <objc/runtime.h>
#include <stdint.h>
#include <stdio.h>
#include <mutex>
#include <string>
#include <unordered_map>

#include "include/cef_app.h"
#include "include/cef_application_mac.h"
#include "include/cef_browser.h"
#include "include/cef_client.h"
#include "include/cef_command_line.h"
#include "include/cef_devtools_message_observer.h"
#include "include/cef_display_handler.h"
#include "include/cef_life_span_handler.h"
#include "include/cef_load_handler.h"
#include "include/cef_process_message.h"
#include "include/cef_registration.h"
#include "include/cef_render_handler.h"
#include "include/cef_task.h"
#include "include/cef_values.h"
#include "include/wrapper/cef_helpers.h"
#include "include/wrapper/cef_library_loader.h"

// Go-side //export callbacks (defined in backend_darwin.go). All run on
// whatever thread CefDoMessageLoopWork is called from — for the qui
// architecture that's the OS main thread (same goroutine as the UI
// loop), so the Go side does not need cross-thread dispatch.
extern "C" {
void quiWebviewOnPaint(uintptr_t handle, const void* buf, int width, int height);
void quiWebviewOnAfterCreated(uintptr_t handle);
void quiWebviewOnBeforeClose(uintptr_t handle);
void quiWebviewOnLoadStart(uintptr_t handle, const char* url);
void quiWebviewOnLoadEnd(uintptr_t handle, const char* url, int httpStatus);
void quiWebviewOnLoadError(uintptr_t handle, const char* url, int errCode, const char* errText);
void quiWebviewOnAddressChange(uintptr_t handle, const char* url);
void quiWebviewOnTitleChange(uintptr_t handle, const char* title);
void quiWebviewOnConsoleMessage(uintptr_t handle, int level, const char* msg);
void quiWebviewOnCursorChange(uintptr_t handle, int cursorType);
// Phase C: JS↔Go bridge callbacks.
void quiWebviewOnDevToolsResult(uintptr_t handle, int message_id, int success, const void* result, int result_size);
void quiWebviewOnHandlerCall(uintptr_t handle, const char* name, int message_id, const void* payload, int payload_size);
}

// CEF macOS requires NSApplication to provide CefAppProtocol semantics.
// GLFW creates NSApplication before we can subclass it, so we retrofit
// protocol behavior onto the existing NSApplication instance via a
// category + method swizzle.
static const void* kQuiHandlingSendEventKey = &kQuiHandlingSendEventKey;

@interface NSApplication (QuiCefAppProtocol) <CefAppProtocol>
- (void)qui_cef_sendEvent:(NSEvent*)event;
@end

@implementation NSApplication (QuiCefAppProtocol)
- (BOOL)isHandlingSendEvent {
  NSNumber* n = objc_getAssociatedObject(self, kQuiHandlingSendEventKey);
  return n ? [n boolValue] : NO;
}

- (void)setHandlingSendEvent:(BOOL)handlingSendEvent {
  objc_setAssociatedObject(
      self, kQuiHandlingSendEventKey, @(handlingSendEvent),
      OBJC_ASSOCIATION_RETAIN_NONATOMIC);
}

- (void)qui_cef_sendEvent:(NSEvent*)event {
  CefScopedSendingEvent sendingEventScoper;
  // After swizzle, calling qui_cef_sendEvent actually invokes the
  // original -sendEvent: implementation.
  [self qui_cef_sendEvent:event];
}
@end

namespace {

// Holds the library loader for the lifetime of the host process. The
// loader uses RAII to dlclose the CEF framework on destruction, so we
// keep it as a process-level singleton.
CefScopedLibraryLoader* g_library_loader = nullptr;
bool g_initialized = false;
bool g_send_event_swizzled = false;

void EnsureCefAppProtocolOnNSApplication() {
    if (g_send_event_swizzled) {
        return;
    }
    Class cls = [NSApplication class];
    SEL originalSel = @selector(sendEvent:);
    SEL swizzledSel = @selector(qui_cef_sendEvent:);
    Method original = class_getInstanceMethod(cls, originalSel);
    Method swizzled = class_getInstanceMethod(cls, swizzledSel);
    if (original && swizzled) {
        method_exchangeImplementations(original, swizzled);
        g_send_event_swizzled = true;
    }
}

// Resolve the Frameworks dir relative to the running executable. CEF
// requires both:
//   - framework_dir_path: …/Contents/Frameworks/Chromium Embedded Framework.framework
//   - browser_subprocess_path: …/Contents/Frameworks/<App> Helper.app/Contents/MacOS/<App> Helper
//
// We derive both from [NSBundle mainBundle] so the binary works no
// matter what the host app is named — the host's .app dir is whatever
// macOS launched.
NSString* HostFrameworksDir() {
    NSBundle* mainBundle = [NSBundle mainBundle];
    if (!mainBundle) {
        return nil;
    }
    NSString* bundlePath = [mainBundle bundlePath];
    if (!bundlePath) {
        return nil;
    }
    return [bundlePath stringByAppendingPathComponent:@"Contents/Frameworks"];
}

NSString* HostHelperExecutable() {
    NSString* frameworks = HostFrameworksDir();
    if (!frameworks) {
        return nil;
    }
    NSString* hostName = [[[NSBundle mainBundle] infoDictionary] objectForKey:@"CFBundleExecutable"];
    if (!hostName) {
        hostName = [[NSProcessInfo processInfo] processName];
    }
    NSString* helperApp = [NSString stringWithFormat:@"%@ Helper.app", hostName];
    NSString* helperExe = [NSString stringWithFormat:@"%@ Helper", hostName];
    return [[[frameworks
        stringByAppendingPathComponent:helperApp]
        stringByAppendingPathComponent:@"Contents/MacOS"]
        stringByAppendingPathComponent:helperExe];
}

NSString* HostFrameworkPath() {
    NSString* frameworks = HostFrameworksDir();
    if (!frameworks) {
        return nil;
    }
    return [frameworks
        stringByAppendingPathComponent:@"Chromium Embedded Framework.framework"];
}

NSString* HostFrameworkResourcesPath() {
    NSString* framework = HostFrameworkPath();
    if (!framework) {
        return nil;
    }
    return [framework stringByAppendingPathComponent:@"Resources"];
}

// CefApp subclass. v1 customizes only the command-line so we can opt
// the embedded Chromium out of macOS Keychain integration — otherwise
// the password-manager init pops a Keychain authorization dialog the
// first time CEF runs, and if the user declines (or the test
// environment can't show it) the renderer process gets stuck in a
// 15-second connection-watchdog respawn loop that beachballs the
// host process.
class QuiCefApp : public CefApp {
public:
    QuiCefApp() {}

    void OnBeforeCommandLineProcessing(
        const CefString& process_type,
        CefRefPtr<CefCommandLine> command_line) override {
        // Only touch the browser process command line; child processes
        // get the inherited copy from the browser, and modifying them
        // is documented as undefined behavior.
        if (!process_type.empty()) {
            return;
        }
        // Skip Keychain — `basic` is Chromium's plaintext fallback
        // store. Acceptable for an embedded browser whose saved
        // passwords are not the user's primary credentials.
        if (!command_line->HasSwitch("password-store")) {
            command_line->AppendSwitchWithValue("password-store", "basic");
        }
        // Disable the disk-encrypt key fetch entirely (the source of
        // the "Encryption is not available" warnings) — we don't ship
        // a credential store with this widget. AppendSwitchWithValue
        // is the correct API for `--disable-features=A,B`; using
        // AppendSwitch("disable-features=A,B") would register a switch
        // literally named "disable-features=A,B" which Chromium
        // ignores silently.
        if (!command_line->HasSwitch("disable-features")) {
            command_line->AppendSwitchWithValue(
                "disable-features",
                "EnablePasswordsAccountStorage,AutofillEnableAccountWalletStorage");
        }
    }

private:
    IMPLEMENT_REFCOUNTING(QuiCefApp);
    DISALLOW_COPY_AND_ASSIGN(QuiCefApp);
};

// Forward decl — DevTools observer holds a reference to the page handle so
// it can route incoming method results back into Go without an extra
// pointer table.
class QuiDevToolsObserver;

// QuiCefClient is the per-browser CefClient + handler bundle. One
// instance per WebView Page. Implements:
//   - CefRenderHandler: GetViewRect (size hint) + OnPaint (CPU pixel
//     callback into Go; Phase A path until IOSurface lands in Phase D).
//   - CefLifeSpanHandler: OnAfterCreated stores the CefBrowser ref so
//     later LoadURL / Resize / Send* calls can target it.
//   - CefLoadHandler: OnLoadStart / OnLoadEnd / OnLoadError forwarded
//     into Go listeners.
//   - CefDisplayHandler: OnTitleChange / OnAddressChange /
//     OnConsoleMessage forwarded into Go listeners.
//   - CefClient::OnProcessMessageReceived (Phase C): receives "qui.call"
//     messages from the renderer-side window.qui Proxy and dispatches
//     into Go via quiWebviewOnHandlerCall.
//
// All Go callbacks pass the opaque uintptr handle that Go assigned at
// qui_webview_cef_page_new time so the Go registry can find the
// matching darwinPage.
class QuiCefClient
    : public CefClient,
      public CefLifeSpanHandler,
      public CefLoadHandler,
      public CefDisplayHandler,
      public CefRenderHandler {
public:
    QuiCefClient(uintptr_t handle, int width, int height, float scale)
        : handle_(handle),
          view_w_(width > 0 ? width : 1),
          view_h_(height > 0 ? height : 1),
          scale_(scale > 0 ? scale : 1.0f) {}

    // CefClient -- expose each handler we implement.
    CefRefPtr<CefLifeSpanHandler> GetLifeSpanHandler() override { return this; }
    CefRefPtr<CefLoadHandler> GetLoadHandler() override { return this; }
    CefRefPtr<CefDisplayHandler> GetDisplayHandler() override { return this; }
    CefRefPtr<CefRenderHandler> GetRenderHandler() override { return this; }

    // CefRenderHandler -------------------------------------------------
    void GetViewRect(CefRefPtr<CefBrowser> /*browser*/, CefRect& rect) override {
        rect = CefRect(0, 0, view_w_, view_h_);
    }

    bool GetScreenInfo(CefRefPtr<CefBrowser> /*browser*/,
                       CefScreenInfo& screen_info) override {
        screen_info.device_scale_factor = scale_;
        screen_info.rect = CefRect(0, 0, view_w_, view_h_);
        screen_info.available_rect = screen_info.rect;
        return true;
    }

    void OnPaint(CefRefPtr<CefBrowser> /*browser*/,
                 PaintElementType type,
                 const RectList& /*dirtyRects*/,
                 const void* buffer,
                 int width,
                 int height) override {
        // Popups (PET_POPUP) are <select> dropdowns etc. drawn in a
        // separate overlay surface; for Phase A we only forward the
        // main view.
        if (type != PET_VIEW) {
            return;
        }
        quiWebviewOnPaint(handle_, buffer, width, height);
    }

    // CefLifeSpanHandler -----------------------------------------------
    // Intercept popups (target=_blank, window.open, …) BEFORE CEF
    // creates a second CefBrowser. Allowing them would break our
    // single-handle event routing (the popup would share this client,
    // pump OnPaint into our buffer, and leave focus / mouse-capture in
    // a broken state when the user closes the popup window).
    //
    // The embedded-webview convention is to navigate the parent
    // browser to the would-be popup URL instead. Most users expect
    // this from an in-app browser; if a future caller wants real
    // popup support we'll need to expose a callback that lets them
    // build a new WebView and adopt the popup browser via window_info.
    bool OnBeforePopup(CefRefPtr<CefBrowser> /*browser*/,
                       CefRefPtr<CefFrame> /*frame*/,
                       int /*popup_id*/,
                       const CefString& target_url,
                       const CefString& /*target_frame_name*/,
                       WindowOpenDisposition /*target_disposition*/,
                       bool /*user_gesture*/,
                       const CefPopupFeatures& /*popup_features*/,
                       CefWindowInfo& /*window_info*/,
                       CefRefPtr<CefClient>& /*client*/,
                       CefBrowserSettings& /*settings*/,
                       CefRefPtr<CefDictionaryValue>& /*extra_info*/,
                       bool* /*no_javascript_access*/) override {
        // Use the captured browser_ rather than the argument so the
        // navigation is queued onto the CEF UI thread the same way as
        // every other public hook; LoadURL is documented as safe from
        // any thread but we keep the mutex discipline consistent.
        std::string url = target_url;
        if (!url.empty()) {
            CefRefPtr<CefBrowser> b;
            {
                std::lock_guard<std::mutex> g(mu_);
                b = browser_;
            }
            if (b) {
                b->GetMainFrame()->LoadURL(url);
            }
        }
        return true; // cancel popup creation
    }

    void OnAfterCreated(CefRefPtr<CefBrowser> browser) override {
        bool is_main = false;
        {
            std::lock_guard<std::mutex> g(mu_);
            // ShowDevTools creates a secondary browser that also routes
            // through this client. Keep browser_ pinned to the inspected
            // main page browser only.
            if (!browser_) {
                browser_ = browser;
                main_browser_id_ = browser ? browser->GetIdentifier() : 0;
                is_main = true;
            }
        }
        if (!is_main) {
            return;
        }
        // Register the DevTools observer here, after the browser exists.
        // Done outside the mutex because AddDevToolsMessageObserver returns
        // a CefRegistration we then stash into the same mutex-guarded slot.
        RegisterDevToolsObserver();
        quiWebviewOnAfterCreated(handle_);
    }

    void OnBeforeClose(CefRefPtr<CefBrowser> browser) override {
        if (!IsMainBrowser(browser)) {
            return;
        }
        {
            std::lock_guard<std::mutex> g(mu_);
            browser_ = nullptr;
            main_browser_id_ = 0;
            // Dropping the registration also detaches the observer.
            devtools_registration_ = nullptr;
        }
        quiWebviewOnBeforeClose(handle_);
    }

    // QuiCefClient is also the CefClient::OnProcessMessageReceived
    // implementation. We use it to receive "qui.call" messages from the
    // renderer-side window.qui Proxy: args are [name (string), id (int),
    // payload (string)]. The Go side runs the user handler off the CEF
    // UI thread and posts a reply back via SendHandlerReply().
    bool OnProcessMessageReceived(CefRefPtr<CefBrowser> /*browser*/,
                                  CefRefPtr<CefFrame> /*frame*/,
                                  CefProcessId source_process,
                                  CefRefPtr<CefProcessMessage> message) override {
        if (source_process != PID_RENDERER || !message || !message->IsValid()) {
            return false;
        }
        if (message->GetName() != "qui.call") {
            return false;
        }
        auto args = message->GetArgumentList();
        if (!args || args->GetSize() < 3) {
            return false;
        }
        std::string name = args->GetString(0);
        int id = args->GetInt(1);
        std::string payload = args->GetString(2);
        // Forward into Go. Go spawns a goroutine for the handler and
        // calls back into SendHandlerReply when done.
        quiWebviewOnHandlerCall(
            handle_,
            name.c_str(),
            id,
            payload.data(),
            static_cast<int>(payload.size()));
        return true;
    }

    // SendHandlerReply ships a "qui.reply" process message back to the
    // renderer's main frame. Args: [id (int), reply (string), err (string)].
    // err is empty for success. Reply payload is treated as UTF-8 bytes.
    void SendHandlerReply(int message_id, const std::string& reply,
                          const std::string& err) {
        CefRefPtr<CefBrowser> b;
        {
            std::lock_guard<std::mutex> g(mu_);
            b = browser_;
        }
        if (!b) {
            return;
        }
        CefRefPtr<CefFrame> frame = b->GetMainFrame();
        if (!frame) {
            return;
        }
        auto msg = CefProcessMessage::Create("qui.reply");
        auto args = msg->GetArgumentList();
        args->SetInt(0, message_id);
        args->SetString(1, reply);
        args->SetString(2, err);
        frame->SendProcessMessage(PID_RENDERER, msg);
    }

    // SendDevToolsMessage forwards a JSON-encoded Runtime.evaluate (or any
    // other DevTools method call) to Chromium's DevTools agent. Replies
    // arrive on QuiDevToolsObserver::OnDevToolsMethodResult and are routed
    // into Go via quiWebviewOnDevToolsResult.
    bool SendDevToolsMessage(const std::string& json) {
        CefRefPtr<CefBrowser> b;
        {
            std::lock_guard<std::mutex> g(mu_);
            b = browser_;
        }
        if (!b) {
            return false;
        }
        return b->GetHost()->SendDevToolsMessage(json.data(), json.size());
    }

    // CefLoadHandler ---------------------------------------------------
    void OnLoadStart(CefRefPtr<CefBrowser> browser,
                     CefRefPtr<CefFrame> frame,
                     TransitionType /*transition_type*/) override {
        if (!IsMainBrowser(browser)) {
            return;
        }
        if (!frame || !frame->IsMain()) {
            return;
        }
        std::string url = frame->GetURL();
        quiWebviewOnLoadStart(handle_, url.c_str());
    }

    void OnLoadEnd(CefRefPtr<CefBrowser> browser,
                   CefRefPtr<CefFrame> frame,
                   int httpStatusCode) override {
        if (!IsMainBrowser(browser)) {
            return;
        }
        if (!frame || !frame->IsMain()) {
            return;
        }
        std::string url = frame->GetURL();
        quiWebviewOnLoadEnd(handle_, url.c_str(), httpStatusCode);
    }

    void OnLoadError(CefRefPtr<CefBrowser> browser,
                     CefRefPtr<CefFrame> frame,
                     ErrorCode errorCode,
                     const CefString& errorText,
                     const CefString& failedUrl) override {
        if (!IsMainBrowser(browser)) {
            return;
        }
        if (!frame || !frame->IsMain()) {
            return;
        }
        std::string url = failedUrl;
        std::string text = errorText;
        quiWebviewOnLoadError(handle_, url.c_str(),
                              static_cast<int>(errorCode), text.c_str());
    }

    // CefDisplayHandler ------------------------------------------------
    void OnAddressChange(CefRefPtr<CefBrowser> browser,
                         CefRefPtr<CefFrame> frame,
                         const CefString& url) override {
        if (!IsMainBrowser(browser)) {
            return;
        }
        if (!frame || !frame->IsMain()) {
            return;
        }
        std::string u = url;
        quiWebviewOnAddressChange(handle_, u.c_str());
    }

    void OnTitleChange(CefRefPtr<CefBrowser> browser,
                       const CefString& title) override {
        if (!IsMainBrowser(browser)) {
            return;
        }
        std::string t = title;
        quiWebviewOnTitleChange(handle_, t.c_str());
    }

    bool OnConsoleMessage(CefRefPtr<CefBrowser> browser,
                          cef_log_severity_t level,
                          const CefString& message,
                          const CefString& /*source*/,
                          int /*line*/) override {
        if (!IsMainBrowser(browser)) {
            return false;
        }
        std::string m = message;
        quiWebviewOnConsoleMessage(handle_, static_cast<int>(level), m.c_str());
        return false; // Let default behavior run (Chrome's own console).
    }

    // CefRenderHandler --------------------------------------------------
    // Beyond GetViewRect / GetScreenInfo / OnPaint declared earlier, we
    // also need OnImeCompositionRangeChanged so the IME candidate
    // window can be anchored next to the actual caret in the page.
    // Character bounds arrive in view-DIP coordinates; we keep the
    // last one for synchronous CaretRect lookups from the macOS IME
    // bridge.
    void OnImeCompositionRangeChanged(CefRefPtr<CefBrowser> /*browser*/,
                                      const CefRange& /*selected_range*/,
                                      const RectList& character_bounds) override {
        if (character_bounds.empty()) {
            return;
        }
        const CefRect& r = character_bounds.back();
        std::lock_guard<std::mutex> g(mu_);
        caret_rect_.x = r.x;
        caret_rect_.y = r.y;
        caret_rect_.width = r.width;
        caret_rect_.height = r.height;
    }

    // CaretRect copies out the last character-bound rect observed via
    // OnImeCompositionRangeChanged. Empty rect = no composition seen
    // yet for this page. Coordinates are widget-local DIPs.
    void CaretRect(int& x, int& y, int& w, int& h) {
        std::lock_guard<std::mutex> g(mu_);
        x = caret_rect_.x;
        y = caret_rect_.y;
        w = caret_rect_.width;
        h = caret_rect_.height;
    }

    bool OnCursorChange(CefRefPtr<CefBrowser> /*browser*/,
                        CefCursorHandle /*cursor*/,
                        cef_cursor_type_t type,
                        const CefCursorInfo& /*custom_cursor_info*/) override {
        // Don't try to apply the cursor here — we're on the CEF UI
        // thread, not the AppKit main loop, and NSCursor changes have
        // to happen on the latter. Forward the enum to Go; the WebView
        // widget translates and hands it to qui's main-goroutine cursor
        // setter via the user-registered listener.
        quiWebviewOnCursorChange(handle_, static_cast<int>(type));
        return true; // We handled it (deferred dispatch into Go).
    }

    // ------------------------------------------------------------------
    // Page-control hooks used by qui_webview_cef_page_* C entry points.
    // Always check browser_ — between Page.Open and OnAfterCreated, or
    // after OnBeforeClose, the browser is nil. Callers tolerate the
    // no-op (load / resize on a closed page is just dropped).
    // ------------------------------------------------------------------

    void Resize(int w, int h, float scale) {
        std::lock_guard<std::mutex> g(mu_);
        view_w_ = w > 0 ? w : 1;
        view_h_ = h > 0 ? h : 1;
        scale_ = scale > 0 ? scale : 1.0f;
        if (browser_) {
            browser_->GetHost()->WasResized();
        }
    }

    void LoadURL(const std::string& url) {
        std::lock_guard<std::mutex> g(mu_);
        if (browser_) {
            browser_->GetMainFrame()->LoadURL(url);
        }
    }

    void Reload() {
        std::lock_guard<std::mutex> g(mu_);
        if (browser_) {
            browser_->Reload();
        }
    }

    void StopLoad() {
        std::lock_guard<std::mutex> g(mu_);
        if (browser_) {
            browser_->StopLoad();
        }
    }

    void GoBack() {
        std::lock_guard<std::mutex> g(mu_);
        if (browser_ && browser_->CanGoBack()) {
            browser_->GoBack();
        }
    }

    void GoForward() {
        std::lock_guard<std::mutex> g(mu_);
        if (browser_ && browser_->CanGoForward()) {
            browser_->GoForward();
        }
    }

    void CloseBrowser() {
        CefRefPtr<CefBrowser> b;
        {
            std::lock_guard<std::mutex> g(mu_);
            b = browser_;
        }
        if (b) {
            // Don't hold mu_ while calling into CEF. CloseBrowser may
            // synchronously trigger lifespan callbacks (including
            // OnBeforeClose), and OnBeforeClose also takes mu_. Holding
            // the lock here can deadlock window close.
            b->GetHost()->CloseBrowser(true);
        }
    }

    void SendMouseMove(int x, int y, uint32_t mods, bool leave) {
        std::lock_guard<std::mutex> g(mu_);
        if (!browser_) return;
        CefMouseEvent ev;
        ev.x = x;
        ev.y = y;
        ev.modifiers = mods;
        browser_->GetHost()->SendMouseMoveEvent(ev, leave);
    }

    void SendMouseClick(int x, int y, int btn, bool mouseUp, int clickCount,
                        uint32_t mods) {
        std::lock_guard<std::mutex> g(mu_);
        if (!browser_) return;
        CefMouseEvent ev;
        ev.x = x;
        ev.y = y;
        ev.modifiers = mods;
        cef_mouse_button_type_t type = MBT_LEFT;
        if (btn == 1) type = MBT_MIDDLE;
        else if (btn == 2) type = MBT_RIGHT;
        browser_->GetHost()->SendMouseClickEvent(ev, type, mouseUp, clickCount);
    }

    void SendMouseWheel(int x, int y, int dx, int dy, uint32_t mods) {
        std::lock_guard<std::mutex> g(mu_);
        if (!browser_) return;
        CefMouseEvent ev;
        ev.x = x;
        ev.y = y;
        ev.modifiers = mods;
        browser_->GetHost()->SendMouseWheelEvent(ev, dx, dy);
    }

    void SendKey(int type, int windowsKeyCode, int nativeKeyCode, int character,
                 uint32_t mods) {
        std::lock_guard<std::mutex> g(mu_);
        if (!browser_) return;

        // macOS editing accelerators in windowless mode are not always
        // translated into Chromium edit commands consistently across
        // layouts/IME states. Handle the common Command shortcuts here
        // as an explicit fallback so copy/paste/select-all work reliably.
        //
        // Only on key-down path:
        //   0 = KEYEVENT_RAWKEYDOWN, 1 = KEYEVENT_KEYDOWN
        const bool is_key_down = (type == 0 || type == 1);
        const uint32_t EVENTFLAG_COMMAND_DOWN = 1u << 7;
        const uint32_t EVENTFLAG_SHIFT_DOWN = 1u << 1;
        if (is_key_down && (mods & EVENTFLAG_COMMAND_DOWN) != 0) {
            CefRefPtr<CefFrame> frame = browser_->GetFocusedFrame();
            if (!frame) {
                frame = browser_->GetMainFrame();
            }
            if (frame) {
                // Windows VK values (A/C/V/X/Z/Y) are ASCII.
                if (windowsKeyCode == 0x5A &&
                    (mods & EVENTFLAG_SHIFT_DOWN) != 0) {
                    frame->Redo(); // Cmd+Shift+Z
                    return;
                }
                switch (windowsKeyCode) {
                case 0x43: frame->Copy();      return; // Cmd+C
                case 0x58: frame->Cut();       return; // Cmd+X
                case 0x56: frame->Paste();     return; // Cmd+V
                case 0x41: frame->SelectAll(); return; // Cmd+A
                case 0x5A: frame->Undo();      return; // Cmd+Z
                case 0x59: frame->Redo();      return; // Cmd+Y
                default:
                    break;
                }
            }
        }

        CefKeyEvent ev;
        ev.type = static_cast<cef_key_event_type_t>(type);
        ev.modifiers = mods;
        ev.windows_key_code = windowsKeyCode;
        ev.native_key_code = nativeKeyCode;
        ev.character = static_cast<char16_t>(character);
        ev.unmodified_character = ev.character;
        ev.is_system_key = false;
        ev.focus_on_editable_field = false;
        browser_->GetHost()->SendKeyEvent(ev);
    }

    void OpenDevTools() {
        CefRefPtr<CefBrowser> b;
        {
            std::lock_guard<std::mutex> g(mu_);
            b = browser_;
        }
        if (!b) {
            return;
        }
        CefWindowInfo wi;
        CefBrowserSettings settings;
        // Some CEF versions/platform wrappers don't expose SetAsPopup on
        // CefWindowInfo. Passing a default-initialized window info keeps
        // this path ABI-compatible and still lets CEF create a DevTools
        // window with platform defaults.
        b->GetHost()->ShowDevTools(
            wi, this, settings, CefPoint(0, 0));
    }

    void SetFocus(bool focus) {
        std::lock_guard<std::mutex> g(mu_);
        if (!browser_) return;
        browser_->GetHost()->SetFocus(focus);
    }

    void ImeSetComposition(const std::string& text, int cursor) {
        std::lock_guard<std::mutex> g(mu_);
        if (!browser_) return;
        std::vector<CefCompositionUnderline> underlines;
        // Single underline spanning the whole composition — matches the
        // default Cocoa NSTextInputClient look and is the minimum that
        // makes IME composition visible in form fields. CEF uses UTF16
        // offsets internally for these ranges; in CEF 147 the
        // CefCompositionUnderline.range field is in *characters of the
        // resulting text* which lines up with UTF16 code units, so we
        // count code units (not bytes, not codepoints) for the end.
        CefString cefText(text);
        size_t len = cefText.length(); // length() returns UTF16 units
        if (len > 0) {
            CefCompositionUnderline u;
            u.range = CefRange(0, static_cast<uint32_t>(len));
            u.color = 0xFF000000;      // black underline
            u.background_color = 0;    // transparent background
            u.thick = 0;
            u.style = CEF_CUS_SOLID;
            underlines.push_back(u);
        }
        // |replacement_range| is documented as only used on OS X, but
        // CefRange::InvalidRange tells Blink "no replacement". The
        // selection range positions the caret at the requested cursor
        // offset (in UTF16 units).
        uint32_t caret = static_cast<uint32_t>(cursor);
        if (caret > len) {
            caret = static_cast<uint32_t>(len);
        }
        browser_->GetHost()->ImeSetComposition(
            cefText, underlines, CefRange::InvalidRange(),
            CefRange(caret, caret));
    }

    void ImeCommitText(const std::string& text) {
        std::lock_guard<std::mutex> g(mu_);
        if (!browser_) return;
        if (text.empty()) {
            // Finalize whatever is currently composing without inserting
            // additional text — same semantics as Cocoa's `unmarkText`.
            browser_->GetHost()->ImeFinishComposingText(false);
            return;
        }
        browser_->GetHost()->ImeCommitText(
            CefString(text), CefRange::InvalidRange(), 0);
    }

    void ImeCancel() {
        std::lock_guard<std::mutex> g(mu_);
        if (!browser_) return;
        browser_->GetHost()->ImeCancelComposition();
    }

    uintptr_t handle() const { return handle_; }

private:
    bool IsMainBrowser(CefRefPtr<CefBrowser> browser) {
        if (!browser) {
            return false;
        }
        std::lock_guard<std::mutex> g(mu_);
        return main_browser_id_ != 0 &&
               browser->GetIdentifier() == main_browser_id_;
    }

    // RegisterDevToolsObserver attaches a QuiDevToolsObserver to the
    // browser. Implemented out-of-line below QuiDevToolsObserver's class
    // body so it can reference its constructor.
    void RegisterDevToolsObserver();

    std::mutex mu_;
    CefRefPtr<CefBrowser> browser_;
    int main_browser_id_ = 0;
    const uintptr_t handle_;
    int view_w_;
    int view_h_;
    float scale_;
    // Last caret bounds reported by OnImeCompositionRangeChanged, in
    // widget-local DIPs. Zero-initialized → empty until the renderer
    // sends its first composition update. Mutex-protected because
    // CaretRect() can be polled from macOS's IME bridge on the qui
    // main goroutine while the CEF UI thread is updating it.
    CefRect caret_rect_;

    // DevTools registration. Held so the observer stays attached for the
    // page's lifetime. Dropped on OnBeforeClose to detach.
    CefRefPtr<CefRegistration> devtools_registration_;

    IMPLEMENT_REFCOUNTING(QuiCefClient);
    DISALLOW_COPY_AND_ASSIGN(QuiCefClient);
};

// QuiDevToolsObserver bridges DevTools-protocol method results back into
// the Go side. AddDevToolsMessageObserver delivers events for the
// browser's entire DevTools session; we only care about method results
// (Runtime.evaluate replies). |result| arrives as a UTF-8 JSON blob —
// either the "result" dict on success or the "error" dict on failure.
class QuiDevToolsObserver : public CefDevToolsMessageObserver {
public:
    explicit QuiDevToolsObserver(uintptr_t handle) : handle_(handle) {}

    void OnDevToolsMethodResult(CefRefPtr<CefBrowser> /*browser*/,
                                int message_id,
                                bool success,
                                const void* result,
                                size_t result_size) override {
        // Forward unchanged to Go; the Go side parses the JSON to translate
        // into JSValue. Passing the byte pointer + size avoids a copy in
        // C++ — Cgo treats it as an opaque blob and the Go-side unsafe.Slice
        // copies what it needs into a string for json.Unmarshal.
        quiWebviewOnDevToolsResult(
            handle_,
            message_id,
            success ? 1 : 0,
            result,
            static_cast<int>(result_size));
    }

private:
    const uintptr_t handle_;

    IMPLEMENT_REFCOUNTING(QuiDevToolsObserver);
    DISALLOW_COPY_AND_ASSIGN(QuiDevToolsObserver);
};

inline void QuiCefClient::RegisterDevToolsObserver() {
    CefRefPtr<CefBrowser> b;
    {
        std::lock_guard<std::mutex> g(mu_);
        b = browser_;
    }
    if (!b) {
        return;
    }
    CefRefPtr<QuiDevToolsObserver> observer(new QuiDevToolsObserver(handle_));
    CefRefPtr<CefRegistration> reg =
        b->GetHost()->AddDevToolsMessageObserver(observer);
    std::lock_guard<std::mutex> g(mu_);
    devtools_registration_ = reg;
}

// Page registry: Go assigns each Page a unique uintptr at construction;
// we hold a CefRefPtr<QuiCefClient> against it so the client outlives
// the local CreateBrowserSync return and survives until the matching
// destroy call. Lookups are short-lived so a single mutex is fine.
std::mutex g_pages_mu;
std::unordered_map<uintptr_t, CefRefPtr<QuiCefClient>> g_pages;

CefRefPtr<QuiCefClient> LookupPage(uintptr_t handle) {
    std::lock_guard<std::mutex> g(g_pages_mu);
    auto it = g_pages.find(handle);
    if (it == g_pages.end()) {
        return nullptr;
    }
    return it->second;
}

} // namespace

// -----------------------------------------------------------------------------
// extern "C" surface called from Cgo.
// -----------------------------------------------------------------------------

extern "C" {

// Load the CEF framework dylib into the host process. Returns 0 on
// success, -1 on failure. Called once per process before any other
// CEF function.
int qui_webview_cef_load_library(void) {
    if (g_library_loader != nullptr) {
        return 0; // Already loaded; idempotent.
    }
    g_library_loader = new CefScopedLibraryLoader();
    if (!g_library_loader->LoadInMain()) {
        delete g_library_loader;
        g_library_loader = nullptr;
        return -1;
    }
    return 0;
}

// Initialize CEF with multi-process + external message pump +
// windowless rendering enabled. Must be called on the main thread
// AFTER load_library. Returns 0 on success, -1 on failure.
//
// argv/argc from Go are not what CEF wants on macOS — Go's os.Args
// is post-runtime-processed. Use _NSGetArgv() / _NSGetArgc() to
// recover the OS-level argv that NSProcessInfo + CEF subprocess
// discovery actually consult. The Go-supplied args are accepted for
// API symmetry but currently ignored on darwin.
int qui_webview_cef_initialize(int /*argc_go*/, char** /*argv_go*/) {
    if (g_initialized) {
        return 0;
    }
    int    argc = *_NSGetArgc();
    char** argv = *_NSGetArgv();
    fprintf(stderr, "[qui-webview] os argc=%d\n", argc);
    for (int i = 0; i < argc && i < 4; i++) {
        fprintf(stderr, "  argv[%d] = %s\n", i, argv[i]);
    }

    NSString* frameworkDir = HostFrameworkPath();
    NSString* resourcesDir = HostFrameworkResourcesPath();
    NSString* helperExe = HostHelperExecutable();
    NSString* mainBundle = [[NSBundle mainBundle] bundlePath];
    NSString* logFile = [NSTemporaryDirectory()
        stringByAppendingPathComponent:@"qui-cef-debug.log"];

    if (!frameworkDir || !resourcesDir || !helperExe || !mainBundle) {
        fprintf(stderr,
            "[qui-webview] cannot resolve .app bundle paths; "
            "ensure the binary runs from a properly packaged .app\n");
        return -1;
    }
    NSFileManager* fm = [NSFileManager defaultManager];
    bool helperExists = [fm isExecutableFileAtPath:helperExe];
    bool frameworkExists = [fm fileExistsAtPath:frameworkDir];
    bool resourcesExists = [fm fileExistsAtPath:resourcesDir];

    fprintf(stderr, "[qui-webview] CefInitialize:\n");
    fprintf(stderr, "  mainBundle    = %s\n", [mainBundle UTF8String]);
    fprintf(stderr, "  frameworkDir  = %s\n", [frameworkDir UTF8String]);
    fprintf(stderr, "  resourcesDir  = %s (%s)\n", [resourcesDir UTF8String],
        resourcesExists ? "exists" : "missing");
    fprintf(stderr, "  helperExe     = %s\n", [helperExe UTF8String]);
    fprintf(stderr, "  helperExecOk  = %s\n", helperExists ? "yes" : "no");
    fprintf(stderr, "  frameworkOk   = %s\n", frameworkExists ? "yes" : "no");
    fprintf(stderr, "  logFile       = %s\n", [logFile UTF8String]);
    fprintf(stderr, "  isMainThread  = %s\n", [NSThread isMainThread] ? "yes" : "no");

    // Ensure NSApplication exists and is compatible with CefAppProtocol.
    [NSApplication sharedApplication];
    EnsureCefAppProtocolOnNSApplication();
    bool cefProtocolOk = [NSApp conformsToProtocol:@protocol(CefAppProtocol)];
    fprintf(stderr, "  cefAppProtoOk = %s (NSApp class=%s)\n",
        cefProtocolOk ? "yes" : "no",
        NSStringFromClass([NSApp class]).UTF8String);

    CefMainArgs main_args(argc, argv);

    // IMPORTANT: declare via the default constructor (not `= {}`).
    // `CefSettings` is `CefStructBase<CefSettingsTraits>` whose base
    // class `cef_settings_t` makes it a C++17 aggregate — and
    // `CefSettings settings = {};` is aggregate-init, which SKIPS the
    // CefStructBase default constructor and its `Init()` hook. The
    // skipped Init() leaves CEF-internal fields in an indeterminate
    // state and CefInitialize() then returns false with exit_code=-1
    // and no log output. Use the default constructor so Init() runs.
    CefSettings settings;
    settings.no_sandbox = 1; // OSR + sandbox is fiddly; revisit Phase D
    settings.windowless_rendering_enabled = 1;
    // External message pump requires OnScheduleMessagePumpWork
    // implementation; leave it disabled for v1 and pump via
    // CefDoMessageLoopWork() in the qui frame loop. The default
    // CEF settings of (external_message_pump=0,
    // multi_threaded_message_loop=0) tell CEF NOT to run its own
    // loop; we manually pump.
    settings.log_severity = LOGSEVERITY_VERBOSE;
    CefString(&settings.log_file).FromString([logFile UTF8String]);
    // Match cefsimple defaults: leave subprocess/framework/bundle paths
    // empty; CEF on macOS auto-discovers them relative to NSBundle.mainBundle
    // for our standard .app layout (see package-app.sh).
    CefRefPtr<QuiCefApp> app(new QuiCefApp());
    if (!CefInitialize(main_args, settings, app, nullptr)) {
        int exit_code = CefGetExitCode();
        fprintf(stderr,
            "[qui-webview] CefInitialize failed (exit_code=%d)\n",
            exit_code);
        return -1;
    }

    g_initialized = true;
    return 0;
}

// Run one iteration of the CEF message loop. Safe to call repeatedly
// per qui frame — CefDoMessageLoopWork is documented as cheap when
// idle. No-op until cef_initialize has succeeded.
void qui_webview_cef_tick(void) {
    if (!g_initialized) {
        return;
    }
    CefDoMessageLoopWork();
}

// Release all CEF resources. After Shutdown, no other CEF function may
// be called (would crash). Idempotent.
void qui_webview_cef_shutdown(void) {
    if (g_initialized) {
        CefShutdown();
        g_initialized = false;
    }
    if (g_library_loader) {
        delete g_library_loader;
        g_library_loader = nullptr;
    }
}

// Report whether CefInitialize has been called and succeeded. Used by
// backend_darwin.go to avoid double-init and to gate calls that
// require an initialized CEF runtime.
int qui_webview_cef_is_initialized(void) {
    return g_initialized ? 1 : 0;
}

// Host-mode hook: tell this translation unit that an external C++ host
// (see webview/host/main.mm) already called CefScopedLibraryLoader
// + CefInitialize from a real main(). After this returns,
// qui_webview_cef_is_initialized() reports true and the Go-side
// ensureInit short-circuits without trying to call CefInitialize
// again (CEF allows exactly one init per process). We deliberately do
// NOT call LoadInMain here: libcef_dll_wrapper.a is statically linked
// into both libqui_host.dylib AND the host binary, but dyld's two-level
// namespace resolves the wrapper functions (cef_load_library /
// cef_initialize / etc.) to the host's copy at load time, so the
// dylib's local wrapper state (g_libcef_handle) stays unused — and
// trying to LoadInMain in the dylib would fail because the host's
// g_libcef_handle is already non-null.
int qui_webview_cef_use_host_init(void) {
    g_initialized = true;
    return 0;
}

// -----------------------------------------------------------------------------
// Per-page entry points. Each takes the Go-assigned uintptr handle and
// fans out to the matching QuiCefClient. handle==0 is reserved for "no
// page" (Go's nil-equivalent) and is treated as a no-op.
// -----------------------------------------------------------------------------

int qui_webview_cef_page_new(uintptr_t handle, int width, int height,
                             float scale, const char* initial_url) {
    if (!g_initialized || handle == 0) {
        return -1;
    }
    CefRefPtr<QuiCefClient> client(new QuiCefClient(handle, width, height, scale));

    CefWindowInfo window_info;
    window_info.SetAsWindowless(0);

    CefBrowserSettings settings;
    settings.windowless_frame_rate = 60;
    // CEF OSR defaults to transparent painting — pages without an
    // explicit body background-color land in the buffer with alpha=0,
    // which DrawImage then renders as invisible. Forcing an opaque
    // background here makes "untouched" pixels white instead of
    // transparent, so the buffer contains the actual rendered page
    // even when the site relies on the user-agent default background.
    settings.background_color = CefColorSetARGB(255, 255, 255, 255);

    std::string url = (initial_url && *initial_url) ? initial_url : "about:blank";

    CefRefPtr<CefBrowser> browser = CefBrowserHost::CreateBrowserSync(
        window_info, client.get(), CefString(url), settings, nullptr, nullptr);
    if (!browser) {
        fprintf(stderr,
            "[qui-webview] CreateBrowserSync returned null (handle=%lu, url=%s)\n",
            (unsigned long)handle, url.c_str());
        return -1;
    }
    {
        std::lock_guard<std::mutex> g(g_pages_mu);
        g_pages[handle] = client;
    }
    // Kick CEF to take the browser as "visible + focused" so OSR
    // generates its first paint. Without these, Chromium's
    // visibility-tracker treats the windowless browser as
    // background and elides the OnPaint until something explicitly
    // marks it visible (we saw OnPaint stop firing entirely until
    // this was added).
    browser->GetHost()->WasHidden(false);
    browser->GetHost()->SetFocus(true);
    browser->GetHost()->Invalidate(PET_VIEW);
    return 0;
}

void qui_webview_cef_page_destroy(uintptr_t handle) {
    if (handle == 0) return;
    CefRefPtr<QuiCefClient> client;
    {
        std::lock_guard<std::mutex> g(g_pages_mu);
        auto it = g_pages.find(handle);
        if (it == g_pages.end()) return;
        client = it->second;
        g_pages.erase(it);
    }
    if (client) {
        client->CloseBrowser();
    }
    // The CefRefPtr drops here; the underlying browser keeps the
    // client alive until OnBeforeClose fires inside the CEF UI loop.
}

void qui_webview_cef_page_load_url(uintptr_t handle, const char* url) {
    if (!url) return;
    auto p = LookupPage(handle);
    if (p) p->LoadURL(url);
}

void qui_webview_cef_page_reload(uintptr_t handle) {
    auto p = LookupPage(handle);
    if (p) p->Reload();
}

void qui_webview_cef_page_stop_load(uintptr_t handle) {
    auto p = LookupPage(handle);
    if (p) p->StopLoad();
}

void qui_webview_cef_page_go_back(uintptr_t handle) {
    auto p = LookupPage(handle);
    if (p) p->GoBack();
}

void qui_webview_cef_page_go_forward(uintptr_t handle) {
    auto p = LookupPage(handle);
    if (p) p->GoForward();
}

void qui_webview_cef_page_resize(uintptr_t handle, int w, int h, float scale) {
    auto p = LookupPage(handle);
    if (p) p->Resize(w, h, scale);
}

void qui_webview_cef_page_send_mouse_move(uintptr_t handle, int x, int y,
                                          unsigned int mods, int leave) {
    auto p = LookupPage(handle);
    if (p) p->SendMouseMove(x, y, mods, leave != 0);
}

void qui_webview_cef_page_send_mouse_click(uintptr_t handle, int x, int y,
                                           int button, int mouseUp,
                                           int clickCount, unsigned int mods) {
    auto p = LookupPage(handle);
    if (p) p->SendMouseClick(x, y, button, mouseUp != 0, clickCount, mods);
}

void qui_webview_cef_page_send_mouse_wheel(uintptr_t handle, int x, int y,
                                           int dx, int dy, unsigned int mods) {
    auto p = LookupPage(handle);
    if (p) p->SendMouseWheel(x, y, dx, dy, mods);
}

void qui_webview_cef_page_send_key(uintptr_t handle, int type,
                                   int windowsKeyCode, int nativeKeyCode,
                                   int character, unsigned int mods) {
    auto p = LookupPage(handle);
    if (p) p->SendKey(type, windowsKeyCode, nativeKeyCode, character, mods);
}

void qui_webview_cef_page_set_focus(uintptr_t handle, int focused) {
    auto p = LookupPage(handle);
    if (p) p->SetFocus(focused != 0);
}

void qui_webview_cef_page_open_devtools(uintptr_t handle) {
    auto p = LookupPage(handle);
    if (p) p->OpenDevTools();
}

void qui_webview_cef_page_ime_set_composition(uintptr_t handle, const char* utf8, int cursor) {
    if (!utf8) return;
    auto p = LookupPage(handle);
    if (p) p->ImeSetComposition(std::string(utf8), cursor);
}

void qui_webview_cef_page_ime_commit(uintptr_t handle, const char* utf8) {
    auto p = LookupPage(handle);
    if (p) p->ImeCommitText(utf8 ? std::string(utf8) : std::string());
}

void qui_webview_cef_page_ime_cancel(uintptr_t handle) {
    auto p = LookupPage(handle);
    if (p) p->ImeCancel();
}

void qui_webview_cef_page_caret_rect(uintptr_t handle, int* x, int* y, int* w, int* h) {
    if (!x || !y || !w || !h) return;
    *x = 0; *y = 0; *w = 0; *h = 0;
    auto p = LookupPage(handle);
    if (p) p->CaretRect(*x, *y, *w, *h);
}

// Phase C: dispatch a DevTools Protocol message (typically a
// Runtime.evaluate Wirth a Go-allocated message_id) into the browser's
// DevTools agent. The asynchronous reply lands on QuiDevToolsObserver
// and then in quiWebviewOnDevToolsResult on the Go side.
//
// Returns 0 on success, -1 if the page is unknown or the browser hasn't
// been created yet.
int qui_webview_cef_page_send_devtools(uintptr_t handle, const char* json,
                                       int json_len) {
    if (!json || json_len <= 0) return -1;
    auto p = LookupPage(handle);
    if (!p) return -1;
    return p->SendDevToolsMessage(std::string(json, json_len)) ? 0 : -1;
}

// Phase C: deliver a Go-side handler reply back to the renderer's
// window.qui Proxy. reply_ptr / reply_len are the JSON-encoded reply
// (empty allowed); err is empty on success, non-empty to reject the JS
// Promise with the given string. No-op if the page is closed.
void qui_webview_cef_page_send_handler_reply(uintptr_t handle, int message_id,
                                             const char* reply_ptr,
                                             int reply_len,
                                             const char* err) {
    auto p = LookupPage(handle);
    if (!p) return;
    std::string reply;
    if (reply_ptr && reply_len > 0) {
        reply.assign(reply_ptr, reply_len);
    }
    std::string error_str = err ? err : "";
    p->SendHandlerReply(message_id, reply, error_str);
}

} // extern "C"
