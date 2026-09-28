package wda

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/devicelab-dev/maestro-runner/pkg/core"
	"github.com/devicelab-dev/maestro-runner/pkg/flow"
)

// lookupCountingDriver answers every lookup with nothing found and counts the
// page sources it served.
func lookupCountingDriver(t *testing.T) (*Driver, *int32) {
	t.Helper()
	var sources int32
	server := mockWDAServer(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/source"):
			atomic.AddInt32(&sources, 1)
			jsonResponse(w, map[string]interface{}{"value": `<XCUIElementTypeApplication type="XCUIElementTypeApplication" name="App" x="0" y="0" width="390" height="844" visible="true"/>`})
		case strings.HasSuffix(r.URL.Path, "/elements"):
			jsonResponse(w, map[string]interface{}{"value": []interface{}{}})
		default:
			jsonResponse(w, map[string]interface{}{"value": map[string]interface{}{"width": 390, "height": 844}})
		}
	})
	t.Cleanup(server.Close)
	d := &Driver{
		client: &Client{baseURL: server.URL, httpClient: http.DefaultClient, sessionID: "s1"},
		info:   &core.PlatformInfo{Platform: "ios", ScreenWidth: 390, ScreenHeight: 844},
	}
	return d, &sources
}

// A condition checked after a long step has no budget left; Maestro still
// looks at the screen once.
func TestALookupWithNoTimeLeftStillLooksOnce(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	d, sources := lookupCountingDriver(t)
	if _, err := d.findElementWithContext(ctx, flow.Selector{Text: "Never there"}); err == nil {
		t.Fatal("found an element on an empty screen")
	}
	if n := atomic.LoadInt32(sources); n != 1 {
		t.Errorf("plain lookup read the screen %d times, want exactly 1", n)
	}

	d, sources = lookupCountingDriver(t)
	sel := flow.Selector{Text: "Never there", Below: &flow.Selector{Text: "Anchor"}}
	if _, err := d.findElementRelativeWithContext(ctx, sel); err == nil {
		t.Fatal("found a relative element on an empty screen")
	}
	if n := atomic.LoadInt32(sources); n != 1 {
		t.Errorf("relative lookup read the screen %d times, want exactly 1", n)
	}
}
