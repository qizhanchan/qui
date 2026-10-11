package mp4x

import (
	"time"

	mp4 "github.com/abema/go-mp4"
)

// Fragmented MP4 (ISO/IEC 14496-12 §8.8) support: a fragmented file has no
// sample table in `moov`; instead each movie fragment (`moof`) carries a
// track fragment (`traf`) whose `trun` lists the samples, and the bytes live
// in the following `mdat`. We walk every `moof` at open time and build the
// same per-sample table that a progressive file gets from `stbl`, so the rest
// of the reader (ReadSample / DTS / Keyframes / SeekKeyframe) is unchanged.

// trun flags (the tfhd flags are exported by go-mp4 as mp4.Tfhd*Present).
const (
	trunDataOffsetPresent       = 0x000001
	trunFirstSampleFlagsPresent = 0x000004
	trunSampleDurationPresent   = 0x000100
	trunSampleSizePresent       = 0x000200
	trunSampleFlagsPresent      = 0x000400
	trunSampleCTOPresent        = 0x000800

	// sample_flags bit 16 (0x00010000) marks a non-sync sample.
	sampleFlagNonSync = 0x00010000
)

// trackByID returns the track with the given ID, or nil.
func (r *Reader) trackByID(id uint32) *Track {
	if r.video != nil && r.video.ID == id {
		return r.video
	}
	if r.audio != nil && r.audio.ID == id {
		return r.audio
	}
	return nil
}

// buildFragments walks every movie fragment and fills each track's fragment
// sample table.
func (r *Reader) buildFragments() error {
	moofs, err := mp4.ExtractBoxes(r.f, nil, []mp4.BoxPath{{mp4.BoxTypeMoof()}})
	if err != nil {
		return err
	}
	for _, mi := range moofs {
		bips, err := mp4.ExtractBoxesWithPayload(r.f, mi, []mp4.BoxPath{
			{mp4.BoxTypeTraf(), mp4.BoxTypeTfhd()},
			{mp4.BoxTypeTraf(), mp4.BoxTypeTfdt()},
			{mp4.BoxTypeTraf(), mp4.BoxTypeTrun()},
		})
		if err != nil {
			return err
		}
		r.appendFragment(mi.Offset, bips)
	}
	// A fragmented moov carries no duration (it lives in mvex/mehd or is
	// implied by the fragments), so derive each track's duration and frame
	// rate from the samples we just read.
	for _, t := range []*Track{r.video, r.audio} {
		if t == nil || len(t.frag) == 0 || t.Timescale == 0 {
			continue
		}
		last := t.frag[len(t.frag)-1]
		end := time.Duration(float64(last.dts+uint64(last.dur)) / float64(t.Timescale) * float64(time.Second))
		if t.Duration <= 0 {
			t.Duration = end
		}
		if t.Kind == KindVideo && t.FPS == 0 && t.Duration > 0 {
			t.FPS = float32(float64(len(t.frag)) / t.Duration.Seconds())
		}
	}
	return nil
}

// appendFragment folds one moof's trafs into the per-track sample tables.
// bips are the traf children in file order (tfhd, tfdt, trun…), so a tfhd
// starts a new track-fragment group.
func (r *Reader) appendFragment(moofOffset uint64, bips []*mp4.BoxInfoWithPayload) {
	var (
		tfhd     *mp4.Tfhd
		base     uint64
		nextDTS  uint64
		runStart uint64
		track    *Track
	)
	for _, bip := range bips {
		switch b := bip.Payload.(type) {
		case *mp4.Tfhd:
			tfhd = b
			track = r.trackByID(b.TrackID)
			nextDTS = 0
			if b.GetFlags()&mp4.TfhdBaseDataOffsetPresent != 0 {
				base = b.BaseDataOffset
			} else {
				// default-base-is-moof, and the spec's fallback for the first
				// track fragment, are both the enclosing moof's first byte.
				base = moofOffset
			}
			runStart = base
		case *mp4.Tfdt:
			nextDTS = b.GetBaseMediaDecodeTime()
		case *mp4.Trun:
			if tfhd == nil || track == nil {
				continue
			}
			runStart, nextDTS = appendRun(track, tfhd, b, base, runStart, nextDTS)
		}
	}
}

// appendRun appends the samples of one trun and returns the offset just past
// its data (the start of the next run when data_offset is absent) and the
// advanced decode time.
func appendRun(track *Track, tfhd *mp4.Tfhd, trun *mp4.Trun, base, runStart, nextDTS uint64) (uint64, uint64) {
	off := runStart
	if trun.GetFlags()&trunDataOffsetPresent != 0 {
		off = uint64(int64(base) + int64(trun.DataOffset))
	}
	for i := range trun.Entries {
		e := &trun.Entries[i]
		size := tfhd.DefaultSampleSize
		if trun.GetFlags()&trunSampleSizePresent != 0 {
			size = e.SampleSize
		}
		dur := tfhd.DefaultSampleDuration
		if trun.GetFlags()&trunSampleDurationPresent != 0 {
			dur = e.SampleDuration
		}
		flags := tfhd.DefaultSampleFlags
		if trun.GetFlags()&trunSampleFlagsPresent != 0 {
			flags = e.SampleFlags
		}
		if i == 0 && trun.GetFlags()&trunFirstSampleFlagsPresent != 0 {
			flags = trun.FirstSampleFlags
		}
		var cto int64
		if trun.GetFlags()&trunSampleCTOPresent != 0 {
			if trun.GetVersion() != 0 {
				cto = int64(e.SampleCompositionTimeOffsetV1)
			} else {
				cto = int64(e.SampleCompositionTimeOffsetV0)
			}
		}
		track.frag = append(track.frag, fragSample{
			off:  off,
			size: size,
			dur:  dur,
			dts:  nextDTS,
			cto:  cto,
			sync: flags&sampleFlagNonSync == 0,
		})
		off += uint64(size)
		nextDTS += uint64(dur)
	}
	return off, nextDTS
}
