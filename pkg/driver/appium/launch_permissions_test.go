package appium

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/devicelab-dev/maestro-runner/pkg/flow"
)

type mobileCall struct {
	Script string
	Args   map[string]interface{}
}

func recordMobileCalls(t *testing.T) (*Driver, *[]mobileCall) {
	t.Helper()
	var calls []mobileCall
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/execute/sync") {
			var body struct {
				Script string                   `json:"script"`
				Args   []map[string]interface{} `json:"args"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			c := mobileCall{Script: body.Script}
			if len(body.Args) > 0 {
				c.Args = body.Args[0]
			}
			calls = append(calls, c)
		}
		writeJSON(w, map[string]interface{}{"value": nil})
	}))
	t.Cleanup(server.Close)
	return createTestAppiumDriver(server), &calls
}

// launchApp applies `permissions:` on every launch, as Maestro does, through
// the setPermissions mapping: `camera: deny` revokes android.permission.CAMERA.
// It used to run only with clearState, and sent `pm grant <app> camera`.
func TestLaunchAppAppliesPermissionsWithoutClearState(t *testing.T) {
	d, calls := recordMobileCalls(t)
	stop := false
	res := d.launchApp(&flow.LaunchAppStep{AppID: "com.x", StopApp: &stop, Permissions: map[string]string{"camera": "deny"}})
	if !res.Success {
		t.Fatalf("launchApp: %s", res.Message)
	}
	var revoked []interface{}
	for _, c := range *calls {
		if c.Script == "mobile: shell" && strings.Contains(strings.Join(toStrings(c.Args["args"]), " "), "grant") {
			t.Errorf("used pm grant: %v", c.Args)
		}
		if c.Script == "mobile: changePermissions" && c.Args["action"] == "revoke" {
			revoked, _ = c.Args["permissions"].([]interface{})
		}
	}
	if len(revoked) != 1 || revoked[0] != "android.permission.CAMERA" {
		t.Errorf("revoked %v, want [android.permission.CAMERA]", revoked)
	}
}

func toStrings(v interface{}) []string {
	var out []string
	if l, ok := v.([]interface{}); ok {
		for _, x := range l {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// setClipboard uses `mobile: setClipboard`, which current drivers support;
// Appium 3's UiAutomator2 driver no longer serves /appium/device/set_clipboard.
func TestSetClipboardUsesMobileCommand(t *testing.T) {
	d, calls := recordMobileCalls(t)
	if res := d.setClipboard(&flow.SetClipboardStep{Text: "hi"}); !res.Success {
		t.Fatalf("setClipboard: %s", res.Message)
	}
	if len(*calls) == 0 || (*calls)[0].Script != "mobile: setClipboard" || (*calls)[0].Args["content"] != "aGk=" {
		t.Errorf("calls = %+v, want mobile: setClipboard with base64 content", *calls)
	}
}
