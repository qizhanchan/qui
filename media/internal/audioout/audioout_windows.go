//go:build windows && (amd64 || arm64)

package audioout

import (
	"errors"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// The WASAPI sink is plain syscalls into COM — no cgo. Every COM call runs
// on one OS-locked goroutine; control methods only touch the shared ring.
// Shared mode with AUTOCONVERTPCM lets the audio engine convert our 16-bit
// stream to the device mix format.

const (
	ringSeconds = 0.5
	// wasapiBuffer is the requested device buffer (100 ns units): pause and
	// flush lag by at most this much.
	wasapiBuffer = 50 * 10000
	wasapiChunk  = 10 * time.Millisecond
	// wasapiLowWater: below this much queued device audio the loop pads
	// with silence; above it, it writes only real content, so a
	// momentarily empty queue never puts a gap into the stream.
	wasapiLowWater = 30 * time.Millisecond

	clsctxAll                  = 0x17
	audclntSharemodeShared     = 0
	audclntStreamflagsAutoconv = 0x80000000
	audclntStreamflagsSRCDefQ  = 0x08000000
	waveFormatPCM              = 1
	eRender, eConsole          = 0, 0
)

type guid struct {
	d1     uint32
	d2, d3 uint16
	d4     [8]byte
}

var (
	clsidMMDeviceEnumerator = guid{0xBCDE0395, 0xE52F, 0x467C, [8]byte{0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E}}
	iidIMMDeviceEnumerator  = guid{0xA95664D2, 0x9614, 0x4F35, [8]byte{0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6}}
	iidIAudioClient         = guid{0x1CB9AD4C, 0xDBFA, 0x4C32, [8]byte{0xB1, 0x78, 0xC2, 0xF5, 0x68, 0xA7, 0x03, 0xB2}}
	iidIAudioRenderClient   = guid{0xF294ACFC, 0x3146, 0x4483, [8]byte{0xA7, 0xBF, 0xAD, 0xDC, 0xA7, 0xC2, 0x60, 0xE2}}

	ole32            = syscall.NewLazyDLL("ole32.dll")
	coInitializeEx   = ole32.NewProc("CoInitializeEx")
	coUninitialize   = ole32.NewProc("CoUninitialize")
	coCreateInstance = ole32.NewProc("CoCreateInstance")
)

// waveFormatEx is WAVEFORMATEX (cbSize = 0, so the tail padding is unused).
type waveFormatEx struct {
	formatTag      uint16
	channels       uint16
	samplesPerSec  uint32
	avgBytesPerSec uint32
	blockAlign     uint16
	bitsPerSample  uint16
	cbSize         uint16
}

// Vtable slots (IUnknown occupies 0–2).
const (
	vtRelease = 2

	vtEnumGetDefaultAudioEndpoint = 4
	vtDeviceActivate              = 3

	vtClientInitialize        = 3
	vtClientGetBufferSize     = 4
	vtClientGetCurrentPadding = 6
	vtClientStart             = 10
	vtClientStop              = 11
	vtClientGetService        = 14

	vtRenderGetBuffer     = 3
	vtRenderReleaseBuffer = 4
)

func comCall(obj unsafe.Pointer, slot int, args ...uintptr) int32 {
	vtbl := *(*unsafe.Pointer)(obj)
	fn := *(*uintptr)(unsafe.Add(vtbl, slot*int(unsafe.Sizeof(uintptr(0)))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{uintptr(obj)}, args...)...)
	return int32(r)
}

func comRelease(obj unsafe.Pointer) {
	if obj != nil {
		comCall(obj, vtRelease)
	}
}

var errWASAPI = errors.New("audioout: WASAPI call failed")

func available() bool { return ole32.Load() == nil }

// sink is the WASAPI Sink.
type sink struct {
	*ring
	quit chan struct{}
	done chan struct{}

	clk       sync.Mutex
	committed int64 // device frames handed to WASAPI
	padding   int64 // frames still queued at sampleAt
	sampleAt  time.Time

	closeOnce sync.Once
}

func open(format Format) (Sink, error) {
	if format.SampleRate <= 0 || format.Channels <= 0 || !available() {
		return nil, ErrNotSupported
	}
	s := &sink{ring: newRing(format, ringSeconds, true),
		quit: make(chan struct{}), done: make(chan struct{})}
	s.setPaused(true)
	ready := make(chan error, 1)
	go s.run(format, ready)
	if err := <-ready; err != nil {
		return nil, ErrNotSupported
	}
	return s, nil
}

// run owns the COM objects for the sink's lifetime.
func (s *sink) run(format Format, ready chan<- error) {
	defer close(s.done)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if hr, _, _ := coInitializeEx.Call(0, 0); int32(hr) < 0 {
		ready <- errWASAPI
		return
	}
	defer coUninitialize.Call()

	var enum, dev, client, render unsafe.Pointer
	defer func() {
		comRelease(render)
		comRelease(client)
		comRelease(dev)
		comRelease(enum)
	}()
	hr, _, _ := coCreateInstance.Call(uintptr(unsafe.Pointer(&clsidMMDeviceEnumerator)), 0, clsctxAll,
		uintptr(unsafe.Pointer(&iidIMMDeviceEnumerator)), uintptr(unsafe.Pointer(&enum)))
	if int32(hr) < 0 || enum == nil {
		ready <- errWASAPI
		return
	}
	if comCall(enum, vtEnumGetDefaultAudioEndpoint, eRender, eConsole, uintptr(unsafe.Pointer(&dev))) < 0 {
		ready <- errWASAPI
		return
	}
	if comCall(dev, vtDeviceActivate, uintptr(unsafe.Pointer(&iidIAudioClient)), clsctxAll, 0, uintptr(unsafe.Pointer(&client))) < 0 {
		ready <- errWASAPI
		return
	}
	fb := 2 * format.Channels
	wf := waveFormatEx{
		formatTag: waveFormatPCM, channels: uint16(format.Channels),
		samplesPerSec: uint32(format.SampleRate), avgBytesPerSec: uint32(format.SampleRate * fb),
		blockAlign: uint16(fb), bitsPerSample: 16,
	}
	if comCall(client, vtClientInitialize, audclntSharemodeShared,
		audclntStreamflagsAutoconv|audclntStreamflagsSRCDefQ, wasapiBuffer, 0,
		uintptr(unsafe.Pointer(&wf)), 0) < 0 {
		ready <- errWASAPI
		return
	}
	var bufFrames uint32
	if comCall(client, vtClientGetBufferSize, uintptr(unsafe.Pointer(&bufFrames))) < 0 || bufFrames == 0 {
		ready <- errWASAPI
		return
	}
	if comCall(client, vtClientGetService, uintptr(unsafe.Pointer(&iidIAudioRenderClient)), uintptr(unsafe.Pointer(&render))) < 0 {
		ready <- errWASAPI
		return
	}
	ready <- nil

	chunk := uint32(float64(format.SampleRate) * wasapiChunk.Seconds())
	lowWater := uint32(float64(format.SampleRate) * wasapiLowWater.Seconds())
	started := false
	tick := time.NewTicker(wasapiChunk / 2)
	defer tick.Stop()
	for {
		var pad uint32
		if comCall(client, vtClientGetCurrentPadding, uintptr(unsafe.Pointer(&pad))) < 0 {
			return
		}
		avail := bufFrames - pad
		if started && pad > lowWater {
			avail = min(avail, uint32(s.fillable()))
		}
		if avail > 0 && (avail >= chunk || !started) {
			var data unsafe.Pointer
			if comCall(render, vtRenderGetBuffer, uintptr(avail), uintptr(unsafe.Pointer(&data))) >= 0 && data != nil {
				s.fill(unsafe.Slice((*byte)(data), int(avail)*fb))
				comCall(render, vtRenderReleaseBuffer, uintptr(avail), 0)
				pad += avail
				s.clk.Lock()
				s.committed += int64(avail)
				s.clk.Unlock()
			}
			if !started {
				comCall(client, vtClientStart)
				started = true
			}
		}
		s.clk.Lock()
		s.padding = int64(pad)
		s.sampleAt = time.Now()
		s.clk.Unlock()
		select {
		case <-s.quit:
			comCall(client, vtClientStop)
			return
		case <-tick.C:
		}
	}
}

func (s *sink) Write(pcm []byte) (int, error) { return s.write(pcm), nil }
func (s *sink) Buffered() time.Duration       { return s.buffered() }
func (s *sink) Flush()                        { s.flush() }
func (s *sink) Play() error {
	s.setPaused(false)
	s.setHold(false)
	return nil
}

// Pause switches the device feed to silence; the clock holds at once,
// while up to the device buffer (~50 ms) of audio still drains.
func (s *sink) Pause() error {
	s.HostTime() // refresh the position the hold freezes
	s.setHold(true)
	s.setPaused(true)
	return nil
}
func (s *sink) SetVolume(v float32) { s.setVolume(v) }

// SetRate changes speed by resampling; the pitch follows the rate.
func (s *sink) SetRate(r float32) { s.setSpeed(r) }

func (s *sink) HostTime() time.Duration {
	s.clk.Lock()
	committed, pad, at := s.committed, s.padding, s.sampleAt
	s.clk.Unlock()
	if at.IsZero() {
		return s.hostTime(0, false)
	}
	dev := committed - pad + int64(time.Since(at).Seconds()*float64(s.rate))
	return s.hostTime(min(dev, committed), true)
}

func (s *sink) Close() error {
	s.closeOnce.Do(func() {
		close(s.quit)
		<-s.done
	})
	return nil
}
