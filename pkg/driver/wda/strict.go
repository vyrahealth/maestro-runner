package wda

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"

	"golang.org/x/text/unicode/norm"

	"github.com/devicelab-dev/maestro-runner/pkg/core"
	"github.com/devicelab-dev/maestro-runner/pkg/flow"
)

// strictSelectors reports whether selectors match the way Maestro's do
// (MAESTRO_STRICT_SELECTORS).
//
// Maestro reads every text and id selector as a regex that must match the
// whole of an attribute, ignoring case, with . matching a newline and ^ $ at
// line ends (Orchestra.kt:1570, 1576 and 1841). The runner's own matching is
// looser: a literal text matches as a case-insensitive substring, so a
// selector for "Resend code" also matched "Resend code in 0:42".
func strictSelectors() bool {
	return os.Getenv("MAESTRO_STRICT_SELECTORS") != ""
}

// maestroRegexes caches compiled selector regexes: the page-source matcher
// asks for one per element.
var maestroRegexes sync.Map

// maestroRegex compiles a text or id selector the way Maestro does, as a
// regex over the whole string with IGNORE_CASE, DOT_MATCHES_ALL and
// MULTILINE. A pattern that does not compile is matched as a literal, as
// Maestro's toRegexSafe does (StringUtils.kt:7-13). Go's regex syntax is
// not Java's: a pattern only Java accepts (a lookahead, say) is a literal
// here.
func maestroRegex(pattern string) *regexp.Regexp {
	if re, ok := maestroRegexes.Load(pattern); ok {
		return re.(*regexp.Regexp)
	}
	// The pattern must compile on its own before it is wrapped: `a)|(b` does
	// not, and wrapped in (?:...) it would.
	body := pattern
	if _, err := regexp.Compile(pattern); err != nil {
		body = regexp.QuoteMeta(pattern)
	}
	re, err := regexp.Compile(`(?ism)\A(?:` + body + `)\z`)
	if err != nil {
		re = regexp.MustCompile(`(?ism)\A(?:` + regexp.QuoteMeta(pattern) + `)\z`)
	}
	maestroRegexes.Store(pattern, re)
	return re
}

// maestroTextMatches is Maestro's text match (Filters.textMatches,
// Filters.kt:58-108) over the texts of an element: the regex matches the
// whole text, or the text equals the pattern as written, each also tried
// with the text's newlines read as spaces. Empty texts are tried too, as
// Maestro tries them. Both sides are NFC-normalized, as matchesText does.
func maestroTextMatches(pattern string, texts ...string) bool {
	pattern = norm.NFC.String(pattern)
	re := maestroRegex(pattern)
	for _, text := range texts {
		text = norm.NFC.String(text)
		stripped := strings.ReplaceAll(text, "\n", " ")
		if re.MatchString(text) || pattern == text || re.MatchString(stripped) || pattern == stripped {
			return true
		}
	}
	return false
}

// maestroIDMatches is Maestro's id match (Filters.idMatches,
// Filters.kt:110-132): the regex matches the whole identifier, or the part
// of it after the last '/'.
func maestroIDMatches(pattern, identifier string) bool {
	re := maestroRegex(pattern)
	if re.MatchString(identifier) {
		return true
	}
	if i := strings.LastIndex(identifier, "/"); i >= 0 {
		return re.MatchString(identifier[i+1:])
	}
	return false
}

// elementIdentifier is the accessibility identifier behind an element's
// name in the page source. Maestro matches ids against the identifier alone
// (IOSDriver.kt:217), but WDA reports the label as the name of an element
// that has no identifier (XCUIElement+FBWebDriverAttributes.m:118-126), and
// the source has no other trace of it. So a name equal to the label counts
// as no identifier. The cost: an identifier that is exactly its element's
// label is missed.
func elementIdentifier(e *ParsedElement) string {
	if e.Name == e.Label {
		return ""
	}
	return e.Name
}

// meansItself reports whether s, read as Maestro reads a selector, matches
// the literal string s. Then an exact, case-insensitive WDA comparison
// finds only elements Maestro's regex also matches: a plain literal, or one
// whose dots stand for themselves ("mastodon.social"). "Continue?" is not:
// its ? makes the "e" optional, and it goes to the page source.
func meansItself(s string) bool {
	return maestroRegex(s).MatchString(s)
}

// predicateLiteral quotes s as an NSPredicate string. ok is false for text a
// query cannot carry: control characters, or a backtick, which would end a
// class-chain predicate.
func predicateLiteral(s string) (string, bool) {
	for _, r := range s {
		if r < 0x20 || r == 0x7f || r == '`' {
			return "", false
		}
	}
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'", true
}

// strictQueryable reports whether a WDA query can decide sel. The queries
// know nothing of size or of index, which Maestro resolves over every match
// on screen; those selectors go to the page source.
func strictQueryable(sel flow.Selector) bool {
	return sel.Width == 0 && sel.Height == 0 && sel.Index == ""
}

// maestroOnScreen reports whether Maestro keeps an element on the strength
// of its own bounds: at least 10% of it on screen (UiElement.kt:31-47,
// ViewHierarchy.kt:115). A 0x0 element scores 0 and is dropped. One with a
// zero width or a zero height, not both, scores 0/0, which is NaN and not
// below 0.1, so it is kept wherever it is. Maestro never asks XCUITest
// whether an element is visible.
func maestroOnScreen(b core.Bounds, screenW, screenH int) bool {
	if b.Width == 0 && b.Height == 0 {
		return false
	}
	if b.Width == 0 || b.Height == 0 {
		return true
	}
	return b.VisiblePercentage(screenW, screenH) >= 0.1
}

// maestroVisibleTree is Maestro's filterOutOfBounds (ViewHierarchy.kt:102-128)
// over a parsed page source: an element stays when it is on screen by its
// own bounds (maestroOnScreen) or has a descendant that stays. It returns
// copies, in tree order, whose Parent and Children link only elements that
// stay, and leaves the parse as it was. A page source that would lose every
// element is kept whole, as Maestro then keeps its root unfiltered
// (ViewHierarchy.kt:29-35).
func maestroVisibleTree(elements []*ParsedElement, screenW, screenH int) []*ParsedElement {
	keep := make(map[*ParsedElement]bool, len(elements))
	// A child comes after its parent in the flattened list, so walking the
	// list backwards settles every child before its parent.
	for i := len(elements) - 1; i >= 0; i-- {
		e := elements[i]
		if keep[e] || maestroOnScreen(e.Bounds, screenW, screenH) {
			keep[e] = true
			if e.Parent != nil {
				keep[e.Parent] = true
			}
		}
	}
	if len(keep) == 0 {
		for _, e := range elements {
			keep[e] = true
		}
	}

	copies := make(map[*ParsedElement]*ParsedElement, len(keep))
	out := make([]*ParsedElement, 0, len(keep))
	for _, e := range elements {
		if !keep[e] {
			continue
		}
		c := *e
		c.Parent, c.Children = copies[e.Parent], nil
		if c.Parent != nil {
			c.Parent.Children = append(c.Parent.Children, &c)
		}
		copies[e] = &c
		out = append(out, &c)
	}
	return out
}

// strictTextByWDA is findElementByWDA's text query with
// MAESTRO_STRICT_SELECTORS set: an exact, case-insensitive comparison with
// the label, value or placeholder, run only for text that means itself (see
// meansItself). Every other text is left to the page source, where the
// matcher applies Maestro's regex.
func (d *Driver) strictTextByWDA(sel flow.Selector, stateFilter string) (*core.ElementInfo, error) {
	lit, ok := predicateLiteral(sel.Text)
	if !ok || !strictQueryable(sel) || !meansItself(sel.Text) {
		return nil, fmt.Errorf("text %q is matched in the page source", sel.Text)
	}
	predicate := fmt.Sprintf("(label ==[c] %s OR value ==[c] %s OR placeholderValue ==[c] %s)%s", lit, lit, lit, stateFilter)
	elemID, err := d.client.FindElement("predicate string", predicate)
	if err != nil || elemID == "" {
		return nil, fmt.Errorf("element not found via WDA")
	}
	return d.getElementInfo(elemID)
}

// strictIDByWDA is findElementByWDA's id query with MAESTRO_STRICT_SELECTORS
// set: a name equal to the id, ignoring case, that is not the label standing
// in for a missing identifier (see elementIdentifier). There is no CONTAINS
// fallback: Maestro's id matches the whole identifier, and `id: row` must
// not find "row-delete". An id that does not mean itself as a regex is
// matched in the page source.
func (d *Driver) strictIDByWDA(sel flow.Selector, stateFilter string) (*core.ElementInfo, error) {
	lit, ok := predicateLiteral(sel.ID)
	if !ok || !strictQueryable(sel) || !meansItself(sel.ID) {
		return nil, fmt.Errorf("id %q is matched in the page source", sel.ID)
	}
	query := fmt.Sprintf("**/XCUIElementTypeAny[`name ==[c] %s AND name != label%s`]", lit, stateFilter)
	if info, err, found := d.findByIDQuery(query); found {
		return info, err
	}
	return nil, fmt.Errorf("element not found via WDA")
}
