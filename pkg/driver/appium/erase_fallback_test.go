package appium

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/devicelab-dev/maestro-runner/pkg/flow"
)

// On iOS the delete-key fallback used Android's press_keycode, which XCUITest
// does not route: every press 404'd and eraseText still reported success.
// It now sends "\b" characters, which WDA types as the delete key.
func TestEraseTextIOSFallbackSendsDeleteCharacters(t *testing.T) {
	var valueBody string
	keycodes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch p := r.URL.Path; {
		case strings.HasSuffix(p, "/element/active") && r.Method == http.MethodGet:
			writeJSON(w, map[string]interface{}{"value": map[string]interface{}{w3cElementKey: "field"}})
		case strings.HasSuffix(p, "/element/field/text"):
			// A secure or custom field that does not expose its text.
			w.WriteHeader(http.StatusNotFound)
			writeJSON(w, map[string]interface{}{"value": map[string]interface{}{"error": "no such element", "message": "x"}})
		case strings.HasSuffix(p, "/element/field/value"):
			b, _ := io.ReadAll(r.Body)
			valueBody = string(b)
			writeJSON(w, map[string]interface{}{"value": nil})
		case strings.Contains(p, "press_keycode"):
			keycodes++
			w.WriteHeader(http.StatusNotFound)
			writeJSON(w, map[string]interface{}{"value": map[string]interface{}{"error": "unknown command", "message": "x"}})
		default:
			writeJSON(w, map[string]interface{}{"value": ""})
		}
	}))
	t.Cleanup(server.Close)
	d := createTestAppiumDriver(server)
	d.platform, d.client.platform = "ios", "ios"

	res := d.eraseText(&flow.EraseTextStep{Characters: 3})
	if !res.Success {
		t.Fatalf("eraseText failed: %s", res.Message)
	}
	if keycodes != 0 {
		t.Errorf("sent %d Android key codes on iOS", keycodes)
	}
	if !strings.Contains(valueBody, `"text":"\b\b\b"`) {
		t.Errorf("value body = %s, want three \\b characters", valueBody)
	}
}

// After tapping one field and then a second one that resolves through page
// source (no element id), inputText on iOS must not type into the first
// field: the remembered id is cleared by every step that can move focus.
func TestIOSInputTextForgetsAnEarlierTappedField(t *testing.T) {
	d := createTestAppiumDriver(httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]interface{}{"value": nil})
	})))
	d.platform = "ios"
	d.lastTappedElementID = "first-field"
	d.forgetTappedElementUnlessTyping(&flow.TapOnPointStep{})
	if d.lastTappedElementID != "" {
		t.Errorf("a tap kept %q; inputText would re-focus that field", d.lastTappedElementID)
	}
	d.lastTappedElementID = "field"
	d.forgetTappedElementUnlessTyping(&flow.InputTextStep{})
	if d.lastTappedElementID != "field" {
		t.Error("inputText itself must keep the tapped element")
	}
}
