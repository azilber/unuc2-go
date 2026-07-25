// Package uc2 decodes UltraCompressor II (UC2) archives, a DOS-era format by
// Nico de Vries. It is a pure-Go port of Jan Bobrowski's libunuc2 C library
// (torinak.com/~jb/unuc2) and decodes the "ultra" method (LZ77 + adaptive
// Huffman) including the built-in supermaster dictionary, archive-defined master
// dictionaries and delta filters. Read-only: it extracts, it does not create.
package uc2

import "io"

// MS-DOS file attribute bits reported in Entry.Attr.
const (
	AttrReadOnly = 1
	AttrHidden   = 2
	AttrSystem   = 4
	AttrDir      = 16
	AttrArchive  = 32
)

// Tag is a Win95/AIP extended metadata record attached to an entry.
type Tag struct {
	Name string
	Data []byte
}

// Entry describes one archive member (a file or a directory).
type Entry struct {
	DirID   uint32 // parent directory id (0 = root)
	ID      uint32 // directory's own id (directories only)
	IsDir   bool
	Size    uint32 // uncompressed size (files)
	CSize   uint32 // compressed size (files)
	DOSTime uint32 // packed MS-DOS date/time
	Attr    uint8
	DOSName [11]byte // space-padded 8.3 name
	Name    string   // resolved UTF-8 name
	HasTags bool
	Tags    []Tag

	// extraction info
	offset uint32
	master uint32
	csum   uint16
	method uint16
}

// Archive is an opened UC2 archive.
//
// By default an Archive is single-goroutine: Extract lazily inflates shared
// master dictionaries on first use. Once Prepare has resolved the dictionaries
// for a set of entries, Extract may be called concurrently on distinct entries
// from that set — the resolved dictionaries are then immutable and the archive
// reader (an io.ReaderAt) is safe for parallel reads.
type Archive struct {
	r io.ReaderAt

	supermaster []byte
	masters     map[uint32]*masterInfo

	cdirBuf []byte
	entries []*Entry
	label   string

	scanned bool
	pcp     bool
}

// Identify reports whether magic looks like the start of a UC2 archive. It
// accepts as few as 4 bytes and grows more confident with more (up to ~29).
func Identify(magic []byte) bool {
	const (
		wantMagic = 0x1a324355
		amag      = 0x01b2c3d4
	)
	if len(magic) < 4 {
		return false
	}
	if le.Uint32(magic[0:]) != wantMagic {
		return false
	}
	if len(magic) < 12 {
		return true
	}
	compLen := le.Uint32(magic[4:])
	if compLen != le.Uint32(magic[8:])-amag {
		return false
	}
	total := compLen + 13
	if len(magic) < 21 {
		return true
	}
	if le.Uint32(magic[13:]) != 1 { // cdir volume
		return false
	}
	if le.Uint32(magic[17:]) >= total { // cdir offset
		return false
	}
	return true
}

// Open reads an archive's header and inflates its central directory. The reader
// must remain valid for the lifetime of the returned Archive (Extract reads from
// it lazily).
func Open(r io.ReaderAt) (*Archive, error) {
	a := &Archive{r: r, masters: map[uint32]*masterInfo{}}

	head := make([]byte, 29) // FHEAD + XHEAD
	if err := a.readAt(head, 0); err != nil {
		return nil, err
	}
	if !Identify(head) {
		return nil, ErrNotUC2
	}

	ver := le.Uint16(head[26:]) // versionNeededToExtract
	if ver >= 203 {
		if ver > 203 {
			return nil, ErrNotUC2
		}
		a.pcp = true
	}

	cdirOffset := le.Uint32(head[17:])
	cdirCsum := le.Uint16(head[21:])
	buf, err := a.decompressCdir(cdirOffset, cdirCsum)
	if err != nil {
		return nil, err
	}
	a.cdirBuf = buf
	if err := a.scan(); err != nil {
		return nil, err
	}
	return a, nil
}

// Entries returns all archive members in central-directory order (directories
// precede their contents; duplicate names appear oldest-first).
func (a *Archive) Entries() []*Entry {
	return a.entries
}

// Label returns the archive's MS-DOS volume label, or "" if none.
func (a *Archive) Label() string {
	return a.label
}

// Prepare inflates and caches every master dictionary that the given entries
// depend on (including the built-in supermaster and any master chains), so that
// subsequent Extract calls on those entries touch only immutable shared state.
// Call it once, from a single goroutine, before extracting entries concurrently.
// It is idempotent and safe to pass entries that need no master.
func (a *Archive) Prepare(entries []*Entry) error {
	if !a.scanned {
		return ErrBadState
	}
	if err := a.ensureSupermaster(); err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir {
			continue
		}
		if err := a.resolveMaster(e.master); err != nil {
			return err
		}
	}
	return nil
}

// Extract decompresses a file entry, streaming the result to w. It verifies the
// entry's checksum and returns ErrDamaged on mismatch. Directory entries produce
// no output.
//
// Extract may be called concurrently on distinct entries once Prepare has
// resolved their master dictionaries; see Archive.
func (a *Archive) Extract(e *Entry, w io.Writer) error {
	if !a.scanned {
		return ErrBadState
	}
	if e.IsDir {
		return nil
	}
	if err := a.resolveMaster(e.master); err != nil {
		return err
	}
	rd := a.stream(int64(e.offset))
	_, sum, err := a.decompress(int(e.method), rd, w, e.master, e.Size)
	if err != nil {
		return err
	}
	if sum != e.csum {
		return ErrDamaged
	}
	return nil
}

// readAt fills p from absolute offset off, requiring a full read.
func (a *Archive) readAt(p []byte, off int64) error {
	n, err := a.r.ReadAt(p, off)
	if n == len(p) {
		return nil
	}
	if err == nil || err == io.EOF {
		return ErrTruncated
	}
	return err
}

// stream returns a sequential reader over the archive starting at off.
func (a *Archive) stream(off int64) io.Reader {
	return &readerAtStream{r: a.r, off: off}
}

// readerAtStream adapts an io.ReaderAt to a sequential io.Reader.
type readerAtStream struct {
	r   io.ReaderAt
	off int64
}

func (s *readerAtStream) Read(p []byte) (int, error) {
	n, err := s.r.ReadAt(p, s.off)
	s.off += int64(n)
	if err == io.EOF && n > 0 {
		err = nil
	}
	return n, err
}
