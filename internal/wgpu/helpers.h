#ifndef QUI_WGPU_HELPERS_H
#define QUI_WGPU_HELPERS_H

#include <stddef.h>
#include <stdint.h>
#include "dawn_webgpu.h"

int quiWGPUOpen(const char* path, char* err, size_t errlen);
WGPUInstance quiWGPUCreateInstance(void);
WGPUAdapter quiWGPURequestAdapter(WGPUInstance inst, WGPUSurface compat, int highPower,
                                  char* err, size_t errlen);
WGPUDevice quiWGPURequestDevice(WGPUInstance inst, WGPUAdapter adapter, const WGPUFeatureName* feats,
                                size_t nfeats, char* err, size_t errlen);
int quiWGPUWaitQueue(WGPUInstance inst, WGPUQueue queue);
int quiWGPUMapRead(WGPUInstance inst, WGPUBuffer buf, size_t off, size_t size);
void quiWGPUAdapterName(WGPUAdapter adapter, char* out, size_t n);
int quiWGPUPopErrorScope(WGPUInstance inst, WGPUDevice dev, char* err, size_t errlen);

#endif
