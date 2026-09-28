package wda

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/devicelab-dev/maestro-runner/pkg/core"
	"github.com/devicelab-dev/maestro-runner/pkg/flow"
)

func TestPermissionAlertSelector(t *testing.T) {
	cases := map[string]string{
		"accept":  "**/XCUIElementTypeButton[`label BEGINSWITH[c] 'Allow' OR label ==[c] 'OK'`]",
		"dismiss": "**/XCUIElementTypeButton[`label BEGINSWITH[c] 'Don'`]",
		"":        "",
		"other":   "",
	}
	for action, want := range cases {
		if got := permissionAlertSelector(action); got != want {
			t.Errorf("permissionAlertSelector(%q) = %q, want %q", action, got, want)
		}
	}
}

func TestCreateSessionWithPermissionAlertsOnly(t *testing.T) {
	t.Setenv("MAESTRO_WDA_PERMISSION_ALERTS_ONLY", "1")
	client, body := recordingServer(t)
	if err := client.CreateSession("com.example.app", "accept"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, ok := sessionCaps(body("/session"))["defaultAlertAction"]; ok {
		t.Error("defaultAlertAction capability sent: its monitor taps any alert's last button")
	}
	settings, _ := body("/appium/settings")["settings"].(map[string]interface{})
	if settings["defaultAlertAction"] != "" {
		t.Errorf("defaultAlertAction setting = %v, want empty", settings["defaultAlertAction"])
	}
	if sel, _ := settings["autoClickAlertSelector"].(string); !strings.Contains(sel, "'Allow'") {
		t.Errorf("autoClickAlertSelector = %q, want the Allow button", sel)
	}
}

func TestCreateSessionWithoutTheSwitchKeepsDefaultAlertAction(t *testing.T) {
	client, body := recordingServer(t)
	if err := client.CreateSession("com.example.app", "accept"); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if sessionCaps(body("/session"))["defaultAlertAction"] != "accept" {
		t.Error("defaultAlertAction capability missing without the switch")
	}
	settings, _ := body("/appium/settings")["settings"].(map[string]interface{})
	if _, ok := settings["autoClickAlertSelector"]; ok {
		t.Error("autoClickAlertSelector sent without the switch")
	}
}

// launchSettings runs launchApp on a real-device driver and returns every
// settings body WDA received.
func launchSettings(t *testing.T, permissions map[string]string) []map[string]interface{} {
	t.Helper()
	var mu sync.Mutex
	var all []map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/appium/settings") {
			raw, _ := io.ReadAll(r.Body)
			var body map[string]interface{}
			_ = json.Unmarshal(raw, &body)
			settings, _ := body["settings"].(map[string]interface{})
			mu.Lock()
			all = append(all, settings)
			mu.Unlock()
		}
		jsonResponse(w, map[string]interface{}{"value": map[string]interface{}{"sessionId": "s1"}})
	}))
	t.Cleanup(server.Close)
	driver := &Driver{
		client: &Client{baseURL: server.URL, httpClient: http.DefaultClient, sessionID: "s1"},
		info:   &core.PlatformInfo{Platform: "ios", IsSimulator: false},
		udid:   "FAKE-UDID-12345",
	}
	if result := driver.launchApp(&flow.LaunchAppStep{AppID: "com.example.app", Permissions: permissions}); !result.Success {
		t.Fatalf("launchApp: %s", result.Message)
	}
	mu.Lock()
	defer mu.Unlock()
	return all
}

func TestLaunchAppWithPermissionAlertsOnly(t *testing.T) {
	t.Setenv("MAESTRO_WDA_PERMISSION_ALERTS_ONLY", "1")
	cases := []struct {
		name        string
		permissions map[string]string
		want        string
	}{
		{"all allowed by default", nil, "'Allow'"},
		{"all denied", map[string]string{"all": "deny"}, "'Don'"},
		{"mixed: the monitor is off", map[string]string{"camera": "allow", "location": "deny"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bodies := launchSettings(t, c.permissions)
			if len(bodies) == 0 {
				t.Fatal("no settings sent")
			}
			last := bodies[len(bodies)-1]
			if last["defaultAlertAction"] != "" {
				t.Errorf("defaultAlertAction = %v, want empty", last["defaultAlertAction"])
			}
			sel, ok := last["autoClickAlertSelector"].(string)
			if !ok {
				t.Fatalf("autoClickAlertSelector missing: %v", last)
			}
			if c.want == "" && sel != "" || c.want != "" && !strings.Contains(sel, c.want) {
				t.Errorf("autoClickAlertSelector = %q, want %q", sel, c.want)
			}
			if _, ok := last["acceptAlertButtonSelector"]; ok {
				t.Error("acceptAlertButtonSelector sent with the switch")
			}
		})
	}
}

func TestLaunchAppWithoutTheSwitchStillAccepts(t *testing.T) {
	bodies := launchSettings(t, nil)
	last := bodies[len(bodies)-1]
	if last["defaultAlertAction"] != "accept" {
		t.Errorf("defaultAlertAction = %v, want accept", last["defaultAlertAction"])
	}
	if _, ok := last["autoClickAlertSelector"]; ok {
		t.Error("autoClickAlertSelector sent without the switch")
	}
}
