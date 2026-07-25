package uc2

import "io"

// bitReader is UC2's LSB-word / MSB-bit reader (libunuc2.c struct bits). It
// pulls 16-bit little-endian words from the underlying reader into an
// accumulator and hands out the most-significant pending bits first.
type bitReader struct {
	bits     uint32
	haveBits uint
	head     uint
	tail     uint
	rd       io.Reader
	buffer   [4 << 10]byte
}

func newBitReader(rd io.Reader) *bitReader {
	return &bitReader{rd: rd}
}

// feed ensures at least n (n <= 16) bits are available in the accumulator.
func (bi *bitReader) feed(n uint) error {
	if bi.haveBits >= n {
		return nil
	}
	have := bi.tail - bi.head
	if have <= 1 {
		if have == 1 {
			bi.buffer[0] = bi.buffer[bi.tail-1]
		}
		bi.tail = have
		r, err := bi.rd.Read(bi.buffer[have:])
		if r <= 0 {
			if err != nil && err != io.EOF {
				return err
			}
			return ErrTruncated
		}
		bi.head = 0
		bi.tail = have + uint(r)
	}
	// Mirrors the C reader: two bytes are consumed unconditionally. On a
	// correct stream at least two bytes are always buffered here.
	bi.bits = bi.bits<<16 | uint32(bi.buffer[bi.head]) | uint32(bi.buffer[bi.head+1])<<8
	bi.head += 2
	bi.haveBits += 16
	return nil
}

func (bi *bitReader) skip(n uint) {
	bi.haveBits -= n
}

func (bi *bitReader) peek(n uint) (uint32, error) {
	if err := bi.feed(n); err != nil {
		return 0, err
	}
	return bi.bits >> (bi.haveBits - n) & ((1 << n) - 1), nil
}

func (bi *bitReader) get(n uint) (uint32, error) {
	v, err := bi.peek(n)
	if err == nil {
		bi.skip(n)
	}
	return v, err
}
