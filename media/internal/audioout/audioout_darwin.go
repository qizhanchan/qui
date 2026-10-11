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
    OSStatus st = AudioQueueNewOutput(fmt, aqCallback, (void*)(intptr_t)id, NULL, NULL, 0, q);
    if (st != 0) return st;
    UInt32 on = 1;
    AudioQueueSetProperty(*q, kAudioQueueProperty_EnableTimePitch, &on, sizeof(on));
    UInt32 algo = kAudioQueueTimePitchAlgorithm_Spectral;
    AudioQueueSetProperty(*q, kAudioQueueProperty_TimePitchAlgorithm, &algo, sizeof(algo));
    return 0;
}
static OSStatus aqAlloc(AudioQueueRef q, int bytes, AudioQueueBufferRef* buf) {
    return AudioQueueAllocateBuffer(q, bytes, buf);
}
static void aqSilence(AudioQueueBufferRef buf) {
    memset(buf->mAudioData, 0, buf->mAudioDataBytesCapacity);
    buf->mAudioDataByteSize = buf->mAudioDataBytesCapacity;
}
static OSStatus aqEnqueue(AudioQueueRef q, AudioQueueBufferRef buf) {
    return AudioQueueEnqueueBuffer(q, buf, 0, NULL);
}
static double aqSampleTime(AudioQueueRef q) {
    AudioTimeStamp ts;
    memset(&ts, 0, sizeof(ts));
    if (AudioQueueGetCurrentTime(q, NULL, &ts, NULL) != 0) return -1;
    if (!(ts.mFlags & kAudioTimeStampSampleTimeValid)) return -1;
    return ts.mSampleTime;
}
*/
import "C"

import (
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

const (
	aqBuffers      = 3
	aqBufferFrames = 2048
	// ringSeconds bounds how much decoded PCM Write accepts ahead of
	// playback.
	ringSeconds = 0.5
)

// sink is the AudioQueue-backed Sink. Rate (time-pitch) and volume are
// native queue parameters; the queue's sample time is the device clock.
type sink struct {
	*ring
	queue   C.AudioQueueRef
	id      int
	started atomic.Bool
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
	s.fill(unsafe.Slice((*byte)(buf), int(length)))
}

func open(format Format) (Sink, error) {
	if format.SampleRate <= 0 || format.Channels <= 0 {
		return nil, ErrNotSupported
	}
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
	s := &sink{ring: newRing(format, ringSeconds, false), queue: q, id: id}
	regMu.Lock()
	reg[id] = s
	regMu.Unlock()

	fb := 2 * format.Channels
	for i := 0; i < aqBuffers; i++ {
		var buf C.AudioQueueBufferRef
		if st := C.aqAlloc(q, C.int(aqBufferFrames*fb), &buf); st != 0 {
			s.Close()
			return nil, ErrNotSupported
		}
		// Prime with silence; the timeline accounts for it.
		C.aqSilence(buf)
		C.aqEnqueue(q, buf)
		s.ring.mu.Lock()
		s.ring.enqueued += aqBufferFrames
		s.ring.mu.Unlock()
	}
	return s, nil
}

func available() bool { return true }

func (s *sink) Write(pcm []byte) (int, error) { return s.write(pcm), nil }
func (s *sink) Buffered() time.Duration       { return s.buffered() }

// Flush drops queued PCM and restarts HostTime at zero. Up to the device
// buffers already enqueued (~0.1 s) of old audio may still be heard.
func (s *sink) Flush() { s.flush() }

func (s *sink) Play() error {
	s.started.Store(true)
	C.AudioQueueStart(s.queue, nil)
	return nil
}

func (s *sink) Pause() error {
	C.AudioQueuePause(s.queue)
	return nil
}

func (s *sink) SetVolume(v float32) {
	v = max(0, min(1, v))
	C.AudioQueueSetParameter(s.queue, C.kAudioQueueParam_Volume, C.Float32(v))
}

// SetRate sets the playback rate (0.5–2.0) with pitch preservation.
func (s *sink) SetRate(r float32) {
	r = max(0.5, min(2, r))
	C.AudioQueueSetParameter(s.queue, C.kAudioQueueParam_PlayRate, C.Float32(r))
}

// HostTime is the content time (since Open or the last Flush) currently
// being heard.
func (s *sink) HostTime() time.Duration {
	st := float64(-1)
	if s.started.Load() {
		st = float64(C.aqSampleTime(s.queue))
	}
	return s.hostTime(int64(st), st >= 0)
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
