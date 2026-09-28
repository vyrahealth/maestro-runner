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

type gestureLog struct {
	mu    sync.Mutex
	paths []string
	body  map[string]interface{}
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
