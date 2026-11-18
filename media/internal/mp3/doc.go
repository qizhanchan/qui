// Package mp3 is a pure-Go MPEG audio decoder: MPEG-1, MPEG-2 (LSF) and
// MPEG-2.5 Layer I, Layer II and Layer III, plus a demuxer for raw
// elementary-stream files (.mp3 / .mp2 / .mpa).
//
// # Origin
//
// Written from ISO/IEC 11172-3 and ISO/IEC 13818-3. The code layout of
// Layer III (region boundaries, count1 overrun handling, intensity stereo
// limits, mixed-block band handling, the 8 kHz band tables) deliberately
// follows FFmpeg's mpegaudiodec so that edge cases resolve the same way.
// The tables are standard data: the Huffman codes in huffman_tables.go and
// the synthesis window in window_table.go were extracted from the
// libmp3lame and libavcodec builds on the development machine and checked
// for completeness (every Huffman table is a complete prefix code); every
// other table is computed at init or typed from the standard.
//
// # Supported
//
//   - Layer III: all 32 Huffman tables (+ count1 tables A/B, linbits
//     escapes), MPEG-1 scale factors with scfsi, MPEG-2/2.5 LSF scale
//     factors, long/start/short/stop and mixed blocks, M/S and intensity
//     stereo (MPEG-1 and LSF), alias reduction, IMDCT with overlap,
//     polyphase synthesis, and the bit reservoir across frames.
//   - Layer I and Layer II (all four MPEG-1 allocation tables, the LSF
//     table, scfsi, grouped quantisers, joint-stereo bound).
//   - Stream: ID3v2 (incl. footer, repeated tags), trailing ID3v1 (+TAG+)
//     and APEv2 tags, sync search confirmed by consecutive headers (also
//     when resyncing past garbage), Xing/Info and VBRI headers (frame
//     count, TOC), LAME/Lavc gapless delay/padding, exact or TOC/CBR
//     estimated seeking, Sniff for container detection.
//   - PCMReader: frame decode + gapless trimming (encoder delay + 529
//     decoder delay, end padding) + sample-accurate seeking with reservoir
//     pre-roll.
//
// Output is float64 internally and float32 (full scale ±1) or int16 at
// the API. On LAME/FFmpeg-encoded streams it matches FFmpeg's mp3float
// and mp2float decoders at about 120-130 dB SNR (see ffmpeg_test.go and
// synth_test.go).
//
// # Known differences and gaps
//
//   - Free-format streams (bitrate index 0) are rejected.
//   - CRC words are skipped, not verified.
//   - FFmpeg's short-block IMDCT drops the last 6 overlap samples of the
//     previous granule. Those samples are zero on standard window
//     sequences (long -> start -> short -> stop), so the two decoders
//     agree there. On non-standard sequences (for example long -> short
//     without a start window, or mixed -> pure short) this package adds
//     the overlap as the standard says, which agrees with mpg123 instead.
//   - After Decoder.Reset, a Layer III frame whose main_data_begin points
//     before the reservoir produces no output (it still refills the
//     reservoir and filterbank), where FFmpeg outputs the frame with the
//     unreadable granules silenced. PCMReader.Seek pre-rolls enough frames
//     that output after a seek is identical to continuous decoding.
//   - A frame cut short by the end of the file is dropped
//     (Stream.ReadFrame returns io.EOF).
package mp3
