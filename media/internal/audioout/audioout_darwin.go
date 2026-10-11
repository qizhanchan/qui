//go:build darwin && cgo

package audioout

/*
#cgo LDFLAGS: -framework AudioToolbox -framework CoreFoundation
#include <AudioToolbox/AudioToolbox.h>
#include <stdlib.h>
#include <string.h>

extern void goAudioRefill(int id, void* buf, int len);

static void aqCallback(void* userData, AudioQueueRef q, AudioQueueBufferRef buf) {
    goAudioRefill((int)(intptr_t)userData, buf->mAudioData, (int)buf->mAudioDataBytesCapacity);
    buf->mAudioDataByteSize = buf->mAudioDataBytesCapacity;
    AudioQueueEnqueueBuffer(q, buf, 0, NULL);
}

static OSStatus aqNew(AudioStreamBasicDescription* fmt, int id, AudioQueueRef* q) {
    return AudioQueueNewOutput(fmt, aqCallback, (void*)(intptr_t)id, NULL, NULL, 0, q);
}
static OSStatus aqAlloc(AudioQueueRef q, int bytes, AudioQueueBufferRef* buf) {
    return AudioQueueAllocateBuffer(q, bytes, buf);
}
static void aqFill(AudioQueueBufferRef buf, const void* src, int len) {
    memcpy(buf->mAudioData, src, len);
    buf->mAudioDataByteSize = len;
}
static OSStatus aqEnqueue(AudioQueueRef q, AudioQueueBufferRef buf) {
    return AudioQueueEnqueueBuffer(q, buf, 0, NULL);
}
*/
import "C"

import (
	"sync"
	"time"
	"unsafe"
)

const (
	aqBuffers    = 4
	aqBufferSize = 16384 // bytes per AudioQueue buffer
)

// sink is the AudioQueue-backed Sink.
type sink struct {
	queue C.AudioQueueRef
	id    int

	mu      sync.Mutex
	ring    []byte
	vol     float32
	started bool
}

var (
	regMu   sync.Mutex
	regNext int
	reg     = map[int]*sink{}
)

//export goAudioRefill
func goAudioRefill(id C.int, buf unsafe.Pointer, length C.int) {
	regMu.Lock()
	s := reg[int(id)]
	regMu.Unlock()
	if s == nil {
		return
	}
	n := int(length)
	out := unsafe.Slice((*byte)(buf), n)
	s.mu.Lock()
	copied := copy(out, s.ring)
	if copied < n {
		for i := copied; i < n; i++ {
			out[i] = 0
		}
	}
	s.ring = s.ring[copied:]
	s.mu.Unlock()
}

func open(format Format) (Sink, error) {
	var asbd C.AudioStreamBasicDescription
	asbd.mSampleRate = C.double(format.SampleRate)
	asbd.mFormatID = C.kAudioFormatLinearPCM
	asbd.mFormatFlags = C.kLinearPCMFormatFlagIsSignedInteger | C.kLinearPCMFormatFlagIsPacked
	asbd.mBytesPerPacket = C.UInt32(2 * format.Channels)
	asbd.mFramesPerPacket = 1
	asbd.mBytesPerFrame = C.UInt32(2 * format.Channels)
	asbd.mChannelsPerFrame = C.UInt32(format.Channels)
	asbd.mBitsPerChannel = 16

	regMu.Lock()
	id := regNext
	regNext++
	regMu.Unlock()

	var q C.AudioQueueRef
	if st := C.aqNew(&asbd, C.int(id), &q); st != 0 {
		return nil, ErrNotSupported
	}
	s := &sink{queue: q, id: id, vol: 1}
	regMu.Lock()
	reg[id] = s
	regMu.Unlock()

	for i := 0; i < aqBuffers; i++ {
		var buf C.AudioQueueBufferRef
		if st := C.aqAlloc(q, aqBufferSize, &buf); st != 0 {
			s.Close()
			return nil, ErrNotSupported
		}
		// Prime with silence so playback can start immediately.
		var silence [1]byte
		C.aqFill(buf, unsafe.Pointer(&silence[0]), 0)
		C.aqEnqueue(q, buf)
	}
	return s, nil
}

func available() bool { return true }

func (s *sink) Write(pcm []byte) (int, error) {
	s.mu.Lock()
	s.ring = append(s.ring, pcm...)
	s.mu.Unlock()
	if !s.started {
		_ = s.Play()
	}
	return len(pcm), nil
}

func (s *sink) Play() error {
	s.mu.Lock()
	s.started = true
	s.mu.Unlock()
	C.AudioQueueStart(s.queue, nil)
	return nil
}

func (s *sink) Pause() error {
	C.AudioQueuePause(s.queue)
	return nil
}

func (s *sink) SetVolume(v float32) {
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	s.mu.Lock()
	s.vol = v
	s.mu.Unlock()
	C.AudioQueueSetParameter(s.queue, C.kAudioQueueParam_Volume, C.Float32(v))
}

func (s *sink) HostTime() time.Duration {
	var ts C.AudioTimeStamp
	var disc C.Boolean
	C.AudioQueueGetCurrentTime(s.queue, nil, &ts, &disc)
	// Prefer the sample time when available.
	secs := float64(ts.mSampleTime) / float64(48000)
	if secs <= 0 {
		secs = float64(ts.mHostTime) / 1e9
	}
	return time.Duration(secs * float64(time.Second))
}

func (s *sink) Close() error {
	if s.queue != nil {
		C.AudioQueueStop(s.queue, C.true)
		C.AudioQueueDispose(s.queue, C.true)
		s.queue = nil
	}
	regMu.Lock()
	delete(reg, s.id)
	regMu.Unlock()
	return nil
}
