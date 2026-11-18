//go:build cgo

package media

import (
	"bytes"
	"errors"
	"os"
	"sync/atomic"
	"testing"
)

// memSource is an in-memory ByteSource that counts reads and fails after
// Close, like a network source must.
type memSource struct {
	*bytes.Reader
	closed atomic.Bool
	reads  atomic.Int64
}

func (m *memSource) ReadAt(p []byte, off int64) (int, error) {
	if m.closed.Load() {
		return 0, errors.New("closed")
	}
	m.reads.Add(1)
	return m.Reader.ReadAt(p, off)
}
func (m *memSource) Close() error { m.closed.Store(true); return nil }

func TestOpenStreamMatchesFFmpeg(t *testing.T) {
	for _, c := range e2eClips {
		t.Run(c.codec, func(t *testing.T) {
			b, err := os.ReadFile(c.file)
			if err != nil {
				t.Fatal(err)
			}
			ms := &memSource{Reader: bytes.NewReader(b)}
			src, err := golangBackend{}.OpenStream(ms)
			if err != nil {
				t.Fatalf("OpenStream: %v", err)
			}
			checkMatchesFFmpeg(t, src, c.codec, c.hash)
			if !ms.closed.Load() {
				t.Error("Source.Close did not close the ByteSource")
			}
		})
	}
}

func TestOpenStreamRejectsGarbage(t *testing.T) {
	ms := &memSource{Reader: bytes.NewReader([]byte("not an mp4 at all, just text"))}
	if _, err := OpenStream(ms); err == nil {
		t.Fatal("OpenStream accepted garbage")
	}
	if !ms.closed.Load() {
		t.Error("failed OpenStream must close the ByteSource")
	}
}

func TestStreamSupportedListsCodecs(t *testing.T) {
	c := StreamSupported()
	if len(c.VideoCodecs) == 0 || len(c.Containers) == 0 {
		t.Fatalf("StreamSupported = %+v", c)
	}
}
