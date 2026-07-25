package uc2

import "testing"

func TestIdentify(t *testing.T) {
	// "CU2\x1a" little-endian magic 0x1a324355.
	magic := []byte{0x55, 0x43, 0x32, 0x1a}
	if !Identify(magic) {
		t.Error("Identify rejected valid magic")
	}
	if Identify([]byte{0, 1, 2, 3}) {
		t.Error("Identify accepted bad magic")
	}
	if Identify([]byte{0x55}) {
		t.Error("Identify accepted too-short input")
	}
}

func TestAssembleName(t *testing.T) {
	cases := []struct {
		in   [11]byte
		want string
	}{
		{[11]byte{'A', 'U', 'T', 'O', 'E', 'X', 'E', 'C', 'B', 'A', 'T'}, "autoexec.bat"},
		{[11]byte{'R', 'E', 'A', 'D', 'M', 'E', ' ', ' ', 'T', 'X', 'T'}, "readme.txt"},
		{[11]byte{'A', ' ', ' ', ' ', ' ', ' ', ' ', ' ', 'A', 'N', 'S'}, "a.ans"},
		{[11]byte{'N', 'O', 'E', 'X', 'T', ' ', ' ', ' ', ' ', ' ', ' '}, "noext"},
	}
	for _, c := range cases {
		if got := assembleName(c.in); got != c.want {
			t.Errorf("assembleName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDeltaRoundTrip(t *testing.T) {
	orig := []byte{10, 20, 30, 9, 19, 29, 8, 18, 28}
	work := append([]byte(nil), orig...)
	enc := newDelta(3)
	enc.apply(work) // forward filter
	dec := newDelta(3)
	out := make([]byte, len(work))
	dec.revert(out, work) // inverse
	for i := range orig {
		if out[i] != orig[i] {
			t.Fatalf("delta round-trip byte %d = %d, want %d", i, out[i], orig[i])
		}
	}
}
