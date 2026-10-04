// main.mm — CEF helper-process entry point.
//
// macOS multi-process CEF spawns this binary five times (once per role:
// GPU / Plugin / Renderer / Alerts / default subprocess) from
// Contents/Frameworks/<App> Helper*.app/Contents/MacOS/<App> Helper*.
// CEF picks the role via command-line args; CefExecuteProcess returns
// the exit code synchronously once that subprocess is done.
//
// Phase C adds the renderer-side half of the JS↔Go bridge here. When
// CefExecuteProcess is dispatched into a renderer subprocess (process
// type "renderer"), the QuiHelperApp::GetRenderProcessHandler hook
// installs QuiRenderProcessHandler, which:
//
//   * OnContextCreated injects a `window.qui` JS Proxy whose `get` trap
//     returns a function that, when called, builds a CefProcessMessage
//     and ships it to PID_BROWSER. The function returns a JS Promise.
//   * OnProcessMessageReceived listens for "qui.reply" coming back from
//     the browser process, looks up the pending promise by message-id,
//     and resolves or rejects it inside the original V8 context.
//
// All renderer-side state (pending promises, message-id counter) is
// per-process and protected by a single mutex because CEF's V8 thread
// (TID_RENDERER) is single-threaded but OnProcessMessageReceived can
// arrive between calls — keeping the mutex makes the map manipulation
// obvious even though contention is practically zero.

#include "include/cef_app.h"
#include "include/cef_browser.h"
#include "include/cef_frame.h"
#include "include/cef_process_message.h"
#include "include/cef_render_process_handler.h"
#include "include/cef_v8.h"
#include "include/cef_values.h"
#include "include/wrapper/cef_library_loader.h"

#include <atomic>
#include <mutex>
#include <string>
#include <unordered_map>

// When CEF sandbox is enabled at build time, helper processes should
// initialize the macOS sandbox before loading libcef.
#if defined(CEF_USE_SANDBOX)
#include "include/cef_sandbox_mac.h"
#endif

namespace {

// PendingCall holds a JS Promise + its V8 context until the matching
// browser-side reply lands. The CefRefPtr keeps the context alive long
// enough to Enter()/Exit() it from OnProcessMessageReceived; without
// the explicit ref it would be GC'd when the original Execute() call
// returned.
struct PendingCall {
    CefRefPtr<CefV8Value> promise;
    CefRefPtr<CefV8Context> context;
};

std::mutex g_pending_mu;
std::unordered_map<int, PendingCall> g_pending;
std::atomic<int> g_next_message_id{1};

// QuiV8Handler is the native function backing window.qui.__invoke.
// Called as __invoke(name, payload), returns a Promise. JS-side glue
// (installed via ExecuteJavaScript) wraps this in a Proxy so call sites
// see `window.qui.<name>(payload)` instead of needing to spell out the
// dispatch.
class QuiV8Handler : public CefV8Handler {
public:
    QuiV8Handler() = default;
    bool Execute(const CefString& /*name*/,
                 CefRefPtr<CefV8Value> /*object*/,
                 const CefV8ValueList& arguments,
                 CefRefPtr<CefV8Value>& retval,
                 CefString& exception) override {
        if (arguments.size() < 2 || !arguments[0]->IsString()) {
            exception = "qui.invoke: expected (name: string, payload: string)";
            return true;
        }
        CefString handler_name = arguments[0]->GetStringValue();
        std::string payload;
        if (arguments[1]->IsString()) {
            payload = arguments[1]->GetStringValue().ToString();
        } else if (!arguments[1]->IsNull() && !arguments[1]->IsUndefined()) {
            exception = "qui.invoke: payload must be a string "
                        "(JSON.stringify your value before calling)";
            return true;
        }

        CefRefPtr<CefV8Context> context = CefV8Context::GetCurrentContext();
        if (!context) {
            exception = "qui.invoke: no current V8 context";
            return true;
        }
        CefRefPtr<CefV8Value> promise = CefV8Value::CreatePromise();
        if (!promise) {
            exception = "qui.invoke: CefV8Value::CreatePromise returned null";
            return true;
        }

        int id = g_next_message_id.fetch_add(1);
        {
            std::lock_guard<std::mutex> g(g_pending_mu);
            g_pending[id] = PendingCall{promise, context};
        }

        // Build the process message and ship it to the browser. We pull
        // the frame off the V8 context so the reply routes back to the
        // same frame (the browser-side reply currently uses GetMainFrame
        // but the symmetry is easy to keep).
        CefRefPtr<CefFrame> frame = context->GetFrame();
        if (!frame) {
            std::lock_guard<std::mutex> g(g_pending_mu);
            g_pending.erase(id);
            exception = "qui.invoke: V8 context has no frame";
            return true;
        }
        auto msg = CefProcessMessage::Create("qui.call");
        auto args = msg->GetArgumentList();
        args->SetString(0, handler_name);
        args->SetInt(1, id);
        args->SetString(2, payload);
        frame->SendProcessMessage(PID_BROWSER, msg);

        retval = promise;
        return true;
    }

private:
    IMPLEMENT_REFCOUNTING(QuiV8Handler);
    DISALLOW_COPY_AND_ASSIGN(QuiV8Handler);
};

// QuiRenderProcessHandler installs the bridge on every new V8 context.
class QuiRenderProcessHandler : public CefRenderProcessHandler {
public:
    QuiRenderProcessHandler() = default;
    void OnContextCreated(CefRefPtr<CefBrowser> /*browser*/,
                          CefRefPtr<CefFrame> /*frame*/,
                          CefRefPtr<CefV8Context> context) override {
        if (!context || !context->Enter()) {
            return;
        }
        CefRefPtr<CefV8Value> global = context->GetGlobal();
        if (!global) {
            context->Exit();
            return;
        }

        // window.__qui_invoke is the bare native function. We install
        // it first, then run a small JS shim to wrap it in a Proxy so
        // call sites can use the natural `window.qui.<name>(...)` form.
        CefRefPtr<CefV8Handler> handler(new QuiV8Handler());
        CefRefPtr<CefV8Value> invokeFn =
            CefV8Value::CreateFunction("__qui_invoke", handler);
        global->SetValue("__qui_invoke", invokeFn,
                         V8_PROPERTY_ATTRIBUTE_DONTENUM);

        // Install the Proxy. The Proxy's get-trap returns a function that
        // forwards (payload) into __qui_invoke(name, payload). Using a
        // Proxy means handler names can be added/removed Go-side without
        // re-injecting JS — the trap fires per call.
        //
        // Reserved keys: `then` is excluded so `Promise.resolve(window.qui)`
        // doesn't think the bridge itself is a thenable; that would
        // accidentally invoke any handler literally named "then". Same
        // story for Symbol-keyed lookups (the trap is only invoked for
        // string keys here because Symbol passes through the |name|
        // function param as a Symbol object that we punt on).
        const char* shim =
            "(function(){"
            "  if (typeof window === 'undefined') return;"
            "  var invoke = window.__qui_invoke;"
            "  if (!invoke) return;"
            "  window.qui = new Proxy(Object.create(null), {"
            "    get: function(_, prop) {"
            "      if (typeof prop !== 'string') return undefined;"
            "      if (prop === 'then') return undefined;"
            "      return function(payload) {"
            "        if (payload === undefined || payload === null) payload = '';"
            "        else if (typeof payload !== 'string') payload = JSON.stringify(payload);"
            "        return invoke(prop, payload);"
            "      };"
            "    },"
            "    has: function(_, prop) { return typeof prop === 'string' && prop !== 'then'; }"
            "  });"
            "})();";
        CefRefPtr<CefV8Value> retval;
        CefRefPtr<CefV8Exception> ex;
        context->Eval(shim, "qui_bridge.js", 0, retval, ex);

        context->Exit();
    }

    void OnContextReleased(CefRefPtr<CefBrowser> /*browser*/,
                           CefRefPtr<CefFrame> /*frame*/,
                           CefRefPtr<CefV8Context> context) override {
        // Drop any pending promises tied to this context so they don't
        // keep references alive forever. Iterate-and-erase pattern is
        // safe with the manual mutex.
        std::lock_guard<std::mutex> g(g_pending_mu);
        for (auto it = g_pending.begin(); it != g_pending.end(); ) {
            if (it->second.context.get() == context.get()) {
                it = g_pending.erase(it);
            } else {
                ++it;
            }
        }
    }

    bool OnProcessMessageReceived(CefRefPtr<CefBrowser> /*browser*/,
                                  CefRefPtr<CefFrame> /*frame*/,
                                  CefProcessId source_process,
                                  CefRefPtr<CefProcessMessage> message) override {
        if (source_process != PID_BROWSER || !message || !message->IsValid()) {
            return false;
        }
        if (message->GetName() != "qui.reply") {
            return false;
        }
        auto args = message->GetArgumentList();
        if (!args || args->GetSize() < 3) {
            return false;
        }
        int id = args->GetInt(0);
        std::string reply = args->GetString(1).ToString();
        std::string err = args->GetString(2).ToString();

        PendingCall pc;
        bool found = false;
        {
            std::lock_guard<std::mutex> g(g_pending_mu);
            auto it = g_pending.find(id);
            if (it != g_pending.end()) {
                pc = it->second;
                g_pending.erase(it);
                found = true;
            }
        }
        if (!found) {
            return true; // routed but stale
        }
        if (!pc.context || !pc.promise || !pc.context->Enter()) {
            return true;
        }
        if (!err.empty()) {
            pc.promise->RejectPromise(err);
        } else {
            // Reply travels as a string (JSON, opaque to the bridge).
            CefRefPtr<CefV8Value> arg = CefV8Value::CreateString(reply);
            pc.promise->ResolvePromise(arg);
        }
        pc.context->Exit();
        return true;
    }

private:
    IMPLEMENT_REFCOUNTING(QuiRenderProcessHandler);
    DISALLOW_COPY_AND_ASSIGN(QuiRenderProcessHandler);
};

// QuiHelperApp wires the render-process handler into the helper
// subprocess. Browser-process logic lives in webview/backend_darwin.cc
// (QuiCefApp) — they're intentionally separate types so each side only
// pays for the handlers it needs.
class QuiHelperApp : public CefApp, public CefRenderProcessHandler {
public:
    QuiHelperApp() : render_handler_(new QuiRenderProcessHandler()) {}

    CefRefPtr<CefRenderProcessHandler> GetRenderProcessHandler() override {
        return render_handler_;
    }

private:
    CefRefPtr<QuiRenderProcessHandler> render_handler_;

    IMPLEMENT_REFCOUNTING(QuiHelperApp);
    DISALLOW_COPY_AND_ASSIGN(QuiHelperApp);
};

}  // namespace

int main(int argc, char* argv[]) {
#if defined(CEF_USE_SANDBOX)
    CefScopedSandboxContext sandbox_context;
    if (!sandbox_context.Initialize(argc, argv)) {
        return 1;
    }
#endif

    CefScopedLibraryLoader library_loader;
    if (!library_loader.LoadInHelper()) {
        return 1;
    }

    CefMainArgs main_args(argc, argv);
    CefRefPtr<CefApp> app(new QuiHelperApp());
    return CefExecuteProcess(main_args, app, nullptr);
}
