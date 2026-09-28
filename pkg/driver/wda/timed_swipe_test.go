package wda

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/devicelab-dev/maestro-runner/pkg/flow"
)

type gestureLog struct {
	mu     sync.Mutex
	paths  []string
	body   map[string]interface{}
	source string // the page source the fake WDA serves
}

func swipeServer(t *testing.T, log *gestureLog) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		log.mu.Lock()
		defer log.mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/window/size"), strings.HasSuffix(r.URL.Path, "/window/rect"):
			jsonResponse(w, map[string]interface{}{"value": map[string]interface{}{"width": 390, "height": 844}})
			return
		case strings.HasSuffix(r.URL.Path, "/source"):
			jsonResponse(w, map[string]interface{}{"value": log.source})
			return
		case strings.HasSuffix(r.URL.Path, "/actions"), strings.HasSuffix(r.URL.Path, "/wda/dragfromtoforduration"):
			log.paths = append(log.paths, r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:])
			_ = json.NewDecoder(r.Body).Decode(&log.body)
		}
		jsonResponse(w, map[string]interface{}{"value": nil})
	}))
}

// The move's own duration, from the W3C body the fake WDA received.
func moveDuration(t *testing.T, body map[string]interface{}) float64 {
	t.Helper()
	actions := body["actions"].([]interface{})[0].(map[string]interface{})["actions"].([]interface{})
	return actions[2].(map[string]interface{})["duration"].(float64)
}

// Where the W3C gesture the fake WDA received starts and ends.
func gesturePoints(t *testing.T, body map[string]interface{}) [4]float64 {
	t.Helper()
	actions := body["actions"].([]interface{})[0].(map[string]interface{})["actions"].([]interface{})
	start, end := actions[0].(map[string]interface{}), actions[2].(map[string]interface{})
	return [4]float64{start["x"].(float64), start["y"].(float64), end["x"].(float64), end["y"].(float64)}
}

// A flow's 80 ms swipe is a fling (a ruler it throws should travel to its end);
// its 600 ms swipe a slow drag. Both need the finger's travel time to be the
// duration, which W3C pointer actions give and dragfromtoforduration does not.
func TestTimedSwipeIsAPointerGestureOfThatLength(t *testing.T) {
	t.Setenv("MAESTRO_WDA_TIMED_SWIPE", "1")
	for _, ms := range []int{80, 600} {
		log := &gestureLog{}
		server := swipeServer(t, log)
		result := createTestDriver(server).swipe(&flow.SwipeStep{Direction: "RIGHT", Duration: ms})
		server.Close()
		if !result.Success {
			t.Fatalf("%d ms: swipe failed: %s", ms, result.Message)
		}
		if strings.Join(log.paths, ",") != "actions" {
			t.Fatalf("%d ms: gesture endpoints %v, want the W3C actions alone", ms, log.paths)
		}
		if got := moveDuration(t, log.body); got != float64(ms) {
			t.Errorf("%d ms: the move took %v ms", ms, got)
		}
	}
}

// A swipe without a duration takes Maestro's default 400 ms with the switch
// (YamlSwipe.kt:58). Without it, that swipe is the drag it has always been.
// (TestSwipeCustomDuration pins the old mapping of a duration onto the drag's
// hold.)
func TestTimedSwipeWithoutDurationTakesMaestrosDefault(t *testing.T) {
	for _, c := range []struct {
		env, paths string
	}{{"1", "actions"}, {"", "dragfromtoforduration"}} {
		t.Setenv("MAESTRO_WDA_TIMED_SWIPE", c.env)
		log := &gestureLog{}
		server := swipeServer(t, log)
		result := createTestDriver(server).swipe(&flow.SwipeStep{Direction: "UP"})
		server.Close()
		if !result.Success {
			t.Fatalf("switch %q: swipe failed: %s", c.env, result.Message)
		}
		if strings.Join(log.paths, ",") != c.paths {
			t.Fatalf("switch %q: gesture endpoints %v, want %s alone", c.env, log.paths, c.paths)
		}
		if c.env != "" {
			if got := moveDuration(t, log.body); got != 400 {
				t.Errorf("the swipe took %v ms, want Maestro's 400", got)
			}
		}
	}
}

// A card at (20, 200) sized 350x400 on a 390x844 screen: its center is (195, 400).
const cardSource = `<AppiumAUT>
  <XCUIElementTypeApplication name="TestApp" enabled="true" visible="true" x="0" y="0" width="390" height="844">
    <XCUIElementTypeOther name="card" label="Card" enabled="true" visible="true" x="20" y="%d" width="350" height="400"/>
  </XCUIElementTypeApplication>
</AppiumAUT>`

// With the switch, a swipe from an element is Maestro's: from the element's
// center, or its point, to 10% or 90% of the SCREEN (0.1*844 = 84.4 cut to 84,
// 0.9*390 = 351), kept on the screen. It used to span 10% to 90% of the
// element's own bounds.
func TestTimedSwipeFromAnElementRunsToTheScreenEdge(t *testing.T) {
	t.Setenv("MAESTRO_WDA_TIMED_SWIPE", "1")
	cases := []struct {
		direction, point string
		cardY            int
		want             [4]float64
	}{
		{"UP", "", 200, [4]float64{195, 400, 195, 84}},
		{"DOWN", "", 200, [4]float64{195, 400, 195, 759}},
		{"LEFT", "", 200, [4]float64{195, 400, 39, 400}},
		{"RIGHT", "", 200, [4]float64{195, 400, 351, 400}},
		{"LEFT", "50%, 85%", 200, [4]float64{195, 540, 39, 540}},
		// A card half below the fold: its center (195, 800) is on screen, and
		// a point on it below the bottom edge (y 960) is kept on the screen.
		{"UP", "", 600, [4]float64{195, 800, 195, 84}},
		{"UP", "50%, 90%", 600, [4]float64{195, 844, 195, 84}},
		// A point outside the element fails, as in Maestro.
		{"UP", "50%, 125%", 200, [4]float64{}},
	}
	for _, c := range cases {
		log := &gestureLog{source: fmt.Sprintf(cardSource, c.cardY)}
		server := swipeServer(t, log)
		step := &flow.SwipeStep{Direction: c.direction, Selector: &flow.Selector{Text: "Card", Point: c.point}}
		step.TimeoutMs = 1000
		result := createTestDriver(server).swipe(step)
		server.Close()
		if c.want == ([4]float64{}) {
			if result.Success {
				t.Errorf("%s from %q: a point outside the element must fail", c.direction, c.point)
			}
			continue
		}
		if !result.Success {
			t.Fatalf("%s from %q: swipe failed: %s", c.direction, c.point, result.Message)
		}
		if strings.Join(log.paths, ",") != "actions" {
			t.Fatalf("%s: gesture endpoints %v, want the W3C actions alone", c.direction, log.paths)
		}
		if got := gesturePoints(t, log.body); got != c.want {
			t.Errorf("%s from %q: swipe %v, want %v", c.direction, c.point, got, c.want)
		}
	}
}
