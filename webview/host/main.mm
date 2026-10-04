// main.mm — browser-process host entry point using cefsimple-style
// startup, then delegating app logic to Go (c-shared exports).

#include <Cocoa/Cocoa.h>
#include <chrono>
#include <thread>

#include "include/cef_app.h"
#include "include/cef_application_mac.h"
#include "include/wrapper/cef_helpers.h"
#include "include/wrapper/cef_library_loader.h"

extern "C" int QuiStart(void);
extern "C" int QuiTick(void);
extern "C" void QuiStop(void);

// Implemented in webview/backend_darwin.cc and linked into libqui_host.dylib.
// Tells the cgo bridge that we (the host) already called CefInitialize and
// it should skip its own initialization path while still populating its
// dlsym table via CefScopedLibraryLoader::LoadInMain.
extern "C" int qui_webview_cef_use_host_init(void);

@interface QuiApplication : NSApplication <CefAppProtocol> {
 @private
  BOOL handlingSendEvent_;
}
@end

@implementation QuiApplication
- (BOOL)isHandlingSendEvent {
  return handlingSendEvent_;
}

- (void)setHandlingSendEvent:(BOOL)handlingSendEvent {
  handlingSendEvent_ = handlingSendEvent;
}

- (void)sendEvent:(NSEvent*)event {
  CefScopedSendingEvent sendingEventScoper;
  [super sendEvent:event];
}
@end

namespace {

NSString* HostFrameworksDir() {
  NSBundle* mainBundle = [NSBundle mainBundle];
  if (!mainBundle) return nil;
  NSString* bundlePath = [mainBundle bundlePath];
  if (!bundlePath) return nil;
  return [bundlePath stringByAppendingPathComponent:@"Contents/Frameworks"];
}

NSString* HostHelperExecutable() {
  NSString* frameworks = HostFrameworksDir();
  if (!frameworks) return nil;
  NSString* hostName = [[[NSBundle mainBundle] infoDictionary]
      objectForKey:@"CFBundleExecutable"];
  if (!hostName) hostName = [[NSProcessInfo processInfo] processName];
  NSString* helperApp = [NSString stringWithFormat:@"%@ Helper.app", hostName];
  NSString* helperExe = [NSString stringWithFormat:@"%@ Helper", hostName];
  return [[[frameworks stringByAppendingPathComponent:helperApp]
      stringByAppendingPathComponent:@"Contents/MacOS"]
      stringByAppendingPathComponent:helperExe];
}

class QuiHostApp : public CefApp {
 public:
  QuiHostApp() = default;
 private:
  IMPLEMENT_REFCOUNTING(QuiHostApp);
  DISALLOW_COPY_AND_ASSIGN(QuiHostApp);
};

}  // namespace

int main(int argc, char* argv[]) {
  fprintf(stderr, "[qui-host] argc=%d\n", argc);
  for (int i = 0; i < argc && i < 4; i++) {
    fprintf(stderr, "  argv[%d] = %s\n", i, argv[i]);
  }

  CefScopedLibraryLoader library_loader;
  if (!library_loader.LoadInMain()) {
    fprintf(stderr, "[qui-host] LoadInMain failed\n");
    return 1;
  }

  CefMainArgs main_args(argc, argv);

  [QuiApplication sharedApplication];
  fprintf(stderr, "[qui-host] NSApp class=%s\n",
      NSStringFromClass([NSApp class]).UTF8String);
  if (![NSApp conformsToProtocol:@protocol(CefAppProtocol)]) {
    fprintf(stderr, "[qui-host] CefAppProtocol missing on NSApp\n");
    return 2;
  }

  NSString* helperExe = HostHelperExecutable();
  NSString* mainBundle = [[NSBundle mainBundle] bundlePath];
  NSString* tmpLog = [NSTemporaryDirectory()
      stringByAppendingPathComponent:@"qui-host-cef-debug.log"];
  if (!helperExe) {
    fprintf(stderr, "[qui-host] helper executable not found\n");
    return 3;
  }
  fprintf(stderr, "[qui-host] mainBundle=%s\n", [mainBundle UTF8String]);
  fprintf(stderr, "[qui-host] helperExe=%s\n", [helperExe UTF8String]);
  fprintf(stderr, "[qui-host] logFile=%s\n", [tmpLog UTF8String]);

  // See backend_darwin.cc for the long-form note: do NOT use
  // `CefSettings settings = {};` — that's aggregate-init and skips
  // CefStructBase::Init(), which makes CefInitialize fail silently
  // with exit_code=-1. The default constructor runs Init() correctly.
  CefSettings settings;
  settings.no_sandbox = 1;
  settings.windowless_rendering_enabled = 1;
  settings.log_severity = LOGSEVERITY_VERBOSE;
  CefString(&settings.log_file).FromString([tmpLog UTF8String]);
  CefString(&settings.browser_subprocess_path).FromString(
      [helperExe UTF8String]);

  if (!CefInitialize(main_args, settings, nullptr, nullptr)) {
    fprintf(stderr, "[qui-host] CefInitialize failed exit_code=%d\n",
        CefGetExitCode());
    return 4;
  }
  fprintf(stderr, "[qui-host] CefInitialize OK\n");

  // Hand the cgo bridge inside libqui_host.dylib a reference to the
  // already-initialized CEF runtime so its own ensureInit() short-circuits.
  if (qui_webview_cef_use_host_init() != 0) {
    fprintf(stderr, "[qui-host] qui_webview_cef_use_host_init failed\n");
    CefShutdown();
    return 6;
  }

  if (QuiStart() != 0) {
    CefShutdown();
    return 5;
  }

  int running = 1;
  while (running != 0) {
    running = QuiTick();
    CefDoMessageLoopWork();
    std::this_thread::sleep_for(std::chrono::milliseconds(16));
  }

  QuiStop();
  CefShutdown();
  return 0;
}
