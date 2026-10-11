package h264

// NAL unit types (spec Table 7-1).
const (
	nalSlice     = 1
	nalDPA       = 2
	nalDPB       = 3
	nalDPC       = 4
	nalIDR       = 5
	nalSEI       = 6
	nalSPS       = 7
	nalPPS       = 8
	nalAUD       = 9
	nalEndSeq    = 10
	nalEndStream = 11
	nalFiller    = 12
)

// nalType returns the 5-bit NAL unit type.
func nalType(h byte) int { return int(h & 0x1f) }

// nalRefIDC returns the nal_ref_idc field.
func nalRefIDC(h byte) int { return int((h >> 5) & 0x03) }

// isVCL reports whether the NAL unit type carries slice data.
func isVCL(typ int) bool { return typ >= 1 && typ <= 5 }

// splitAnnexB splits an Annex-B byte stream into NAL units, each returned
// as the raw bytes after the start code (header byte first). It tolerates
// both 3- and 4-byte start codes.
func splitAnnexB(data []byte) [][]byte {
	var out [][]byte
	n := len(data)
	i := 0
	// Find the first start code.
	for {
		if i+3 <= n && data[i] == 0 && data[i+1] == 0 && data[i+2] == 1 {
			i += 3
			break
		}
		if i+4 <= n && data[i] == 0 && data[i+1] == 0 && data[i+2] == 0 && data[i+3] == 1 {
			i += 4
			break
		}
		i++
		if i >= n {
			return out
		}
	}
	start := i
	for i < n {
		if i+3 <= n && data[i] == 0 && data[i+1] == 0 && (data[i+2] == 1 || (i+4 <= n && data[i+2] == 0 && data[i+3] == 1)) {
			end := i
			// Trim a single trailing zero that belongs to the next
			// start code's prefix.
			if end > start && data[end-1] == 0 {
				end--
			}
			if end > start {
				out = append(out, data[start:end])
			}
			if data[i+2] == 1 {
				i += 3
			} else {
				i += 4
			}
			start = i
			continue
		}
		i++
	}
	if start < n {
		out = append(out, data[start:n])
	}
	return out
}
