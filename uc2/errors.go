package uc2

import "errors"

// Sentinel errors returned by the library. They mirror the negative status
// codes of the original C library (libunuc2.c) but are exposed as idiomatic Go
// errors so callers can match them with errors.Is.
var (
	// ErrBadState is returned when the archive has not been scanned yet (call
	// Entries before Extract).
	ErrBadState = errors.New("uc2: bad state")
	// ErrDamaged indicates the archive is corrupt or fails a checksum.
	ErrDamaged = errors.New("uc2: damaged archive")
	// ErrTruncated indicates the archive ended before decoding completed.
	ErrTruncated = errors.New("uc2: truncated")
	// ErrUnimplemented is returned for features the original library never
	// implemented (PCP archives, Turbo compression method 80).
	ErrUnimplemented = errors.New("uc2: unimplemented")
	// ErrInternal indicates an internal invariant was violated (e.g. the
	// embedded supermaster failed its self-check).
	ErrInternal = errors.New("uc2: internal error")
	// ErrNotUC2 indicates the input is not a UC2 archive.
	ErrNotUC2 = errors.New("uc2: not a UC2 archive")
)
