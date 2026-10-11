package audioout

import (
	"encoding/binary"
	"math"
	"sync"
	"time"
)

// ring is the platform-independent half of a Sink: the bounded PCM queue
// Write fills, and the content clock. Each platform sink pulls device
// buffers through fill and reports which device frame is being heard;
// hostTime maps that back to content time through the segments fill
// recorded, so the clock is exact across underruns, pauses, Flush and
// rate changes.
type ring struct {
	rate       int
	chs        int
	frameBytes int
	capBytes   int
	// soft selects software rate (linear resampling — pitch follows the
	// rate) and software volume, for sinks without native support.
	soft bool

	mu       sync.Mutex
	buf      []byte
	head     int // read offset into buf
	epoch    int64
	content  float64 // content frames consumed since the last Flush
	frac     float64 // resampler phase within the next content frame
	enqueued int64   // device frames handed out by fill since start
	segs     [512]segment
	nsegs    int
	paused   bool
	speed    float64
	volume   float64
	lastPos  float64
	// hold freezes the reported clock (soft sinks, from Pause to Play):
	// audio a sound server already holds may keep playing briefly after a
	// pause, but the content clock should stop when asked to.
	hold    bool
	holdPos float64
}

// segment maps one device buffer to the content it carried.
type segment struct {
	devStart int64   // first device frame of the buffer
	epoch    int64   // Flush epoch the content belongs to
	content  float64 // content frame (since the last Flush) at devStart
	frames   int64   // device frames that carried content
	speed    float64 // content frames per device frame
}

func newRing(f Format, capSeconds float64, soft bool) *ring {
	fb := 2 * f.Channels
	return &ring{rate: f.SampleRate, chs: f.Channels, frameBytes: fb,
		capBytes: int(float64(f.SampleRate)*capSeconds) * fb,
		soft:     soft, speed: 1, volume: 1}
}

// write accepts at most the free queue space.
func (r *ring) write(pcm []byte) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	free := r.capBytes - (len(r.buf) - r.head)
	n := min(len(pcm), max(free, 0))
	n -= n % r.frameBytes
	r.buf = append(r.buf, pcm[:n]...)
	return n
}

func (r *ring) buffered() time.Duration {
	r.mu.Lock()
	n := (len(r.buf) - r.head) / r.frameBytes
	r.mu.Unlock()
	return time.Duration(float64(n) / float64(r.rate) * float64(time.Second))
}

func (r *ring) flush() {
	r.mu.Lock()
	r.buf = r.buf[:0]
	r.head = 0
	r.epoch++
	r.content = 0
	r.frac = 0
	r.lastPos = 0
	r.holdPos = 0
	r.mu.Unlock()
}

// setHold freezes (or releases) the reported clock at its current value.
func (r *ring) setHold(h bool) {
	r.mu.Lock()
	if h && !r.hold {
		r.holdPos = r.lastPos
	}
	r.hold = h
	r.mu.Unlock()
}

// fillable returns how many device frames fill can produce from queued
// content alone (no silence padding) at the current speed.
func (r *ring) fillable() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := (len(r.buf) - r.head) / r.frameBytes
	if r.soft && r.speed != 1 {
		n = int(float64(n-2) / r.speed)
	}
	return max(n, 0)
}

func (r *ring) setPaused(p bool) {
	r.mu.Lock()
	r.paused = p
	r.mu.Unlock()
}

func (r *ring) setSpeed(s float32) {
	r.mu.Lock()
	r.speed = float64(max(0.5, min(2, s)))
	r.mu.Unlock()
}

func (r *ring) setVolume(v float32) {
	r.mu.Lock()
	r.volume = float64(max(0, min(1, v)))
	r.mu.Unlock()
}

// fill writes len(out) bytes of device audio: queued content (silence when
// paused — soft sinks keep the device running — or when the queue runs
// dry) and records which content the buffer carries.
func (r *ring) fill(out []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := len(out) / r.frameBytes
	speed := 1.0
	if r.soft {
		speed = r.speed
	}
	produced := 0
	if !(r.soft && r.paused) {
		if speed == 1 {
			r.frac = 0 // back at normal speed: drop the sub-frame phase
			c := min(len(r.buf)-r.head, n*r.frameBytes)
			c -= c % r.frameBytes
			copy(out, r.buf[r.head:r.head+c])
			r.head += c
			produced = c / r.frameBytes
			r.content += float64(produced)
		} else {
			produced = r.resample(out, n, speed)
		}
		if r.soft && r.volume != 1 {
			g := r.volume
			for i := 0; i < produced*r.frameBytes; i += 2 {
				v := float64(int16(binary.LittleEndian.Uint16(out[i:])))
				binary.LittleEndian.PutUint16(out[i:], uint16(int16(math.Round(v*g))))
			}
		}
	}
	clear(out[produced*r.frameBytes:])
	if r.head > len(r.buf)/2 && r.head > 1<<16 {
		r.buf = append(r.buf[:0], r.buf[r.head:]...)
		r.head = 0
	}
	seg := segment{devStart: r.enqueued, epoch: r.epoch, frames: int64(produced), speed: speed,
		content: r.content - float64(produced)*speed}
	r.segs[r.nsegs%len(r.segs)] = seg
	r.nsegs++
	r.enqueued += int64(n)
}

// resample produces up to n device frames by linear interpolation at the
// given speed and returns how many it produced. Caller holds r.mu.
func (r *ring) resample(out []byte, n int, speed float64) int {
	fb, chs := r.frameBytes, r.chs
	avail := (len(r.buf) - r.head) / fb
	src := r.buf[r.head:]
	sample := func(frame, ch int) float64 {
		return float64(int16(binary.LittleEndian.Uint16(src[frame*fb+2*ch:])))
	}
	pos := r.frac
	k := 0
	for ; k < n; k++ {
		i := int(pos)
		if i+1 >= avail {
			break
		}
		t := pos - float64(i)
		for ch := 0; ch < chs; ch++ {
			v := sample(i, ch)*(1-t) + sample(i+1, ch)*t
			binary.LittleEndian.PutUint16(out[k*fb+2*ch:], uint16(int16(math.Round(v))))
		}
		pos += speed
	}
	used := int(pos)
	r.head += used * fb
	r.frac = pos - float64(used)
	r.content += float64(k) * speed
	return k
}

// hostTime maps device frame dev (the one being heard) to content time.
func (r *ring) hostTime(dev int64, valid bool) time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	pos := r.lastPos
	if valid {
		for i := 1; i <= min(r.nsegs, len(r.segs)); i++ {
			seg := r.segs[(r.nsegs-i)%len(r.segs)]
			if seg.devStart <= dev {
				if seg.epoch == r.epoch {
					pos = seg.content + float64(min(dev-seg.devStart, seg.frames))*seg.speed
				}
				break
			}
		}
	}
	if pos < r.lastPos && pos+float64(r.rate)/10 > r.lastPos {
		pos = r.lastPos // never step backwards by jitter
	}
	r.lastPos = pos
	if r.hold {
		pos = r.holdPos
	}
	return time.Duration(pos / float64(r.rate) * float64(time.Second))
}
