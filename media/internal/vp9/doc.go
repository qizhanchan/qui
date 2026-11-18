// Package vp9 is a pure-Go VP9 video decoder for the media backend.
//
// # Scope
//
// Profile 0 (8-bit 4:2:0), which is what browsers receive almost always:
// uncompressed and compressed headers, the boolean decoder, forward and
// backward probability adaptation, segmentation, loop filter deltas,
// tiles (columns run on goroutines), intra and inter prediction (all
// interpolation filters, compound prediction, scaled references for
// resolution changes), 4x4–32x32 DCT/ADST and lossless WHT, the loop
// filter (rows run on goroutines), hidden frames, show_existing_frame and
// superframes. Profiles 1–3 (4:4:4, 10/12-bit) return ErrUnsupported.
//
// Output is bit-exact with libvpx / FFmpeg on every fixture in
// decode_test.go (hashes pinned there).
//
// # Origin
//
// Written from the VP9 bitstream specification. The constant tables in
// tables_gen.go (default probabilities, scan orders, interpolation
// kernels, quantizer tables, loop-filter masks) were extracted from a
// libvpx build. libvpx is Copyright (c) 2010, The WebM Project authors,
// and is distributed under the BSD 3-Clause license:
//
//	Redistribution and use in source and binary forms, with or without
//	modification, are permitted provided that the following conditions are
//	met:
//
//	  * Redistributions of source code must retain the above copyright
//	    notice, this list of conditions and the following disclaimer.
//
//	  * Redistributions in binary form must reproduce the above copyright
//	    notice, this list of conditions and the following disclaimer in
//	    the documentation and/or other materials provided with the
//	    distribution.
//
//	  * Neither the name of Google, nor the WebM Project, nor the names
//	    of its contributors may be used to endorse or promote products
//	    derived from this software without specific prior written
//	    permission.
//
//	THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
//	"AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
//	LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
//	A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
//	HOLDER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
//	SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
//	LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
//	DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
//	THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
//	(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
//	OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
//
// # Known gaps
//
// Intra-only frames are implemented but have no fixture; VP9 in MP4
// (vp09 sample entry) is not wired; a superframe with two shown frames
// outputs both (libvpx outputs the last); no SIMD.
package vp9
