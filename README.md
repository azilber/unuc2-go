# unuc2-go

A **pure-Go** command-line tool and library for unpacking **UltraCompressor II
(UC2)** archives — a DOS-era packer by Nico de Vries that beat ZIP by compressing
similar files together against shared dictionaries.

## About this project

This is a from-scratch port of Jan Bobrowski's C library
[`libunuc2`](https://torinak.com/~jb/unuc2/) to pure Go — no cgo, no external
decompression libraries (the only dependency is `spf13/cobra` for the CLI). It
reimplements the UC2 "ultra" codec (LZ77 + adaptive Huffman over a 64 KiB
window), including:

- the built-in **supermaster** dictionary (the compressed `super.bin` blob,
  embedded via `//go:embed` and inflated at runtime),
- archive-defined **master dictionaries** and their dependency chains,
- the **delta** filters, CP850 → UTF-8 name handling, long names, and the
  central-directory format.

Files are decompressed **in parallel** across CPU cores. The full
reverse-engineering and porting write-up — including what `super.bin` actually
is and why it can't be replaced by generated code — is in
[CONVERSION.md](CONVERSION.md).

> Decode only: like the reference library, this tool extracts archives but does
> not create them.

## Build

Requires Go 1.26+.

```bash
make            # builds ./bin/unuc2
make install    # installs unuc2 into $GOBIN / $GOPATH/bin
make test       # run tests
make race       # run tests under the race detector
make help       # list all targets
```

Other targets: `vet`, `fmt`, `lint` (golangci-lint if installed), `tidy`,
`clean`. Without `make`, `go build ./cmd/unuc2` works too.

## CLI usage

```bash
unuc2 -l archive.uc2            # list
unuc2 -laT archive.uc2         # list, all versions, tab-separated
unuc2 -t archive.uc2           # test (decode, verify checksums, write nothing)
unuc2 -p archive.uc2 file      # decompress a file to stdout
unuc2 -d out/ archive.uc2      # extract into out/
unuc2 archive.uc2 '*.txt'      # extract matching files (glob, ;VERSION selectors)
```

Key flags: `-l/--list`, `-1/--names-only`, `-a/--all`, `-t/--test`,
`-p/--pipe`, `-d/--dest`, `-f/--overwrite`, `-C/--chdir`, `-T/--tab`,
`-D/--no-meta` (repeat to also skip file metadata), `-j/--jobs`, `-v/--verbose`.

### Parallelism

Extraction and testing decompress files **concurrently**, defaulting to
`runtime.NumCPU()` workers; use `-j/--jobs N` to change the count (`-j1` forces
sequential). A single file cannot be parallelized — the codec is inherently
sequential — so the speedup scales with the *number* of files, not their size.
Pipe-to-stdout (`-p`) stays sequential to preserve output order.

## Library usage

```go
f, _ := os.Open("archive.uc2")
defer f.Close()

arch, err := uc2.Open(f)
if err != nil { log.Fatal(err) }

for _, e := range arch.Entries() {
    if e.IsDir { continue }
    out, _ := os.Create(e.Name)
    if err := arch.Extract(e, out); err != nil {
        log.Printf("%s: %v", e.Name, err)
    }
    out.Close()
}
```

`uc2.Open` takes any `io.ReaderAt`; `Extract` streams to any `io.Writer` and
verifies the entry's checksum. For concurrent extraction, call
`arch.Prepare(entries)` once, after which `Extract` is safe to call from multiple
goroutines on distinct entries. Errors are sentinels (`uc2.ErrDamaged`,
`uc2.ErrTruncated`, `uc2.ErrUnimplemented`, …) matchable with `errors.Is`.

## Credits & license

- Original **UltraCompressor II** and the reference decompression sources:
  **Nico de Vries** — <https://nicodevries.com/professional/>.
- The C library **`libunuc2`** / **`unuc2`** this port is based on:
  **Jan Bobrowski** — <https://torinak.com/~jb/unuc2/>.

`libunuc2` is LGPL-3.0 and `unuc2` is GPL-3.0 (© Jan Bobrowski). This Go port
follows the same licensing.
