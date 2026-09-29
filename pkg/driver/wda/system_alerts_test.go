package wda

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/devicelab-dev/maestro-runner/pkg/core"
)

const facetimeAlertText = "FaceTime App Required\nThe FaceTime app is needed to open this link."

// springBoardWDA answers the calls a system alert check makes. SpringBoard
// shows the alert text (none when empty) with buttons, and the session reads
// SpringBoard while defaultActiveApplication points at it, unless appOnly.
// A call to /alert/dismiss, which taps whatever alert is up when it arrives,
// fails the test.
type springBoardWDA struct {
	t      *testing.T
	driver *Driver

	mu         sync.Mutex
	text       string
	buttons    []string
	appOnly    bool     // WDA keeps reading the app under test: SpringBoard is not in the foreground
	failText   bool     // /alert/text fails, as when the accessibility connection drops
	found      []string // what the button query finds; nil finds one button while an alert is up
	clickFails bool     // the click fails as WDA does for an element that is gone
	// onCall runs before each call is answered, with how many calls like it
	// there have been, this one included.
	onCall func(call string, n int)

	failSettings int // the next settings calls that fail
	targeted     bool
	calls        []string
	settings     []map[string]interface{}
	chains       []string // the class chains queried
	clicked      []string // the elements clicked
}

// alertButtonID is the element the button query finds by default.
const alertButtonID = "alert-button-1"

func newSpringBoardWDA(t *testing.T, text string, buttons ...string) *springBoardWDA {
	t.Helper()
	f := &springBoardWDA{t: t, text: text, buttons: buttons}
	server := mockWDAServer(f.serve)
	t.Cleanup(server.Close)
	f.driver = &Driver{
		client: &Client{baseURL: server.URL, httpClient: http.DefaultClient, sessionID: "s1"},
		info:   &core.PlatformInfo{Platform: "ios"},
	}
	return f
}

func (f *springBoardWDA) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	path := strings.TrimPrefix(r.URL.Path, "/session/s1")
	call := r.Method + " " + path
	f.calls = append(f.calls, call)
	if f.onCall != nil {
		f.onCall(call, count(f.calls, call))
	}
	var body map[string]interface{}
	if raw, _ := io.ReadAll(r.Body); len(raw) > 0 {
		_ = json.Unmarshal(raw, &body)
	}
	fail := func(status int, code, message string) {
		w.WriteHeader(status)
		jsonResponse(w, map[string]interface{}{"value": map[string]interface{}{"error": code, "message": message}})
	}
	noAlert := func() {
		fail(http.StatusNotFound, "no such alert", "An attempt was made to operate on a modal dialog when one was not open")
	}

	switch {
	case call == "POST /appium/settings":
		settings, _ := body["settings"].(map[string]interface{})
		f.settings = append(f.settings, settings)
		if f.failSettings > 0 {
			f.failSettings--
			fail(http.StatusInternalServerError, "unknown error", "settings could not be applied")
			return
		}
		f.targeted = settings["defaultActiveApplication"] == springBoardBundleID
		jsonResponse(w, map[string]interface{}{"value": settings})
	case call == "GET /wda/activeAppInfo":
		bundleID := "com.example.app"
		if f.targeted && !f.appOnly {
			bundleID = springBoardBundleID
		}
		jsonResponse(w, map[string]interface{}{"value": map[string]interface{}{"bundleId": bundleID, "pid": 1, "name": ""}})
	case call == "GET /alert/text":
		switch {
		case f.failText:
			fail(http.StatusInternalServerError, "unknown error", "the accessibility connection was lost")
		case f.text == "":
			noAlert()
		default:
			jsonResponse(w, map[string]interface{}{"value": f.text})
		}
	case call == "GET /wda/alert/buttons":
		if f.text == "" {
			noAlert()
			return
		}
		jsonResponse(w, map[string]interface{}{"value": f.buttons})
	case call == "POST /elements":
		if using, _ := body["using"].(string); using != "class chain" {
			f.t.Errorf("elements found by %q, want a class chain", using)
		}
		chain, _ := body["value"].(string)
		f.chains = append(f.chains, chain)
		ids := f.found
		if ids == nil && f.text != "" {
			ids = []string{alertButtonID}
		}
		elements := make([]interface{}, 0, len(ids))
		for _, id := range ids {
			elements = append(elements, map[string]interface{}{"ELEMENT": id})
		}
		jsonResponse(w, map[string]interface{}{"value": elements})
	case r.Method == "POST" && strings.HasPrefix(path, "/element/") && strings.HasSuffix(path, "/click"):
		id := strings.TrimSuffix(strings.TrimPrefix(path, "/element/"), "/click")
		if f.clickFails || f.text == "" {
			fail(http.StatusNotFound, "stale element reference",
				"The element identified by \""+id+"\" is either not present or it has expired from the internal cache. Try to find it again")
			return
		}
		f.clicked = append(f.clicked, id)
		f.text, f.buttons = "", nil
		jsonResponse(w, map[string]interface{}{"value": nil})
	default:
		f.t.Errorf("unexpected WDA call %s %s", r.Method, r.URL.Path)
		jsonResponse(w, map[string]interface{}{"value": nil})
	}
}

// with changes what the fake does from the next call on.
func (f *springBoardWDA) with(change func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	change()
}

// seen returns the calls made and the settings bodies sent, in order.
func (f *springBoardWDA) seen() ([]string, []map[string]interface{}) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls), slices.Clone(f.settings)
}

// taps returns the class chains queried and the elements clicked, in order.
func (f *springBoardWDA) taps() (chains, clicked []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.chains), slices.Clone(f.clicked)
}

// count is how many of calls are call.
func count(calls []string, call string) int {
	n := 0
	for _, c := range calls {
		if c == call {
			n++
		}
	}
	return n
}

var (
	targetSpringBoard = map[string]interface{}{"defaultActiveApplication": springBoardBundleID, "respectSystemAlerts": true}
	backToTheApp      = map[string]interface{}{"defaultActiveApplication": "auto", "respectSystemAlerts": false}
)

// ticks writes a class chain with ~ for each backtick, which a Go raw string
// cannot hold.
func ticks(chain string) string { return strings.ReplaceAll(chain, "~", "`") }

// facetimeCancelChain is the query for the FaceTime alert's Cancel, as sent.
var facetimeCancelChain = ticks(`**/XCUIElementTypeAlert[~label == "FaceTime App Required" OR name == "FaceTime App Required"~]/**/XCUIElementTypeButton[~label == "Cancel"~]`)

func TestSystemAlertCheckSendsNothingWithoutTheSwitch(t *testing.T) {
	f := newSpringBoardWDA(t, facetimeAlertText, "Go to App Store", "Cancel")
	alert, err := f.driver.DismissSystemAlert()
	if alert != nil || err != nil {
		t.Fatalf("DismissSystemAlert() = %+v, %v; want nil, nil", alert, err)
	}
	if calls, _ := f.seen(); len(calls) != 0 {
		t.Errorf("WDA calls without MAESTRO_WDA_DISMISS_SYSTEM_ALERTS: %v", calls)
	}
}

func TestSystemAlertCheckSendsNothingWithoutASession(t *testing.T) {
	t.Setenv("MAESTRO_WDA_DISMISS_SYSTEM_ALERTS", "1")
	f := newSpringBoardWDA(t, facetimeAlertText, "Cancel")
	f.driver.client.sessionID = ""
	if alert, err := f.driver.DismissSystemAlert(); alert != nil || err != nil {
		t.Fatalf("DismissSystemAlert() = %+v, %v; want nil, nil", alert, err)
	}
	if calls, _ := f.seen(); len(calls) != 0 {
		t.Errorf("WDA calls without a session: %v", calls)
	}
}

func TestNoSystemAlertIsOnlyTheCheck(t *testing.T) {
	t.Setenv("MAESTRO_WDA_DISMISS_SYSTEM_ALERTS", "1")
	f := newSpringBoardWDA(t, "")
	alert, err := f.driver.DismissSystemAlert()
	if alert != nil || err != nil {
		t.Fatalf("DismissSystemAlert() = %+v, %v; want nil, nil", alert, err)
	}
	calls, settings := f.seen()
	want := []string{"POST /appium/settings", "GET /wda/activeAppInfo", "GET /alert/text", "POST /appium/settings"}
	if !slices.Equal(calls, want) {
		t.Errorf("calls = %v, want %v", calls, want)
	}
	if len(settings) != 2 || !reflect.DeepEqual(settings[0], targetSpringBoard) || !reflect.DeepEqual(settings[1], backToTheApp) {
		t.Errorf("settings = %v, want SpringBoard targeted, then the app again", settings)
	}
}

func TestFaceTimeAlertIsDismissedWithCancel(t *testing.T) {
	t.Setenv("MAESTRO_WDA_DISMISS_SYSTEM_ALERTS", "1")
	f := newSpringBoardWDA(t, facetimeAlertText, "Go to App Store", "Cancel")
	alert, err := f.driver.DismissSystemAlert()
	if err != nil {
		t.Fatalf("DismissSystemAlert: %v", err)
	}
	if alert == nil || alert.Dismissed != "Cancel" || alert.Title != "FaceTime App Required" {
		t.Fatalf("alert = %+v, want FaceTime App Required dismissed with Cancel", alert)
	}
	if !slices.Equal(alert.Buttons, []string{"Go to App Store", "Cancel"}) || alert.Text != facetimeAlertText {
		t.Errorf("alert = %+v, want its text and both buttons", alert)
	}
	calls, settings := f.seen()
	// The alert is read again just before the tap, and the tap is a click on
	// the one button a query scoped to that alert finds.
	want := []string{"POST /appium/settings", "GET /wda/activeAppInfo", "GET /alert/text", "GET /wda/alert/buttons",
		"GET /alert/text", "POST /elements", "POST /element/" + alertButtonID + "/click", "POST /appium/settings"}
	if !slices.Equal(calls, want) {
		t.Errorf("calls = %v, want %v", calls, want)
	}
	chains, clicked := f.taps()
	if !slices.Equal(chains, []string{facetimeCancelChain}) {
		t.Errorf("queried %q, want %q", chains, facetimeCancelChain)
	}
	if !slices.Equal(clicked, []string{alertButtonID}) {
		t.Errorf("clicked %v, want the button found", clicked)
	}
	if !reflect.DeepEqual(settings[len(settings)-1], backToTheApp) {
		t.Errorf("last settings = %v, want the app targeted again", settings[len(settings)-1])
	}
}

// changeOnTheSecondRead makes the alert text and buttons change just before
// the check reads the alert again for the tap.
func (f *springBoardWDA) changeOnTheSecondRead(text string, buttons ...string) {
	f.with(func() {
		f.onCall = func(call string, n int) {
			if call == "GET /alert/text" && n == 2 {
				f.text, f.buttons = text, buttons
			}
		}
	})
}

// assertNoTap fails t when the check looked for a button or clicked one.
func (f *springBoardWDA) assertNoTap(t *testing.T, wantChains int) {
	t.Helper()
	chains, clicked := f.taps()
	if len(chains) != wantChains || len(clicked) > 0 {
		t.Errorf("queried %q and clicked %v; want %d queries and no click", chains, clicked, wantChains)
	}
	if _, settings := f.seen(); !reflect.DeepEqual(settings[len(settings)-1], backToTheApp) {
		t.Errorf("last settings = %v, want the app targeted again", settings[len(settings)-1])
	}
}

func TestSystemAlertThatClosesBeforeTheTapIsNotTapped(t *testing.T) {
	t.Setenv("MAESTRO_WDA_DISMISS_SYSTEM_ALERTS", "1")
	f := newSpringBoardWDA(t, facetimeAlertText, "Go to App Store", "Cancel")
	f.changeOnTheSecondRead("")
	if alert, err := f.driver.DismissSystemAlert(); alert != nil || err != nil {
		t.Fatalf("DismissSystemAlert() = %+v, %v; want nil, nil for an alert that is gone", alert, err)
	}
	f.assertNoTap(t, 0)
}

// Had SpringBoard's alert closed, WDA would read the app under test again,
// and the app's own dialog, with a Cancel of its own, is what it reports.
func TestSystemAlertThatChangesBeforeTheTapIsNotTapped(t *testing.T) {
	t.Setenv("MAESTRO_WDA_DISMISS_SYSTEM_ALERTS", "1")
	f := newSpringBoardWDA(t, facetimeAlertText, "Go to App Store", "Cancel")
	f.changeOnTheSecondRead("Delete meal?\nIt goes from your day.", "Cancel", "Delete")
	if alert, err := f.driver.DismissSystemAlert(); alert != nil || err != nil {
		t.Fatalf("DismissSystemAlert() = %+v, %v; want nil, nil for an alert that changed", alert, err)
	}
	f.assertNoTap(t, 0)
}

func TestSystemAlertButtonFoundNowhereIsNotTapped(t *testing.T) {
	t.Setenv("MAESTRO_WDA_DISMISS_SYSTEM_ALERTS", "1")
	f := newSpringBoardWDA(t, facetimeAlertText, "Go to App Store", "Cancel")
	f.with(func() { f.found = []string{} })
	alert, err := f.driver.DismissSystemAlert()
	if err != nil || alert == nil || alert.Dismissed != "" {
		t.Fatalf("DismissSystemAlert() = %+v, %v; want the alert, not dismissed", alert, err)
	}
	f.assertNoTap(t, 1)
}

func TestSystemAlertButtonFoundTwiceIsNotTapped(t *testing.T) {
	t.Setenv("MAESTRO_WDA_DISMISS_SYSTEM_ALERTS", "1")
	f := newSpringBoardWDA(t, facetimeAlertText, "Go to App Store", "Cancel")
	f.with(func() { f.found = []string{"alert-button-1", "alert-button-2"} })
	alert, err := f.driver.DismissSystemAlert()
	if err != nil || alert == nil || alert.Dismissed != "" {
		t.Fatalf("DismissSystemAlert() = %+v, %v; want the alert, not dismissed", alert, err)
	}
	f.assertNoTap(t, 1)
}

func TestSystemAlertButtonThatGoesStaleIsAnError(t *testing.T) {
	t.Setenv("MAESTRO_WDA_DISMISS_SYSTEM_ALERTS", "1")
	f := newSpringBoardWDA(t, facetimeAlertText, "Go to App Store", "Cancel")
	f.with(func() { f.clickFails = true })
	alert, err := f.driver.DismissSystemAlert()
	if err == nil || !strings.Contains(err.Error(), "is either not present") {
		t.Errorf("err = %v, want the stale element error", err)
	}
	if alert == nil || alert.Dismissed != "" {
		t.Errorf("alert = %+v, want it found and not dismissed", alert)
	}
	calls, settings := f.seen()
	if count(calls, "POST /element/"+alertButtonID+"/click") != 1 {
		t.Errorf("calls = %v, want one click", calls)
	}
	if !reflect.DeepEqual(settings[len(settings)-1], backToTheApp) {
		t.Errorf("last settings = %v, want the app targeted again", settings[len(settings)-1])
	}
}

func TestSystemAlertTitleIsQuotedInTheButtonQuery(t *testing.T) {
	t.Setenv("MAESTRO_WDA_DISMISS_SYSTEM_ALERTS", "1")
	f := newSpringBoardWDA(t, "Can’t Open \"Maps\\Home\"\nThe link could not be opened.", "Cancel")
	if _, err := f.driver.DismissSystemAlert(); err != nil {
		t.Fatalf("DismissSystemAlert: %v", err)
	}
	want := ticks(`**/XCUIElementTypeAlert[~label == "Can’t Open \"Maps\\Home\"" OR name == "Can’t Open \"Maps\\Home\""~]/**/XCUIElementTypeButton[~label == "Cancel"~]`)
	if chains, _ := f.taps(); !slices.Equal(chains, []string{want}) {
		t.Errorf("queried %q, want %q", chains, want)
	}
}

func TestAlertButtonChainQuotesBothStrings(t *testing.T) {
	// The title's backticks are doubled in the chain; both sides show every
	// backtick as ~.
	got := strings.ReplaceAll(alertButtonChain("Say `hi` \"now\"", `a"b\c`), "`", "~")
	want := `**/XCUIElementTypeAlert[~label == "Say ~~hi~~ \"now\"" OR name == "Say ~~hi~~ \"now\""~]/**/XCUIElementTypeButton[~label == "a\"b\\c"~]`
	if got != want {
		t.Errorf("alertButtonChain = %s\nwant                %s", got, want)
	}
}

func TestDenyListedSystemAlertIsNotTapped(t *testing.T) {
	t.Setenv("MAESTRO_WDA_DISMISS_SYSTEM_ALERTS", "1")
	f := newSpringBoardWDA(t, "Trust This Computer?\nYour settings and data will be accessible from this computer.",
		"Trust", "Don’t Trust")
	alert, err := f.driver.DismissSystemAlert()
	if err != nil {
		t.Fatalf("DismissSystemAlert: %v", err)
	}
	if alert == nil || alert.Dismissed != "" || alert.Title != "Trust This Computer?" {
		t.Fatalf("alert = %+v, want Trust This Computer? left alone", alert)
	}
	if !slices.Equal(alert.Buttons, []string{"Trust", "Don’t Trust"}) {
		t.Errorf("buttons = %q, want both, for the step's error", alert.Buttons)
	}
	f.assertNoTap(t, 0)
}

func TestSystemAlertWithoutAClosingButtonIsNotTapped(t *testing.T) {
	t.Setenv("MAESTRO_WDA_DISMISS_SYSTEM_ALERTS", "1")
	f := newSpringBoardWDA(t, facetimeAlertText, "Go to App Store", "Open")
	alert, err := f.driver.DismissSystemAlert()
	if err != nil {
		t.Fatalf("DismissSystemAlert: %v", err)
	}
	if alert == nil || alert.Dismissed != "" {
		t.Fatalf("alert = %+v, want it found and left alone", alert)
	}
	f.assertNoTap(t, 0)
}

// With WDA still reading the app, an alert it reported could be the app's own
// dialog, here one whose Cancel the check would otherwise tap, so it reads none.
func TestNoAlertIsReadWhileWDAReadsTheApp(t *testing.T) {
	t.Setenv("MAESTRO_WDA_DISMISS_SYSTEM_ALERTS", "1")
	f := newSpringBoardWDA(t, "Discard your changes?", "Cancel", "Discard")
	f.with(func() { f.appOnly = true })
	alert, err := f.driver.DismissSystemAlert()
	if alert != nil || err != nil {
		t.Fatalf("DismissSystemAlert() = %+v, %v; want nil, nil", alert, err)
	}
	calls, settings := f.seen()
	want := []string{"POST /appium/settings", "GET /wda/activeAppInfo", "POST /appium/settings"}
	if !slices.Equal(calls, want) {
		t.Errorf("calls = %v, want %v", calls, want)
	}
	if !reflect.DeepEqual(settings[len(settings)-1], backToTheApp) {
		t.Errorf("last settings = %v, want the app targeted again", settings[len(settings)-1])
	}
}

func TestAppTargetingIsRestoredWhenTheCheckFailsMidway(t *testing.T) {
	t.Setenv("MAESTRO_WDA_DISMISS_SYSTEM_ALERTS", "1")
	f := newSpringBoardWDA(t, facetimeAlertText, "Cancel")
	f.with(func() { f.failText = true })
	alert, err := f.driver.DismissSystemAlert()
	if err == nil || alert != nil {
		t.Fatalf("DismissSystemAlert() = %+v, %v; want the failed read", alert, err)
	}
	calls, settings := f.seen()
	if calls[len(calls)-1] != "POST /appium/settings" || !reflect.DeepEqual(settings[len(settings)-1], backToTheApp) {
		t.Errorf("calls = %v, settings = %v: the app was not targeted again", calls, settings)
	}
	f.assertNoTap(t, 0)
}

func TestAppTargetingIsRestoredWhenTargetingSpringBoardFails(t *testing.T) {
	t.Setenv("MAESTRO_WDA_DISMISS_SYSTEM_ALERTS", "1")
	f := newSpringBoardWDA(t, facetimeAlertText, "Cancel")
	f.with(func() { f.failSettings = 1 })
	if _, err := f.driver.DismissSystemAlert(); err == nil {
		t.Fatal("DismissSystemAlert() passed although WDA refused the settings")
	}
	calls, settings := f.seen()
	want := []string{"POST /appium/settings", "POST /appium/settings"}
	if !slices.Equal(calls, want) || !reflect.DeepEqual(settings[1], backToTheApp) {
		t.Errorf("calls = %v, settings = %v; want the app targeted again after the failed request", calls, settings)
	}
}

func TestRestoringTheAppTargetingIsSentAgainWhenItFails(t *testing.T) {
	f := newSpringBoardWDA(t, "")
	f.driver.restoreAppTargeting()
	if calls, _ := f.seen(); len(calls) != 1 {
		t.Fatalf("calls = %v, want the restore once when it lands", calls)
	}

	f.with(func() { f.failSettings = 1 })
	f.driver.restoreAppTargeting()
	calls, settings := f.seen()
	if len(calls) != 3 || !reflect.DeepEqual(settings[1], backToTheApp) || !reflect.DeepEqual(settings[2], backToTheApp) {
		t.Errorf("calls = %v, settings = %v; want a failed restore sent a second time", calls, settings)
	}
}

func TestAppTargetingGoesBackToTheDefaultActiveApp(t *testing.T) {
	t.Setenv("MAESTRO_WDA_DISMISS_SYSTEM_ALERTS", "1")
	t.Setenv("MAESTRO_WDA_DEFAULT_ACTIVE_APP", "com.apple.ServicesPaymentAngel")
	f := newSpringBoardWDA(t, "")
	if _, err := f.driver.DismissSystemAlert(); err != nil {
		t.Fatalf("DismissSystemAlert: %v", err)
	}
	_, settings := f.seen()
	want := map[string]interface{}{"defaultActiveApplication": "com.apple.ServicesPaymentAngel", "respectSystemAlerts": false}
	if !reflect.DeepEqual(settings[len(settings)-1], want) {
		t.Errorf("last settings = %v, want %v", settings[len(settings)-1], want)
	}
}

func TestSystemAlertButton(t *testing.T) {
	cases := []struct {
		name    string
		text    string
		buttons []string
		want    string
	}{
		{"cancel", facetimeAlertText, []string{"Go to App Store", "Cancel"}, "Cancel"},
		{"the whole label, in any case, as the alert has it", "Restore “FaceTime”?", []string{"Show in App Store", " cancel "}, " cancel "},
		{"only the whole label", "Download Paused", []string{"Cancel Download", "Close Window"}, ""},
		{"preferred in list order", "Low Battery\n10% battery remaining", []string{"Close", "Not Now"}, "Not Now"},
		{"remind me later", "Software Available", []string{"Remind Me Later", "Details"}, "Remind Me Later"},
		{"a permission prompt", "Allow “Vyra” to use your location?", []string{"Allow Once", "Don’t Allow"}, ""},
		{"trust", "Trust This Computer?", []string{"Trust", "Don’t Trust"}, ""},
		{"trust, even with a Cancel", "Trust This Computer?", []string{"Trust", "Cancel"}, ""},
		{"a refusal in the message", "Apple Account\nSign in to continue using iCloud.", []string{"Not Now", "Continue"}, ""},
		{"a refusal in any case", "APPLE ID VERIFICATION", []string{"Cancel"}, ""},
		{"no closing button", facetimeAlertText, []string{"Go to App Store", "Open"}, ""},
		{"no buttons", "No SIM", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, why := systemAlertButton(c.text, c.buttons)
			if got != c.want {
				t.Errorf("systemAlertButton(%q, %q) = %q (%s), want %q", c.text, c.buttons, got, why, c.want)
			}
			if got == "" && why == "" {
				t.Error("an alert left alone has no reason")
			}
		})
	}
}

// The lists are spelled out here, so that a word or a button dropped from
// either list fails a test.
var (
	agreedRefusals = []string{"trust", "password", "passcode", "apple id", "apple account", "sign in", "allow",
		"pay", "purchase", "buy", "subscribe", "delete", "erase", "update", "install"}
	agreedButtons = []string{"Cancel", "Not Now", "Close", "Later", "Dismiss", "Remind Me Later", "Ignore"}
)

func TestSystemAlertListsAreTheAgreedOnes(t *testing.T) {
	if !slices.Equal(systemAlertRefusals, agreedRefusals) {
		t.Errorf("systemAlertRefusals = %q, want %q", systemAlertRefusals, agreedRefusals)
	}
	if !slices.Equal(systemAlertButtons, agreedButtons) {
		t.Errorf("systemAlertButtons = %q, want %q", systemAlertButtons, agreedButtons)
	}
}

func TestEverySystemAlertRefusalLeavesAnAlertAlone(t *testing.T) {
	for _, word := range agreedRefusals {
		for _, text := range []string{strings.ToUpper(word) + " required", "A notice\nThis is about " + word + "."} {
			if got, _ := systemAlertButton(text, []string{"Cancel"}); got != "" {
				t.Errorf("an alert %q is dismissed with %q", text, got)
			}
		}
	}
}

func TestEverySystemAlertButtonDismisses(t *testing.T) {
	for _, label := range agreedButtons {
		if got, _ := systemAlertButton("A notice", []string{"Go Somewhere", strings.ToUpper(label)}); got != strings.ToUpper(label) {
			t.Errorf("an alert with %q is dismissed with %q", strings.ToUpper(label), got)
		}
	}
}

func TestIsNoAlert(t *testing.T) {
	cases := map[string]bool{
		"WDA error: An attempt was made to operate on a modal dialog when one was not open": true,
		"WDA error: no such alert":                     true,
		"WDA error: the accessibility connection lost": false,
	}
	for msg, want := range cases {
		if got := isNoAlert(errString(msg)); got != want {
			t.Errorf("isNoAlert(%q) = %v, want %v", msg, got, want)
		}
	}
	if isNoAlert(nil) {
		t.Error("isNoAlert(nil) = true")
	}
}

type errString string

func (e errString) Error() string { return string(e) }
