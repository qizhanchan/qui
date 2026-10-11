// Command aac-to-wav demuxes an MP4's AAC track with go-mp4 and decodes it
// to PCM with a pure-Go AAC-LC decoder, writing a .wav.
//
// It proves the pure-Go audio path (no cgo, no ffmpeg) and doubles as the
// oracle for a self-written AAC decoder: same file in, same PCM out.
//
// Usage:
//
//	go run ./cmd/aac-to-wav input.mp4 [out.wav]
package main

import (
	"encoding/binary"
	"fmt"
	"os"

	mp4 "github.com/abema/go-mp4"
	"github.com/skrashevich/go-aac/pkg/decoder"

	"github.com/qizhanchan/qui/tools/media-poc/internal/mp4util"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: aac-to-wav <input.mp4> [out.wav]")
		os.Exit(2)
	}
	out := "audio.wav"
	if len(os.Args) >= 3 {
		out = os.Args[2]
	}
	if err := run(os.Args[1], out); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(in, out string) error {
	f, err := os.Open(in)
	if err != nil {
		return err
	}
	defer f.Close()

	info, err := mp4.Probe(f)
	if err != nil {
		return fmt.Errorf("probe: %w", err)
	}
	at := mp4util.AudioTrack(info)
	if at == nil {
		return fmt.Errorf("no mp4a (AAC) track in %s", in)
	}
	sampleRate := int(at.Timescale)
	channels := 2
	if at.MP4A != nil && at.MP4A.ChannelCount > 0 {
		channels = int(at.MP4A.ChannelCount)
	}
	fmt.Printf("audio: %d Hz x %d ch  samples=%d  ~%.2fs\n",
		sampleRate, channels, len(at.Samples), mp4util.Seconds(at.Duration, at.Timescale))

	asc, err := mp4util.AudioSpecificConfig(f)
	if err != nil {
		return fmt.Errorf("ASC: %w", err)
	}
	fmt.Printf("AudioSpecificConfig: % x\n", asc)

	dec := decoder.New()
	if err := dec.SetASC(asc); err != nil {
		return fmt.Errorf("SetASC: %w", err)
	}

	var pcm []int16
	nFrames := 0
	for i := range at.Samples {
		au, err := mp4util.ReadSample(f, at, i)
		if err != nil {
			return err
		}
		interleaved, err := dec.DecodeFrame(au)
		if err != nil {
			fmt.Printf("  frame %d decode error: %v\n", i, err)
			continue
		}
		for _, s := range interleaved {
			v := int(s * 32767.0)
			if v > 32767 {
				v = 32767
			} else if v < -32768 {
				v = -32768
			}
			pcm = append(pcm, int16(v))
		}
		nFrames++
	}

	if err := writeWAV(out, pcm, sampleRate, channels); err != nil {
		return err
	}
	fmt.Printf("DECODED %d AAC frames -> %d samples/ch (%d int16) -> %s\n",
		nFrames, len(pcm)/maxInt(channels, 1), len(pcm), out)
	return nil
}

func writeWAV(path string, pcm []int16, sampleRate, channels int) error {
	w, err := os.Create(path)
	if err != nil {
		return err
	}
	defer w.Close()
	dataSize := len(pcm) * 2
	byteRate := sampleRate * channels * 2
	header := make([]byte, 44)
	copy(header[0:], "RIFF")
	binary.LittleEndian.PutUint32(header[4:], uint32(36+dataSize))
	copy(header[8:], "WAVE")
	copy(header[12:], "fmt ")
	binary.LittleEndian.PutUint32(header[16:], 16)
	binary.LittleEndian.PutUint16(header[20:], 1)
	binary.LittleEndian.PutUint16(header[22:], uint16(channels))
	binary.LittleEndian.PutUint32(header[24:], uint32(sampleRate))
	binary.LittleEndian.PutUint32(header[28:], uint32(byteRate))
	binary.LittleEndian.PutUint16(header[32:], uint16(channels*2))
	binary.LittleEndian.PutUint16(header[34:], 16)
	copy(header[36:], "data")
	binary.LittleEndian.PutUint32(header[40:], uint32(dataSize))
	if _, err := w.Write(header); err != nil {
		return err
	}
	buf := make([]byte, dataSize)
	for i, s := range pcm {
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(s))
	}
	_, err = w.Write(buf)
	return err
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
