package wda

import (
	"fmt"
	"strings"
	"testing"

	"github.com/devicelab-dev/maestro-runner/pkg/core"
	"github.com/devicelab-dev/maestro-runner/pkg/flow"
)

// screen wraps elements in a 390x844 application.
func screen(elements ...string) string {
	return `<AppiumAUT><XCUIElementTypeApplication type="XCUIElementTypeApplication" x="0" y="0" width="390" height="844">` +
		strings.Join(elements, "") + `</XCUIElementTypeApplication></AppiumAUT>`
}

// el is one page-source element; children, if any, go inside it.
func el(kind, attrs string, y int, children ...string) string {
	return fmt.Sprintf(`<XCUIElementType%[1]s type="XCUIElementType%[1]s" %[2]s x="20" y="%[3]d" width="300" height="40">%[4]s</XCUIElementType%[1]s>`,
		kind, attrs, y, strings.Join(children, ""))
}

// lookupBoth finds sel in source once without the switch and once with it,
// and leaves the switch off.
func lookupBoth(t *testing.T, source string, sel flow.Selector) (plain, strict *core.ElementInfo) {
	t.Helper()
	server := sourceServer(t, source)
	defer server.Close()
	t.Setenv("MAESTRO_STRICT_SELECTORS", "")
	plain, _ = createTestDriver(server).findElementOnce(sel)
	t.Setenv("MAESTRO_STRICT_SELECTORS", "1")
	strict, _ = createTestDriver(server).findElementOnce(sel)
	t.Setenv("MAESTRO_STRICT_SELECTORS", "")
	return plain, strict
}

func yOf(info *core.ElementInfo) string {
	if info == nil {
		return "none"
	}
	return fmt.Sprint(info.Bounds.Y)
}

// below compares tops: an element is below an anchor when its top is below
// the anchor's top (Filters.kt:165-167), not when it clears the anchor's
// bottom.
func TestStrictBelowComparesTops(t *testing.T) {
	source := screen(
		el("StaticText", `label="Header"`, 100),
		el("StaticText", `label="Badge"`, 120),
	)
	plain, strict := lookupBoth(t, source, flow.Selector{Text: "Badge", Below: &flow.Selector{Text: "Header"}})
	if plain != nil {
		t.Fatalf("setup: without the switch an overlapping element is not below, got y=%s", yOf(plain))
	}
	if yOf(strict) != "120" {
		t.Errorf("with the switch found y=%s, want the badge at y=120", yOf(strict))
	}
}

// Matches stay in tree order: the first row in the tree wins, not the row
// nearest the anchor; and a row counts when it is below any anchor, not only
// the first anchor that has rows below it.
func TestStrictRelativeKeepsTreeOrderAndAnyAnchor(t *testing.T) {
	nearest := screen(
		el("StaticText", `label="Header"`, 100),
		el("StaticText", `label="Row"`, 600),
		el("StaticText", `label="Row"`, 200),
	)
	plain, strict := lookupBoth(t, nearest, flow.Selector{Text: "Row", Below: &flow.Selector{Text: "Header"}})
	if yOf(plain) != "200" || yOf(strict) != "600" {
		t.Errorf("nearest vs tree order: without the switch y=%s (want 200), with it y=%s (want 600)", yOf(plain), yOf(strict))
	}

	anyAnchor := screen(
		el("StaticText", `label="Row"`, 200),
		el("StaticText", `label="Tab"`, 400),
		el("StaticText", `label="Tab"`, 100),
		el("StaticText", `label="Row"`, 500),
	)
	plain, strict = lookupBoth(t, anyAnchor, flow.Selector{Text: "Row", Below: &flow.Selector{Text: "Tab"}})
	if yOf(plain) != "500" || yOf(strict) != "200" {
		t.Errorf("first anchor vs any anchor: without the switch y=%s (want 500), with it y=%s (want 200)", yOf(plain), yOf(strict))
	}
}

// The deepest match in each branch, then the first of those in tree order
// (Filters.kt:284-297, Maestro.kt:544). The runner took the deepest match
// anywhere.
func TestStrictDeepestPerBranchThenFirst(t *testing.T) {
	source := screen(
		el("Cell", `label="Save"`, 100, el("StaticText", `label="Save"`, 110)),
		el("Other", ``, 300, el("Other", ``, 300, el("Other", ``, 300, el("Button", `label="Save"`, 320)))),
	)
	plain, strict := lookupBoth(t, source, flow.Selector{Text: "Save"})
	if yOf(plain) != "320" || yOf(strict) != "110" {
		t.Errorf("without the switch y=%s (want the deepest overall, 320), with it y=%s (want the first branch's deepest, 110)", yOf(plain), yOf(strict))
	}
}

// index sorts by position, top to bottom then left to right (Filters.kt:33-36,
// 241-252). An index past the end matches nothing.
func TestStrictIndexSortsByPosition(t *testing.T) {
	source := screen(
		el("StaticText", `label="Row"`, 300),
		el("StaticText", `label="Row"`, 100),
		el("StaticText", `label="Row"`, 200),
	)
	for _, tc := range []struct {
		index, plain, strict string
	}{
		{"0", "300", "100"},
		{"1", "100", "200"},
		{"-1", "200", "300"},
		{"5", "300", "none"},
	} {
		plain, strict := lookupBoth(t, source, flow.Selector{Text: "Row", Index: tc.index})
		if yOf(plain) != tc.plain || yOf(strict) != tc.strict {
			t.Errorf("index %s: without the switch y=%s (want %s), with it y=%s (want %s)", tc.index, yOf(plain), tc.plain, yOf(strict), tc.strict)
		}
	}
}

// Maestro taps the element it found; the runner moved a page-source hit to
// its nearest clickable ancestor.
func TestStrictTapsTheElementItself(t *testing.T) {
	source := screen(el("Cell", ``, 100, el("StaticText", `label="Open"`, 115)))
	server := sourceServer(t, source)
	defer server.Close()

	info, err := createTestDriver(server).findElementForTap(flow.Selector{Text: "Open"}, false, 300)
	if err != nil || info.Bounds.Y != 100 {
		t.Fatalf("setup: without the switch the tap goes to the cell: %+v, %v", info, err)
	}
	t.Setenv("MAESTRO_STRICT_SELECTORS", "1")
	info, err = createTestDriver(server).findElementForTap(flow.Selector{Text: "Open"}, false, 300)
	if err != nil {
		t.Fatal(err)
	}
	if info.Bounds.Y != 115 {
		t.Errorf("with the switch the tap goes to y=%d, want the text itself at y=115", info.Bounds.Y)
	}
}

// containsChild means a direct child in the tree (Filters.kt:198-205), not an
// element whose bounds cover the child's. Here the card's bounds do not
// cover its button, as happens with a container whose content overflows it.
func TestStrictContainsChildIsTheTree(t *testing.T) {
	source := screen(el("Other", `name="card"`, 100, el("Button", `label="Buy"`, 150)))
	plain, strict := lookupBoth(t, source, flow.Selector{ContainsChild: &flow.Selector{Text: "Buy"}})
	if yOf(plain) == "100" {
		t.Fatal("setup: without the switch the lookup is by bounds")
	}
	if yOf(strict) != "100" {
		t.Errorf("with the switch found y=%s, want the card that holds the button (y=100)", yOf(strict))
	}

	desc := screen(
		el("Other", `name="list"`, 100, el("Other", ``, 100, el("StaticText", `label="Total"`, 120))),
		el("Other", `name="empty"`, 400),
	)
	_, strict = lookupBoth(t, desc, flow.Selector{ID: "list|empty", ContainsDescendants: []*flow.Selector{{Text: "Total"}}})
	if yOf(strict) != "100" {
		t.Errorf("containsDescendants found y=%s, want the list whose subtree holds Total (y=100)", yOf(strict))
	}
}

// childOf searches the parent's subtree, the parent included
// (Orchestra.kt:1435-1441).
func TestStrictChildOf(t *testing.T) {
	source := screen(
		el("Other", `name="panel-a"`, 100, el("StaticText", `label="Total"`, 120)),
		el("Other", `name="panel-b"`, 400, el("StaticText", `label="Total"`, 420)),
	)
	_, strict := lookupBoth(t, source, flow.Selector{Text: "Total", ChildOf: &flow.Selector{ID: "panel-b"}})
	if yOf(strict) != "420" {
		t.Errorf("childOf panel-b found y=%s, want 420", yOf(strict))
	}
	_, strict = lookupBoth(t, source, flow.Selector{ID: "panel-b", ChildOf: &flow.Selector{ID: "panel-b"}})
	if yOf(strict) != "400" {
		t.Errorf("childOf includes the parent itself: found y=%s, want 400", yOf(strict))
	}
	_, strict = lookupBoth(t, source, flow.Selector{Text: "Total", ChildOf: &flow.Selector{ID: "panel-c"}})
	if strict != nil {
		t.Errorf("a missing parent should find nothing, got y=%s", yOf(strict))
	}
}

// An anchor is resolved by its own full selector, index included.
func TestStrictAnchorWithIndex(t *testing.T) {
	source := screen(
		el("StaticText", `label="Tab"`, 100),
		el("StaticText", `label="Row"`, 150),
		el("StaticText", `label="Tab"`, 400),
		el("StaticText", `label="Row"`, 450),
	)
	_, strict := lookupBoth(t, source, flow.Selector{Text: "Row", Below: &flow.Selector{Text: "Tab", Index: "1"}})
	if yOf(strict) != "450" {
		t.Errorf("below the second tab found y=%s, want 450", yOf(strict))
	}
}

// A count counts the deepest matches, as Maestro chooses among them.
func TestStrictCountCountsDeepestMatches(t *testing.T) {
	elements, err := ParsePageSource(screen(el("Cell", `label="Save"`, 100, el("StaticText", `label="Save"`, 110))))
	if err != nil {
		t.Fatal(err)
	}
	if got := CountVisibleMatches(elements, flow.Selector{Text: "Save"}, 390, 844); got != 2 {
		t.Fatalf("setup: without the switch the cell and its text both count, got %d", got)
	}
	t.Setenv("MAESTRO_STRICT_SELECTORS", "1")
	if got := CountVisibleMatches(elements, flow.Selector{Text: "Save"}, 390, 844); got != 1 {
		t.Errorf("with the switch count = %d, want 1: the cell gives way to its text", got)
	}
}

func TestParseIndex(t *testing.T) {
	for in, want := range map[string]int{"0": 0, "2": 2, "-1": -1, "1.0": 1, " 3 ": 3} {
		if got, err := parseIndex(in); err != nil || got != want {
			t.Errorf("parseIndex(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	if _, err := parseIndex("first"); err == nil {
		t.Error("a word is not an index")
	}
}
