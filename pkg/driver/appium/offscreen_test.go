package appium

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/devicelab-dev/maestro-runner/pkg/flow"
)

// iOS assertVisible used no visibility check at all, so a row below the fold
// passed. An element less than 10% inside the viewport now fails.
func TestIOSAssertVisibleRejectsOffScreenElements(t *testing.T) {
	for name, tc := range map[string]struct {
		y    int
		want bool
	}{
		"on screen":      {y: 300, want: true},
		"below the fold": {y: 1500, want: false},
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch p := r.URL.Path; {
				case strings.HasSuffix(p, "/element") && r.Method == http.MethodPost:
					writeJSON(w, map[string]interface{}{"value": map[string]interface{}{w3cElementKey: "row"}})
				case strings.HasSuffix(p, "/rect"):
					writeJSON(w, map[string]interface{}{"value": map[string]interface{}{"x": 0, "y": tc.y, "width": 390, "height": 60}})
				default:
					writeJSON(w, map[string]interface{}{"value": ""})
				}
			}))
			t.Cleanup(server.Close)
			d := createTestAppiumDriver(server)
			d.platform, d.client.platform = "ios", "ios"
			d.client.screenW, d.client.screenH = 390, 844

			res := d.assertVisible(&flow.AssertVisibleStep{Selector: flow.Selector{ID: "row"}})
			if res.Success != tc.want {
				t.Errorf("success = %v (%s), want %v", res.Success, res.Message, tc.want)
			}
		})
	}
}

// Page-source counts ignore matches outside the viewport, as Maestro does.
func TestCountIgnoresOffScreenMatches(t *testing.T) {
	src := `<?xml version="1.0" encoding="UTF-8"?>
<hierarchy rotation="0">
  <android.widget.TextView text="Item" displayed="true" bounds="[0,100][500,200]"/>
  <android.widget.TextView text="Item" displayed="true" bounds="[0,3000][500,3100]"/>
</hierarchy>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/source") {
			writeJSON(w, map[string]interface{}{"value": src})
			return
		}
		writeJSON(w, map[string]interface{}{"value": ""})
	}))
	t.Cleanup(server.Close)
	d := createTestAppiumDriver(server)
	res := d.assertVisible(&flow.AssertVisibleStep{Selector: flow.Selector{Text: "Item"}, Count: "1"})
	if !res.Success {
		t.Errorf("want exactly 1 visible Item (the other is off screen): %s", res.Message)
	}
}
