package uc2

// delta is UC2's byte delta filter (libunuc2.c delta_*). Methods 21-49 store a
// column-wise difference over a stride of 1..10 bytes; revert reconstructs the
// original bytes, apply is the inverse (used to pre-condition a master's history
// so back-references into it see undeltaed bytes).
type delta struct {
	size  uint8
	index uint8
	val   [8]uint8
}

func newDelta(size uint8) delta {
	return delta{size: size}
}

func (d *delta) revert(dst, src []byte) {
	for i := range src {
		v := src[i] + d.val[d.index]
		d.val[d.index] = v
		dst[i] = v
		d.index++
		if d.index == d.size {
			d.index = 0
		}
	}
}

func (d *delta) apply(p []byte) {
	for i := range p {
		v := p[i]
		p[i] = v - d.val[d.index]
		d.val[d.index] = v
		d.index++
		if d.index == d.size {
			d.index = 0
		}
	}
}
