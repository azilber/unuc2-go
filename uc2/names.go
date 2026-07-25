package uc2

import "unicode/utf8"

// Name handling (libunuc2.c "names" section): CP850 → UTF-8 conversion, DOS 8.3
// assembly, and Win95 long-name decoding.

// appendUTF8 appends CP850 byte c to dst as UTF-8, optionally lower-casing.
func appendUTF8(dst []byte, lower bool, c byte) []byte {
	if c < 128 {
		if lower && c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		return append(dst, c)
	}
	tbl := &cp850
	if lower {
		tbl = &cp850Lower
	}
	var buf [utf8.UTFMax]byte
	n := utf8.EncodeRune(buf[:], rune(tbl[c-128]))
	return append(dst, buf[:n]...)
}

// dosNameField expands a raw 11-byte record name field into a space-padded 8.3
// name (libunuc2.c copy_dos_name).
func dosNameField(s []byte) [11]byte {
	var out [11]byte
	d := 0
	z := 8
	si := 0
	readByte := func() (byte, bool) {
		if si >= len(s) {
			return 0, false
		}
		c := s[si]
		si++
		return c, true
	}
	for {
		for {
			c, ok := readByte()
			if !ok || c == 0 {
				for d < z {
					out[d] = ' '
					d++
				}
				break
			}
			out[d] = c
			d++
			if d >= z {
				break
			}
		}
		d = 8
		if d < z {
			break
		}
		z = d + 3
	}
	return out
}

// assembleName builds "name.ext" (lower-cased, space-trimmed) from a 8.3 field
// (libunuc2.c assemble_name).
func assembleName(dosName [11]byte) string {
	dst := make([]byte, 0, 16)
	s := 0
	z := 8
	for {
		for z > s && dosName[z-1] == ' ' {
			z--
		}
		if s > 0 {
			if s == z {
				break
			}
			dst = append(dst, '.')
		}
		for s < z {
			dst = appendUTF8(dst, true, dosName[s])
			s++
		}
		s = 8
		if s < z {
			break
		}
		z = s + 3
	}
	return string(dst)
}

// longName decodes a Win95 long-name tag (CP850, case preserved) up to the first
// NUL (libunuc2.c copy_long_name).
func longName(data []byte) string {
	dst := make([]byte, 0, len(data))
	for _, c := range data {
		if c == 0 {
			break
		}
		dst = appendUTF8(dst, false, c)
	}
	return string(dst)
}
