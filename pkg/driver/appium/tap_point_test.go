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
