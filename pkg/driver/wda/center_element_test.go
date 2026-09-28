package wda

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/devicelab-dev/maestro-runner/pkg/flow"
)

// listFake is a fake WDA with one target row. targetY picks where the row is
// after the given number of scrolls. When moving is set another row shifts on
// every scroll, so the list never reads as stuck; unset, every capture is the
// same, like a list at its end.
type listFake struct {
	mu      sync.Mutex
	scrolls int
	sources int
	targetY func(scrolls int) int
	targetH int
	moving  bool
}

func (f *listFake) server(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		f.mu.Lock()
		defer f.mu.Unlock()
		path := r.URL.Path
		switch {
		case strings.Contains(path, "/dragfromtoforduration"), strings.HasSuffix(path, "/actions"):
			f.scrolls++
			jsonResponse(w, map[string]interface{}{"status": 0})
		case strings.HasSuffix(path, "/source"):
			f.sources++
			other := 0
			if f.moving {
				other = f.scrolls
			}
			jsonResponse(w, map[string]interface{}{"value": fmt.Sprintf(`<AppiumAUT>
  <XCUIElementTypeApplication name="TestApp" enabled="true" visible="true" x="0" y="0" width="390" height="844">
    <XCUIElementTypeStaticText name="Other" label="Other" enabled="true" visible="true" x="20" y="%d" width="200" height="40"/>
    <XCUIElementTypeButton name="target" label="Target" enabled="true" visible="true" x="20" y="%d" width="350" height="%d"/>
  </XCUIElementTypeApplication>
</AppiumAUT>`, 10+other, f.targetY(f.scrolls), f.targetH)})
		case strings.Contains(path, "/window/size"):
			jsonResponse(w, map[string]interface{}{"value": map[string]interface{}{"width": 390.0, "height": 844.0}})
		default:
			jsonResponse(w, map[string]interface{}{"status": 0})
		}
	}))
}

func (f *listFake) run(t *testing.T, step *flow.ScrollUntilVisibleStep) (bool, string, int) {
	t.Helper()
	server := f.server(t)
	defer server.Close()
	result := createTestDriver(server).scrollUntilVisible(step)
	f.mu.Lock()
	defer f.mu.Unlock()
	return result.Success, result.Message, f.scrolls
}

func centerStep(center bool) *flow.ScrollUntilVisibleStep {
	return &flow.ScrollUntilVisibleStep{
		Element:       flow.Selector{Text: "Target"},
		Direction:     "down",
		CenterElement: center,
		BaseStep:      flow.BaseStep{TimeoutMs: 60000},
	}
}

// A row fully on screen near the bottom (center 725 of 844) passes the plain
// test at once. With centerElement it is scrolled once more, to the middle
// (center 425): Maestro wants its center above 70% of the screen.
func TestScrollUntilVisibleCenterElementScrollsTheRowToTheMiddle(t *testing.T) {
	at := func(scrolls int) int {
		if scrolls == 0 {
			return 700
		}
		return 400
	}
	for _, c := range []struct {
		center  bool
		scrolls int
	}{{false, 0}, {true, 1}} {
		ok, msg, scrolls := (&listFake{targetY: at, targetH: 50, moving: true}).run(t, centerStep(c.center))
		if !ok {
			t.Fatalf("centerElement=%v: step failed: %s", c.center, msg)
		}
		if scrolls != c.scrolls {
			t.Errorf("centerElement=%v: %d scrolls, want %d", c.center, scrolls, c.scrolls)
		}
	}
}

// A row that never reaches the middle is looked at five times, each look
// followed by a scroll (Maestro's retry count 0 to 4), and then the plain
// test decides: a fully visible row is taken, a half-hidden one is not.
func TestScrollUntilVisibleCenterElementSettlesAfterFiveLooks(t *testing.T) {
	ok, msg, scrolls := (&listFake{targetY: func(int) int { return 700 }, targetH: 50, moving: true}).run(t, centerStep(true))
	if !ok {
		t.Fatalf("a fully visible row must be taken once the five looks are spent: %s", msg)
	}
	if scrolls != 5 {
		t.Errorf("%d scrolls, want 5", scrolls)
	}

	step := centerStep(true)
	step.MaxScrolls = 8
	ok, _, scrolls = (&listFake{targetY: func(int) int { return 819 }, targetH: 50, moving: true}).run(t, step)
	if ok {
		t.Fatal("a row half below the fold must not be taken by the plain test")
	}
	if scrolls != 8 {
		t.Errorf("%d scrolls, want all 8 the step allows", scrolls)
	}
}

// At the end of a list nothing can move the row nearer the middle, so once
// the list reads as stuck the plain test decides at once, where Maestro would
// spend its remaining looks first.
func TestScrollUntilVisibleCenterElementAtTheEndOfTheList(t *testing.T) {
	ok, msg, scrolls := (&listFake{targetY: func(int) int { return 700 }, targetH: 50}).run(t, centerStep(true))
	if !ok {
		t.Fatalf("a fully visible last row must be taken: %s", msg)
	}
	if scrolls != 2 {
		t.Errorf("%d scrolls, want the 2 that show the list is stuck", scrolls)
	}

	ok, msg, _ = (&listFake{targetY: func(int) int { return 819 }, targetH: 50}).run(t, centerStep(true))
	if ok || !strings.Contains(msg, "made no progress") {
		t.Errorf("a half-hidden last row must fail as stuck, got success=%v %q", ok, msg)
	}
}

// At 10% or less on screen centerElement does not try to center: the plain
// test decides, so a 10% sliver passes visibilityPercentage 10 where it is.
// (Less than 10% is never found here: the page source drops it.)
func TestScrollUntilVisibleCenterElementLeavesASliverToThePlainTest(t *testing.T) {
	step := centerStep(true)
	step.VisibilityPercentage = 10
	ok, msg, scrolls := (&listFake{targetY: func(int) int { return 834 }, targetH: 100, moving: true}).run(t, step)
	if !ok {
		t.Fatalf("step failed: %s", msg)
	}
	if scrolls != 0 {
		t.Errorf("%d scrolls, want 0", scrolls)
	}
}

// On a real phone a page source takes 2 to 3 s, and each pass used to read
// two: one for the lookup and one to tell whether the list still moves. One
// serves both now.
func TestScrollUntilVisibleReadsOnePageSourceAPass(t *testing.T) {
	f := &listFake{targetY: func(int) int { return 2000 }, targetH: 50, moving: true}
	step := centerStep(false)
	step.MaxScrolls = 4
	ok, _, scrolls := f.run(t, step)
	if ok {
		t.Fatal("a row below the screen was taken as found")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if scrolls != 4 || f.sources != 4 {
		t.Errorf("%d scrolls read %d page sources, want 4 and 4", scrolls, f.sources)
	}
}
