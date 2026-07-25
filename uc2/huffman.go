package uc2

// Adaptive Huffman decoding for the UC2 "ultra" method (libunuc2.c, "huffman"
// section). Code lengths are transmitted delta-coded against the previous
// tree; each tree is expanded into a flat 2^13-entry lookup table where every
// entry packs {length<<24 | payload}.

const (
	maxCodeBits = 13
	lookupSize  = 1 << maxCodeBits // 8192

	numByteSym = 256
	numDistSym = 60
	numLenSym  = 28
	numSymbols = numByteSym + numDistSym + numLenSym // 344

	numLoAsciiSym = 28
	numHiByteSym  = 128

	numDeltaCodes = maxCodeBits + 1 // 14
	numExtraCodes = 1
	numLenCodes   = numDeltaCodes + numExtraCodes // 15

	repeatCode = maxCodeBits + 1 // 14
	minRepeat  = 6
)

// vval is the length-delta permutation: vval[prevLen][code] yields the new code
// length. Row 0 (prevLen 0) walks lengths in ascending order; later rows put the
// most likely lengths (near the previous length) first.
var vval = [numDeltaCodes][numDeltaCodes]uint8{
	{0, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1},
	{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 0},
	{2, 1, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 0},
	{3, 2, 4, 1, 5, 6, 7, 8, 9, 10, 11, 12, 13, 0},
	{4, 3, 5, 2, 6, 1, 7, 8, 9, 10, 11, 12, 13, 0},
	{5, 4, 6, 3, 7, 2, 8, 1, 9, 10, 11, 12, 13, 0},
	{6, 5, 7, 4, 8, 3, 9, 2, 10, 1, 11, 12, 13, 0},
	{7, 6, 8, 5, 9, 4, 10, 3, 11, 2, 12, 1, 13, 0},
	{8, 7, 9, 6, 10, 5, 11, 4, 12, 3, 13, 2, 0, 1},
	{9, 8, 10, 7, 11, 6, 12, 5, 13, 4, 0, 3, 2, 1},
	{10, 9, 11, 8, 12, 7, 13, 6, 0, 5, 4, 3, 2, 1},
	{11, 10, 12, 9, 13, 8, 0, 7, 6, 5, 4, 3, 2, 1},
	{12, 11, 13, 10, 0, 9, 8, 7, 6, 5, 4, 3, 2, 1},
	{13, 12, 0, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1},
}

// defaultLengths writes the built-in code-length table (RLE-encoded as
// count,value pairs) used as the starting point and whenever the stream signals
// a reset. The counts sum to numSymbols (344).
func defaultLengths(d []uint8) {
	rle := []uint8{
		10, 9, 1, 7, 1, 9, 1, 7, 19, 9, 1, 7, 13, 8, 1, 7, 11, 8, 1, 7,
		33, 8, 1, 7, 35, 8, 128, 10, 16, 6, 12, 7, 6, 8, 10, 9, 16, 10,
		9, 4, 9, 5, 10, 6, 0,
	}
	di := 0
	s := 0
	n := rle[0]
	for {
		v := rle[s+1]
		s += 2
		for ; n > 0; n-- {
			d[di] = v
			di++
		}
		n = rle[s]
		if n == 0 {
			break
		}
	}
}

// dcinfo carries the previous tree's code lengths between blocks.
type dcinfo struct {
	symprev [numSymbols]uint8
}

func (dc *dcinfo) init() {
	defaultLengths(dc.symprev[:])
}

// huff decodes one symbol using a flat lookup table.
func huff(table []uint32, bi *bitReader) (uint32, error) {
	b, err := bi.peek(maxCodeBits)
	if err != nil {
		return 0, err
	}
	c := table[b]
	bi.skip(uint(c >> 24))
	return c & 0xffffff, nil
}

// htMktree expands canonical code lengths into the flat lookup table. Symbols
// [0,nlit) map to themselves; symbols [nlit,nlit+ncodes) map to codes[i-nlit].
func htMktree(table []uint32, lengths []uint8, nlit, ncodes int, codes []uint32) error {
	nsym := nlit + ncodes
	p := 0
	for l := 1; l <= maxCodeBits; l++ {
		for i := 0; i < nsym; i++ {
			if int(lengths[i]) != l {
				continue
			}
			n := 1 << (maxCodeBits - l)
			if p+n > lookupSize {
				return ErrDamaged
			}
			var c uint32
			if i < nlit {
				c = uint32(i)
			} else {
				c = codes[i-nlit]
			}
			c |= uint32(l) << 24
			for ; n > 0; n-- {
				table[p] = c
				p++
			}
		}
	}
	for p < lookupSize {
		table[p] = 1 << 24
		p++
	}
	return nil
}

// htDec reads the next set of symbol code lengths (libunuc2.c ht_dec). It either
// resets to the defaults or decodes a new tree that is delta-coded against the
// previous one, writing numSymbols lengths into lengths and updating dc.
func htDec(lengths []uint8, dc *dcinfo, bi *bitReader, table []uint32) error {
	t, err := bi.get(1)
	if err != nil {
		return err
	}
	if t == 0 {
		defaultLengths(dc.symprev[:])
		defaultLengths(lengths)
		return nil
	}

	sel, err := bi.get(2)
	if err != nil {
		return err
	}

	var tlengths [numLenCodes]uint8
	for i := 0; i < numLenCodes; i++ {
		b, err := bi.get(3)
		if err != nil {
			return err
		}
		tlengths[i] = uint8(b)
	}
	if err := htMktree(table, tlengths[:], numLenCodes, 0, nil); err != nil {
		return err
	}

	// Decode the delta-code stream. Its length depends on which alphabet
	// subsets (low ASCII / high bytes) the selector enables.
	var stream [numSymbols]uint8
	symp := 0
	syme := numSymbols - numLoAsciiSym - numHiByteSym
	if sel&1 != 0 {
		syme += numLoAsciiSym
	}
	if sel&2 != 0 {
		syme += numHiByteSym
	}
	var val uint8
	for symp < syme {
		c, err := huff(table, bi)
		if err != nil {
			return err
		}
		if c == repeatCode {
			c, err = huff(table, bi)
			if err != nil {
				return err
			}
			for n := int(c) + minRepeat - 1; n > 0; n-- {
				stream[symp] = val
				symp++
			}
		} else {
			val = uint8(c)
			stream[symp] = uint8(c)
			symp++
		}
	}

	// Scatter the decoded delta codes back into full symbol positions. Each
	// rle word is {count in low 9 bits, 0x200 = take from stream else length 0}.
	rle := [4][]uint16{
		{0x009, 0x202, 0x1, 0x202, 0x12, 0x260, 0x80, 0x258},
		{0x280, 0x80, 0x258},
		{0x009, 0x202, 0x1, 0x202, 0x12, 0x338},
		{0x358},
	}
	p := rle[sel]
	pi := 0
	i := 0
	sp := 0
	for {
		v := p[pi]
		pi++
		end := i + int(v&0x1ff)
		for {
			if v&0x200 != 0 {
				lengths[i] = vval[dc.symprev[i]][stream[sp]]
				sp++
			} else {
				lengths[i] = 0
			}
			i++
			if i >= end {
				break
			}
		}
		if sp >= syme {
			break
		}
	}

	copy(dc.symprev[:], lengths[:numSymbols])
	return nil
}
