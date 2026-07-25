package uc2

import (
	"bytes"
	"encoding/binary"
)

// Central-directory parsing (libunuc2.c "cdir" section). The whole cdir is
// inflated into a buffer and then walked record by record.

// Record type tags.
const (
	recDir    = 1
	recFile   = 2
	recMaster = 3
	recEnd    = 4
)

const tagLongName = "AIP:Win95 LongN\x00"

var le = binary.LittleEndian

// cursor is a bounds-checked forward reader over the inflated cdir buffer.
type cursor struct {
	b []byte
	p int
}

func (c *cursor) take(n int) ([]byte, bool) {
	if n < 0 || c.p+n > len(c.b) {
		return nil, false
	}
	s := c.b[c.p : c.p+n]
	c.p += n
	return s, true
}

// decompressCdir inflates the central directory located at offset and verifies
// its checksum (libunuc2.c decompress_cdir).
func (a *Archive) decompressCdir(offset uint32, wantCsum uint16) ([]byte, error) {
	comp := make([]byte, 10) // COMPRESS record
	if err := a.readAt(comp, int64(offset)); err != nil {
		return nil, err
	}
	method := le.Uint16(comp[4:])
	master := le.Uint32(comp[6:])
	if master != masterNone {
		return nil, ErrDamaged
	}

	var out bytes.Buffer
	out.Grow(0x4000)
	rd := a.stream(int64(offset) + 10)
	_, sum, err := a.decompress(int(method), rd, &out, masterNone, 100_000_000)
	if err != nil {
		return nil, err
	}
	if sum != wantCsum {
		return nil, ErrDamaged
	}
	return out.Bytes(), nil
}

// scan walks the whole central directory once: it collects entries, registers
// masters, and reads the trailing volume label (libunuc2.c uc2_read_cdir +
// read_entry + uc2_get_tag + uc2_finish_cdir, folded into a single pass).
func (a *Archive) scan() error {
	cur := &cursor{b: a.cdirBuf}
	for {
		oh, ok := cur.take(1)
		if !ok {
			return ErrTruncated
		}
		switch oh[0] {
		case recFile, recDir:
			e, err := parseEntry(cur, oh[0])
			if err != nil {
				return err
			}
			if err := readTags(cur, e); err != nil {
				return err
			}
			a.entries = append(a.entries, e)

		case recMaster:
			rec, ok := cur.take(38)
			if !ok {
				return ErrTruncated
			}
			if le.Uint32(rec[30:]) != 1 { // LOCATION.volume
				return ErrUnimplemented
			}
			mi := &masterInfo{
				id:     le.Uint32(rec[0:]),
				size:   le.Uint16(rec[16:]),
				csize:  le.Uint32(rec[20:]),
				method: le.Uint16(rec[24:]),
				master: le.Uint32(rec[26:]),
				offset: le.Uint32(rec[34:]),
			}
			if mi.master == masterDedede {
				mi.master = masterSuper
			}
			a.masters[mi.id] = mi

		case recEnd:
			return a.readTail(cur)

		default:
			return ErrDamaged
		}
	}
}

// parseEntry decodes one Dir or File record into an Entry (libunuc2.c read_entry).
func parseEntry(cur *cursor, typ byte) (*Entry, error) {
	sz := 22 + 4
	if typ == recFile {
		sz = 22 + 6 + 10 + 8
	}
	rc, ok := cur.take(sz)
	if !ok {
		return nil, ErrTruncated
	}
	e := &Entry{
		DirID:   le.Uint32(rc[0:]),
		Attr:    rc[4],
		DOSTime: le.Uint32(rc[5:]),
		HasTags: rc[21] != 0,
	}
	copy(e.DOSName[:], dosNameFieldSlice(rc[9:20]))
	if typ == recFile {
		e.Size = le.Uint32(rc[22:])
		e.csum = le.Uint16(rc[26:])
		e.CSize = le.Uint32(rc[28:])
		e.method = le.Uint16(rc[32:])
		e.master = le.Uint32(rc[34:])
		if le.Uint32(rc[38:]) != 1 { // LOCATION.volume
			return nil, ErrUnimplemented
		}
		e.offset = le.Uint32(rc[42:])
	} else {
		e.ID = le.Uint32(rc[22:])
		e.IsDir = true
	}
	if !e.HasTags {
		e.Name = assembleName(e.DOSName)
	}
	return e, nil
}

// readTags consumes the tag list following a tagged entry, filling the long name
// when present (libunuc2.c uc2_get_tag).
func readTags(cur *cursor, e *Entry) error {
	if !e.HasTags {
		return nil
	}
	for {
		x, ok := cur.take(21) // EXTMETA
		if !ok {
			return ErrDamaged
		}
		size := int(le.Uint32(x[16:]))
		data, ok := cur.take(size)
		if !ok {
			return ErrDamaged
		}
		name := string(bytes.TrimRight(x[0:16], "\x00"))
		e.Tags = append(e.Tags, Tag{Name: name, Data: append([]byte(nil), data...)})
		if string(x[0:16]) == tagLongName {
			e.Name = longName(data)
		}
		if x[20] == 0 { // no more tags
			break
		}
	}
	if e.Name == "" {
		e.Name = assembleName(e.DOSName)
	}
	return nil
}

// readTail reads the archive trailer and extracts the volume label (libunuc2.c
// uc2_finish_cdir).
func (a *Archive) readTail(cur *cursor) error {
	a.scanned = true
	t, ok := cur.take(21) // XTAIL + aserial
	if !ok {
		return ErrTruncated
	}
	label := t[6:17]
	if i := bytes.IndexByte(label, 0); i >= 0 {
		label = label[:i]
	}
	a.label = string(bytes.TrimRight(label, " "))
	return nil
}

// dosNameFieldSlice adapts the raw 11-byte name field to the 8.3 expander.
func dosNameFieldSlice(s []byte) []byte {
	out := dosNameField(s)
	return out[:]
}
