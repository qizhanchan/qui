package mp4x

import (
	"bytes"
	"errors"
	"time"

	mp4 "github.com/abema/go-mp4"
)

// Media Source Extensions: a byte stream is an initialization segment
// (ftyp + moov, no samples) followed by media segments (moof + mdat
// pairs), each delivered on its own. OpenInit parses the former into a
// Reader with the tracks' configuration and no samples; ParseSegment
// pulls the samples out of each media segment.

// OpenInit parses an initialization segment.
func OpenInit(data []byte) (*Reader, error) {
	if len(data) == 0 {
		return nil, errors.New("mp4x: empty initialization segment")
	}
	return OpenReader(bytes.NewReader(data), int64(len(data)))
}

// SegSample is one sample of a media segment.
type SegSample struct {
	Track    *Track
	DTS, PTS time.Duration
	Dur      time.Duration
	Sync     bool
	Data     []byte // a slice of the segment
}

// ParseSegment extracts the samples of a media segment (one or more
// moof + mdat pairs) for the reader's tracks. Data slices alias seg.
func (r *Reader) ParseSegment(seg []byte) ([]SegSample, error) {
	rs := bytes.NewReader(seg)
	moofs, err := mp4.ExtractBoxes(rs, nil, []mp4.BoxPath{{mp4.BoxTypeMoof()}})
	if err != nil {
		return nil, err
	}
	var out []SegSample
	for _, mi := range moofs {
		bips, err := mp4.ExtractBoxesWithPayload(rs, mi, []mp4.BoxPath{
			{mp4.BoxTypeTraf(), mp4.BoxTypeTfhd()},
			{mp4.BoxTypeTraf(), mp4.BoxTypeTfdt()},
			{mp4.BoxTypeTraf(), mp4.BoxTypeTrun()},
		})
		if err != nil {
			return nil, err
		}
		// Collect the runs into scratch tracks so appendRun's arithmetic
		// is shared with the file path.
		scratch := map[*Track]*Track{}
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
					base = mi.Offset
				}
				runStart = base
			case *mp4.Tfdt:
				nextDTS = b.GetBaseMediaDecodeTime()
			case *mp4.Trun:
				if tfhd == nil || track == nil {
					continue
				}
				st := scratch[track]
				if st == nil {
					st = &Track{}
					scratch[track] = st
				}
				runStart, nextDTS = appendRun(st, tfhd, b, base, runStart, nextDTS)
			}
		}
		for t, st := range scratch {
			for _, s := range st.frag {
				end := s.off + uint64(s.size)
				if end > uint64(len(seg)) {
					return out, errors.New("mp4x: sample outside its media segment")
				}
				out = append(out, SegSample{
					Track: t,
					DTS:   t.toDuration(int64(s.dts) - t.shift),
					PTS:   t.toDuration(int64(s.dts) + s.cto - t.shift),
					Dur:   t.toDuration(int64(s.dur)),
					Sync:  s.sync,
					Data:  seg[s.off:end],
				})
			}
		}
	}
	return out, nil
}
