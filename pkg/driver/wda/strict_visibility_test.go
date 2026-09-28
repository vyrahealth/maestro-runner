package wda

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/devicelab-dev/maestro-runner/pkg/core"
	"github.com/devicelab-dev/maestro-runner/pkg/flow"
)

// Maestro keeps an element that is at least 10% on screen; a 0x0 one is
// dropped, and one with a zero width or height is kept (0/0 is NaN, and NaN
// is not below 0.1).
func TestMaestroOnScreen(t *testing.T) {
	for _, tc := range []struct {
		name string
		b    core.Bounds
		want bool
	}{
		{"on screen", core.Bounds{X: 10, Y: 10, Width: 100, Height: 50}, true},
		{"0x0", core.Bounds{X: 10, Y: 10}, false},
		{"zero height", core.Bounds{X: 10, Y: 10, Width: 100}, true},
		{"zero width, off screen", core.Bounds{X: 10, Y: 5000, Height: 40}, true},
		{"10% on screen", core.Bounds{X: 0, Y: 834, Width: 100, Height: 100}, true},
		{"9% on screen", core.Bounds{X: 0, Y: 835, Width: 100, Height: 100}, false},
		{"covers the screen", core.Bounds{X: -10, Y: -10, Width: 500, Height: 900}, true},
		{"off screen", core.Bounds{X: 0, Y: 2000, Width: 100, Height: 50}, false},
	} {
		if got := maestroOnScreen(tc.b, 390, 844); got != tc.want {
			t.Errorf("%s: maestroOnScreen(%+v) = %v, want %v", tc.name, tc.b, got, tc.want)
		}
	}
}

// A tall list, under 10% of it on screen, holds a row that is on screen: the
// list stays because the row does. A list wholly off screen goes with its
// rows. The copies' children are narrowed to what stays; the parse is not.
const tallListSource = `<AppiumAUT>
  <XCUIElementTypeApplication type="XCUIElementTypeApplication" x="0" y="0" width="390" height="844">
    <XCUIElementTypeTable type="XCUIElementTypeTable" name="feed" x="0" y="-9000" width="390" height="9100">
      <XCUIElementTypeCell type="XCUIElementTypeCell" name="row-visible" label="Visible row" x="0" y="20" width="390" height="60"/>
      <XCUIElementTypeCell type="XCUIElementTypeCell" name="row-above" label="Row above" x="0" y="-500" width="390" height="60"/>
    </XCUIElementTypeTable>
    <XCUIElementTypeTable type="XCUIElementTypeTable" name="gone" x="0" y="3000" width="390" height="900">
      <XCUIElementTypeCell type="XCUIElementTypeCell" name="row-gone" label="Gone row" x="0" y="3000" width="390" height="60"/>
    </XCUIElementTypeTable>
    <XCUIElementTypeOther type="XCUIElementTypeOther" name="divider" x="0" y="400" width="390" height="0"/>
  </XCUIElementTypeApplication>
</AppiumAUT>`

func TestMaestroVisibleTree(t *testing.T) {
	elements, err := ParsePageSource(tallListSource)
	if err != nil {
		t.Fatal(err)
	}
	kept := map[string]*ParsedElement{}
	for _, e := range maestroVisibleTree(elements, 390, 844) {
		kept[e.Name] = e
	}
	for _, name := range []string{"feed", "row-visible", "divider"} {
		if kept[name] == nil {
			t.Errorf("%s was dropped; Maestro keeps it", name)
		}
	}
	for _, name := range []string{"row-above", "gone", "row-gone"} {
		if kept[name] != nil {
			t.Errorf("%s was kept; Maestro drops it", name)
		}
	}
	if feed := kept["feed"]; feed != nil {
		if len(feed.Children) != 1 || feed.Children[0] != kept["row-visible"] || kept["row-visible"].Parent != feed {
			t.Errorf("the kept list should link only its kept row, got %d children", len(feed.Children))
		}
	}
	for _, e := range elements {
		if e.Name == "feed" && len(e.Children) != 2 {
			t.Errorf("the parse was changed: feed has %d children", len(e.Children))
		}
	}

	// Nothing on screen at all: the tree is kept whole, as Maestro keeps it.
	offscreen, _ := ParsePageSource(`<AppiumAUT><XCUIElementTypeApplication type="XCUIElementTypeApplication" x="0" y="5000" width="390" height="844"><XCUIElementTypeButton type="XCUIElementTypeButton" name="b" x="0" y="5000" width="10" height="10"/></XCUIElementTypeApplication></AppiumAUT>`)
	if got := len(maestroVisibleTree(offscreen, 390, 844)); got != 2 {
		t.Errorf("an all-off-screen tree kept %d elements, want both", got)
	}

	// Several top-level elements are one tree for that rule: an off-screen
	// one goes when another stays.
	flat := []*ParsedElement{
		{Name: "on", Bounds: core.Bounds{X: 50, Y: 100, Width: 200, Height: 50}},
		{Name: "off", Bounds: core.Bounds{X: 500, Y: 100, Width: 200, Height: 50}},
	}
	if got := maestroVisibleTree(flat, 390, 844); len(got) != 1 || got[0].Name != "on" {
		t.Errorf("kept %d top-level elements, want only the one on screen", len(got))
	}
}

// Through the page-source lookup: with the switch the list whose row is on
// screen, and the zero-height divider, are found; without it they are not.
func TestStrictPageSourceVisibility(t *testing.T) {
	server := sourceServer(t, tallListSource)
	defer server.Close()
	lookup := func(id string) error {
		_, err := createTestDriver(server).findElementByPageSourceOnce(flow.Selector{ID: id})
		return err
	}
	if lookup("feed") == nil || lookup("divider") == nil {
		t.Fatal("setup: without the switch both are filtered out")
	}
	t.Setenv("MAESTRO_STRICT_SELECTORS", "1")
	if err := lookup("feed"); err != nil {
		t.Errorf("the list with a row on screen should be found: %v", err)
	}
	if err := lookup("divider"); err != nil {
		t.Errorf("the zero-height divider should be found: %v", err)
	}
	if err := lookup("row-gone"); !isNotFound(err) {
		t.Errorf("an off-screen row should not be found, got %v", err)
	}
}

// elementServer finds every WDA query as one element, with the given rect
// and /displayed answer (0 fails the read).
func elementServer(t *testing.T, rect [4]int, displayed int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		p := r.URL.Path
		switch {
		case strings.HasSuffix(p, "/elements"):
			jsonResponse(w, map[string]interface{}{"value": []map[string]interface{}{{"ELEMENT": "el"}}})
		case strings.HasSuffix(p, "/element") && r.Method == "POST":
			jsonResponse(w, map[string]interface{}{"value": map[string]interface{}{"ELEMENT": "el"}})
		case strings.HasSuffix(p, "/rect"):
			jsonResponse(w, map[string]interface{}{"value": map[string]interface{}{"x": rect[0], "y": rect[1], "width": rect[2], "height": rect[3]}})
		case strings.HasSuffix(p, "/displayed"):
			if displayed == 0 {
				w.WriteHeader(http.StatusInternalServerError)
				jsonResponse(w, map[string]interface{}{"value": map[string]interface{}{"error": "unknown error", "message": "no snapshot"}})
				return
			}
			jsonResponse(w, map[string]interface{}{"value": displayed > 0})
		default:
			jsonResponse(w, map[string]interface{}{"status": 0})
		}
	}))
}

// An element WDA finds is kept or not by its bounds alone with the switch.
// Without it, displayed=true, or a displayed read that failed, passed an
// element wholly off screen, and a zero-height element was refused.
func TestStrictElementInfoGoesByBounds(t *testing.T) {
	for _, tc := range []struct {
		name              string
		rect              [4]int
		displayed         int // 1 true, -1 false, 0 read fails
		plain, strictWant bool
	}{
		{"displayed but off screen", [4]int{0, 2000, 100, 50}, 1, true, false},
		{"displayed read failed, off screen", [4]int{0, 2000, 100, 50}, 0, true, false},
		{"not displayed but on screen", [4]int{0, 100, 100, 50}, -1, true, true},
		{"zero height on screen", [4]int{0, 100, 100, 0}, 1, false, true},
		{"0x0", [4]int{0, 100, 0, 0}, 1, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := elementServer(t, tc.rect, tc.displayed)
			defer server.Close()
			_, err := createTestDriver(server).getElementInfo("el")
			if got := err == nil; got != tc.plain {
				t.Errorf("without the switch accepted = %v, want %v (%v)", got, tc.plain, err)
			}
			t.Setenv("MAESTRO_STRICT_SELECTORS", "1")
			_, err = createTestDriver(server).getElementInfo("el")
			if got := err == nil; got != tc.strictWant {
				t.Errorf("with the switch accepted = %v, want %v (%v)", got, tc.strictWant, err)
			}
		})
	}
}

// Without a screen size only the size can be judged: an element needs a
// width or a height, and neither may be negative.
func TestStrictElementInfoWithoutScreenSize(t *testing.T) {
	t.Setenv("MAESTRO_STRICT_SELECTORS", "1")
	for _, tc := range []struct {
		rect [4]int
		want bool
	}{
		{[4]int{0, 2000, 100, 50}, true},
		{[4]int{0, 100, 100, 0}, true},
		{[4]int{0, 100, 0, 0}, false},
		{[4]int{0, 100, -1, 44}, false},
	} {
		server := elementServer(t, tc.rect, 1)
		d := createTestDriver(server)
		d.info.ScreenWidth, d.info.ScreenHeight = 0, 0
		_, err := d.getElementInfo("el")
		server.Close()
		if got := err == nil; got != tc.want {
			t.Errorf("rect %v: accepted = %v, want %v (%v)", tc.rect, got, tc.want, err)
		}
	}
}
