//go:build cgo

package media

import (
	"context"
	"time"

	"github.com/qizhanchan/qui/media/internal/mp4x"
)

// sampleTrack is a track's samples in decode order, as the pure-Go
// decoders consume them: an MP4 file's sample table (fileTrack), or the
// samples a Media Source Extensions SourceBuffer has been given
// (mseTrack, which grows while it plays and blocks the decoder until the
// page appends what it needs).
type sampleTrack interface {
	count() int
	pts(i int) time.Duration
	dur(i int) time.Duration
	read(buf []byte, i int) ([]byte, error)
	// seekKeyframe is the sync sample at or before t; sampleAt the
	// sample presented at t.
	seekKeyframe(t time.Duration) int
	sampleAt(t time.Duration) int
	// more blocks until sample n exists (true) or never will: the stream
	// ended, the track closed or ctx is done (false).
	more(ctx context.Context, n int) bool
	// gen changes whenever existing indices moved (samples inserted
	// before the end or removed): a decoder resyncs by time.
	gen() uint64
}

// fileTrack is a sampleTrack over an mp4x track.
type fileTrack struct {
	r *mp4x.Reader
	t *mp4x.Track
}

func (f fileTrack) count() int                            { return f.t.SampleCount() }
func (f fileTrack) pts(i int) time.Duration                { return f.r.PTS(f.t, i) }
func (f fileTrack) dur(i int) time.Duration                { return f.r.SampleDuration(f.t, i) }
func (f fileTrack) read(buf []byte, i int) ([]byte, error) { return f.r.ReadSampleInto(buf, f.t, i) }
func (f fileTrack) seekKeyframe(t time.Duration) int       { return f.r.SeekKeyframe(f.t, t) }
func (f fileTrack) sampleAt(t time.Duration) int           { return f.r.SampleAtTime(f.t, t) }
func (f fileTrack) more(_ context.Context, n int) bool      { return n < f.t.SampleCount() }
func (f fileTrack) gen() uint64                            { return 0 }
