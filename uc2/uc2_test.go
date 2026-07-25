package uc2

import "testing"

// TestSupermaster inflates the embedded super.bin and checks it produces the
// exact 48 KiB dictionary with the expected checksum. This exercises the whole
// bit-reader / adaptive-Huffman / ultra-LZ / checksum stack.
func TestSupermaster(t *testing.T) {
	a := &Archive{masters: map[uint32]*masterInfo{}}
	if err := a.ensureSupermaster(); err != nil {
		t.Fatalf("ensureSupermaster: %v", err)
	}
	if len(a.supermaster) != superMasterSize {
		t.Fatalf("supermaster size = %d, want %d", len(a.supermaster), superMasterSize)
	}
	cs := newCsum()
	cs.update(a.supermaster)
	if got := cs.get(); got != superMasterCsum {
		t.Fatalf("supermaster csum = %#04x, want %#04x", got, superMasterCsum)
	}
}
