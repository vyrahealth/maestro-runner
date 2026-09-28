package wda

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/devicelab-dev/maestro-runner/pkg/core"
	"github.com/devicelab-dev/maestro-runner/pkg/flow"
)

// The executor drives Maestro's iOS tap through this method (tap_options.go).
var _ interface{ WaitUntilScreenIsStatic(int) bool } = (*Driver)(nil)

// frameServer is a fake WDA whose screenshots play frames in turn, the last one
// repeating; an empty frame is a failed screenshot. With moving set, every
// screenshot differs from the last. It counts screenshots.
type frameServer struct {
	mu     sync.Mutex
	frames []string
	moving bool
	shots  int
}

func (f *frameServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !strings.HasSuffix(r.URL.Path, "/screenshot") {
			jsonResponse(w, map[string]interface{}{"value": nil})
			return
		}
		f.mu.Lock()
		i := f.shots
		f.shots++
		f.mu.Unlock()
		frame := fmt.Sprintf("frame-%d", i)
		if !f.moving {
			frame = f.frames[min(i, len(f.frames)-1)]
		}
		if frame == "" {
			w.WriteHeader(http.StatusInternalServerError)
			jsonResponse(w, map[string]interface{}{"value": map[string]interface{}{"error": "unknown error", "message": "cannot take a screenshot"}})
			return
		}
		jsonResponse(w, map[string]interface{}{"value": base64.StdEncoding.EncodeToString([]byte(frame))})
	}))
	t.Cleanup(server.Close)
	return server
}

func (f *frameServer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.shots
}

func TestWaitUntilScreenIsStatic(t *testing.T) {
	for _, tc := range []struct {
		name      string
		frames    []string
		want      bool
		wantShots int
	}{
		{"a still screen costs two screenshots", []string{"A"}, true, 2},
		{"an animation ends", []string{"A", "B", "C", "C"}, true, 4},
		// Any difference is movement: Maestro compares hashes, not a tolerance.
		{"one byte differs", []string{"frame-1", "frame-2", "frame-2"}, true, 3},
		{"a failed screenshot ends the wait", []string{"A", ""}, false, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := &frameServer{frames: tc.frames}
			d := createTestDriver(fs.start(t))
			if got := d.WaitUntilScreenIsStatic(3000); got != tc.want {
				t.Errorf("static = %v, want %v", got, tc.want)
			}
			if got := fs.count(); got != tc.wantShots {
				t.Errorf("screenshots = %d, want %d", got, tc.wantShots)
			}
		})
	}
}

// A screen that never holds still is given up on at the limit.
func TestWaitUntilScreenIsStaticGivesUpAtTheLimit(t *testing.T) {
	fs := &frameServer{moving: true}
	d := createTestDriver(fs.start(t))
	start := time.Now()
	if d.WaitUntilScreenIsStatic(300) {
		t.Fatal("a screen that keeps changing is not static")
	}
	if elapsed := time.Since(start); elapsed < 300*time.Millisecond || elapsed > 2*time.Second {
		t.Errorf("gave up after %v, want about 300ms", elapsed)
	}
}

// screenWDA is a fake WDA for the settle: one element, el1, found by id, whose
// rect reads play xs in turn (the last repeating), and screenshots that are
// all the same unless moving is set. It logs the requests that matter, in
// order: "screenshot", "rect", "click", "tap", "actions", "drag", "source".
type screenWDA struct {
	mu     sync.Mutex
	xs     []int
	moving bool
	shots  int
	rects  int
	log    []string
}

func (f *screenWDA) start(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		f.mu.Lock()
		defer f.mu.Unlock()
		p := r.URL.Path
		switch {
		case strings.HasSuffix(p, "/screenshot"):
			f.log = append(f.log, "screenshot")
			f.shots++
			frame := "still"
			if f.moving {
				frame = fmt.Sprintf("frame-%d", f.shots)
			}
			jsonResponse(w, map[string]interface{}{"value": base64.StdEncoding.EncodeToString([]byte(frame))})
		case strings.HasSuffix(p, "/elements"):
			jsonResponse(w, map[string]interface{}{"value": []map[string]interface{}{{"ELEMENT": "el1"}}})
		case strings.HasSuffix(p, "/element/el1/rect"):
			f.log = append(f.log, "rect")
			x := 100
			if len(f.xs) > 0 {
				x = f.xs[min(f.rects, len(f.xs)-1)]
			}
			f.rects++
			jsonResponse(w, map[string]interface{}{"value": map[string]interface{}{"x": x, "y": 400, "width": 80, "height": 40}})
		case strings.HasSuffix(p, "/element/el1/displayed"):
			jsonResponse(w, map[string]interface{}{"value": true})
		case strings.HasSuffix(p, "/element/el1/click"):
			f.log = append(f.log, "click")
			jsonResponse(w, map[string]interface{}{"value": nil})
		case strings.HasSuffix(p, "/wda/tap"):
			f.log = append(f.log, "tap")
			jsonResponse(w, map[string]interface{}{"value": nil})
		case strings.HasSuffix(p, "/actions"):
			f.log = append(f.log, "actions")
			jsonResponse(w, map[string]interface{}{"value": nil})
		case strings.HasSuffix(p, "/wda/dragfromtoforduration"):
			f.log = append(f.log, "drag")
			jsonResponse(w, map[string]interface{}{"value": nil})
		case strings.HasSuffix(p, "/source"):
			f.log = append(f.log, "source")
			jsonResponse(w, map[string]interface{}{"value": `<?xml version="1.0"?>
<AppiumAUT>
  <XCUIElementTypeApplication name="App" x="0" y="0" width="390" height="844" visible="true">
    <XCUIElementTypeButton label="Continue" x="40" y="700" width="310" height="50" visible="true" enabled="true"/>
  </XCUIElementTypeApplication>
</AppiumAUT>`})
		default:
			jsonResponse(w, map[string]interface{}{"value": nil})
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// steps is the request log, joined for comparison.
func (f *screenWDA) steps() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.log, ",")
}

var tapByID = &flow.TapOnStep{Selector: flow.Selector{ID: "go"}}

func shortenScreenSettle(t *testing.T, ms int) {
	t.Helper()
	orig := screenSettleLimitMs
	screenSettleLimitMs = ms
	t.Cleanup(func() { screenSettleLimitMs = orig })
}

// With the switch, an element tap waits for a still screen between its
// lookup and the tap (Maestro.kt:228), and the screen settles again after it.
// On a still screen each settle is two screenshots.
func TestSettleAroundAnElementTap(t *testing.T) {
	t.Setenv("MAESTRO_WDA_SETTLE", "1")
	f := &screenWDA{}
	d := createTestDriver(f.start(t))
	if res := d.Execute(tapByID); !res.Success {
		t.Fatalf("tap failed: %s", res.Message)
	}
	if got, want := f.steps(), "rect,screenshot,screenshot,click,screenshot,screenshot"; got != want {
		t.Errorf("requests %s, want %s", got, want)
	}
}

// Without it, nothing is added: the tap is the lookup and the click.
func TestNoSettleWithoutTheSwitch(t *testing.T) {
	t.Setenv("MAESTRO_WDA_SETTLE", "")
	f := &screenWDA{}
	d := createTestDriver(f.start(t))
	if res := d.Execute(tapByID); !res.Success {
		t.Fatalf("tap failed: %s", res.Message)
	}
	if got, want := f.steps(), "rect,click"; got != want {
		t.Errorf("requests %s, want %s", got, want)
	}
}

// The steps Maestro settles after, and one it does not.
func TestSettleAfterStepsThatChangeTheScreen(t *testing.T) {
	t.Setenv("MAESTRO_WDA_SETTLE", "1")
	for _, tc := range []struct {
		name string
		step flow.Step
		want string
	}{
		{"point tap", &flow.TapOnPointStep{X: 10, Y: 20}, "tap,screenshot,screenshot"},
		// A swipe waits for a still screen before it too (IOSDriver.kt:269).
		{"swipe", &flow.SwipeStep{Direction: "UP"}, "screenshot,screenshot,drag,screenshot,screenshot"},
		{"scroll", &flow.ScrollStep{Direction: "DOWN"}, "screenshot,screenshot,drag,screenshot,screenshot"},
		{"press key", &flow.PressKeyStep{Key: "Enter"}, "screenshot,screenshot"},
		{"a read changes nothing", &flow.CopyTextFromStep{Selector: flow.Selector{ID: "go"}}, "rect"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &screenWDA{}
			d := createTestDriver(f.start(t))
			if res := d.Execute(tc.step); !res.Success {
				t.Fatalf("step failed: %s", res.Message)
			}
			if got := f.steps(); got != tc.want {
				t.Errorf("requests %s, want %s", got, tc.want)
			}
		})
	}
}

// A failed step is not waited on, as in Maestro, where the command throws.
func TestNoSettleAfterAFailedStep(t *testing.T) {
	t.Setenv("MAESTRO_WDA_SETTLE", "1")
	f := &screenWDA{}
	d := createTestDriver(f.start(t))
	if res := d.Execute(&flow.BackStep{}); res.Success {
		t.Fatal("back is not supported on iOS")
	}
	if got := f.steps(); got != "" {
		t.Errorf("requests %s, want none", got)
	}
}

// The rect watch after a tap is the default's wait; with the switch the
// screen settle after the tap has already done it.
func TestSettleReplacesTheRectWatchAfterATap(t *testing.T) {
	t.Setenv("MAESTRO_WDA_SETTLE", "1")
	f := &screenWDA{}
	d := createTestDriver(f.start(t))
	d.lastTapID = "el1"
	d.Execute(&flow.TapOnPointStep{X: 10, Y: 20})
	if got, want := f.steps(), "tap,screenshot,screenshot"; got != want {
		t.Errorf("requests %s, want %s", got, want)
	}
}

// After a scroll the element is read again until it has stopped moving,
// and the step reports where it came to rest (Maestro.kt:230-242). Two reads
// agreeing is enough; the lookup's own read counts as the first.
func TestElementTapAfterAScrollWaitsForTheElementToRest(t *testing.T) {
	t.Setenv("MAESTRO_WDA_SETTLE", "1")
	// The lookup reads x=100; the list is still gliding: 120, 130, then rests.
	f := &screenWDA{xs: []int{100, 120, 130, 130}}
	d := createTestDriver(f.start(t))
	d.recentScroll = true
	res := d.Execute(tapByID)
	if !res.Success {
		t.Fatalf("tap failed: %s", res.Message)
	}
	if got, want := f.steps(), "rect,screenshot,screenshot,rect,rect,rect,click,screenshot,screenshot"; got != want {
		t.Errorf("requests %s, want %s", got, want)
	}
	if res.Element == nil || res.Element.Bounds.X != 130 {
		t.Errorf("tapped element at %+v, want it where it came to rest (x=130)", res.Element)
	}
	if d.recentScroll {
		t.Error("the tap should use up the scroll")
	}
}

// An element that has not moved costs one more read after a scroll.
func TestElementAtRestAfterAScrollCostsOneRead(t *testing.T) {
	t.Setenv("MAESTRO_WDA_SETTLE", "1")
	f := &screenWDA{}
	d := createTestDriver(f.start(t))
	d.recentScroll = true
	d.Execute(tapByID)
	if got, want := f.steps(), "rect,screenshot,screenshot,rect,click,screenshot,screenshot"; got != want {
		t.Errorf("requests %s, want %s", got, want)
	}
}

// A screen that never holds still gets the element's resting place read too,
// in place of the hierarchy settle Maestro falls back on.
func TestElementTapOnAMovingScreenWaitsForTheElementToRest(t *testing.T) {
	t.Setenv("MAESTRO_WDA_SETTLE", "1")
	shortenScreenSettle(t, 200)
	f := &screenWDA{moving: true}
	d := createTestDriver(f.start(t))
	start := time.Now()
	res := d.Execute(tapByID)
	if !res.Success {
		t.Fatalf("tap failed: %s", res.Message)
	}
	if f.rects != 2 {
		t.Errorf("rect reads = %d, want the lookup's and one more", f.rects)
	}
	// Two settles at the limit, one before the tap and one after it.
	if elapsed := time.Since(start); elapsed < 400*time.Millisecond || elapsed > 3*time.Second {
		t.Errorf("took %v, want the two limits", elapsed)
	}
}

// An element with no WDA id, found in the page source, is read again from the
// page source.
func TestRestingBoundsWithoutAnIDReadsThePageSource(t *testing.T) {
	f := &screenWDA{}
	d := createTestDriver(f.start(t))
	b := d.restingBounds(flow.Selector{Text: "Continue"}, &core.ElementInfo{Bounds: core.Bounds{X: 40, Y: 700, Width: 310, Height: 50}})
	if b != (core.Bounds{X: 40, Y: 700, Width: 310, Height: 50}) {
		t.Errorf("bounds %+v", b)
	}
	if got := f.steps(); got != "source" {
		t.Errorf("requests %s, want one page source", got)
	}
}

// A swipe or a scroll marks the next element tap to wait for the element; a
// tap, of any kind, or a launch clears it (Maestro.kt:88, 181-212, 387).
func TestRecentScrollIsSetByAScrollAndUsedByTheNextTap(t *testing.T) {
	t.Setenv("MAESTRO_WDA_SETTLE", "")
	f := &screenWDA{}
	d := createTestDriver(f.start(t))
	d.Execute(&flow.SwipeStep{Direction: "UP"})
	if !d.recentScroll {
		t.Fatal("a swipe should mark a recent scroll")
	}
	d.Execute(&flow.AssertVisibleStep{Selector: flow.Selector{ID: "go"}})
	if !d.recentScroll {
		t.Fatal("an assertion does not use it up")
	}
	d.Execute(&flow.TapOnPointStep{X: 1, Y: 1})
	if d.recentScroll {
		t.Fatal("a point tap uses it up too")
	}
	d.Execute(&flow.ScrollStep{Direction: "DOWN"})
	d.settleAfter(&flow.LaunchAppStep{}, &core.CommandResult{})
	if d.recentScroll {
		t.Fatal("launchApp clears it")
	}
}

// An optional tapOn that finds nothing taps nothing, so the scroll is still
// there for the next tap to let come to rest (performTap clears it in Maestro,
// Maestro.kt:387).
func TestRecentScrollOutlivesAnOptionalTapThatFoundNothing(t *testing.T) {
	d := createTestDriver((&screenWDA{}).start(t))
	d.recentScroll = true
	d.settleAfter(&flow.TapOnStep{Selector: flow.Selector{Text: "Not now"}, BaseStep: flow.BaseStep{Optional: true}},
		core.SuccessResult("Optional element not found, skipping tap", nil))
	if !d.recentScroll {
		t.Error("a tap that did not happen should leave the scroll marked")
	}
	d.settleAfter(&flow.TapOnStep{Selector: flow.Selector{Text: "Item"}}, core.SuccessResult("Tapped element", &core.ElementInfo{}))
	if d.recentScroll {
		t.Error("a tap that happened uses it up")
	}
}
