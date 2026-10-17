package appium

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/devicelab-dev/maestro-runner/pkg/flow"
)

// Two "Save" buttons; only the second carries id "confirm_save". A text-only
// native query answers with the first, so id and state have to be checked in
// page source, where every selector field is matched together.
const twoSaveButtons = `<?xml version="1.0" encoding="UTF-8"?>
<hierarchy rotation="0">
  <android.widget.Button resource-id="com.app:id/draft_save" text="Save" clickable="true" enabled="true" checked="false" displayed="true" bounds="[0,100][500,200]"/>
  <android.widget.Button resource-id="com.app:id/confirm_save" text="Save" clickable="true" enabled="true" checked="true" displayed="true" bounds="[0,1000][500,1100]"/>
</hierarchy>`

func twoSaveServer(t *testing.T) (*Driver, *[]byte) {
	t.Helper()
	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch p := r.URL.Path; {
		case strings.HasSuffix(p, "/source"):
			writeJSON(w, map[string]interface{}{"value": twoSaveButtons})
		case strings.HasSuffix(p, "/actions"):
			body, _ = io.ReadAll(r.Body)
			writeJSON(w, map[string]interface{}{"value": nil})
		case strings.HasSuffix(p, "/element") && r.Method == http.MethodPost:
			// A native text query finds the first "Save" (the wrong one).
			writeJSON(w, map[string]interface{}{"value": map[string]interface{}{w3cElementKey: "draft"}})
		case strings.HasSuffix(p, "/rect"):
			writeJSON(w, map[string]interface{}{"value": map[string]interface{}{"x": 0, "y": 100, "width": 500, "height": 100}})
		default:
			writeJSON(w, map[string]interface{}{"value": ""})
		}
	}))
	t.Cleanup(server.Close)
	return createTestAppiumDriver(server), &body
}

func TestTapOnTextWithIDOrStateMatchesAllFields(t *testing.T) {
	checked := true
	for name, sel := range map[string]flow.Selector{
		"text + id":      {Text: "Save", ID: "confirm_save"},
		"text + checked": {Text: "Save", Checked: &checked},
	} {
		t.Run(name, func(t *testing.T) {
			d, body := twoSaveServer(t)
			if res := d.tapOn(&flow.TapOnStep{Selector: sel}); !res.Success {
				t.Fatalf("tapOn failed: %s", res.Message)
			}
			if x, y := firstMove(t, *body); x != 250 || y != 1050 {
				t.Errorf("tapped (%v,%v), want the matching button at (250,1050)", x, y)
			}
		})
	}
}

// A literal id tries the exact id before any id containing it: the substring
// query answers with the first match in tree order, which could be a longer
// id ("login_hint") ahead of the one asked for ("login").
func TestLiteralIDTriesExactBeforeSubstring(t *testing.T) {
	var order []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/element") && r.Method == http.MethodPost {
			b, _ := io.ReadAll(r.Body)
			order = append(order, string(b))
			writeJSON(w, map[string]interface{}{"value": map[string]interface{}{w3cElementKey: "e"}})
			return
		}
		writeJSON(w, map[string]interface{}{"value": map[string]interface{}{"x": 0, "y": 0, "width": 10, "height": 10}})
	}))
	t.Cleanup(server.Close)
	d := createTestAppiumDriver(server)
	if _, err := d.findElementDirect(flow.Selector{ID: "login"}); err != nil {
		t.Fatal(err)
	}
	if len(order) == 0 || !strings.Contains(order[0], `"using":"id"`) {
		t.Errorf("first query = %v, want the exact id strategy", order)
	}
}

// pressKey on Android accepts the uiautomator2 driver's keys, and delete is
// backspace (67), not forward-delete (112).
func TestPressKeyAndroidKeyCodes(t *testing.T) {
	for key, want := range map[string]string{"delete": `"keycode":67`, "menu": `"keycode":82`, "dpad_down": `"keycode":20`, "search": `"keycode":84`} {
		var body string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if strings.Contains(r.URL.Path, "press_keycode") {
				b, _ := io.ReadAll(r.Body)
				body = string(b)
			}
			writeJSON(w, map[string]interface{}{"value": nil})
		}))
		d := createTestAppiumDriver(server)
		res := d.pressKey(&flow.PressKeyStep{Key: key})
		server.Close()
		if !res.Success || !strings.Contains(body, want) {
			t.Errorf("%s: success=%v body=%s, want %s", key, res.Success, body, want)
		}
	}
}

// The extended orientations reach the device on Android through /rotation;
// /orientation accepts only PORTRAIT and LANDSCAPE, so they used to fail.
func TestAndroidExtendedOrientationsUseRotation(t *testing.T) {
	for o, want := range map[string]string{"LANDSCAPE_LEFT": `"z":90`, "UPSIDE_DOWN": `"z":180`, "LANDSCAPE_RIGHT": `"z":270`} {
		var path, body string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.Method == http.MethodPost {
				b, _ := io.ReadAll(r.Body)
				path, body = r.URL.Path, string(b)
			}
			writeJSON(w, map[string]interface{}{"value": nil})
		}))
		d := createTestAppiumDriver(server)
		res := d.setOrientation(&flow.SetOrientationStep{Orientation: o})
		server.Close()
		if !res.Success || !strings.HasSuffix(path, "/rotation") || !strings.Contains(body, want) {
			t.Errorf("%s: success=%v %s %s, want /rotation with %s", o, res.Success, path, body, want)
		}
	}
}
