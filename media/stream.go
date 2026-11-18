package media

import (
	"errors"
	"io"
)

// ByteSource is media bytes that do not live in a local file: a network
// resource fetched with range requests, an in-memory blob. ReadAt may block
// until the requested bytes are available (a progressively downloaded
// resource) and must return an error once Close has been called, so a
// decoder parked on it unblocks.
type ByteSource interface {
	io.ReaderAt
	// Size is the total length in bytes; it must be known up front.
	Size() int64
	// Close releases the source and fails pending and later reads.
	Close() error
}

// streamBackend is implemented by backends that can demux and decode from
// a ByteSource rather than a path.
type streamBackend interface {
	OpenStream(bs ByteSource) (Source, error)
	StreamCapabilities() Capabilities
}

// streamBackendFor picks the backend for ByteSources: the active backend
// when it reads streams, otherwise the pure-Go pipeline (the platform
// backend only opens paths).
func streamBackendFor() streamBackend {
	if b, ok := activeBackend().(streamBackend); ok {
		return b
	}
	if backendHasOverride {
		return nil
	}
	if b, ok := backendRegistry["golang"].(streamBackend); ok {
		return b
	}
	return nil
}

// OpenStream opens media from bs and enumerates its tracks. It takes
// ownership of bs: the returned Source closes it, and so does a failed
// open. Hand the Source to VideoView.OpenSource or AudioPlayer.OpenSource.
func OpenStream(bs ByteSource) (Source, error) {
	if bs == nil {
		return nil, errors.New("media: nil ByteSource")
	}
	b := streamBackendFor()
	if b == nil {
		_ = bs.Close()
		return nil, ErrNotSupported
	}
	src, err := b.OpenStream(bs)
	if err != nil {
		_ = bs.Close()
		return nil, err
	}
	return src, nil
}

// StreamSupported reports what OpenStream can decode — the answer an HTML
// media element's canPlayType gives. Zero when streams are unsupported.
func StreamSupported() Capabilities {
	if b := streamBackendFor(); b != nil {
		return b.StreamCapabilities()
	}
	return Capabilities{}
}

// BufferedSource is optionally implemented by a Source whose bytes arrive
// over time: Buffered reports the presentation-time ranges whose samples
// are already available, for HTMLMediaElement.buffered.
type BufferedSource interface {
	Buffered() []TimeRange
}

// TimeRange is a half-open presentation-time interval.
type TimeRange struct {
	Start, End float64 // seconds
}

// ByteRanger is optionally implemented by a ByteSource that downloads
// progressively: BufferedBytes lists the byte ranges [start, end) already
// held, sorted and disjoint. Sources opened from such a ByteSource
// implement BufferedSource.
type ByteRanger interface {
	BufferedBytes() [][2]int64
}

// coveredBy reports whether [off, off+n) lies inside one of the sorted,
// disjoint ranges.
func coveredBy(ranges [][2]int64, off, n int64) bool {
	lo, hi := 0, len(ranges)
	for lo < hi { // first range whose end > off
		m := (lo + hi) / 2
		if ranges[m][1] <= off {
			lo = m + 1
		} else {
			hi = m
		}
	}
	return lo < len(ranges) && ranges[lo][0] <= off && off+n <= ranges[lo][1]
}
