package wda

import (
	"strings"
	"testing"

	"github.com/devicelab-dev/maestro-runner/pkg/flow"
)

// Maestro's id match: the regex covers the whole identifier, ignoring case,
// or the part after its last '/' (Filters.kt:110-132).
func TestMaestroIDMatches(t *testing.T) {
	for _, tc := range []struct {
		pattern, id string
		want        bool
	}{
		{"submit", "submit", true},
		{"Submit", "submit", true},
		{"submit", "submit-button", false},
		{"enriched-text", "set-enriched-text-button", false},
		{"button", "screen/button", true},
		{"sub.*", "submit", true},
		{"home.screen", "home.screen", true},
		{"row", "", false},
		{".*", "", true},
	} {
		if got := maestroIDMatches(tc.pattern, tc.id); got != tc.want {
			t.Errorf("maestroIDMatches(%q, %q) = %v, want %v", tc.pattern, tc.id, got, tc.want)
		}
	}
}

// WDA names an element without an identifier after its label, so a name
// equal to the label is not taken for an identifier.
func TestElementIdentifier(t *testing.T) {
	if got := elementIdentifier(&ParsedElement{Name: "continue-button", Label: "Continue"}); got != "continue-button" {
		t.Errorf("identifier = %q, want the name", got)
	}
	if got := elementIdentifier(&ParsedElement{Name: "Continue", Label: "Continue"}); got != "" {
		t.Errorf("identifier = %q, want none: the name is the label", got)
	}
	if got := elementIdentifier(&ParsedElement{Name: "screen-root"}); got != "screen-root" {
		t.Errorf("identifier = %q, want the name of an unlabelled element", got)
	}
}

const idSource = `<AppiumAUT>
  <XCUIElementTypeApplication type="XCUIElementTypeApplication" x="0" y="0" width="390" height="844">
    <XCUIElementTypeStaticText type="XCUIElementTypeStaticText" name="Settings" label="Settings" value="Settings" x="20" y="60" width="200" height="30"/>
    <XCUIElementTypeButton type="XCUIElementTypeButton" name="set-enriched-text-button" label="Rich text" x="20" y="200" width="300" height="44"/>
    <XCUIElementTypeButton type="XCUIElementTypeButton" name="settings" label="Open settings" x="20" y="300" width="300" height="44"/>
  </XCUIElementTypeApplication>
</AppiumAUT>`

func TestStrictIDInPageSource(t *testing.T) {
	elements, err := ParsePageSource(idSource)
	if err != nil {
		t.Fatal(err)
	}
	count := func(id string) int { return len(FilterBySelector(elements, flow.Selector{ID: id})) }

	if count("enriched-text") != 1 {
		t.Fatal("setup: without the switch an id matches as an unanchored regex")
	}

	t.Setenv("MAESTRO_STRICT_SELECTORS", "1")
	if n := count("enriched-text"); n != 0 {
		t.Errorf("id enriched-text matched %d elements; the id must be the whole identifier", n)
	}
	matches := FilterBySelector(elements, flow.Selector{ID: "SETTINGS"})
	if len(matches) != 1 || matches[0].Label != "Open settings" {
		t.Errorf("id SETTINGS should match only the element whose identifier is settings, got %d", len(matches))
	}
}

// With the switch, a literal id is one exact, case-insensitive query that
// leaves out a label standing in for an identifier, with no CONTAINS
// fallback; a regex id is not asked of WDA.
func TestStrictIDQueries(t *testing.T) {
	t.Setenv("MAESTRO_STRICT_SELECTORS", "1")
	s := &strictQueryServer{source: idSource}
	server := s.start(t)
	defer server.Close()
	d := createTestDriver(server)

	_, _ = d.findElementOnce(flow.Selector{ID: "row.delete"})
	sent := s.sent()
	if len(sent) != 1 || !strings.Contains(sent[0], "name ==[c] 'row.delete' AND name != label") {
		t.Errorf("queries = %q, want one exact name query", sent)
	}

	s.bodies = nil
	_, _ = d.findElementOnce(flow.Selector{ID: "row-[0-9]+"})
	if sent := s.sent(); len(sent) != 0 {
		t.Errorf("a regex id went to WDA: %q", sent)
	}
}
