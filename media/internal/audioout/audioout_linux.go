//go:build linux && cgo

package audioout

/*
#cgo LDFLAGS: -ldl
#include <dlfcn.h>
#include <errno.h>
#include <stddef.h>

// ALSA is loaded with dlopen, so building needs no ALSA headers and a
// machine without libasound simply reports ErrNotSupported. Enum values
// are the stable ALSA ABI: SND_PCM_STREAM_PLAYBACK = 0,
// SND_PCM_FORMAT_S16_LE = 2, SND_PCM_ACCESS_RW_INTERLEAVED = 3,
// SND_PCM_NONBLOCK = 1, SND_PCM_STATE_PREPARED = 2.
static struct {
    int  (*open)(void**, const char*, int, int);
    int  (*set_params)(void*, int, int, unsigned, unsigned, int, unsigned);
    long (*writei)(void*, const void*, unsigned long);
    int  (*recover)(void*, int, int);
    int  (*delay)(void*, long*);
    int  (*drop)(void*);
    int  (*prepare)(void*);
    int  (*pause)(void*, int);
    int  (*close)(void*);
    int  (*state)(void*);
    int  (*start)(void*);
    int  (*wait)(void*, int);
} A;

static int alsa_load(void) {
    void* h = dlopen("libasound.so.2", RTLD_NOW | RTLD_LOCAL);
    if (!h) return 0;
    A.open = dlsym(h, "snd_pcm_open");
    A.set_params = dlsym(h, "snd_pcm_set_params");
    A.writei = dlsym(h, "snd_pcm_writei");
    A.recover = dlsym(h, "snd_pcm_recover");
    A.delay = dlsym(h, "snd_pcm_delay");
    A.drop = dlsym(h, "snd_pcm_drop");
    A.prepare = dlsym(h, "snd_pcm_prepare");
    A.pause = dlsym(h, "snd_pcm_pause");
    A.close = dlsym(h, "snd_pcm_close");
    A.state = dlsym(h, "snd_pcm_state");
    A.start = dlsym(h, "snd_pcm_start");
    A.wait = dlsym(h, "snd_pcm_wait");
    return A.open && A.set_params && A.writei && A.recover && A.delay &&
           A.drop && A.prepare && A.pause && A.close && A.state && A.start && A.wait;
}

static int a_open(void** pcm, unsigned ch, unsigned rate, unsigned latency_us) {
    int err = A.open(pcm, "default", 0, 1);
    if (err < 0) return err;
    err = A.set_params(*pcm, 2, 3, ch, rate, 1, latency_us);
    if (err < 0) { A.close(*pcm); *pcm = NULL; }
    return err;
}
// a_write writes frames without blocking, recovering from underruns. It
// returns the frames written (0 when the device is full or after a
// recovered underrun) or a negative error. The stream is started
// explicitly: some plugins (pulse) never reach the default start
// threshold.
static long a_write(void* pcm, const void* buf, unsigned long frames) {
    long r = A.writei(pcm, buf, frames);
    if (r == -EAGAIN) return 0;
    if (r < 0) {
        int e = A.recover(pcm, (int)r, 1);
        return e < 0 ? e : 0;
    }
    if (A.state(pcm) == 2) A.start(pcm);
    return r;
}
// a_wait waits for room in the device buffer, retrying a start that
// failed (a full, never-started buffer would otherwise stall forever).
static void a_wait(void* pcm, int ms) {
    if (A.state(pcm) == 2) A.start(pcm);
    A.wait(pcm, ms);
}
static long a_delay(void* pcm) {
    long d = 0;
    if (A.delay(pcm, &d) < 0) return 0;
    return d < 0 ? 0 : d;
}
static int  a_pause(void* pcm, int on) { return A.pause(pcm, on); }
static void a_reset(void* pcm) { A.drop(pcm); A.prepare(pcm); }
static void a_close(void* pcm) { A.drop(pcm); A.close(pcm); }
*/
import "C"

import (
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

const (
	ringSeconds = 0.5
	// alsaLatencyUS is the requested device buffer. Sound servers' ALSA
	// plugins (pulse) underrun and stall with small buffers, so it is
	// generous; Flush drops it and Pause uses snd_pcm_pause, so neither
	// waits for it to drain.
	alsaLatencyUS = 500000
	// alsaChunk is how much the writer hands ALSA per call.
	alsaChunk = 20 * time.Millisecond
	// alsaLowWater: below this much queued device audio the writer pads
	// with silence rather than let the device underrun. Above it, it
	// writes only real content, so a momentarily empty queue never puts a
	// gap into the stream.
	alsaLowWater = 60 * time.Millisecond
)

var (
	alsaOnce sync.Once
	alsaOK   bool
)

func alsaLoaded() bool {
	alsaOnce.Do(func() { alsaOK = C.alsa_load() != 0 })
	return alsaOK
}

func available() bool { return alsaLoaded() }

// sink is the ALSA Sink (non-blocking PCM). A writer goroutine owns the
// device: it feeds queued PCM in small chunks, applies pause/flush
// requests, and samples the device delay on every pass; HostTime
// extrapolates from the latest sample. Nothing is written before the
// first Play.
type sink struct {
	*ring
	pcm   unsafe.Pointer
	chunk int // frames per write
	quit  chan struct{}
	done  chan struct{}

	wantPlay atomic.Bool
	flushReq atomic.Bool

	clk       sync.Mutex
	committed int64     // device frames accepted by ALSA
	delay     int64     // frames queued in the device at sampleAt
	sampleAt  time.Time // when delay was read

	closeOnce sync.Once
}

func open(format Format) (Sink, error) {
	if format.SampleRate <= 0 || format.Channels <= 0 || !alsaLoaded() {
		return nil, ErrNotSupported
	}
	var pcm unsafe.Pointer
	if C.a_open(&pcm, C.uint(format.Channels), C.uint(format.SampleRate), alsaLatencyUS) < 0 {
		return nil, ErrNotSupported
	}
	s := &sink{ring: newRing(format, ringSeconds, true), pcm: pcm,
		chunk: max(64, int(float64(format.SampleRate)*alsaChunk.Seconds())),
		quit:  make(chan struct{}), done: make(chan struct{})}
	go s.run()
	return s, nil
}

// sample records written frames and the current device delay.
func (s *sink) sample(written int64) {
	d := int64(C.a_delay(s.pcm))
	s.clk.Lock()
	s.committed += written
	s.delay = d
	s.sampleAt = time.Now()
	s.clk.Unlock()
}

func (s *sink) run() {
	defer close(s.done)
	fb := s.frameBytes
	buf := make([]byte, s.chunk*fb)
	lowWater := int(float64(s.rate) * alsaLowWater.Seconds())
	started := false  // first Play seen
	hwPaused := false // paused with snd_pcm_pause
	softPause := false
	idle := time.NewTicker(10 * time.Millisecond)
	defer idle.Stop()
	for {
		select {
		case <-s.quit:
			return
		default:
		}
		if s.flushReq.Swap(false) {
			// Dropped frames count as played: they map to the old epoch,
			// which hostTime ignores. The device timeline resumes at the
			// committed count, so realign the ring's to it.
			C.a_reset(s.pcm)
			hwPaused = false
			s.sample(0)
			s.clk.Lock()
			committed := s.committed
			s.clk.Unlock()
			s.ring.mu.Lock()
			s.ring.enqueued = committed
			s.ring.mu.Unlock()
		}
		play := s.wantPlay.Load()
		switch {
		case play && !started:
			started = true
		case play && hwPaused:
			C.a_pause(s.pcm, 0)
			hwPaused = false
		case play && softPause:
			s.setPaused(false)
			softPause = false
		case !play && started && !hwPaused && !softPause:
			if C.a_pause(s.pcm, 1) == 0 {
				hwPaused = true
			} else {
				// No hardware pause: keep the device running on silence.
				s.setPaused(true)
				softPause = true
			}
		}
		if !started || hwPaused {
			s.sample(0)
			select {
			case <-s.quit:
				return
			case <-idle.C:
			}
			continue
		}
		n := s.chunk
		if !softPause {
			s.clk.Lock()
			queued := s.delay
			s.clk.Unlock()
			if queued > int64(lowWater) {
				n = min(n, s.fillable())
			}
		}
		if n == 0 {
			select {
			case <-s.quit:
				return
			case <-idle.C:
			}
			s.sample(0)
			continue
		}
		s.fill(buf[:n*fb])
		for off := 0; off < n; {
			w := int(C.a_write(s.pcm, unsafe.Pointer(&buf[off*fb]), C.ulong(n-off)))
			if w < 0 {
				return // device gone; the clock stops
			}
			if w == 0 {
				select {
				case <-s.quit:
					return
				default:
				}
				if s.flushReq.Load() {
					break // the rest of the chunk is flushed content anyway
				}
				C.a_wait(s.pcm, 20)
			}
			off += w
			s.sample(int64(w))
		}
	}
}

func (s *sink) Write(pcm []byte) (int, error) { return s.write(pcm), nil }
func (s *sink) Buffered() time.Duration       { return s.buffered() }
func (s *sink) Play() error {
	s.wantPlay.Store(true)
	s.setHold(false)
	return nil
}

func (s *sink) Pause() error {
	s.HostTime() // refresh the position the hold freezes
	s.setHold(true)
	s.wantPlay.Store(false)
	return nil
}
func (s *sink) SetVolume(v float32) { s.setVolume(v) }

// Flush drops queued PCM — including what is already in the device buffer
// — and restarts HostTime at zero.
func (s *sink) Flush() {
	s.flush()
	s.flushReq.Store(true)
}

// SetRate changes speed by resampling; unlike the macOS sink the pitch
// follows the rate.
func (s *sink) SetRate(r float32) { s.setSpeed(r) }

func (s *sink) HostTime() time.Duration {
	s.clk.Lock()
	committed, delay, at := s.committed, s.delay, s.sampleAt
	s.clk.Unlock()
	if at.IsZero() {
		return s.hostTime(0, false)
	}
	dev := committed - delay
	if s.wantPlay.Load() {
		dev += int64(time.Since(at).Seconds() * float64(s.rate))
	}
	return s.hostTime(min(dev, committed), true)
}

func (s *sink) Close() error {
	s.closeOnce.Do(func() {
		close(s.quit)
		<-s.done
		C.a_close(s.pcm)
	})
	return nil
}
