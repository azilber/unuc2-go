package main

import "github.com/azilber/unuc2-go/uc2"

// The CLI mirrors the original unuc2.c: archive entries are arranged into a tree
// of nodes, then selected (by pattern or "all") and visited for listing,
// testing, piping or extraction.

type node struct {
	entry    *uc2.Entry
	parent   *node
	children []*node // in directory order
	version  int     // 0 = newest; higher = older duplicate
	visit    bool    // on the path to a marked node
	marked   bool    // explicitly selected
}

// tree holds the root node and an index of directories by id.
type tree struct {
	root    *node
	dirByID map[uint32]*node
}

func newTree() *tree {
	root := &node{entry: &uc2.Entry{IsDir: true}}
	return &tree{root: root, dirByID: map[uint32]*node{}}
}

// add inserts an entry, resolving its parent directory and folding duplicate
// file names into versions (newest first), matching unuc2.c new_entry.
func (t *tree) add(e *uc2.Entry) (missingParent bool) {
	dir := t.root
	if e.DirID != 0 {
		if d, ok := t.dirByID[e.DirID]; ok {
			dir = d
		} else {
			missingParent = true
		}
	}
	ne := &node{entry: e, parent: dir}

	if e.IsDir {
		dir.children = append(dir.children, ne)
		t.dirByID[e.ID] = ne
		return missingParent
	}

	insertAt := -1
	for i, c := range dir.children {
		if !c.entry.IsDir && c.entry.Name == e.Name {
			c.version++
			if insertAt < 0 {
				insertAt = i
			}
		}
	}
	if insertAt < 0 {
		dir.children = append(dir.children, ne)
	} else {
		dir.children = append(dir.children, nil)
		copy(dir.children[insertAt+1:], dir.children[insertAt:])
		dir.children[insertAt] = ne
	}
	return missingParent
}
