package uiautomator2

import (
	"testing"

	"github.com/devicelab-dev/maestro-runner/pkg/flow"
)

// The reported failure (#161): a price field reading "7000.00" contains "0"
// and beat the switch whose text is exactly "0", so the tap landed wrong.
func TestFilterBySelector_PrefersExactText(t *testing.T) {
	elements := []*ParsedElement{{Text: "7000.00"}, {Text: "0"}, {Text: "Limit"}}
	got := FilterBySelector(elements, flow.Selector{Text: "0"})
	if len(got) != 1 || got[0].Text != "0" {
		t.Fatalf("expected only the exact match, got %d", len(got))
	}
}

func TestFilterBySelector_ExactContentDescCounts(t *testing.T) {
	elements := []*ParsedElement{{ContentDesc: "Add to cart"}, {ContentDesc: "Add"}}
	got := FilterBySelector(elements, flow.Selector{Text: "Add"})
	if len(got) != 1 || got[0].ContentDesc != "Add" {
		t.Fatalf("exact content-desc should win, got %d", len(got))
	}
}

// Text matches whole, as in Maestro (#188): no element's text IS "Cancel", so
// nothing matches. The substring match this used to pin is written as a regex.
func TestFilterBySelector_NoSubstringMatch(t *testing.T) {
	elements := []*ParsedElement{{Text: "Good till Cancel"}, {Text: "Cancel order"}}
	if got := FilterBySelector(elements, flow.Selector{Text: "Cancel"}); len(got) != 0 {
		t.Fatalf("plain text must not match part of a value, got %d", len(got))
	}
	if got := FilterBySelector(elements, flow.Selector{Text: ".*Cancel.*"}); len(got) != 2 {
		t.Fatalf("expected both matches for .*Cancel.*, got %d", len(got))
	}
}

// The #188 / #178 reports: "Open" tapped a "Talk · Open" row and "English"
// matched a "Language: English" content-desc.
func TestFilterBySelector_Issue188WholeText(t *testing.T) {
	elements := []*ParsedElement{
		{Text: "Talk · Open"},
		{ContentDesc: "Language: English"},
		{ResourceID: "com.app:id/login_button"},
	}
	for _, sel := range []flow.Selector{{Text: "Open"}, {Text: "English"}, {ID: "login"}} {
		if got := FilterBySelector(elements, sel); len(got) != 0 {
			t.Errorf("%s must not match part of a value, got %+v", sel.Describe(), *got[0])
		}
	}
	if got := FilterBySelector(elements, flow.Selector{Text: ".*Open.*"}); len(got) != 1 {
		t.Errorf(".*Open.* should match the row, got %d", len(got))
	}
	if got := FilterBySelector(elements, flow.Selector{ID: "login_button"}); len(got) != 1 {
		t.Errorf("id after the package prefix should match, got %d", len(got))
	}
}

// The guard on the fix: an exact text match must not bypass the rest of the
// selector — that is the OR behaviour removed in #157/#158/#160.
func TestFilterBySelector_ExactTextStillRequiresID(t *testing.T) {
	elements := []*ParsedElement{
		{ResourceID: "cart-button", Text: "Cart"},
		{ResourceID: "product-item-Appium", Text: "product-item-Appium"},
	}
	sel := flow.Selector{ID: "cart-button", Text: "product-item-Appium"}
	if got := FilterBySelector(elements, sel); len(got) != 0 {
		t.Errorf("id and text are on different elements; expected no match, got %+v", got[0])
	}
}

// A regex selector is not a literal and keeps regex semantics.
func TestFilterBySelector_RegexUnaffectedByExactPreference(t *testing.T) {
	elements := []*ParsedElement{{Text: "7000.00"}, {Text: "0"}}
	got := FilterBySelector(elements, flow.Selector{Text: ".*000.*"})
	if len(got) != 1 || got[0].Text != "7000.00" {
		t.Fatalf("regex should match the price only, got %d", len(got))
	}
}
