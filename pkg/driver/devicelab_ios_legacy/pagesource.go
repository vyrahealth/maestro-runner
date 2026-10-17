package devicelab_ios_legacy

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/devicelab-dev/maestro-runner/pkg/core"
	"github.com/devicelab-dev/maestro-runner/pkg/flow"
)

// findInSnapshot resolves a Maestro selector against a flat node list from
// the runner. Returns the best match (or all candidates for index= queries).
// Selector semantics mirror the existing wda driver: substring/regex on
// text+label+value+placeholder, exact-or-regex on id (accessibility
// identifier), state filters, width/height tolerance.
func findInSnapshot(nodes []SnapshotNode, sel flow.Selector) []*SnapshotNode { //nolint:unused
	if len(nodes) == 0 {
		return nil
	}
	var hits []*SnapshotNode
	for i := range nodes {
		n := &nodes[i]
		if matchesSelector(n, sel) {
			hits = append(hits, n)
		}
	}
	return hits
}

func matchesSelector(n *SnapshotNode, sel flow.Selector) bool {
	// State filters first — cheap.
	if sel.Enabled != nil && n.Enabled != *sel.Enabled {
		return false
	}
	if sel.Selected != nil && n.Selected != *sel.Selected {
		return false
	}
	if sel.Focused != nil && n.Focused != *sel.Focused {
		return false
	}

	// Text matches against label, value, or placeholder. Empty selector
	// text means "no text constraint".
	if sel.Text != "" && !matchesText(sel.Text, n.Label, n.Value, n.PlaceholderValue) {
		return false
	}

	// ID matches against accessibility identifier.
	if sel.ID != "" && !matchesID(sel.ID, n.Identifier) {
		return false
	}

	// Size matching (with tolerance) — used by graphical assertions.
	if sel.Width > 0 || sel.Height > 0 {
		if !withinTolerance(int(n.Rect.Width), sel.Width, sel.Tolerance) {
			return false
		}
		if !withinTolerance(int(n.Rect.Height), sel.Height, sel.Tolerance) {
			return false
		}
	}

	return true
}

// matchesID reports whether an id selector matches an accessibility
// identifier as Maestro's idMatches does: a case-insensitive regex over the
// whole identifier, so `id: enriched-text` is not `set-enriched-text-button`
// (#128, #188). An empty pattern matches anything.
func matchesID(pattern, id string) bool {
	if pattern == "" {
		return true
	}
	return core.MatchesIDMaestro(pattern, id)
}

// preferExactID narrows a set of id-selector matches to only those whose
// identifier equals the selector id exactly, when at least one such exact
// match exists. matchesID ignores case, as Maestro does, so of "Login" and
// "login" the one written as in the selector wins. It was added when
// matchesID matched substrings and `id: enriched-text` could resolve to
// `set-enriched-text-button` (#128). No-op for empty/regex ids or when no
// exact match is present.
func preferExactID(hits []SnapshotNode, sel flow.Selector) []SnapshotNode {
	if sel.ID == "" || looksLikeRegex(sel.ID) || len(hits) < 2 {
		return hits
	}
	var exact []SnapshotNode
	for _, n := range hits {
		if n.Identifier == sel.ID {
			exact = append(exact, n)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	return hits
}

// preferExactText narrows survivors to those whose text matches the literal
// pattern exactly, when any of them do.
//
// Added when literal text matched by contains, so `text: "0"` resolved to a
// price field reading "7000.00" ahead of the switch whose text is exactly "0"
// (#161). Text now matches whole, as in Maestro (#188), so "0" no longer
// matches "7000.00" at all; this still puts an exact value ahead of one a
// dotted or line-wrapped selector matches only as a regex.
//
// Applied AFTER the full selector has been satisfied, never instead of it. An
// exact text match that skipped the rest of the selector would return an
// element with the right text and the wrong id — the OR behaviour removed in
// #157/#158/#160. Reported by @nt-ben-leblond (#161).
//
// Same rule as preferExactID above, for text.
func preferExactText(hits []SnapshotNode, sel flow.Selector) []SnapshotNode {
	if sel.Text == "" || looksLikeRegex(sel.Text) || len(hits) < 2 {
		return hits
	}
	var exact []SnapshotNode
	for _, n := range hits {
		if strings.EqualFold(n.Label, sel.Text) || strings.EqualFold(n.Value, sel.Text) || strings.EqualFold(n.PlaceholderValue, sel.Text) {
			exact = append(exact, n)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	return hits
}

// matchesText returns true if `pattern` matches any of the given texts, as
// Maestro's textMatches does: the selector is a case-insensitive regex that
// must match a whole value, so "Open" is not "Talk · Open" (#188); a partial
// match is written `.*Open.*`. An empty pattern matches anything.
// snapshotMatching puts matches in the pattern's own case first when several
// elements match (#151).
func matchesText(pattern string, texts ...string) bool {
	if pattern == "" {
		return true
	}
	return core.MatchesTextMaestro(pattern, texts...)
}

func withinTolerance(actual, expected, tolerance int) bool {
	if expected == 0 {
		return true
	}
	delta := actual - expected
	if delta < 0 {
		delta = -delta
	}
	return delta <= tolerance
}

// looksLikeRegex returns true if `text` contains characters that suggest
// the caller meant it as a regex pattern. Matches WDA's heuristic.
func looksLikeRegex(text string) bool {
	for _, c := range text {
		switch c {
		case '*', '+', '?', '^', '$', '\\', '(', ')', '[', ']', '{', '}', '|':
			return true
		}
	}
	return false
}

// toElementInfo converts a SnapshotNode to core.ElementInfo for return to
// the flow runner. Bounds use ints — XCUI gives us floats but Maestro
// selectors expect ints. We round to nearest.
func toElementInfo(n *SnapshotNode) *core.ElementInfo {
	if n == nil {
		return nil
	}
	return &core.ElementInfo{
		ID:                 n.Identifier,
		Text:               firstNonEmpty(n.Value, n.Label, n.PlaceholderValue),
		Class:              n.Type,
		AccessibilityLabel: n.Label,
		Bounds: core.Bounds{
			X:      int(round(n.Rect.X)),
			Y:      int(round(n.Rect.Y)),
			Width:  int(round(n.Rect.Width)),
			Height: int(round(n.Rect.Height)),
		},
		Visible:  n.Rect.Width > 0 && n.Rect.Height > 0,
		Enabled:  n.Enabled,
		Focused:  n.Focused,
		Selected: n.Selected,
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func round(f float64) float64 {
	if f >= 0 {
		return float64(int(f + 0.5))
	}
	return float64(int(f - 0.5))
}

// selectByIndex picks one element from a multi-match using selector.Index.
// Supports numeric indices (0, 1, …) and the special string "0" for first.
func selectByIndex(candidates []*SnapshotNode, index string) *SnapshotNode {
	if len(candidates) == 0 {
		return nil
	}
	if index == "" {
		return candidates[0]
	}
	i, err := strconv.Atoi(index)
	if err != nil {
		return candidates[0]
	}
	// An index past the matches names no element; it used to fall back to
	// the first match (#188).
	if i < 0 || i >= len(candidates) {
		return nil
	}
	return candidates[i]
}

// preferExactCase puts the nodes a regex text selector matches in its own case
// ahead of those it matches only when case is ignored, keeping order otherwise,
// so `^SIGN OUT$` picks the "SIGN OUT" button over a "Sign out" row (#151).
func preferExactCase(nodes []SnapshotNode, pattern string) []SnapshotNode {
	if len(nodes) < 2 || !looksLikeRegex(pattern) {
		return nodes
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nodes
	}
	var exact, rest []SnapshotNode
	for _, n := range nodes {
		hit := false
		for _, t := range []string{n.Label, n.Value, n.PlaceholderValue} {
			if t != "" && re.MatchString(t) {
				hit = true
				break
			}
		}
		if hit {
			exact = append(exact, n)
		} else {
			rest = append(rest, n)
		}
	}
	return append(exact, rest...)
}
