package opus

// PVQ codeword decoding (RFC 6716 section 4.3.4.2), the generic
// (SMALL_FOOTPRINT) path of celt/cwrs.c.

// ncwrsURow computes U(n,0..k+1) into u and returns V(n,k).
func ncwrsURow(n, k int, u []uint32) uint32 {
	l := k + 2
	u[0] = 0
	u[1] = 1
	for j := 2; j < l; j++ {
		u[j] = uint32(j<<1) - 1
	}
	for j := 2; j < n; j++ {
		unext(u[1:], k+1, 1)
	}
	return u[k] + u[k+1]
}

func unext(ui []uint32, n int, ui0 uint32) {
	j := 1
	for {
		ui1 := ui[j] + ui[j-1] + ui0
		ui[j-1] = ui0
		ui0 = ui1
		j++
		if j >= n {
			break
		}
	}
	ui[j-1] = ui0
}

func uprev(ui []uint32, n int, ui0 uint32) {
	j := 1
	for {
		ui1 := ui[j] - ui[j-1] - ui0
		ui[j-1] = ui0
		ui0 = ui1
		j++
		if j >= n {
			break
		}
	}
	ui[j-1] = ui0
}

func cwrsi(n, k int, i uint32, y []int, u []uint32) {
	for j := 0; j < n; j++ {
		p := u[k+1]
		s := 0
		if i >= p {
			s = -1
			i -= p
		}
		yj := k
		p = u[k]
		for p > i && k > 0 {
			k--
			p = u[k]
		}
		i -= p
		yj -= k
		y[j] = (yj + s) ^ s
		uprev(u, k+2, 0)
	}
}

// decodePulses decodes a vector of n entries with k unit pulses.
func decodePulses(y []int, n, k int, d *rangeDecoder, u []uint32) {
	u = u[:k+2]
	v := ncwrsURow(n, k, u)
	cwrsi(n, k, d.uint(v), y, u)
}
