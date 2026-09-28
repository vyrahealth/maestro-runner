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
	after  string // when set, the page source once a gesture has come in
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
			if log.after != "" {
				log.source = log.after
			}
		}
		jsonResponse(w, map[string]interface{}{"value": nil})
	}))
}

// The swipe's duration, from the W3C body the fake WDA received: the rest on
// the end after the 100 ms move, which must be Maestro's shape.
func swipeDuration(t *testing.T, body map[string]interface{}) float64 {
	t.Helper()
	actions := body["actions"].([]interface{})[0].(map[string]interface{})["actions"].([]interface{})
	var shape []string
	for _, a := range actions {
		item := a.(map[string]interface{})
		shape = append(shape, fmt.Sprintf("%v %v", item["type"], item["duration"]))
	}
	if len(actions) != 5 || shape[0] != "pointerMove 0" || shape[1] != "pointerDown <nil>" ||
		shape[2] != "pointerMove 100" || !strings.HasPrefix(shape[3], "pause ") || shape[4] != "pointerUp <nil>" {
		t.Fatalf("gesture %v, want a 100 ms move and then a pause", shape)
	}
	return actions[3].(map[string]interface{})["duration"].(float64)
}

// Where the W3C gesture the fake WDA received starts and ends.
func gesturePoints(t *testing.T, body map[string]interface{}) [4]float64 {
	t.Helper()
	actions := body["actions"].([]interface{})[0].(map[string]interface{})["actions"].([]interface{})
	start, end := actions[0].(map[string]interface{}), actions[2].(map[string]interface{})
	return [4]float64{start["x"].(float64), start["y"].(float64), end["x"].(float64), end["y"].(float64)}
}

// Maestro's swipe crosses in 100 ms whatever its duration, and the duration
// is how long the finger then rests on the end before it lifts
// (EventRecord.swift:31-38). An 80 ms swipe and a 600 ms one differ only in
// that rest, which dragfromtoforduration cannot give.
func TestTimedSwipeIsMaestrosGesture(t *testing.T) {
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
		if got := swipeDuration(t, log.body); got != float64(ms) {
			t.Errorf("%d ms: the finger rested %v ms", ms, got)
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
			if got := swipeDuration(t, log.body); got != 400 {
				t.Errorf("the swipe's duration is %v ms, want Maestro's 400", got)
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

// With the switch, a scroll is Maestro's: from the middle of the screen
// (195, 422) to 10% of its height, with a duration of 333 ms
// (IOSDriver.kt:240-250), and
// the same swipe mirrored for the other directions. It was a 0.3 s hold and a
// drag from 66.7% to 33.3% of the height. A `speed:` still sets the duration.
func TestTimedScrollIsMaestrosSwipeFromTheMiddle(t *testing.T) {
	t.Setenv("MAESTRO_WDA_TIMED_SWIPE", "1")
	cases := []struct {
		direction string
		speed     int
		want      [4]float64
		ms        float64
	}{
		{"down", 0, [4]float64{195, 422, 195, 84}, 333},
		{"up", 0, [4]float64{195, 422, 195, 759}, 333},
		{"left", 0, [4]float64{195, 422, 351, 422}, 333},
		{"right", 0, [4]float64{195, 422, 39, 422}, 333},
		{"down", 40, [4]float64{195, 422, 195, 84}, 601},
	}
	for _, c := range cases {
		log := &gestureLog{}
		server := swipeServer(t, log)
		result := createTestDriver(server).scroll(&flow.ScrollStep{Direction: c.direction, Speed: c.speed})
		server.Close()
		if !result.Success {
			t.Fatalf("%s: scroll failed: %s", c.direction, result.Message)
		}
		if strings.Join(log.paths, ",") != "actions" {
			t.Fatalf("%s: gesture endpoints %v, want the W3C actions alone", c.direction, log.paths)
		}
		if got := gesturePoints(t, log.body); got != c.want {
			t.Errorf("%s: scroll %v, want %v", c.direction, got, c.want)
		}
		if got := swipeDuration(t, log.body); got != c.ms {
			t.Errorf("%s at speed %d: duration %v ms, want %v", c.direction, c.speed, got, c.ms)
		}
	}
	server := swipeServer(t, &gestureLog{})
	defer server.Close()
	if result := createTestDriver(server).scroll(&flow.ScrollStep{Direction: "sideways"}); result.Success {
		t.Error("an unknown direction must fail")
	}
}

// With the switch, each of scrollUntilVisible's scrolls is Maestro's
// (Orchestra.kt:825-829): the swipe from the middle, 40% of the screen, with
// the duration Maestro makes of the speed (Commands.kt:151-156): 601 ms at the
// default speed 40, 1 ms at 100, and Maestro's fallback of 40 ms above 100.
// It was the runner's 1/3-screen drag after a 0.3 s hold.
func TestTimedScrollUntilVisibleScrollsLikeMaestro(t *testing.T) {
	t.Setenv("MAESTRO_WDA_TIMED_SWIPE", "1")
	cases := []struct {
		direction string
		speed     int
		want      [4]float64
		ms        float64
	}{
		{"down", 0, [4]float64{195, 422, 195, 84}, 601},
		{"up", 0, [4]float64{195, 422, 195, 759}, 601},
		{"right", 0, [4]float64{195, 422, 39, 422}, 601},
		{"down", 100, [4]float64{195, 422, 195, 84}, 1},
		{"down", 90, [4]float64{195, 422, 195, 84}, 101},
		{"down", 150, [4]float64{195, 422, 195, 84}, 40},
	}
	for _, c := range cases {
		// Half the target is on screen until the first scroll, all of it after.
		log := &gestureLog{source: fmt.Sprintf(cardSource, 644), after: fmt.Sprintf(cardSource, 200)}
		server := swipeServer(t, log)
		result := createTestDriver(server).scrollUntilVisible(&flow.ScrollUntilVisibleStep{
			Element: flow.Selector{Text: "Card"}, Direction: c.direction, Speed: c.speed,
		})
		server.Close()
		if !result.Success {
			t.Fatalf("%s at speed %d: step failed: %s", c.direction, c.speed, result.Message)
		}
		if strings.Join(log.paths, ",") != "actions" {
			t.Fatalf("%s: gesture endpoints %v, want one W3C gesture", c.direction, log.paths)
		}
		if got := gesturePoints(t, log.body); got != c.want {
			t.Errorf("%s: scroll %v, want %v", c.direction, got, c.want)
		}
		if got := swipeDuration(t, log.body); got != c.ms {
			t.Errorf("%s at speed %d: duration %v ms, want %v", c.direction, c.speed, got, c.ms)
		}
	}
}
