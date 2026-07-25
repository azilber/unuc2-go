package uc2

// csum is UC2's streaming block checksum (libunuc2.c csum_*). It folds the data
// into a 16-bit XOR of little-endian words, seeded with 0xA55A. The 0x10000 bit
// is a carry marker: when a chunk ends on an odd byte the high bit is set so the
// next chunk consumes one byte to realign — this lets the checksum be computed
// incrementally across flushed blocks and still match a whole-buffer pass.
type csum struct {
	value uint32
}

func newCsum() csum {
	return csum{value: 0xA55A}
}

func (cs *csum) update(p []byte) {
	n := len(p)
	if n == 0 {
		return
	}
	v := cs.value
	e := n - 1
	i := 0
	if v > 0xffff {
		v ^= uint32(p[0]) << 8
		i = 1
	}
	for i < e {
		v ^= uint32(p[i]) | uint32(p[i+1])<<8
		i += 2
	}
	v &= 0xffff
	if i == e {
		v ^= uint32(p[i]) | 0x10000
	}
	cs.value = v
}

func (cs *csum) get() uint16 {
	return uint16(cs.value)
}
