package appium

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/devicelab-dev/maestro-runner/pkg/flow"
)

// androidWholeTextSource holds only elements whose text, content-desc or id
// contains a selector below without being it (#188).
const androidWholeTextSource = `<?xml version="1.0" encoding="UTF-8"?>
<hierarchy rotation="0">
  <android.widget.FrameLayout bounds="[0,0][1080,2340]" class="android.widget.FrameLayout" enabled="true" displayed="true">
    <android.widget.TextView text="Talk · Open" resource-id="com.app:id/login_button" bounds="[0,100][1080,200]" clickable="true" enabled="true" displayed="true" />
    <android.widget.TextView text="" content-desc="Language: English" bounds="[0,300][1080,400]" enabled="true" displayed="true" />
  </android.widget.FrameLayout>
</hierarchy>`

// androidQueryServer answers every UiAutomator query with "no such element"
// except the ones a substring match would hit, records the queries, and
// serves androidWholeTextSource as the page source.
type androidQueryServer struct {
	mu      sync.Mutex
	queries []string
}

func (s *androidQueryServer) handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch {
	case strings.HasSuffix(r.URL.Path, "/source"):
		writeJSON(w, map[string]interface{}{"value": androidWholeTextSource})
	case strings.HasSuffix(r.URL.Path, "/element") && r.Method == http.MethodPost:
		var body struct{ Using, Value string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		s.mu.Lock()
		s.queries = append(s.queries, body.Value)
		s.mu.Unlock()
		// A partial query would find the look-alike on screen.
		if isPartialQuery(body.Value) {
			writeJSON(w, map[string]interface{}{"value": map[string]interface{}{w3cElementKey: "look-alike"}})
			return
		}
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]interface{}{"value": map[string]interface{}{"error": "no such element", "message": "from test server"}})
	default:
		writeJSON(w, map[string]interface{}{"value": nil})
	}
}

// isPartialQuery reports a UiSelector query that matches part of a value: a
// *Contains call, or a regex with a wildcard other than the optional package
// prefix of an id.
func isPartialQuery(q string) bool {
	return strings.Contains(q, "Contains(") || strings.Contains(strings.ReplaceAll(q, "(?:.*/)?", ""), ".*")
}

func newAndroidQueryDriver(t *testing.T) (*Driver, *androidQueryServer) {
	t.Helper()
	s := &androidQueryServer{}
	server := httptest.NewServer(http.HandlerFunc(s.handler))
	t.Cleanup(server.Close)
	return createTestAppiumDriver(server), s
}

// `text: Open` must not find a "Talk · Open" row, `text: English` a
// "Language: English" content-desc, nor `id: login` "login_button", through
// either the native queries or the page source (#188).
func TestAndroidSelectorsDoNotMatchPartially(t *testing.T) {
	for _, sel := range []flow.Selector{{Text: "Open"}, {Text: "English"}, {ID: "login"}} {
		d, s := newAndroidQueryDriver(t)
		if info, err := d.findElementDirect(sel); err == nil {
			t.Errorf("%s: found %+v, want not found", sel.Describe(), info)
		}
		if sel.Text != "" {
			if info, err := d.findElementForTapDirect(sel); err == nil {
				t.Errorf("%s (tap): found %+v, want not found", sel.Describe(), info)
			}
		}
		for _, q := range s.queries {
			if isPartialQuery(q) {
				t.Errorf("%s: sent a partial query: %s", sel.Describe(), q)
			}
		}
	}
}

func TestAndroidQueriesMatchWhole(t *testing.T) {
	d, s := newAndroidQueryDriver(t)
	_, _ = d.findElementDirect(flow.Selector{ID: "login"})
	// The exact id strategy goes first; the UiAutomator query that follows
	// must match the whole id.
	if !containsQuery(s.queries, `new UiSelector().resourceIdMatches("(?ims)(?:.*/)?(?:login)")`) {
		t.Errorf("id query = %q", s.queries)
	}

	d, s = newAndroidQueryDriver(t)
	_, _ = d.findElementDirect(flow.Selector{Text: "Open"})
	if len(s.queries) < 2 || s.queries[0] != `new UiSelector().textMatches("(?ims)(?:Open)")` ||
		s.queries[1] != `new UiSelector().descriptionMatches("(?ims)(?:Open)")` {
		t.Errorf("text probes = %q", s.queries)
	}

	d, s = newAndroidQueryDriver(t)
	_, _ = d.findElementForTapDirect(flow.Selector{Text: "Open"})
	if len(s.queries) == 0 || s.queries[0] != `new UiSelector().text("Open").clickable(true)` {
		t.Errorf("first tap query = %q", s.queries)
	}
}

func containsQuery(queries []string, want string) bool {
	for _, q := range queries {
		if q == want {
			return true
		}
	}
	return false
}
