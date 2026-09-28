package wda

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/devicelab-dev/maestro-runner/pkg/flow"
)

// Maestro's text match: the regex covers the whole text, ignoring case, with
// . matching a newline, or the text equals the pattern as written
// (Filters.kt:58-108, Orchestra.kt:1841).
func TestMaestroTextMatches(t *testing.T) {
	for _, tc := range []struct {
		pattern, text string
		want          bool
	}{
		{"Resend code", "Resend code", true},
		{"Resend code", "resend CODE", true},
		{"Resend code", "Resend code in 0:42", false}, // not a substring match
		{"code", "Resend code", false},
		{"Resend code.*", "Resend code in 0:42", true},
		{"^SIGN OUT$", "Sign out", true},       // IGNORE_CASE applies to regexes too
		{"Sign.*now", "Sign in\nnow", true},    // . matches a newline
		{"Resend code", "Resend\ncode", true},  // a newline also reads as a space
		{"Price (USD)", "Price (USD)", true},   // equal to the pattern as written
		{"Price (USD)", "price (usd)", false},  // the equality is case-sensitive
		{"Price (USD)", "price usd", true},     // and the regex is not
		{"Continue?", "Continue?", true},       // equality again
		{"Continue?", "Continu", true},         // the ? makes the e optional
		{"Tap [here", "tap [HERE", true},       // not a regex: a literal
		{"a)|(b", "a", false},                  // not a regex, so no alternation
		{"a)|(b", "A)|(B", true},               // but the literal matches
		{".*", "", true},                       // empty texts are tried too
		{`.*\b67\b.*`, "about 67 kcal", true},  // \b
		{"(?i)test offer", "TEST OFFER", true}, // an inline flag is harmless
		{"line2", "line1\nline2", false},       // still the whole text
		{"é", "é", true},                      // NFC on both sides
		{"mastodon.social", "mastodonXsocial", true},
	} {
		if got := maestroTextMatches(tc.pattern, tc.text); got != tc.want {
			t.Errorf("maestroTextMatches(%q, %q) = %v, want %v", tc.pattern, tc.text, got, tc.want)
		}
	}
}

func TestMeansItself(t *testing.T) {
	for s, want := range map[string]bool{
		"Resend code":     true,
		"mastodon.social": true,
		"Don't allow":     true,
		"Continue?":       false,
		"^SIGN OUT$":      false,
		"Tap [here":       true, // taken literally
		`\d+ items`:       false,
	} {
		if got := meansItself(s); got != want {
			t.Errorf("meansItself(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestPredicateLiteral(t *testing.T) {
	if got, ok := predicateLiteral(`Don't \ stop`); !ok || got != `'Don\'t \\ stop'` {
		t.Errorf("predicateLiteral = %s, %v", got, ok)
	}
	for _, s := range []string{"two\nlines", "a `tick`"} {
		if _, ok := predicateLiteral(s); ok {
			t.Errorf("predicateLiteral(%q) should refuse", s)
		}
	}
}

// strictQueryServer records the WDA queries it gets. queries maps a
// substring of a query body to the element it finds; anything else finds
// nothing. rects gives each element's bounds.
type strictQueryServer struct {
	mu      sync.Mutex
	bodies  []string
	queries map[string]string
	rects   map[string][4]int
	source  string
}

func (s *strictQueryServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		p := r.URL.Path
		switch {
		case strings.HasSuffix(p, "/source"):
			jsonResponse(w, map[string]interface{}{"value": s.source})
		case (strings.HasSuffix(p, "/element") || strings.HasSuffix(p, "/elements")) && r.Method == "POST":
			b, _ := io.ReadAll(r.Body)
			body := string(b)
			s.mu.Lock()
			s.bodies = append(s.bodies, body)
			s.mu.Unlock()
			for sub, id := range s.queries {
				if strings.Contains(body, sub) {
					if strings.HasSuffix(p, "/elements") {
						jsonResponse(w, map[string]interface{}{"value": []map[string]interface{}{{"ELEMENT": id}}})
					} else {
						jsonResponse(w, map[string]interface{}{"value": map[string]interface{}{"ELEMENT": id}})
					}
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)
			jsonResponse(w, map[string]interface{}{"value": map[string]interface{}{"error": "no such element"}})
		case strings.HasSuffix(p, "/rect"):
			for id, b := range s.rects {
				if strings.Contains(p, "/element/"+id+"/") {
					jsonResponse(w, map[string]interface{}{"value": map[string]interface{}{"x": b[0], "y": b[1], "width": b[2], "height": b[3]}})
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)
			jsonResponse(w, map[string]interface{}{"value": map[string]interface{}{"error": "stale element reference"}})
		case strings.HasSuffix(p, "/displayed"):
			jsonResponse(w, map[string]interface{}{"value": true})
		default:
			jsonResponse(w, map[string]interface{}{"status": 0})
		}
	}))
}

func (s *strictQueryServer) sent() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.bodies...)
}

const resendSource = `<AppiumAUT>
  <XCUIElementTypeApplication type="XCUIElementTypeApplication" x="0" y="0" width="390" height="844">
    <XCUIElementTypeStaticText type="XCUIElementTypeStaticText" value="Resend code in 0:42" label="Resend code in 0:42" x="20" y="400" width="300" height="30"/>
  </XCUIElementTypeApplication>
</AppiumAUT>`

// The false pass behind the switch: a literal "Resend code" matched the
// countdown label "Resend code in 0:42". With the switch it does not.
func TestStrictTextIsAWholeMatch(t *testing.T) {
	s := &strictQueryServer{source: resendSource}
	server := s.start(t)
	defer server.Close()
	step := &flow.AssertVisibleStep{BaseStep: flow.BaseStep{TimeoutMs: 300}, Selector: flow.Selector{Text: "Resend code"}}

	if res := createTestDriver(server).assertVisible(step); !res.Success {
		t.Fatalf("setup: without the switch the substring matches: %s", res.Message)
	}

	t.Setenv("MAESTRO_STRICT_SELECTORS", "1")
	if res := createTestDriver(server).assertVisible(step); res.Success {
		t.Error("with the switch, \"Resend code\" must not match \"Resend code in 0:42\"")
	}
	step.Selector.Text = "Resend code.*"
	if res := createTestDriver(server).assertVisible(step); !res.Success {
		t.Errorf("a regex over the whole text still matches: %s", res.Message)
	}
}

// Text that means itself is asked of WDA as an exact, case-insensitive
// comparison over label, value and placeholder; a regex is not asked of WDA
// at all.
func TestStrictTextQueries(t *testing.T) {
	t.Setenv("MAESTRO_STRICT_SELECTORS", "1")
	s := &strictQueryServer{source: resendSource}
	server := s.start(t)
	defer server.Close()
	d := createTestDriver(server)

	_, _ = d.findElementOnce(flow.Selector{Text: "Don't allow"})
	sent := s.sent()
	want := `(label ==[c] 'Don\\'t allow' OR value ==[c] 'Don\\'t allow' OR placeholderValue ==[c] 'Don\\'t allow')`
	if len(sent) != 1 || !strings.Contains(sent[0], want) {
		t.Errorf("queries = %q, want one containing %s", sent, want)
	}

	s.bodies = nil
	_, _ = d.findElementOnce(flow.Selector{Text: "^Sign out$"})
	if sent := s.sent(); len(sent) != 0 {
		t.Errorf("a regex text went to WDA: %q", sent)
	}
}

const emailTapSource = `<AppiumAUT>
  <XCUIElementTypeApplication type="XCUIElementTypeApplication" x="0" y="0" width="390" height="844">
    <XCUIElementTypeTextField type="XCUIElementTypeTextField" placeholderValue="Email address" value="Email address" label="" x="20" y="100" width="300" height="44"/>
    <XCUIElementTypeButton type="XCUIElementTypeButton" label="Email" x="20" y="200" width="300" height="44"/>
  </XCUIElementTypeApplication>
</AppiumAUT>`

// A tap looks the element up as any other step does. The runner's tap
// strategy tried text fields first by substring, so a field whose
// placeholder contains "Email" outranked the button labelled "Email".
func TestStrictTapDoesNotPreferContainingTextField(t *testing.T) {
	s := &strictQueryServer{
		source: emailTapSource,
		queries: map[string]string{
			"XCUIElementTypeTextField[`(label CONTAINS[c] 'Email'": "field",
			"==[c] 'Email'": "button",
		},
		rects: map[string][4]int{"field": {20, 100, 300, 44}, "button": {20, 200, 300, 44}},
	}
	server := s.start(t)
	defer server.Close()

	info, err := createTestDriver(server).findElementForTap(flow.Selector{Text: "Email"}, false, 500)
	if err != nil || info.Bounds.Y != 100 {
		t.Fatalf("setup: without the switch the field wins: %+v, %v", info, err)
	}

	t.Setenv("MAESTRO_STRICT_SELECTORS", "1")
	info, err = createTestDriver(server).findElementForTap(flow.Selector{Text: "Email"}, false, 500)
	if err != nil {
		t.Fatal(err)
	}
	if info.Bounds.Y != 200 {
		t.Errorf("tap went to y=%d, want the button labelled Email (y=200)", info.Bounds.Y)
	}
}
