package wda

import (
	"testing"

	"github.com/devicelab-dev/maestro-runner/pkg/flow"
)

// A swipe's start and end without a % sign are points, as in Maestro
// (YamlFluentCommand.kt:880-908), kept on the 390x844 screen. They were read
// as percentages: "100, 500" swiped from 100% and 500% of the screen.
func TestSwipeStartAndEndWithoutPercentAreScreenPoints(t *testing.T) {
	t.Setenv("MAESTRO_WDA_TIMED_SWIPE", "")
	cases := []struct {
		start, end string
		want       [4]float64
	}{
		{"100, 500", "100, 200", [4]float64{100, 500, 100, 200}},
		{"100,500", "600, -20", [4]float64{100, 500, 390, 0}},
	}
	for _, c := range cases {
		log := &gestureLog{}
		server := swipeServer(t, log)
		result := createTestDriver(server).swipe(&flow.SwipeStep{Start: c.start, End: c.end})
		server.Close()
		if !result.Success {
			t.Fatalf("%q to %q: swipe failed: %s", c.start, c.end, result.Message)
		}
		got := [4]float64{}
		for i, k := range []string{"fromX", "fromY", "toX", "toY"} {
			got[i], _ = log.body[k].(float64)
		}
		if got != c.want {
			t.Errorf("%q to %q: swipe %v, want %v", c.start, c.end, got, c.want)
		}
	}

	// Maestro reads whole numbers only.
	server := swipeServer(t, &gestureLog{})
	defer server.Close()
	if result := createTestDriver(server).swipe(&flow.SwipeStep{Start: "100.5, 500", End: "100, 200"}); result.Success {
		t.Error("a start that is not whole points must fail")
	}
}
