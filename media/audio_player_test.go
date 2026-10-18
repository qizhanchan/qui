package media

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeAudioSource serves an audio-only source backed by a fake renderer so
// AudioPlayer's public state machine can be tested without a device.
type fakeAudioSource struct {
	track  *AudioTrackInfo
	rend   *fakeAudioRenderer
	closed bool
}

func (s *fakeAudioSource) VideoTrack() *VideoTrackInfo  { return nil }
func (s *fakeAudioSource) AudioTrack() *AudioTrackInfo  { return s.track }
func (s *fakeAudioSource) AudioRenderer() AudioRenderer { return s.rend }

func (s *fakeAudioSource) NextVideoFrame(context.Context) (VideoFrame, error) {
	return nil, ErrNoTrack
}
func (s *fakeAudioSource) SeekVideo(time.Duration, bool) error     { return ErrNoTrack }
func (s *fakeAudioSource) OpenScrubDecoder() (ScrubDecoder, error) { return nil, ErrNoTrack }
func (s *fakeAudioSource) Close() error                            { s.closed = true; return nil }

type audioTestBackend struct{ src *fakeAudioSource }

func (audioTestBackend) Capabilities() Capabilities {
	return Capabilities{AudioCodecs: []string{"aac"}}
}

func (b audioTestBackend) OpenSource(string) (Source, error) { return b.src, nil }

// newTestAudioPlayer installs a fake backend with an already-open source.
func newTestAudioPlayer(t *testing.T) (*AudioPlayer, *fakeAudioRenderer) {
	t.Helper()
	rend := &fakeAudioRenderer{}
	src := &fakeAudioSource{
		track: &AudioTrackInfo{Codec: "aac", SampleRate: 44100, Channels: 2, Duration: time.Minute},
		rend:  rend,
	}
	restore := swapBackend(t, audioTestBackend{src: src})
	t.Cleanup(restore)

	p, err := NewAudioPlayer()
	if err != nil {
		t.Fatalf("NewAudioPlayer: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	if err := p.Open("song.m4a"); err != nil {
		t.Fatalf("Open: %v", err)
	}
	return p, rend
}

func TestAudioPlayerCloseSemantics(t *testing.T) {
	p, _ := newTestAudioPlayer(t)
	if err := p.Play(); err != nil {
		t.Fatalf("Play: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if got := p.State(); got != StateIdle {
		t.Errorf("state after Close = %v, want StateIdle", got)
	}
	for name, fn := range map[string]func() error{
		"Play":   p.Play,
		"Pause":  p.Pause,
		"Stop":   p.Stop,
		"SeekTo": func() error { return p.SeekTo(0) },
		"Open":   func() error { return p.Open("song.m4a") },
	} {
		if err := fn(); !errors.Is(err, ErrClosed) {
			t.Errorf("%s after Close = %v, want ErrClosed", name, err)
		}
	}
}

// The renderer's optional error hook must feed AudioPlayer.OnError.
func TestAudioPlayerOnErrorWired(t *testing.T) {
	p, rend := newTestAudioPlayer(t)
	errc := make(chan error, 1)
	p.OnError(func(err error) {
		select {
		case errc <- err:
		default:
		}
	})
	handler := rend.onErr
	if handler == nil {
		t.Fatal("renderer implements errorSink but AudioPlayer did not wire SetErrorHandler")
	}
	boom := errors.New("aac frame broken")
	handler(boom)
	select {
	case got := <-errc:
		if !errors.Is(got, boom) {
			t.Errorf("OnError got %v, want %v", got, boom)
		}
	case <-time.After(time.Second):
		t.Fatal("OnError never fired")
	}
}

// OnEnded replaces the previous registration, and the renderer is rewired
// each time, so the latest callback is the one that fires.
func TestAudioPlayerOnEndedReplaces(t *testing.T) {
	p, rend := newTestAudioPlayer(t)
	var calls []string
	p.OnEnded(func() { calls = append(calls, "first") })
	p.OnEnded(func() { calls = append(calls, "second") })

	fn := rend.onEnded
	if fn == nil {
		t.Fatal("renderer has no OnEnded handler")
	}
	fn()
	if len(calls) != 1 || calls[0] != "second" {
		t.Errorf("fired %v, want exactly [second]", calls)
	}
	if got := p.State(); got != StateEnded {
		t.Errorf("state after natural end = %v, want StateEnded", got)
	}
}

// SeekTo clamps to the track duration regardless of what the backend does.
func TestAudioPlayerSeekClampsToDuration(t *testing.T) {
	p, rend := newTestAudioPlayer(t)
	if err := p.SeekTo(2 * time.Minute); err != nil {
		t.Fatalf("SeekTo: %v", err)
	}
	if got, want := rend.hostTime, time.Minute; got != want {
		t.Errorf("seek target = %v, want %v (clamped to the track duration)", got, want)
	}
}
