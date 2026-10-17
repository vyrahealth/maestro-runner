package appium

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/devicelab-dev/maestro-runner/pkg/flow"
)

// rowServer answers every element query with one full-width row, bounds
// [0,639][1080,855] (the row from #175), and records the /actions a tap sends.
func rowServer(t *testing.T, platform string) (*Driver, *[]byte, *int) {
	t.Helper()
	var body []byte
	clicks := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		p := r.URL.Path
		switch {
		case strings.HasSuffix(p, "/actions"):
			body, _ = io.ReadAll(r.Body)
			writeJSON(w, map[string]interface{}{"value": nil})
		case strings.HasSuffix(p, "/click"):
			clicks++
			writeJSON(w, map[string]interface{}{"value": nil})
		case strings.HasSuffix(p, "/element") && r.Method == http.MethodPost:
			writeJSON(w, map[string]interface{}{"value": map[string]interface{}{w3cElementKey: "row"}})
		case strings.HasSuffix(p, "/rect"):
			writeJSON(w, map[string]interface{}{"value": map[string]interface{}{"x": 0, "y": 639, "width": 1080, "height": 216}})
		case strings.HasSuffix(p, "/displayed"):
			writeJSON(w, map[string]interface{}{"value": true})
		default:
			writeJSON(w, map[string]interface{}{"value": ""})
		}
	}))
	t.Cleanup(server.Close)
	d := createTestAppiumDriver(server)
	d.platform = platform
	d.client.platform = platform
	return d, &body, &clicks
}

func firstMove(t *testing.T, body []byte) (float64, float64) {
	t.Helper()
	if len(body) == 0 {
		t.Fatal("no /actions were sent")
	}
	a := capturedActions(t, body)[0]
	if a["type"] != "pointerMove" {
		t.Fatalf("first action = %v, want a pointerMove", a)
	}
	return a["x"].(float64), a["y"].(float64)
}

// #175: with a selector, `point` is relative to the matched element. The
// switch sits at the right edge of a full-width row; the centre opens the row.
func TestTapOnPointIsRelativeToTheElement(t *testing.T) {
	d, body, _ := rowServer(t, "android")
	step := &flow.TapOnStep{Selector: flow.Selector{ID: "alarm_row"}, Point: "90%,50%"}
	if res := d.tapOn(step); !res.Success {
		t.Fatalf("tapOn failed: %s", res.Message)
	}
	if x, y := firstMove(t, *body); x != 972 || y != 747 {
		t.Errorf("tapped (%v,%v), want (972,747) inside the switch", x, y)
	}
}

func TestTapOnWithoutPointStillTapsTheCentre(t *testing.T) {
	d, body, _ := rowServer(t, "android")
	if res := d.tapOn(&flow.TapOnStep{Selector: flow.Selector{ID: "alarm_row"}}); !res.Success {
		t.Fatalf("tapOn failed: %s", res.Message)
	}
	if x, y := firstMove(t, *body); x != 540 || y != 747 {
		t.Errorf("tapped (%v,%v), want the centre (540,747)", x, y)
	}
}

// On iOS an element click always lands on the centre, so a point has to go
// through a coordinate tap; with no point the element click stays.
func TestTapOnPointOnIOSUsesACoordinateTap(t *testing.T) {
	d, body, clicks := rowServer(t, "ios")
	step := &flow.TapOnStep{Selector: flow.Selector{ID: "alarm_row"}, Point: "90%,50%"}
	if res := d.tapOn(step); !res.Success {
		t.Fatalf("tapOn failed: %s", res.Message)
	}
	if *clicks != 0 {
		t.Errorf("used an element click (%d), which ignores the point", *clicks)
	}
	if x, y := firstMove(t, *body); x != 972 || y != 747 {
		t.Errorf("tapped (%v,%v), want (972,747)", x, y)
	}
	if d.lastTappedElementID != "row" {
		t.Errorf("lastTappedElementID = %q, want it kept for inputText", d.lastTappedElementID)
	}

	d, _, clicks = rowServer(t, "ios")
	if res := d.tapOn(&flow.TapOnStep{Selector: flow.Selector{ID: "alarm_row"}}); !res.Success {
		t.Fatalf("tapOn failed: %s", res.Message)
	}
	if *clicks != 1 {
		t.Errorf("element clicks = %d, want 1 when no point is given", *clicks)
	}
}

func TestTapOnPointLongPressIsRelativeToTheElement(t *testing.T) {
	d, body, _ := rowServer(t, "android")
	step := &flow.TapOnStep{Selector: flow.Selector{ID: "alarm_row"}, Point: "90%,50%", LongPress: true}
	if res := d.tapOn(step); !res.Success {
		t.Fatalf("tapOn failed: %s", res.Message)
	}
	if x, y := firstMove(t, *body); x != 972 || y != 747 {
		t.Errorf("pressed (%v,%v), want (972,747)", x, y)
	}
}

// scroll left/right swipe horizontally across the middle of the screen; they
// used to be rejected, so horizontal carousels could not be scrolled.
func TestScrollLeftRightSwipesHorizontally(t *testing.T) {
	for dir, want := range map[string][4]float64{
		"right": {720, 1170, 360, 1170}, // content from the right: finger right → left
		"left":  {360, 1170, 720, 1170},
		"down":  {540, 1560, 540, 780},
	} {
		d, body, _ := rowServer(t, "android")
		if res := d.scroll(&flow.ScrollStep{Direction: dir}); !res.Success {
			t.Fatalf("%s: %s", dir, res.Message)
		}
		acts := capturedActions(t, *body)
		var last map[string]interface{}
		for _, a := range acts {
			if a["type"] == "pointerMove" {
				last = a
			}
		}
		first := acts[0]
		got := [4]float64{first["x"].(float64), first["y"].(float64), last["x"].(float64), last["y"].(float64)}
		if got != want {
			t.Errorf("%s: swipe %v, want %v", dir, got, want)
		}
	}
}

// dragAndDrop's `point:` next to a selector is relative to the element, like
// tapOn's; it used to start at the element's centre.
func TestDragFromPointInsideTheElement(t *testing.T) {
	d, _, _ := rowServer(t, "android")
	x, y, _, err := d.resolveDragPoint(flow.Selector{ID: "alarm_row", Point: "90%,50%"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if x != 972 || y != 747 {
		t.Errorf("drag point (%d,%d), want (972,747)", x, y)
	}
}

// swipe distance: 0.2 travels 20% of the screen height; it was ignored.
func TestSwipeHonoursDistance(t *testing.T) {
	d, body, _ := rowServer(t, "android")
	if res := d.swipe(&flow.SwipeStep{Direction: "UP", Distance: 0.2}); !res.Success {
		t.Fatalf("swipe: %s", res.Message)
	}
	acts := capturedActions(t, *body)
	var startY, endY float64
	for _, a := range acts {
		if a["type"] == "pointerMove" {
			if startY == 0 {
				startY = a["y"].(float64)
			}
			endY = a["y"].(float64)
		}
	}
	if travel := startY - endY; travel != 468 { // 20% of 2340
		t.Errorf("swiped %v px, want 468 (20%% of 2340)", travel)
	}
}
