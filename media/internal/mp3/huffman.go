package mp3

// huffTree is a binary decoding tree: node i has children tree[2i] and
// tree[2i+1]; a child value >= 0 is the next node, a negative value v is
// the leaf symbol ^v.
type huffTree struct {
	nodes []int32
	xlen  int
}

var huffTrees [34]*huffTree

func init() {
	for _, src := range huffSrc {
		t := &huffTree{nodes: []int32{0, 0}, xlen: src.xlen}
		for sym := range src.code {
			code, n := src.code[sym], int(src.len[sym])
			node := 0
			for i := n - 1; i >= 0; i-- {
				b := int(code>>uint(i)) & 1
				slot := 2*node + b
				if i == 0 {
					t.nodes[slot] = ^int32(sym)
					break
				}
				if t.nodes[slot] <= 0 { // 0 = empty (node 0 is the root, never a child)
					t.nodes = append(t.nodes, 0, 0)
					t.nodes[slot] = int32(len(t.nodes)/2 - 1)
				}
				node = int(t.nodes[slot])
			}
		}
		huffTrees[src.id] = t
	}
}

// decode reads one codeword and returns its symbol index, or -1 when the
// bits do not form a codeword (only possible in incomplete trees, which
// these are not) or the read ran past the end.
func (t *huffTree) decode(b *bitReader) int {
	node := 0
	for depth := 0; depth < 24; depth++ {
		v := t.nodes[2*node+b.bit()]
		if v < 0 {
			return int(^v)
		}
		if v == 0 {
			return -1
		}
		node = int(v)
	}
	return -1
}
