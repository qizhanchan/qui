// Package opus is a pure-Go Opus audio decoder (RFC 6716, with the RFC 8251
// updates), written to produce the same output as libopus 1.6.
//
// # API
//
//	d, _ := opus.NewDecoder(48000, 2)        // 48/24/16/12/8 kHz, 1 or 2 channels
//	pcm, err := d.Decode(packet, pcm[:0])    // appends interleaved float32 (±1.0)
//	pcm, err = d.DecodePLC(pcm)              // conceal one lost packet
//	d.Reset()                                // after a seek
//
//	h, _ := opus.ParseOpusHead(codecPrivate) // Ogg / Matroska identification header
//	m, _ := opus.NewMultistreamDecoder(h)    // channel mapping families 0, 1, 255
//	n, _ := opus.PacketDuration(packet)      // samples at 48 kHz
//
// The decoders do not skip Head.PreSkip; the caller trims it. Decoder.SetGain
// applies the header's output gain (the multistream decoder does that itself).
//
// # Supported
//
//   - Packet framing: TOC byte, frame packing codes 0-3 (CBR/VBR, padding,
//     with the RFC 8251 padding fix), self-delimited framing for multistream,
//     all frame durations from 2.5 to 60 ms (up to 120 ms per packet).
//   - SILK: NB/MB/WB, mono and stereo (mid/side prediction, mid-only frames),
//     10/20/40/60 ms, packet loss concealment and comfort noise. SILK is a
//     fixed-point port of the libopus decoder (including its post-RFC changes:
//     precomputed NLSF weights, LPC_fit, saturating synthesis, PLC energy and
//     CNG gain fixes), so SILK output is bit-exact with libopus, as is the
//     SILK resampler to 24/48 kHz (and the down-samplers for 8/12 kHz output).
//     LBRR (in-band FEC) data is parsed and skipped, never decoded.
//   - CELT: all bandwidths and frame sizes, transients and short blocks,
//     TF resolution changes, spreading, band folding (including the RFC 8251
//     hybrid folding change), anti-collapse, dynamic allocation and skipping,
//     intensity and dual stereo, the pitch post-filter, de-emphasis and
//     decimation for lower output rates. CELT runs in float like the libopus
//     float build.
//   - Hybrid mode (SILK 0-8 kHz + CELT from band 17), mode transitions with
//     and without redundancy frames (CELT->SILK and SILK->CELT), the silence
//     frame used to fade out CELT on hybrid->SILK switches.
//   - Packet loss concealment: SILK PLC + CNG; CELT pitch-based periodic PLC
//     with LPC extrapolation, falling back to energy-shaped noise, followed by
//     the libopus 1.6 recovery logic (prefilter-and-fold, safe energy
//     prediction after loss).
//   - Multistream (RFC 7845 mapping families 0, 1 and 255) with silent
//     (255) channels and the header's output gain.
//
// Malformed packets are rejected with an error and never panic (a recover
// safety net additionally resets the decoder state if one ever slips
// through).
//
// # Accuracy
//
// Measured against FFmpeg's libopus 1.6.1 decoder (float output) on the
// fixtures in media/testdata/opus: SILK-only streams, including concealment,
// are bit-exact; CELT and hybrid streams are at ~138 dB and ~157 dB SNR
// respectively; spliced SILK/CELT/hybrid mode switches at ~106 dB and CELT
// concealment at 79-103 dB (float rounding in the pitch/LPC extrapolation).
//
// # Not supported
//
//   - Decoding of LBRR / in-band FEC (decode_fec), DRED, the deep (neural)
//     PLC, OSCE/LACE/NoLACE enhancement and BWE: all libopus options that are
//     off by default.
//   - The Opus 1.6 extension payload (QEXT, 96 kHz) carried in padding: it
//     is ignored, as by a decoder built without QEXT.
//   - Opus custom modes and the encoder.
//   - Soft clipping (libopus applies it only for int16 output).
//
// The SILK and CELT code is ported from the reference implementation in
// RFC 6716 Appendix A and from libopus (Copyright Xiph.Org, Skype Limited,
// Octasic, Jean-Marc Valin, Timothy B. Terriberry, CSIRO, Gregory Maxwell,
// Mark Borgerding, Erik de Castro Lopo; BSD license).
package opus
