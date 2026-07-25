// Command unuc2 lists, tests and extracts UltraCompressor II (UC2) archives. It
// is a pure-Go reimplementation of Jan Bobrowski's unuc2 tool, built on the
// sibling uc2 package.
package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/azilber/unuc2-go/uc2"
	"github.com/spf13/cobra"
)

// version is overrideable at build time with -ldflags "-X main.version=…".
var version = "0.8"

type options struct {
	list      bool
	namesOnly bool
	all       bool
	test      bool
	overwrite bool
	pipe      bool
	toStdout  bool
	tab       bool
	verbose   bool
	dest      string
	chdir     string
	noMeta    int // 1: skip dir meta, 2: also skip file meta
	jobs      int // parallel decompression workers
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "unuc2:", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	opt := &options{}
	cmd := &cobra.Command{
		Use:   "unuc2 [flags] archive.uc2 [files...]",
		Short: "List, test and extract UltraCompressor II (UC2) archives",
		Long: "unuc2 extracts UltraCompressor II (UC2) archives.\n\n" +
			"With no [files], the whole archive is selected; otherwise each\n" +
			"argument is a shell glob (with optional ;VERSION selector).",
		Args:          cobra.ArbitraryArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd, opt, args)
		},
	}

	f := cmd.Flags()
	f.BoolVarP(&opt.list, "list", "l", false, "list contents")
	f.BoolVarP(&opt.namesOnly, "names-only", "1", false, "list names only (implies --list)")
	f.BoolVarP(&opt.all, "all", "a", false, "all versions of duplicated files")
	f.BoolVarP(&opt.test, "test", "t", false, "test (decode without writing)")
	f.BoolVarP(&opt.overwrite, "overwrite", "f", false, "overwrite existing files")
	f.StringVarP(&opt.dest, "dest", "d", "", "destination directory to extract to")
	f.StringVarP(&opt.chdir, "chdir", "C", "", "change to directory before extracting")
	f.BoolVarP(&opt.pipe, "pipe", "p", false, "decompress to stdout")
	f.BoolVarP(&opt.toStdout, "stdout", "c", false, "decompress to stdout (alias of --pipe)")
	f.CountVarP(&opt.noMeta, "no-meta", "D", "do not set dir times/perms (repeat for files too)")
	f.BoolVarP(&opt.tab, "tab", "T", false, "tab-separated listing")
	f.BoolVarP(&opt.verbose, "verbose", "v", false, "verbose / print version")
	f.IntVarP(&opt.jobs, "jobs", "j", runtime.NumCPU(), "parallel decompression workers (extract/test)")
	return cmd
}

func run(cmd *cobra.Command, opt *options, args []string) error {
	if opt.namesOnly {
		opt.list = true
	}
	if opt.toStdout {
		opt.pipe = true
	}
	out := cmd.OutOrStdout()

	if len(args) == 0 {
		if opt.verbose { // "-v" alone prints the version
			fmt.Fprintln(out, version)
			return nil
		}
		return fmt.Errorf("archive not given")
	}

	if opt.chdir != "" {
		if err := os.Chdir(opt.chdir); err != nil {
			return err
		}
	}

	archivePath := args[0]
	patterns := args[1:]

	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	arch, err := uc2.Open(f)
	if err != nil {
		return fmt.Errorf("%s: %w", archivePath, err)
	}

	t := newTree()
	for _, e := range arch.Entries() {
		if t.add(e) {
			fmt.Fprintf(cmd.ErrOrStderr(), "unuc2: missing dir of %s\n", e.Name)
		}
	}

	if len(patterns) == 0 {
		t.mark(t.root, true, opt.all)
	} else {
		for _, p := range patterns {
			t.matchPattern(p, opt.all)
		}
	}

	app := &app{opt: opt, arch: arch, out: out, errw: cmd.ErrOrStderr()}

	switch {
	case opt.list:
		return app.runList(t)
	case opt.test:
		return app.runTest(t)
	case opt.pipe:
		return app.runPipe(t)
	default:
		return app.runExtract(t)
	}
}

// jobs returns the effective worker count (at least 1).
func (a *app) jobs() int {
	if a.opt.jobs < 1 {
		return 1
	}
	return a.opt.jobs
}

type app struct {
	opt  *options
	arch *uc2.Archive
	out  io.Writer
	errw io.Writer
}

func (a *app) sep() byte {
	if a.opt.tab {
		return '\t'
	}
	return ' '
}

// runList prints the selected entries (unuc2.c list mode).
func (a *app) runList(t *tree) error {
	sizeW := 0
	if !a.opt.tab {
		var max uint32
		_, _ = visitSelected(t.root, func(ne *node, c cause) (bool, error) {
			if c == visitFile && ne.entry.Size > max {
				max = ne.entry.Size
			}
			return true, nil
		})
		sizeW = len(strconv.FormatUint(uint64(max), 10))
	}
	_, err := visitSelected(t.root, func(ne *node, c cause) (bool, error) {
		if ne.marked && c != leaveDir {
			a.printEntry(ne, sizeW)
		}
		return true, nil
	})
	if err != nil {
		return err
	}
	if !a.opt.tab {
		if label := a.arch.Label(); label != "" {
			fmt.Fprintf(a.out, "Label: %s\n", label)
		}
	}
	return nil
}

// runPipe decompresses selected files to stdout, in order (unuc2.c pipe mode).
// Output is an ordered byte stream, so this path stays sequential.
func (a *app) runPipe(t *tree) error {
	_, err := visitSelected(t.root, func(ne *node, c cause) (bool, error) {
		if c != visitFile {
			return true, nil
		}
		e := ne.entry
		if err := a.arch.Extract(e, a.out); err != nil {
			fmt.Fprintf(a.errw, "unuc2: %s: %v\n", e.Name, err)
		}
		return true, nil
	})
	return err
}

// runTest decodes selected files (verifying checksums) without writing output,
// in parallel across workers. Each file's messages are buffered and emitted in
// tree order, so the output is identical to a sequential run (unuc2.c test mode).
func (a *app) runTest(t *tree) error {
	files := collectFiles(t)
	if err := a.arch.Prepare(entriesOf(files)); err != nil {
		return err
	}
	outs := make([]bytes.Buffer, len(files))
	errs := make([]bytes.Buffer, len(files))
	runPool(len(files), a.jobs(), func(i int) {
		e := files[i].entry
		if a.opt.verbose {
			fmt.Fprintf(&outs[i], "Testing %s %d bytes\n", e.Name, e.Size)
		}
		if err := a.arch.Extract(e, io.Discard); err != nil {
			fmt.Fprintf(&errs[i], "unuc2: %s: %v\n", e.Name, err)
		}
	})
	for i := range files {
		_, _ = a.out.Write(outs[i].Bytes())
		_, _ = a.errw.Write(errs[i].Bytes())
	}
	return nil
}

// fileTask is one file to extract to disk at a resolved absolute path.
type fileTask struct {
	path string
	ne   *node
}

// dirTask is a directory whose metadata is applied after its contents.
type dirTask struct {
	path string
	ne   *node
}

// runExtract writes selected files (and directories) to disk (unuc2.c extract).
// Directories are created and the task list is built sequentially (preserving
// parent-before-child order); files are then decompressed in parallel; finally
// per-file messages and directory metadata are applied in tree order.
func (a *app) runExtract(t *tree) error {
	prefix := ""
	if a.opt.dest != "" {
		prefix = strings.TrimRight(a.opt.dest, "/") + "/"
	}

	// Phase 1 (sequential): create directories, collect file/dir tasks in order.
	var files []fileTask
	var dirs []dirTask
	var stack []string
	if _, err := visitSelected(t.root, func(ne *node, c cause) (bool, error) {
		switch c {
		case visitFile:
			files = append(files, fileTask{prefix + ne.entry.Name, ne})
		case enterDir:
			stack = append(stack, prefix)
			prefix += ne.entry.Name + "/"
			if err := os.Mkdir(prefix, 0o777); err != nil && !os.IsExist(err) {
				return false, err
			}
		case leaveDir:
			dirs = append(dirs, dirTask{strings.TrimRight(prefix, "/"), ne})
			prefix = stack[len(stack)-1]
			stack = stack[:len(stack)-1]
		}
		return true, nil
	}); err != nil {
		return err
	}

	// Phase 2 (parallel): decompress files. Each task writes only to its own slot.
	entries := make([]*uc2.Entry, len(files))
	for i, ft := range files {
		entries[i] = ft.ne.entry
	}
	if err := a.arch.Prepare(entries); err != nil {
		return err
	}
	errs := make([]bytes.Buffer, len(files))
	runPool(len(files), a.jobs(), func(i int) {
		ft := files[i]
		e := ft.ne.entry
		if _, err := os.Stat(ft.path); err == nil && !a.opt.overwrite {
			fmt.Fprintf(&errs[i], "unuc2: %s: file exists\n", ft.path)
			return
		}
		f, err := os.Create(ft.path)
		if err != nil {
			fmt.Fprintf(&errs[i], "unuc2: %s: %v\n", ft.path, err)
			return
		}
		if err := a.arch.Extract(e, f); err != nil {
			fmt.Fprintf(&errs[i], "unuc2: %s: %v\n", e.Name, err)
		}
		if err := f.Close(); err != nil {
			fmt.Fprintf(&errs[i], "unuc2: %s: %v\n", ft.path, err)
		}
		if a.opt.noMeta < 2 {
			setAttrs(ft.path, e)
		}
	})

	// Phase 3 (sequential): emit messages and apply directory metadata in order.
	for i := range files {
		_, _ = a.errw.Write(errs[i].Bytes())
	}
	if a.opt.noMeta < 1 {
		for _, dt := range dirs {
			setAttrs(dt.path, dt.ne.entry)
		}
	}
	return nil
}

// collectFiles returns the selected file nodes in tree order.
func collectFiles(t *tree) []*node {
	var files []*node
	_, _ = visitSelected(t.root, func(ne *node, c cause) (bool, error) {
		if c == visitFile {
			files = append(files, ne)
		}
		return true, nil
	})
	return files
}

// entriesOf extracts the uc2 entries from a slice of nodes.
func entriesOf(nodes []*node) []*uc2.Entry {
	es := make([]*uc2.Entry, len(nodes))
	for i, n := range nodes {
		es[i] = n.entry
	}
	return es
}

// printEntry renders one listing row (unuc2.c print_entry).
func (a *app) printEntry(ne *node, sizeW int) {
	e := ne.entry
	sep := a.sep()
	var b strings.Builder

	if !a.opt.namesOnly {
		b.WriteString(attrString(e.Attr))
		b.WriteByte(sep)
		b.WriteString(formatTime(e.DOSTime, sep == ' '))
		b.WriteByte(sep)
		if sep == ' ' {
			if e.IsDir {
				fmt.Fprintf(&b, "%*s", sizeW, "")
			} else {
				fmt.Fprintf(&b, "%*d", sizeW, e.Size)
			}
		} else if !e.IsDir {
			b.WriteString(strconv.FormatUint(uint64(e.Size), 10))
		}
		b.WriteByte(sep)
	}
	if e.DirID != 0 {
		b.WriteString(dirPath(ne))
	}
	b.WriteString(e.Name)
	if !a.opt.namesOnly {
		if e.IsDir && sep == ' ' {
			b.WriteByte('/')
		}
		if ne.version != 0 {
			if sep == ' ' {
				b.WriteByte(';')
			} else {
				b.WriteByte(sep)
			}
			b.WriteString(strconv.Itoa(ne.version))
		}
	}
	b.WriteByte('\n')
	io.WriteString(a.out, b.String())
}

// dirPath builds the "dir/sub/" prefix for an entry (unuc2.c print_dir_path).
func dirPath(ne *node) string {
	p := ne.parent
	if p == nil {
		return "?/"
	}
	prefix := ""
	if p.entry.DirID != 0 {
		prefix = dirPath(p)
	}
	return prefix + p.entry.Name + "/"
}

// attrString renders MS-DOS attribute bits as "adlshr" (unuc2.c print_entry).
func attrString(attr uint8) string {
	const names = "adlshr"
	out := []byte(names)
	a := attr
	for i := range out {
		if a&0x20 == 0 {
			out[i] = '-'
		}
		a <<= 1
	}
	return string(out)
}

// formatTime renders a packed MS-DOS timestamp, padded to 19 columns when
// aligned (unuc2.c print_time).
func formatTime(t uint32, pad bool) string {
	s := ""
	if t != 0 {
		s = fmt.Sprintf("%04d-%02d-%02d %02d:%02d",
			1980+(t>>25), t>>21&15, t>>16&31, t>>11&31, t>>5&63)
		if sec := t << 1 & 62; sec < 60 {
			s += fmt.Sprintf(":%02d", sec)
		}
	}
	if pad && len(s) < 19 {
		s += strings.Repeat(" ", 19-len(s))
	}
	return s
}

// setAttrs restores the modification time and read-only permission of a path
// (unuc2.c set_attrs).
func setAttrs(path string, e *uc2.Entry) {
	if dt := e.DOSTime; dt != 0 {
		tm := time.Date(
			1980+int(dt>>25), time.Month(dt>>21&15), int(dt>>16&31),
			int(dt>>11&31), int(dt>>5&63), int(dt<<1&62), 0, time.Local)
		_ = os.Chtimes(path, tm, tm)
	}
	if e.Attr&uc2.AttrReadOnly != 0 {
		_ = os.Chmod(path, 0o444)
	}
}
