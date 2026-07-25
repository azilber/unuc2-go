package uc2

import _ "embed"

// superCompressed is the UC2 "supermaster" preset dictionary, shipped in its
// original compressed form (UC2 ultra method 4). It is opaque, curated data —
// a 48 KiB corpus of common byte patterns baked into UltraCompressor II — and
// cannot be generated algorithmically. At runtime resolveMaster inflates it
// once into the 49,152-byte supermaster used to decompress files and masters
// that reference it. The inflated result must have checksum 0x1E55.
//
//go:embed super.bin
var superCompressed []byte

const (
	superMasterSize = 49152 // 48 KiB inflated supermaster
	superMasterCsum = 0x1E55
)
