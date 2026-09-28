package wda

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/devicelab-dev/maestro-runner/pkg/flow"
)

// touchServer is a fake WDA with one element, el1, found by id, at rect and
// displayed as given. It logs clicks, coordinate taps and focus reads, and
// keeps the body of the last W3C actions request.
type touchServer struct {
	mu        sync.Mutex
	rect      map[string]interface{}
	displayed bool
	class     string
	log       []string
	actions   map[string]interface{}
}

func (f *touchServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		f.mu.Lock()
		defer f.mu.Unlock()
		p := r.URL.Path
		switch {
		case strings.HasSuffix(p, "/elements"):
			jsonResponse(w, map[string]interface{}{"value": []map[string]interface{}{{"ELEMENT": "el1"}}})
		case strings.HasSuffix(p, "/element/el1/rect"):
			jsonResponse(w, map[string]interface{}{"value": f.rect})
		case strings.HasSuffix(p, "/element/el1/displayed"):
			jsonResponse(w, map[string]interface{}{"value": f.displayed})
		case strings.HasSuffix(p, "/element/el1/name"):
			jsonResponse(w, map[string]interface{}{"value": f.class})
		case strings.HasSuffix(p, "/element/el1/click"):
			f.log = append(f.log, "click")
			jsonResponse(w, map[string]interface{}{"value": nil})
		case strings.HasSuffix(p, "/element/active"):
			f.log = append(f.log, "active")
			jsonResponse(w, map[string]interface{}{"value": map[string]interface{}{"ELEMENT": "el1"}})
		case strings.HasSuffix(p, "/wda/tap"):
			f.log = append(f.log, "tap")
			jsonResponse(w, map[string]interface{}{"value": nil})
		case strings.HasSuffix(p, "/actions"):
			f.log = append(f.log, "actions")
			_ = json.NewDecoder(r.Body).Decode(&f.actions)
			jsonResponse(w, map[string]interface{}{"value": nil})
		default:
			jsonResponse(w, map[string]interface{}{"value": nil})
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// touch returns where the actions request touched and how long it held.
func (f *touchServer) touch(t *testing.T) (x, y, holdMs float64) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.actions == nil {
		t.Fatal("no actions request")
	}
	finger := f.actions["actions"].([]interface{})[0].(map[string]interface{})
	items := finger["actions"].([]interface{})
	var types []string
	for _, it := range items {
		types = append(types, it.(map[string]interface{})["type"].(string))
	}
	if got := strings.Join(types, ","); got != "pointerMove,pointerDown,pause,pointerUp" {
		t.Fatalf("actions %s, want a single touch", got)
	}
	move := items[0].(map[string]interface{})
	return move["x"].(float64), move["y"].(float64), items[2].(map[string]interface{})["duration"].(float64)
}

func rectOf(x, y, w, h int) map[string]interface{} {
	return map[string]interface{}{"x": x, "y": y, "width": w, "height": h}
}

// With the switch an element tap is Maestro's: a 100 ms touch at the centre
// of the element's bounds, and no click.
func TestCoordinateTapTouchesTheElementsCentre(t *testing.T) {
	t.Setenv("MAESTRO_WDA_COORDINATE_TAP", "1")
	f := &touchServer{rect: rectOf(100, 400, 80, 40), displayed: true}
	d := createTestDriver(f.start(t))
	if res := d.Execute(&flow.TapOnStep{Selector: flow.Selector{ID: "go"}}); !res.Success {
		t.Fatalf("tap failed: %s", res.Message)
	}
	if got := strings.Join(f.log, ","); got != "actions" {
		t.Fatalf("requests %s, want the touch alone", got)
	}
	if x, y, hold := f.touch(t); x != 140 || y != 420 || hold != 100 {
		t.Errorf("touched (%v, %v) for %v ms, want (140, 420) for 100 ms", x, y, hold)
	}
}

// An element XCUITest reports not displayed, kept because its bounds are on
// screen, is touched the same way: the click is what went looking for it.
// Part of it is off the left edge, so the touch goes to the part on screen.
func TestCoordinateTapOnAnElementReportedNotDisplayed(t *testing.T) {
	t.Setenv("MAESTRO_WDA_COORDINATE_TAP", "1")
	f := &touchServer{rect: rectOf(-40, 400, 120, 40), displayed: false}
	d := createTestDriver(f.start(t))
	if res := d.Execute(&flow.TapOnStep{Selector: flow.Selector{ID: "go"}}); !res.Success {
		t.Fatalf("tap failed: %s", res.Message)
	}
	if x, y, _ := f.touch(t); x != 40 || y != 420 {
		t.Errorf("touched (%v, %v), want (40, 420), the centre of the part on screen", x, y)
	}
	if strings.Contains(strings.Join(f.log, ","), "click") {
		t.Error("no click with the switch set")
	}
}

// A text field is touched too, with no focus check after it: Maestro makes
// none, and inputText fails loudly when nothing took focus.
func TestCoordinateTapOnATextField(t *testing.T) {
	t.Setenv("MAESTRO_WDA_COORDINATE_TAP", "1")
	f := &touchServer{rect: rectOf(20, 200, 350, 44), displayed: true, class: "XCUIElementTypeTextField"}
	d := createTestDriver(f.start(t))
	if res := d.Execute(&flow.TapOnStep{Selector: flow.Selector{ID: "email"}}); !res.Success {
		t.Fatalf("tap failed: %s", res.Message)
	}
	if got := strings.Join(f.log, ","); got != "actions" {
		t.Errorf("requests %s, want the touch alone", got)
	}
}

// Without the switch the element is clicked, as before.
func TestElementTapClicksWithoutTheSwitch(t *testing.T) {
	t.Setenv("MAESTRO_WDA_COORDINATE_TAP", "")
	f := &touchServer{rect: rectOf(100, 400, 80, 40), displayed: true}
	d := createTestDriver(f.start(t))
	if res := d.Execute(&flow.TapOnStep{Selector: flow.Selector{ID: "go"}}); !res.Success {
		t.Fatalf("tap failed: %s", res.Message)
	}
	if got := strings.Join(f.log, ","); got != "click" {
		t.Errorf("requests %s, want the click alone", got)
	}
}

// Bounds wholly off screen still fail the step rather than touch nothing.
func TestCoordinateTapOffScreenFails(t *testing.T) {
	t.Setenv("MAESTRO_WDA_COORDINATE_TAP", "1")
	f := &touchServer{rect: rectOf(100, 2000, 80, 40), displayed: true}
	d := createTestDriver(f.start(t))
	if res := d.tapOn(&flow.TapOnStep{Selector: flow.Selector{ID: "go"}}); res.Success {
		t.Fatal("a tap on an element below the screen should fail")
	}
	if len(f.log) != 0 {
		t.Errorf("requests %v, want none", f.log)
	}
}
