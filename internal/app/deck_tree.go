package app

import (
	"sort"
	"strconv"
	"strings"
)

// DeckOption mirrors the hierarchical deck tree item used in Settings.
type DeckOption struct {
	FullName   string
	Label      string
	Depth      int
	HasSubdeck bool
}

type deckTreeNode struct {
	fullName string
	label    string
	children map[string]*deckTreeNode
}

// BuildDeckOptions organizes flat "::" Anki deck names into an indented natural-sorted tree.
func BuildDeckOptions(decks []string) []DeckOption {
	roots := make(map[string]*deckTreeNode)

	for _, raw := range decks {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		parts := strings.Split(trimmed, "::")
		curr := roots
		var pathParts []string
		for _, part := range parts {
			seg := strings.TrimSpace(part)
			if seg == "" {
				continue
			}
			pathParts = append(pathParts, seg)
			fullName := strings.Join(pathParts, "::")
			node, ok := curr[seg]
			if !ok {
				node = &deckTreeNode{
					fullName: fullName,
					label:    seg,
					children: make(map[string]*deckTreeNode),
				}
				curr[seg] = node
			}
			curr = node.children
		}
	}

	var out []DeckOption
	var walk func(m map[string]*deckTreeNode, depth int)
	walk = func(m map[string]*deckTreeNode, depth int) {
		list := make([]*deckTreeNode, 0, len(m))
		for _, n := range m {
			list = append(list, n)
		}
		sort.Slice(list, func(i, j int) bool {
			return naturalLess(list[i].label, list[j].label)
		})
		for _, n := range list {
			hasSub := len(n.children) > 0
			out = append(out, DeckOption{
				FullName:   n.fullName,
				Label:      n.label,
				Depth:      depth,
				HasSubdeck: hasSub,
			})
			if hasSub {
				walk(n.children, depth+1)
			}
		}
	}
	walk(roots, 0)
	return out
}

func FilterDeckOptions(decks []string, query string) []DeckOption {
	all := BuildDeckOptions(decks)
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return all
	}
	var out []DeckOption
	for _, opt := range all {
		if strings.Contains(strings.ToLower(opt.FullName), q) {
			out = append(out, opt)
		}
	}
	return out
}

func naturalLess(a, b string) bool {
	al := strings.ToLower(a)
	bl := strings.ToLower(b)
	ia, ib := 0, 0
	for ia < len(al) && ib < len(bl) {
		da := al[ia] >= '0' && al[ia] <= '9'
		db := bl[ib] >= '0' && bl[ib] <= '9'
		if da && db {
			ea, eb := ia, ib
			for ea < len(al) && al[ea] >= '0' && al[ea] <= '9' {
				ea++
			}
			for eb < len(bl) && bl[eb] >= '0' && bl[eb] <= '9' {
				eb++
			}
			na, _ := strconv.ParseInt(al[ia:ea], 10, 64)
			nb, _ := strconv.ParseInt(bl[ib:eb], 10, 64)
			if na != nb {
				return na < nb
			}
			ia, ib = ea, eb
			continue
		}
		if al[ia] != bl[ib] {
			return al[ia] < bl[ib]
		}
		ia++
		ib++
	}
	return len(al) < len(bl)
}

