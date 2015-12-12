package main

import (
	"strings"
)

func NewTree(backend *httpRoute) *node {
	return &node{
		backend:  backend,
		children: make(map[string]*node, 0),
	}
}

type node struct {
	children map[string]*node
	backend  *httpRoute
}

// insert a new route into the tree, replacing entry if it exists
func (n *node) Insert(path string, backend *httpRoute) {
	cur := n
	for part, i := slice(path, 0); ; part, i = slice(path, i) {
		if part != "" {
			child, _ := cur.children[part]
			if child == nil {
				// insert node if it doesn't exist
				child = &node{children: make(map[string]*node, 0)}
				cur.children[part] = child
			}
			cur = child
		}
		if i == -1 {
			break
		}
	}
	cur.backend = backend // finally set the backend for this node
}

// lookup returns the best match for a given path
func (n *node) Lookup(path string) *httpRoute {
	cur := n
	prev := n
	for part, i := slice(path, 0); ; part, i = slice(path, i) {
		if part != "" {
			cur = cur.children[part]
			if cur == nil {
				// can't progress any deeper, return last backend we saw
				return prev.backend
			}
			if cur.backend != nil {
				prev = cur // update last seen backend
			}
		}
		if i == -1 {
			break
		}
	}
	if cur.backend != nil {
		return cur.backend
	}
	return prev.backend
}

type ancestor struct {
	node *node
	part string
}

func (n *node) Remove(path string) {
	ancestors := make([]ancestor, 0) // record visited ancestors
	cur := n
	for part, i := slice(path, 0); ; part, i = slice(path, i) {
		if part != "" {
			ancestors = append(ancestors, ancestor{part: part, node: cur})
			cur = cur.children[part]
			if cur == nil {
				return // nothing to delete
			}
		}
		if i == -1 {
			break
		}
	}
	cur.backend = nil // nil out the backend for this node
	// if this is a leaf iterate over the ancestors cleaning up empty nodes
	if len(cur.children) == 0 {
		for i := len(ancestors) - 1; i >= 0; i-- { // we go backwards
			parent := ancestors[i].node
			part := ancestors[i].part
			delete(parent.children, part)
			if parent.backend != nil || len(parent.children) > 0 {
				break // node is either not empty or has children
			}
		}
	}
}

// slices string into path segments performing 0 heap allocation
func slice(path string, start int) (segment string, next int) {
	if len(path) == 0 || start < 0 || start >= len(path) {
		return "", -1
	}
	end := strings.IndexRune(path[start:], '/')
	if end == -1 {
		return path[start:], -1
	}
	if path[start:start+end] == "/" {
		return "", start + end + 1
	}
	return path[start : start+end], start + end + 1
}
