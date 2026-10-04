// cefsimple_bridge_main.mm
// Browser-process entry that keeps cefsimple bundle/runtime, then
// dlopens libqui_host.dylib and drives QuiStart/QuiTick/QuiStop.

#include <Cocoa/Cocoa.h>
#include <dlfcn.h>

#include <chrono>
#include <thread>

#include "include/cef_app.h"
#include "include/cef_application_mac.h"
#include "include/wrapper/cef_helpers.h"
#include "include/wrapper/cef_library_loader.h"

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

using QuiStartFn = int (*)(void);
using QuiTickFn = int (*)(void);
using QuiStopFn = void (*)(void);

struct QuiHostAPI {
  void* handle = nullptr;
  QuiStartFn start = nullptr;
  QuiTickFn tick = nullptr;
  QuiStopFn stop = nullptr;
};

bool LoadQuiHost(QuiHostAPI* out) {
  NSString* dylibPath = [[[NSBundle mainBundle] bundlePath]
      stringByAppendingPathComponent:@"Contents/Frameworks/libqui_host.dylib"];
  if (!dylibPath) {
    fprintf(stderr, "[qui-cefsimple] invalid dylib path\n");
    return false;
  }
  out->handle = dlopen([dylibPath UTF8String], RTLD_NOW | RTLD_LOCAL);
  if (!out->handle) {
    fprintf(stderr, "[qui-cefsimple] dlopen failed: %s\n", dlerror());
    return false;
  }
  out->start = reinterpret_cast<QuiStartFn>(dlsym(out->handle, "QuiStart"));
  out->tick = reinterpret_cast<QuiTickFn>(dlsym(out->handle, "QuiTick"));
  out->stop = reinterpret_cast<QuiStopFn>(dlsym(out->handle, "QuiStop"));
  if (!out->start || !out->tick || !out->stop) {
    fprintf(stderr, "[qui-cefsimple] dlsym missing Qui* symbols\n");
    dlclose(out->handle);
    out->handle = nullptr;
    return false;
  }
  return true;
}

void UnloadQuiHost(QuiHostAPI* api) {
  if (api->handle) {
    dlclose(api->handle);
    api->handle = nullptr;
  }
}

}  // namespace

int main(int argc, char* argv[]) {
  CefScopedLibraryLoader library_loader;
  if (!library_loader.LoadInMain()) {
    fprintf(stderr, "[qui-cefsimple] LoadInMain failed\n");
    return 1;
  }

  CefMainArgs main_args(argc, argv);
  const int exit_code = CefExecuteProcess(main_args, nullptr, nullptr);
  if (exit_code >= 0) {
    return exit_code;
  }

  [QuiApplication sharedApplication];
  if (![NSApp conformsToProtocol:@protocol(CefAppProtocol)]) {
    fprintf(stderr, "[qui-cefsimple] NSApp missing CefAppProtocol\n");
    return 2;
  }

  CefSettings settings = {};
  settings.size = sizeof(settings);
  settings.no_sandbox = 1;
  settings.windowless_rendering_enabled = 1;
  if (!CefInitialize(main_args, settings, nullptr, nullptr)) {
    fprintf(stderr, "[qui-cefsimple] CefInitialize failed (exit=%d)\n",
            CefGetExitCode());
    return 3;
  }

  QuiHostAPI host;
  if (!LoadQuiHost(&host)) {
    CefShutdown();
    return 4;
  }
  if (host.start() != 0) {
    UnloadQuiHost(&host);
    CefShutdown();
    return 5;
  }

  int running = 1;
  while (running != 0) {
    running = host.tick();
    CefDoMessageLoopWork();
    std::this_thread::sleep_for(std::chrono::milliseconds(16));
  }

  host.stop();
  UnloadQuiHost(&host);
  CefShutdown();
  return 0;
}
