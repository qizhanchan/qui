//go:build cgo && !js

// Loader and synchronous wrappers around WebGPU's asynchronous calls.
//
// Every future is waited for with wgpuInstanceWaitAny (WaitAnyOnly
// callback mode on an instance created with TimedWaitAny), so callbacks
// run on the calling thread, inside the wait, and nothing ever calls back
// into Go from a thread Go does not know — except the uncaptured-error and
// device-lost callbacks, which forward to quiWGPUGoError.

#include "helpers.h"

#include <stdio.h>
#include <string.h>

#ifdef _WIN32
#include <windows.h>
static HMODULE quiLib;
static void* quiSym(const char* name) { return (void*)GetProcAddress(quiLib, name); }
#else
#include <dlfcn.h>
static void* quiLib;
static void* quiSym(const char* name) { return dlsym(quiLib, name); }
#endif

#include "_cgo_export.h"

int quiWGPULoadProcs(void* (*sym)(const char*));

int quiWGPUOpen(const char* path, char* err, size_t errlen) {
#ifdef _WIN32
    quiLib = LoadLibraryA(path);
    if (quiLib == NULL) {
        snprintf(err, errlen, "LoadLibrary failed (%lu)", (unsigned long)GetLastError());
        return -1;
    }
#else
    quiLib = dlopen(path, RTLD_NOW | RTLD_LOCAL);
    if (quiLib == NULL) {
        const char* e = dlerror();
        snprintf(err, errlen, "%s", e ? e : "dlopen failed");
        return -1;
    }
#endif
    int missing = quiWGPULoadProcs(quiSym);
    if (missing > 0) snprintf(err, errlen, "%d WebGPU entry points missing", missing);
    return missing;
}

static void quiCopyMsg(char* dst, size_t n, WGPUStringView m) {
    if (n == 0) return;
    size_t len = m.data == NULL ? 0 : (m.length == WGPU_STRLEN ? strlen(m.data) : m.length);
    if (len >= n) len = n - 1;
    if (len) memcpy(dst, m.data, len);
    dst[len] = 0;
}

static int quiWait(WGPUInstance inst, WGPUFuture f) {
    WGPUFutureWaitInfo w = {f, 0};
    WGPUWaitStatus s = wgpuInstanceWaitAny(inst, 1, &w, UINT64_MAX);
    return s == WGPUWaitStatus_Success && w.completed;
}

WGPUInstance quiWGPUCreateInstance(void) {
    WGPUInstanceFeatureName feats[] = {WGPUInstanceFeatureName_TimedWaitAny};
    WGPUInstanceDescriptor d = {0};
    d.requiredFeatureCount = 1;
    d.requiredFeatures = feats;
    return wgpuCreateInstance(&d);
}

typedef struct {
    int done;
    int ok;
    void* handle;
    char* err;
    size_t errlen;
} quiResult;

static void quiOnAdapter(WGPURequestAdapterStatus status, WGPUAdapter adapter, WGPUStringView msg,
                         void* u1, void* u2) {
    (void)u2;
    quiResult* r = (quiResult*)u1;
    r->done = 1;
    r->ok = status == WGPURequestAdapterStatus_Success;
    r->handle = adapter;
    if (!r->ok) quiCopyMsg(r->err, r->errlen, msg);
}

WGPUAdapter quiWGPURequestAdapter(WGPUInstance inst, WGPUSurface compat, int highPower,
                                  char* err, size_t errlen) {
    WGPURequestAdapterOptions o = {0};
    o.featureLevel = WGPUFeatureLevel_Core;
    o.powerPreference = highPower ? WGPUPowerPreference_HighPerformance : WGPUPowerPreference_LowPower;
    o.compatibleSurface = compat;
    quiResult r = {0, 0, NULL, err, errlen};
    WGPURequestAdapterCallbackInfo cb = {0};
    cb.mode = WGPUCallbackMode_WaitAnyOnly;
    cb.callback = quiOnAdapter;
    cb.userdata1 = &r;
    if (!quiWait(inst, wgpuInstanceRequestAdapter(inst, &o, cb)) || !r.ok) {
        if (r.ok == 0 && err[0] == 0) snprintf(err, errlen, "no WebGPU adapter");
        return NULL;
    }
    return (WGPUAdapter)r.handle;
}

static void quiOnDevice(WGPURequestDeviceStatus status, WGPUDevice device, WGPUStringView msg,
                        void* u1, void* u2) {
    (void)u2;
    quiResult* r = (quiResult*)u1;
    r->done = 1;
    r->ok = status == WGPURequestDeviceStatus_Success;
    r->handle = device;
    if (!r->ok) quiCopyMsg(r->err, r->errlen, msg);
}

static void quiOnError(WGPUDevice const* device, WGPUErrorType type, WGPUStringView msg,
                       void* u1, void* u2) {
    (void)device; (void)u1; (void)u2;
    size_t len = msg.data == NULL ? 0 : (msg.length == WGPU_STRLEN ? strlen(msg.data) : msg.length);
    quiWGPUGoError((int)type, (char*)msg.data, (int)len);
}

static void quiOnLost(WGPUDevice const* device, WGPUDeviceLostReason reason, WGPUStringView msg,
                      void* u1, void* u2) {
    (void)device; (void)u1; (void)u2;
    if (reason == WGPUDeviceLostReason_Destroyed || reason == WGPUDeviceLostReason_CallbackCancelled) return;
    size_t len = msg.data == NULL ? 0 : (msg.length == WGPU_STRLEN ? strlen(msg.data) : msg.length);
    quiWGPUGoError(-1, (char*)msg.data, (int)len);
}

WGPUDevice quiWGPURequestDevice(WGPUInstance inst, WGPUAdapter adapter, const WGPUFeatureName* feats,
                                size_t nfeats, char* err, size_t errlen) {
    WGPUDeviceDescriptor d = {0};
    d.requiredFeatureCount = nfeats;
    d.requiredFeatures = feats;
    d.deviceLostCallbackInfo.mode = WGPUCallbackMode_AllowSpontaneous;
    d.deviceLostCallbackInfo.callback = quiOnLost;
    d.uncapturedErrorCallbackInfo.callback = quiOnError;
    quiResult r = {0, 0, NULL, err, errlen};
    WGPURequestDeviceCallbackInfo cb = {0};
    cb.mode = WGPUCallbackMode_WaitAnyOnly;
    cb.callback = quiOnDevice;
    cb.userdata1 = &r;
    if (!quiWait(inst, wgpuAdapterRequestDevice(adapter, &d, cb)) || !r.ok) {
        if (err[0] == 0) snprintf(err, errlen, "device request failed");
        return NULL;
    }
    return (WGPUDevice)r.handle;
}

static void quiOnWorkDone(WGPUQueueWorkDoneStatus status, WGPUStringView msg, void* u1, void* u2) {
    (void)msg; (void)u2;
    *(int*)u1 = status == WGPUQueueWorkDoneStatus_Success ? 1 : -1;
}

int quiWGPUWaitQueue(WGPUInstance inst, WGPUQueue queue) {
    int st = 0;
    WGPUQueueWorkDoneCallbackInfo cb = {0};
    cb.mode = WGPUCallbackMode_WaitAnyOnly;
    cb.callback = quiOnWorkDone;
    cb.userdata1 = &st;
    return quiWait(inst, wgpuQueueOnSubmittedWorkDone(queue, cb)) && st == 1;
}

static void quiOnMap(WGPUMapAsyncStatus status, WGPUStringView msg, void* u1, void* u2) {
    (void)msg; (void)u2;
    *(int*)u1 = status == WGPUMapAsyncStatus_Success ? 1 : -1;
}

int quiWGPUMapRead(WGPUInstance inst, WGPUBuffer buf, size_t off, size_t size) {
    int st = 0;
    WGPUBufferMapCallbackInfo cb = {0};
    cb.mode = WGPUCallbackMode_WaitAnyOnly;
    cb.callback = quiOnMap;
    cb.userdata1 = &st;
    return quiWait(inst, wgpuBufferMapAsync(buf, WGPUMapMode_Read, off, size, cb)) && st == 1;
}

// quiWGPUAdapterName writes "description (backend)" into out.
void quiWGPUAdapterName(WGPUAdapter adapter, char* out, size_t n) {
    WGPUAdapterInfo info = {0};
    if (wgpuAdapterGetInfo(adapter, &info) != WGPUStatus_Success) {
        snprintf(out, n, "unknown adapter");
        return;
    }
    char dev[256];
    quiCopyMsg(dev, sizeof dev, info.device);
    if (dev[0] == 0) quiCopyMsg(dev, sizeof dev, info.description);
    const char* be = "?";
    switch (info.backendType) {
    case WGPUBackendType_Metal: be = "Metal"; break;
    case WGPUBackendType_Vulkan: be = "Vulkan"; break;
    case WGPUBackendType_D3D12: be = "D3D12"; break;
    case WGPUBackendType_D3D11: be = "D3D11"; break;
    case WGPUBackendType_OpenGL: be = "OpenGL"; break;
    case WGPUBackendType_OpenGLES: be = "OpenGLES"; break;
    default: break;
    }
    snprintf(out, n, "%s (%s)", dev, be);
    wgpuAdapterInfoFreeMembers(info);
}

// quiWGPUCompileStatus pops the validation error scope a shader-module or
// pipeline creation was wrapped in and copies its message to err (empty
// when the object is valid).
static void quiOnPop(WGPUPopErrorScopeStatus status, WGPUErrorType type, WGPUStringView msg,
                     void* u1, void* u2) {
    quiResult* r = (quiResult*)u1;
    (void)u2;
    r->done = 1;
    r->ok = status == WGPUPopErrorScopeStatus_Success && type == WGPUErrorType_NoError;
    if (!r->ok) quiCopyMsg(r->err, r->errlen, msg);
}

int quiWGPUPopErrorScope(WGPUInstance inst, WGPUDevice dev, char* err, size_t errlen) {
    quiResult r = {0, 0, NULL, err, errlen};
    WGPUPopErrorScopeCallbackInfo cb = {0};
    cb.mode = WGPUCallbackMode_WaitAnyOnly;
    cb.callback = quiOnPop;
    cb.userdata1 = &r;
    if (!quiWait(inst, wgpuDevicePopErrorScope(dev, cb))) return 0;
    return r.ok;
}
