package main

import (
	"path"
	"strconv"
	"strings"
)

// Selection logic ported from unuc2.c (mark / match_pattern / visit_selected).

type cause int

const (
	visitFile cause = iota
	enterDir
	leaveDir
)

// mark selects a node. When visit is set it also selects the node's children
// (only the newest version unless allVersions), and always marks the path up to
// the root as "to be visited" (unuc2.c mark).
func (t *tree) mark(n *node, visit, allVersions bool) {
	if n.marked {
		return
	}
	n.marked = true
	if visit {
		n.visit = true
		for _, c := range n.children {
			if allVersions || c.version == 0 {
				t.mark(c, true, allVersions)
			}
		}
	}
	for p := n.parent; p != nil && !p.visit; p = p.parent {
		p.visit = true
	}
}

const (
	modeIntermediateDirs = iota
	modeFilesAndSpecificDirs
	modeDirs
)

// matchPattern selects entries matching a (possibly multi-segment) glob pattern,
// honoring ";N" / ";*" version selectors (unuc2.c match_pattern).
func (t *tree) matchPattern(pattern string, allVersions bool) {
	selected := []*node{t.root}
	version := 0
	if allVersions {
		version = -1
	}
	p := pattern
	for {
		var mode int
		var rest string
		if slash := strings.IndexByte(p, '/'); slash < 0 {
			mode = modeFilesAndSpecificDirs
			p, version = parseVersionSuffix(p, version)
		} else {
			mode = modeIntermediateDirs
			rest = p[slash+1:]
			if rest == "" {
				mode = modeDirs
			}
			p = p[:slash]
		}

		var next []*node
		for _, dir := range selected {
			for _, ne := range dir.children {
				if !ne.entry.IsDir {
					if mode == modeFilesAndSpecificDirs &&
						(version < 0 || ne.version == version) &&
						fnmatch(p, ne.entry.Name) {
						t.mark(ne, false, allVersions)
					}
					continue
				}
				if mode == modeIntermediateDirs {
					if fnmatch(p, ne.entry.Name) {
						next = append(next, ne)
					}
					continue
				}
				if ne.entry.Name == p || fnmatch(p, ne.entry.Name) {
					t.mark(ne, mode == modeDirs, allVersions)
				}
			}
		}

		if mode != modeIntermediateDirs {
			return
		}
		selected = next
		p = rest
	}
}

// parseVersionSuffix strips a trailing ";*" (all versions) or ";N" (specific
// version) selector from a final path segment.
func parseVersionSuffix(p string, version int) (string, int) {
	if len(p) <= 2 {
		return p, version
	}
	if strings.HasSuffix(p, ";*") {
		return p[:len(p)-2], -1
	}
	if isDigit(p[len(p)-1]) {
		i := len(p)
		for i > 3 && isDigit(p[i-1]) {
			i--
		}
		if i >= 1 && p[i-1] == ';' {
			if v, err := strconv.Atoi(p[i:]); err == nil {
				return p[:i-1], v
			}
		}
	}
	return p, version
}

// fnmatch reports whether name matches the shell glob pattern. Segments never
// contain '/', so path.Match's semantics coincide with the C fnmatch call.
func fnmatch(pattern, name string) bool {
	ok, err := path.Match(pattern, name)
	return err == nil && ok
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

// visitSelected walks marked/visited nodes: files first (visitFile), then
// subdirectories (enterDir, recurse, leaveDir). The callback returns false to
// stop early (unuc2.c visit_selected).
func visitSelected(dir *node, cb func(*node, cause) (bool, error)) (bool, error) {
	for _, ne := range dir.children {
		if ne.entry.IsDir || (!ne.visit && !ne.marked) {
			continue
		}
		if ok, err := cb(ne, visitFile); err != nil || !ok {
			return ok, err
		}
	}
	for _, ne := range dir.children {
		if !ne.entry.IsDir || (!ne.visit && !ne.marked) {
			continue
		}
		if ok, err := cb(ne, enterDir); err != nil || !ok {
			return ok, err
		}
		if ok, err := visitSelected(ne, cb); err != nil || !ok {
			return ok, err
		}
		if !ne.marked {
			continue
		}
		if ok, err := cb(ne, leaveDir); err != nil || !ok {
			return ok, err
		}
	}
	return true, nil
}
