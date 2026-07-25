package uc2

import (
	"bytes"
	"io"
)

// Master (preset dictionary) handling (libunuc2.c "master" section).
const (
	masterSuper  = 0 // built-in supermaster
	masterNone   = 1 // 512 zero bytes
	masterFirst  = 2 // first archive-defined master id
	masterDedede = 0xdededede
)

// masterInfo describes an archive-defined master: a preset dictionary that other
// files (or masters) can be compressed against. Masters may chain via com.master.
type masterInfo struct {
	id     uint32
	size   uint16
	offset uint32
	csize  uint32
	method uint16
	master uint32 // dictionary this master itself was compressed against

	data []byte // nil until resolved
}

// decompress dispatches a compression method to the ultra decoder with the
// appropriate delta stride (libunuc2.c decompressor).
func (a *Archive) decompress(method int, rd io.Reader, wr io.Writer, master uint32, limit uint32) (produced uint32, sum uint16, err error) {
	switch {
	case method >= 1 && method <= 9:
		return a.decompressUltra(master, 0, rd, wr, limit)
	case method >= 30 && method <= 39:
		return a.decompressUltra(master, uint8(method-29), rd, wr, limit)
	case method >= 40 && method <= 49:
		return a.decompressUltra(master, uint8(method-39), rd, wr, limit)
	case method >= 21 && method <= 29:
		return a.decompressUltra(master, 1, rd, wr, limit)
	case method == 80:
		return 0, 0, ErrUnimplemented // Turbo compression
	default:
		return 0, 0, ErrDamaged
	}
}

// useMaster copies the master dictionary identified by id into buf (which must
// be the 64 KiB window) and returns its length, seeding the LZ history.
func (a *Archive) useMaster(buf []byte, id uint32) (int, error) {
	switch id {
	case masterSuper:
		copy(buf[:superMasterSize], a.supermaster)
		return superMasterSize, nil
	case masterNone:
		for i := 0; i < 512; i++ {
			buf[i] = 0
		}
		return 512, nil
	default:
		mi := a.masters[id]
		if mi == nil || mi.data == nil {
			return 0, ErrDamaged
		}
		n := int(mi.size)
		copy(buf[:n], mi.data)
		return n, nil
	}
}

// ensureSupermaster lazily inflates the embedded supermaster dictionary the
// first time any master is resolved (libunuc2.c resolve_master).
func (a *Archive) ensureSupermaster() error {
	if a.supermaster != nil {
		return nil
	}
	var out bytes.Buffer
	out.Grow(superMasterSize)
	_, sum, err := a.decompress(4, bytes.NewReader(superCompressed), &out, masterNone, superMasterSize)
	if err != nil {
		return err
	}
	if out.Len() != superMasterSize || sum != superMasterCsum {
		return ErrInternal
	}
	a.supermaster = out.Bytes()
	return nil
}

// resolveMaster makes sure the supermaster and the full dependency chain for the
// given master id are inflated and cached before extraction.
func (a *Archive) resolveMaster(master uint32) error {
	if a.pcp {
		return ErrUnimplemented // PCP archives
	}
	if err := a.ensureSupermaster(); err != nil {
		return err
	}
	if master < masterFirst {
		return nil
	}

	// Walk the dependency chain outward, detecting cycles, collecting the
	// masters that still need inflating (deepest dependency last).
	var chain []*masterInfo
	seen := map[uint32]bool{}
	for master >= masterFirst {
		m := a.masters[master]
		if m == nil {
			return ErrDamaged
		}
		if m.data != nil {
			break
		}
		if seen[master] {
			return ErrDamaged // circular dependency
		}
		seen[master] = true
		chain = append(chain, m)
		master = m.master
	}

	// Inflate from the deepest dependency back up, so each master's own
	// dictionary is available when it is decoded.
	for i := len(chain) - 1; i >= 0; i-- {
		mi := chain[i]
		mi.data = make([]byte, mi.size)
		rd := io.NewSectionReader(a.r, int64(mi.offset), 1<<62)
		_, _, err := a.decompress(int(mi.method), rd, sliceWriter(mi.data), mi.master, uint32(mi.size))
		if err != nil {
			return err
		}
	}
	return nil
}

// sliceWriter returns an io.Writer that fills dst exactly, discarding overflow —
// masters decode to a known size.
func sliceWriter(dst []byte) io.Writer {
	return &sliceWr{dst: dst}
}

type sliceWr struct {
	dst []byte
	n   int
}

func (w *sliceWr) Write(p []byte) (int, error) {
	c := copy(w.dst[w.n:], p)
	w.n += c
	return len(p), nil
}
