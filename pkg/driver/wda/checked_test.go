package wda

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/devicelab-dev/maestro-runner/pkg/flow"
)

// Two switches with the same label, one off and one on, under a heading.
const checkedPageSource = `<?xml version="1.0" encoding="UTF-8"?>
<AppiumAUT>
  <XCUIElementTypeApplication type="XCUIElementTypeApplication" name="TestApp" label="TestApp" enabled="true" visible="true" x="0" y="0" width="390" height="844">
    <XCUIElementTypeStaticText type="XCUIElementTypeStaticText" value="Settings" label="Settings" enabled="true" visible="true" x="20" y="60" width="200" height="30"/>
    <XCUIElementTypeSwitch type="XCUIElementTypeSwitch" value="0" label="Alerts" enabled="true" visible="true" x="300" y="100" width="51" height="31"/>
    <XCUIElementTypeSwitch type="XCUIElementTypeSwitch" value="1" label="Alerts" enabled="true" visible="true" x="300" y="200" width="51" height="31"/>
  </XCUIElementTypeApplication>
</AppiumAUT>`

func TestParsePageSourceDerivesChecked(t *testing.T) {
	elements, err := ParsePageSource(`<AppiumAUT>
  <XCUIElementTypeSwitch type="XCUIElementTypeSwitch" name="on" value="1" x="0" y="0" width="10" height="10"/>
  <XCUIElementTypeSwitch type="XCUIElementTypeSwitch" name="off" value="0" x="0" y="0" width="10" height="10"/>
  <XCUIElementTypeCheckBox type="XCUIElementTypeCheckBox" name="box" value="1" x="0" y="0" width="10" height="10"/>
  <XCUIElementTypeToggle type="XCUIElementTypeToggle" name="toggle" value="1" x="0" y="0" width="10" height="10"/>
  <XCUIElementTypeButton type="XCUIElementTypeButton" name="selected-button" value="1" x="0" y="0" width="10" height="10"/>
  <XCUIElementTypeStaticText type="XCUIElementTypeStaticText" name="one" value="1" x="0" y="0" width="10" height="10"/>
</AppiumAUT>`)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"on": true, "off": false, "box": true, "toggle": true, "selected-button": false, "one": false}
	for _, e := range elements {
		if e.Checked != want[e.Name] {
			t.Errorf("%s: Checked = %v, want %v", e.Name, e.Checked, want[e.Name])
		}
	}
}

func TestMatchesSelectorChecked(t *testing.T) {
	on := &ParsedElement{Type: "XCUIElementTypeSwitch", Label: "Alerts", Value: "1", Checked: true}
	off := &ParsedElement{Type: "XCUIElementTypeSwitch", Label: "Alerts", Value: "0"}
	yes, no := true, false
	if !matchesSelector(on, flow.Selector{Text: "Alerts", Checked: &yes}) || matchesSelector(off, flow.Selector{Text: "Alerts", Checked: &yes}) {
		t.Error("checked: true should match only the switch that is on")
	}
	if !matchesSelector(off, flow.Selector{Text: "Alerts", Checked: &no}) || matchesSelector(on, flow.Selector{Text: "Alerts", Checked: &no}) {
		t.Error("checked: false should match only the switch that is off")
	}
}

// checkedServer answers every WDA query with an element, so a lookup that
// still went through the predicate queries would take whichever switch WDA
// found first, whatever its state.
func checkedServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		p := r.URL.Path
		switch {
		case strings.HasSuffix(p, "/source"):
			jsonResponse(w, map[string]interface{}{"value": checkedPageSource})
		case strings.HasSuffix(p, "/elements"):
			jsonResponse(w, map[string]interface{}{"value": []map[string]interface{}{{"ELEMENT": "first-switch"}}})
		case strings.HasSuffix(p, "/element") && r.Method == "POST":
			jsonResponse(w, map[string]interface{}{"value": map[string]interface{}{"ELEMENT": "first-switch"}})
		case strings.HasSuffix(p, "/rect"):
			jsonResponse(w, map[string]interface{}{"value": map[string]interface{}{"x": 300, "y": 100, "width": 51, "height": 31}})
		case strings.HasSuffix(p, "/displayed"):
			jsonResponse(w, map[string]interface{}{"value": true})
		default:
			jsonResponse(w, map[string]interface{}{"status": 0})
		}
	}))
}

// checked was dropped on iOS with a warning, so checked: true matched a
// switch in either state. It now picks the switch in the state asked for, on
// the tap path and the assert path alike.
func TestCheckedSelectorPicksSwitchInThatState(t *testing.T) {
	server := checkedServer(t)
	defer server.Close()
	d := createTestDriver(server)
	yes, no := true, false

	info, err := d.findElementForTap(flow.Selector{Text: "Alerts", Checked: &yes}, false, 500)
	if err != nil {
		t.Fatal(err)
	}
	if info.Bounds.Y != 200 {
		t.Errorf("tapOn checked: true found the switch at y=%d, want the one that is on (y=200)", info.Bounds.Y)
	}

	info, err = d.findElement(flow.Selector{Text: "Alerts", Checked: &yes}, false, 500)
	if err != nil {
		t.Fatal(err)
	}
	if info.Bounds.Y != 200 {
		t.Errorf("assertVisible checked: true found the switch at y=%d, want the one that is on (y=200)", info.Bounds.Y)
	}

	info, err = d.findElement(flow.Selector{Text: "Alerts", Checked: &no}, false, 500)
	if err != nil {
		t.Fatal(err)
	}
	if info.Bounds.Y != 100 {
		t.Errorf("assertVisible checked: false found the switch at y=%d, want the one that is off (y=100)", info.Bounds.Y)
	}

	info, err = d.findElementForTap(flow.Selector{Checked: &yes, Below: &flow.Selector{Text: "Settings"}}, false, 500)
	if err != nil {
		t.Fatal(err)
	}
	if info.Bounds.Y != 200 {
		t.Errorf("checked: true below a heading found y=%d, want the switch that is on (y=200)", info.Bounds.Y)
	}
}
