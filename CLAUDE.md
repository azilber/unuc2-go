# CLAUDE.md

Operating guide for agents working in this repo. Read before changing decode code.

## Overview

`unuc2-go` is a pure-Go, **decode-only** port of Jan Bobrowski's C library
`libunuc2` — it unpacks UltraCompressor II (UC2) archives. No cgo; the only
third-party dependency is `spf13/cobra` (CLI). Requires Go 1.26+.

## Build / test / lint

```bash
make            # build ./bin/unuc2
make test       # go test ./...
make race       # go test -race ./...   <- ALWAYS run after touching concurrency
make vet        # go vet ./...
make fmt        # gofmt -w .
make lint       # golangci-lint (if installed)
make clean      # remove ./bin
```

Lint config is `.golangci.yml`; it excludes errcheck on `fmt.Fprint*` /
`io.WriteString` (writes to already-open stdout/stderr). Keep the tree
`gofmt`-clean and `golangci-lint`-clean (0 issues).

## Architecture

- `uc2/` — library. Files: `bits` (LSB-word/MSB-bit reader), `csum` (block
  checksum), `huffman` (adaptive Huffman), `ultra` (LZ77 + 64 KiB ring window),
  `delta` (byte delta filters), `master` (dictionary resolution + method
  dispatch), `cdir` (central-directory scan), `names`/`cp850` (CP850→UTF-8, 8.3,
  long names), `embed` (`super.bin`), `errors`, `uc2` (public API).
- `cmd/unuc2/` — CLI (cobra). `main` (flags + modes + output formatting), `tree`
  (entry tree, duplicate→version folding), `match` (glob selection, mark/visit),
  `pool` (bounded worker pool).

Public API: `Open(io.ReaderAt)`, `Entries`, `Extract(e, w)`, `Prepare(entries)`,
`Label`, `Identify`. Errors are sentinels: `ErrDamaged`, `ErrTruncated`,
`ErrUnimplemented`, `ErrNotUC2`, `ErrBadState`, `ErrInternal` (use `errors.Is`).

Decode flow: read header → inflate central directory → scan records (dir/file/
master/end) → resolve master dictionaries → ultra-decode each file.

## Invariants (change these and decoding breaks silently — surfaces only as `ErrDamaged`)

- **`uc2/super.bin`** is the compressed **supermaster** dictionary — opaque,
  curated data that **cannot be regenerated in code**. It must stay `//go:embed`'d
  in `uc2/`. It inflates (ultra method 4, NoMaster) to exactly **49,152 bytes with
  checksum `0x1E55`** — `TestSupermaster` is the primary end-to-end self-test.
- UC2 magic `0x1a324355`; checksum seed `0xA55A`; Huffman lookup is 13-bit, 344
  symbols (256 byte + 60 distance + 28 length); end-of-block distance `125*512+1`.
- Master ids: `0` = supermaster, `1` = none (512 zero bytes), `≥2` =
  archive-defined; the sentinel `0xdededede` maps to the supermaster.
- Method → codec: `1–9` ultra (no delta); `21–29` delta 1; `30–39` delta n−29;
  `40–49` delta n−39. Unimplemented (return `ErrUnimplemented`): `80` Turbo,
  version `203` PCP, multi-volume (`volume ≠ 1`).

## Intentional deviation from the C source

In `uc2/ultra.go`, sized streams (file / master / supermaster) stop as soon as
`cb.limit` reaches 0 instead of reading the trailing end-of-stream bit — this
avoids over-reading a bounded input (e.g. the embedded `super.bin`). The central
directory uses a huge limit and still terminates on the natural end bit. Output
is byte-identical. **Preserve this** when editing the decode loop; don't
"restore" the trailing-bit read.

## Concurrency

`Archive` is single-goroutine by default (`Extract` lazily inflates shared
dictionaries). After `Prepare(entries)` resolves the supermaster and all needed
masters, those dictionaries are immutable and `Extract` may be called
**concurrently on distinct entries** (`io.ReaderAt` is parallel-safe). No mutex
is held across decode I/O — that was deliberate; keep it that way. The CLI
parallelizes **extract** and **test** via `cmd/unuc2/pool.go` (`-j/--jobs`,
default `NumCPU`). A single file is inherently sequential (LZ back-references);
pipe (`-p`) stays sequential to preserve stdout order. Run `make race` after any
change here.

## Conventions

- **Faithful port.** Keep internals close to the C and cite `libunuc2.c` sections
  in comments; prefer correctness-preserving translation over refactors.
- **CLI output must stay byte-identical to the reference C `unuc2`**: CP850→UTF-8
  names, DOS attribute string `adlshr`, `;version` suffixes, tab vs aligned
  columns, and the `Label:` line. `pflag` clustered shorthands (`-laT`, `-p`) are
  preserved so the Midnight Commander VFS script keeps working.
- **No personal identifiers** (usernames, `/home/...` or other absolute machine
  paths) in `.md` files.

## Testing

In-repo tests are self-contained; the sample-archive fixture was removed.
`TestSupermaster` is the end-to-end anchor (it drives bits → Huffman → ultra →
checksum). For full parity, build the reference C `unuc2` and diff its output
against this tool on any UC2 archive (see README).

## Credits / license

Original UltraCompressor II by **Nico de Vries**; the C `libunuc2`/`unuc2` this
port is based on is by **Jan Bobrowski** (LGPL-3.0 / GPL-3.0). This port follows
the same licensing.
