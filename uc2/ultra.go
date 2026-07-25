package uc2

import "io"

// The "ultra" method: LZ77 over a 64 KiB circular window seeded with a master
// dictionary, with adaptive Huffman-coded literals, distances and lengths
// (libunuc2.c "ultra"/"cbuf"/"decompress" sections).

// Distance codes: payload = extraBits<<20 | 1<<16 | base. Bit 16 marks a match
// (vs a literal byte, whose payload is just its value < 256).
func dcode(base, extra uint32) uint32 { return extra<<20 | 1<<16 | base }

// Length codes: payload = extraBits<<20 | base.
func lcode(base, extra uint32) uint32 { return extra<<20 | base }

var dCodes = [numDistSym]uint32{
	dcode(1, 0), dcode(2, 0), dcode(3, 0), dcode(4, 0), dcode(5, 0), dcode(6, 0), dcode(7, 0), dcode(8, 0),
	dcode(9, 0), dcode(10, 0), dcode(11, 0), dcode(12, 0), dcode(13, 0), dcode(14, 0), dcode(15, 0), dcode(16, 4),
	dcode(32, 4), dcode(48, 4), dcode(64, 4), dcode(80, 4), dcode(96, 4), dcode(112, 4), dcode(128, 4), dcode(144, 4),
	dcode(160, 4), dcode(176, 4), dcode(192, 4), dcode(208, 4), dcode(224, 4), dcode(240, 4), dcode(256, 8), dcode(512, 8),
	dcode(768, 8), dcode(1024, 8), dcode(1280, 8), dcode(1536, 8), dcode(1792, 8), dcode(2048, 8), dcode(2304, 8), dcode(2560, 8),
	dcode(2816, 8), dcode(3072, 8), dcode(3328, 8), dcode(3584, 8), dcode(3840, 8), dcode(4096, 12), dcode(8192, 12), dcode(12288, 12),
	dcode(16384, 12), dcode(20480, 12), dcode(24576, 12), dcode(28672, 12), dcode(32768, 12), dcode(36864, 12), dcode(40960, 12), dcode(45056, 12),
	dcode(49152, 12), dcode(53248, 12), dcode(57344, 12), dcode(61440, 12),
}

var lCodes = [numLenSym]uint32{
	lcode(3, 0), lcode(4, 0), lcode(5, 0), lcode(6, 0), lcode(7, 0), lcode(8, 0), lcode(9, 0), lcode(10, 0),
	lcode(11, 1), lcode(13, 1), lcode(15, 1), lcode(17, 1), lcode(19, 1), lcode(21, 1), lcode(23, 1), lcode(25, 1),
	lcode(27, 3), lcode(35, 3), lcode(43, 3), lcode(51, 3), lcode(59, 3), lcode(67, 3), lcode(75, 3), lcode(83, 3),
	lcode(91, 6), lcode(155, 9), lcode(667, 11), lcode(2715, 15),
}

// eobMark is the distance value that terminates a block. It is 125*512+1,
// unreachable by any real back-reference into the 64 KiB window.
const eobMark = 125*512 + 1

// The block loop keeps producing until fewer than this many bytes of window
// space remain — enough to hold one maximum-length copy (2715 + 32767).
const flushThreshold = 35482

// cbuffer is the 64 KiB circular output window. head/tail wrap mod 65536, so
// back-references naturally reach into the master dictionary and prior output.
type cbuffer struct {
	head  uint16
	tail  uint16
	limit uint32
	csum  csum
	data  [0x10000]byte
}

func (cb *cbuffer) have() uint16 { return cb.tail - cb.head }

func (cb *cbuffer) space() uint32 { return uint32(len(cb.data)) - uint32(cb.have()) - 1 }

// flush drains produced bytes to wr, updating the checksum and (if delta is
// active) reverting the delta filter through dbuf.
func (cb *cbuffer) flush(wr io.Writer, db *delta, dbuf []byte) error {
	for {
		n := uint32(cb.have())
		if n == 0 {
			return nil
		}
		if u := 0x10000 - uint32(cb.head); n > u {
			n = u
		}
		if cb.limit < n {
			n = cb.limit
		}
		p := cb.data[cb.head : uint32(cb.head)+n]
		cb.csum.update(p)
		if dbuf != nil {
			db.revert(dbuf[:n], p)
			p = dbuf[:n]
		}
		if _, err := wr.Write(p); err != nil {
			return err
		}
		cb.head += uint16(n)
		cb.limit -= n
		if cb.limit == 0 {
			return nil
		}
	}
}

type ultra struct {
	bi      *bitReader
	dc      dcinfo
	cb      cbuffer
	bdTable [lookupSize]uint32
	lTable  [lookupSize]uint32
}

// decodeHT reads the block header. It returns more=false at the end of the
// stream, or builds the byte/distance and length tables for the next block.
func (u *ultra) decodeHT() (more bool, err error) {
	b, err := u.bi.get(1)
	if err != nil {
		return false, err
	}
	if b == 0 {
		return false, nil
	}
	var lengths [numSymbols]uint8
	if err := htDec(lengths[:], &u.dc, u.bi, u.bdTable[:]); err != nil {
		return false, err
	}
	if err := htMktree(u.bdTable[:], lengths[:], numByteSym, numDistSym, dCodes[:]); err != nil {
		return false, err
	}
	if err := htMktree(u.lTable[:], lengths[numByteSym+numDistSym:], 0, numLenSym, lCodes[:]); err != nil {
		return false, err
	}
	return true, nil
}

// decompressBlock emits literals and copies into the window until the window is
// nearly full (more=true) or the end-of-block marker is hit (more=false).
func (u *ultra) decompressBlock() (more bool, err error) {
	for {
		c, err := huff(u.bdTable[:], u.bi)
		if err != nil {
			return false, err
		}
		if c&(1<<16) == 0 {
			u.cb.data[u.cb.tail] = byte(c)
			u.cb.tail++
		} else {
			dist := c & 0xffff
			if extra := c >> 20 & 0xf; extra != 0 {
				e, err := u.bi.get(uint(extra))
				if err != nil {
					return false, err
				}
				dist += e
			}

			c, err = huff(u.lTable[:], u.bi)
			if err != nil {
				return false, err
			}

			if dist == eobMark {
				return false, nil
			}

			length := c & 0xffff
			if extra := c >> 20 & 0xf; extra != 0 {
				e, err := u.bi.get(uint(extra))
				if err != nil {
					return false, err
				}
				length += e
			}
			for ; length > 0; length-- {
				u.cb.data[u.cb.tail] = u.cb.data[u.cb.tail-uint16(dist)]
				u.cb.tail++
			}
		}
		if u.cb.space() < flushThreshold {
			return true, nil
		}
	}
}

// decompressUltra runs the ultra decoder end to end, returning the number of
// bytes produced and the block checksum.
func (a *Archive) decompressUltra(master uint32, deltaSize uint8, rd io.Reader, wr io.Writer, limit uint32) (produced uint32, sum uint16, err error) {
	u := &ultra{bi: newBitReader(rd)}

	size, err := a.useMaster(u.cb.data[:], master)
	if err != nil {
		return 0, 0, err
	}
	u.cb.limit = limit
	u.cb.head = uint16(size)
	u.cb.tail = uint16(size)
	u.cb.csum = newCsum()

	var dbuf []byte
	var db delta
	if deltaSize != 0 {
		if master != masterSuper {
			db = newDelta(deltaSize)
			db.apply(u.cb.data[:u.cb.tail])
		}
		dbuf = make([]byte, len(u.cb.data))
		db = newDelta(deltaSize)
	}

	u.dc.init()
	for {
		more, err := u.decodeHT()
		if err != nil {
			return 0, 0, err
		}
		if !more {
			break
		}
		for {
			blockMore, berr := u.decompressBlock()
			if ferr := u.cb.flush(wr, &db, dbuf); ferr != nil {
				return 0, 0, ferr
			}
			// A sized stream (file/master/supermaster) is complete once the
			// expected byte count is produced; stop before reading the next
			// header, which for a bounded input would run past its end. The
			// unbounded cdir (huge limit) instead terminates on the trailing
			// end-of-stream bit read by decodeHT.
			if u.cb.limit == 0 {
				return limit, u.cb.csum.get(), nil
			}
			if berr != nil {
				return 0, 0, berr
			}
			if !blockMore {
				break
			}
		}
	}

	return limit - u.cb.limit, u.cb.csum.get(), nil
}
