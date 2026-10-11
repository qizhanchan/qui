package h264

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
)

// TestDecodePFrameMatchesReference decodes Baseline/CAVLC streams with inter
// (P) frames produced by OpenH264's encoder and asserts every decoded frame is
// byte-identical to OpenH264's own decoder (the oracle). Streams and hashes
// come from tools/media-poc/cmd/oh264-vectors.
func TestDecodePFrameMatchesReference(t *testing.T) {
	type frame struct{ y, cb, cr string }
	cases := []struct {
		name string
		file string
		w, h int
		got  []frame
	}{
		{
			"64x48_qp26", "testdata/baseline_p_64x48.h264", 64, 48,
			[]frame{
				{"d1edefd1452493762de9ad1f077b0788ac349891de1f887f305f0177b7e23179", "5675f2a6766f1996a54b454283f86cedeb592487fef472e3be4fca6130cd584c", "165b60270b6345ece43f18cede40b109451970aeb6cf2ca522c2dd5e41f203e4"},
				{"8fe95b85ffacbc13976f526d57f2da9da79cfe5b378a2d0996ed0ff23a7c4765", "9cdf0ab3277912d255b4d9a3a7747a358b02b5e83fc1b85fa0c1c0d0a0f6830b", "08019e21160092d3f9d9da04bdc0a5669d8524bedcc47b79d16ae2fd855ff368"},
				{"a291b14747d4b650dfe4014ed2209230d03b00a3fbe4c6e5d3f79859720076c3", "a175ce00731b9fa842a214d0a832b77814c80a501ecd639000496f1adbf80e10", "4884a3343f8f69f7ba0c7901c3da7bb745a85eac4a99833315403f43752ce910"},
				{"ae4d7de73bed9f3883305ec9739ab720fb544cb42329a70f175299bd4ff7a335", "18bbbd79eab93d933c262771310ba2753bc806ef78d7ccf519f6e236cb3a11f7", "bc8274a5027d1942f245556974808893999d9eb02a3ae5860b9ecc9a09f82a8b"},
			},
		},
		{
			"96x64_qp30", "testdata/baseline_p_96x64.h264", 96, 64,
			[]frame{
				{"337cfe0fb97e607c5a9c9ec48593a482d15575359007c373bf940f9a0fb73255", "c773d61be7dcee31f0bd03826a7ef82f4dfedf09982c5b1bd0f258ad108e8fd5", "aa21045506f5a963699178484895a9143cb260fd7f53b241f32432b9ef0feee6"},
				{"bfe6aa51d1d5dc19a53871246995c1b7b3ed191c6faf9d91516953997920be75", "e5680a93614f85267f2b6a484819e9af5c822e94aa4e4602e5feca776a5ce37a", "2fce9331cbdd60f8015cb118ffeb60d2d37dfc526cd39ae4691cb852468260c5"},
				{"3d7b96a4e30b2330462ca2e1852bcc826a8d3d55c663a0e86e97f71102a174f0", "6c7de6c25894ebce8994de27d4552a181c5b4fff455af81c4ac0e9fc753b61d3", "ccbd0dfba346808ad0c3335f599b622e2f3ddd6e30c1248813b2f40dcaa10a9e"},
				{"c9e541401236b282e099e3c10043c722bad7497ac7640d0fb94d0af1edc2461f", "271709007c0036e57c1ae9f2ba5d25123b451cfeb57181d6d89f12c037d60453", "bc36a10e8de1aee3d1459f943e34c8f01134a25fdc4e662ee7e5f9be0e5ca491"},
				{"34b51e2a9c27e04742814a5fbeff6cecdee674a777ba4b3a9debbb2b79c2cd09", "b9a2a5328470f56c7816403ac019c959f3e1ac42930613e1b3a38e8c8a2c38c9", "c14d5523bbc62c30658e6785ce93f7d3536d9ba35ec42c01654d17013b688a94"},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data, err := os.ReadFile(c.file)
			if err != nil {
				t.Skipf("missing testdata: %v", err)
			}
			frames, err := New().DecodeAllAnnexB(data)
			if err != nil {
				t.Fatalf("DecodeAllAnnexB: %v", err)
			}
			if len(frames) != len(c.got) {
				t.Fatalf("decoded %d frames, want %d", len(frames), len(c.got))
			}
			for i, f := range frames {
				w, h := f.Width, f.Height
				if w != c.w || h != c.h {
					t.Fatalf("frame %d is %dx%d, want %dx%d", i, w, h, c.w, c.h)
				}
				cw, ch := chromaSize(w), chromaSize(h)
				checks := []struct {
					name string
					data []byte
					want string
				}{
					{"Y", f.Y.Pix[:w*h], c.got[i].y},
					{"Cb", f.Cb.Pix[:cw*ch], c.got[i].cb},
					{"Cr", f.Cr.Pix[:cw*ch], c.got[i].cr},
				}
				for _, ck := range checks {
					got := sha256.Sum256(ck.data)
					if hex.EncodeToString(got[:]) != ck.want {
						t.Errorf("frame %d %s mismatch:\n got  %x\n want %s", i, ck.name, got, ck.want)
					}
				}
			}
		})
	}
}
