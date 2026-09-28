package wda

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/devicelab-dev/maestro-runner/pkg/core"
	"github.com/devicelab-dev/maestro-runner/pkg/flow"
)

// The page-source lookup with MAESTRO_STRICT_SELECTORS set: a port of how
// Maestro turns a selector into a filter and takes its first match
// (Orchestra.kt:1422-1499 and 1560-1718, Maestro.kt:533-552, Filters.kt).
//
// In short: the elements matching the selector's own fields are narrowed to
// the deepest match in each branch, in tree order; the relative fields keep
// those that relate to ANY element their anchor selector finds; then index
// picks by position (top to bottom, then left to right), or else the first
// is taken. On iOS Maestro has no clickable flag, so its clickable-first
// sort changes nothing. The element found is the one tapped, not a
// clickable ancestor.

// maestroFind resolves sel in the elements of a page source (in tree order)
// as Maestro's findElement does: inside the subtree of its childOf parent
// when it has one, then the first match.
func maestroFind(elements []*ParsedElement, sel flow.Selector) (*ParsedElement, error) {
	nodes := elements
	if sel.ChildOf != nil {
		parent, err := maestroParent(elements, *sel.ChildOf)
		if err != nil {
			return nil, err
		}
		nodes = subtree(parent)
	}
	matches, err := maestroFilter(nodes, sel)
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, notFound("no elements match selector")
	}
	return matches[0], nil
}

// maestroParent resolves a childOf selector (resolveParentHierarchy,
// Orchestra.kt:1549-1558): its own childOf first, then its first match.
func maestroParent(nodes []*ParsedElement, sel flow.Selector) (*ParsedElement, error) {
	if sel.ChildOf != nil {
		grandparent, err := maestroParent(nodes, *sel.ChildOf)
		if err != nil {
			return nil, err
		}
		nodes = subtree(grandparent)
	}
	matches, err := maestroFilter(nodes, sel)
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, notFound("childOf: no element matches the parent selector")
	}
	return matches[0], nil
}

// subtree is e and its descendants in tree order (TreeNode.aggregate).
func subtree(e *ParsedElement) []*ParsedElement {
	out := []*ParsedElement{e}
	for _, c := range e.Children {
		out = append(out, subtree(c)...)
	}
	return out
}

// hasOwnFields reports whether sel has fields of its own, as opposed to
// relative ones: Maestro's basic filters (Orchestra.kt:1567-1688).
func hasOwnFields(sel flow.Selector) bool {
	return sel.Text != "" || sel.ID != "" || sel.Width > 0 || sel.Height > 0 ||
		sel.Enabled != nil || sel.Selected != nil || sel.Checked != nil || sel.Focused != nil
}

// maestroFilter is the filter buildFilter makes of sel (Orchestra.kt:1560-1718),
// applied to nodes. childOf is not part of it: Maestro resolves childOf in
// findElement alone, so an anchor's or a descendant's childOf is ignored,
// as it is there.
func maestroFilter(nodes []*ParsedElement, sel flow.Selector) ([]*ParsedElement, error) {
	result := nodes
	if hasOwnFields(sel) {
		result = deepestMatches(nodes, func(e *ParsedElement) bool { return matchesSelector(e, sel) })
	}

	// Each relative filter is a set over the same nodes; intersecting keeps
	// the order of the basic matches (Filters.kt:38-43).
	keepIn := func(set map[*ParsedElement]bool) {
		var kept []*ParsedElement
		for _, e := range result {
			if set[e] {
				kept = append(kept, e)
			}
		}
		result = kept
	}
	for _, rel := range []struct {
		anchor *flow.Selector
		holds  func(e, anchor core.Bounds) bool
	}{
		// Filters.kt:165-179: tops and left edges only.
		{sel.Below, func(e, a core.Bounds) bool { return e.Y > a.Y }},
		{sel.Above, func(e, a core.Bounds) bool { return e.Y < a.Y }},
		{sel.LeftOf, func(e, a core.Bounds) bool { return e.X < a.X }},
		{sel.RightOf, func(e, a core.Bounds) bool { return e.X > a.X }},
		// Not a Maestro field: insideOf keeps its meaning (the centre inside
		// an anchor), for any anchor like the others.
		{sel.InsideOf, func(e, a core.Bounds) bool { return e.CenterInside(a) }},
	} {
		if rel.anchor == nil {
			continue
		}
		anchors, err := maestroFilter(nodes, *rel.anchor)
		if err != nil {
			return nil, err
		}
		set := make(map[*ParsedElement]bool)
		for _, e := range nodes {
			for _, a := range anchors {
				if rel.holds(e.Bounds, a.Bounds) {
					set[e] = true
					break
				}
			}
		}
		keepIn(set)
	}

	// containsChild: a direct child the child selector finds (Filters.kt:198-205).
	if sel.ContainsChild != nil {
		children, err := maestroFilter(nodes, *sel.ContainsChild)
		if err != nil {
			return nil, err
		}
		isChild := make(map[*ParsedElement]bool, len(children))
		for _, c := range children {
			isChild[c] = true
		}
		set := make(map[*ParsedElement]bool)
		for _, e := range nodes {
			for _, c := range e.Children {
				if isChild[c] {
					set[e] = true
					break
				}
			}
		}
		keepIn(set)
	}

	// containsDescendants: for every selector, a child whose subtree the
	// selector finds something in (Filters.kt:207-218).
	for _, desc := range sel.ContainsDescendants {
		if desc == nil {
			continue
		}
		found, err := descendantFinder(*desc)
		if err != nil {
			return nil, err
		}
		set := make(map[*ParsedElement]bool)
		for _, e := range nodes {
			for _, c := range e.Children {
				if found(c) {
					set[e] = true
					break
				}
			}
		}
		keepIn(set)
	}

	if sel.Index == "" {
		return result, nil
	}
	return maestroIndex(result, sel.Index)
}

// descendantFinder returns whether desc finds an element at or under a node:
// Maestro applies the descendant's whole filter to the node alone, and then
// to each of its children in turn (Filters.kt:208-210).
func descendantFinder(desc flow.Selector) (func(*ParsedElement) bool, error) {
	// An index that is not a number fails the lookup, as in Maestro.
	if desc.Index != "" {
		if _, err := parseIndex(desc.Index); err != nil {
			return nil, err
		}
	}
	memo := make(map[*ParsedElement]bool)
	var found func(e *ParsedElement) bool
	found = func(e *ParsedElement) bool {
		if v, ok := memo[e]; ok {
			return v
		}
		matches, _ := maestroFilter([]*ParsedElement{e}, desc)
		v := len(matches) > 0
		for _, c := range e.Children {
			if v {
				break
			}
			v = found(c)
		}
		memo[e] = v
		return v
	}
	return found, nil
}

// deepestMatches is Maestro's deepestMatchingElement (Filters.kt:284-297)
// over nodes: for each node the matches deepest in its subtree, where a
// match gives way to a matching descendant, in tree order and each once.
func deepestMatches(nodes []*ParsedElement, match func(*ParsedElement) bool) []*ParsedElement {
	memo := make(map[*ParsedElement][]*ParsedElement)
	var deepest func(e *ParsedElement) []*ParsedElement
	deepest = func(e *ParsedElement) []*ParsedElement {
		if r, ok := memo[e]; ok {
			return r
		}
		var r []*ParsedElement
		for _, c := range e.Children {
			r = append(r, deepest(c)...)
		}
		if len(r) == 0 && match(e) {
			r = []*ParsedElement{e}
		}
		memo[e] = r
		return r
	}
	seen := make(map[*ParsedElement]bool)
	var out []*ParsedElement
	for _, e := range nodes {
		for _, m := range deepest(e) {
			if !seen[m] {
				seen[m] = true
				out = append(out, m)
			}
		}
	}
	return out
}

// parseIndex reads index as Maestro does, a number cut to an integer
// (Orchestra.kt:1700-1702): "1" and "1.0" are 1, "-1" is the last.
func parseIndex(index string) (int, error) {
	f, err := strconv.ParseFloat(strings.TrimSpace(index), 64)
	if err != nil {
		return 0, fmt.Errorf("index %q is not a number", index)
	}
	return int(f), nil
}

// maestroIndex is Maestro's index filter (Filters.kt:33-36 and 241-252):
// the matches sorted top to bottom, then left to right, and the one at
// index, counted from the end when negative. An index past either end
// matches nothing; it does not fall back to the first.
func maestroIndex(matches []*ParsedElement, index string) ([]*ParsedElement, error) {
	idx, err := parseIndex(index)
	if err != nil {
		return nil, err
	}
	sorted := append([]*ParsedElement(nil), matches...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Bounds.Y != sorted[j].Bounds.Y {
			return sorted[i].Bounds.Y < sorted[j].Bounds.Y
		}
		return sorted[i].Bounds.X < sorted[j].Bounds.X
	})
	if idx < 0 {
		idx += len(sorted)
	}
	if idx < 0 || idx >= len(sorted) {
		return nil, nil
	}
	return []*ParsedElement{sorted[idx]}, nil
}

// strictMatch is findElementByPageSourceOnce and findElementRelativeOnce
// with MAESTRO_STRICT_SELECTORS set: Maestro's lookup over elements already
// filtered for visibility, returning the element itself.
func (d *Driver) strictMatch(sel flow.Selector, elements []*ParsedElement) (*core.ElementInfo, error) {
	e, err := maestroFind(elements, sel)
	if err != nil {
		if isNotFound(err) && sel.Text != "" {
			if closest := ClosestTexts(elements, sel.Text, 3); len(closest) > 0 {
				return nil, notFound("%v; closest on-screen texts: %s", err, strings.Join(closest, ", "))
			}
		}
		return nil, err
	}
	info := &core.ElementInfo{
		Text:    elementText(e),
		Bounds:  e.Bounds,
		Enabled: e.Enabled,
		Visible: e.Displayed,
	}
	if note := rescueNote(e); note != "" {
		info.MatchNote = note
	}
	return info, nil
}
