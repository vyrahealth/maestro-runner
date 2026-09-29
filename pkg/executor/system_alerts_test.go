package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/devicelab-dev/maestro-runner/pkg/core"
	"github.com/devicelab-dev/maestro-runner/pkg/driver/wda"
	"github.com/devicelab-dev/maestro-runner/pkg/flow"
	"github.com/devicelab-dev/maestro-runner/pkg/report"
)

// These run flows on the WDA driver against a fake phone, so what is checked
// is what WebDriverAgent is sent.

const (
	facetimeAlert  = "FaceTime App Required\nThe FaceTime app is needed to open this link."
	trustAlert     = "Trust This Computer?\nYour settings and data will be accessible from this computer."
	springBoardApp = "com.apple.springboard"
)

// phoneWDA is WebDriverAgent on a phone whose app has a Call button and shows
// "Connected" unless a SpringBoard alert covers it. A tap on Call brings up
// the alert in raise, as the app's facetime: link does on a phone without
// FaceTime. The alert's buttons are found, while SpringBoard is read, by a
// class chain naming the alert's title and the button, and a click on the
// element found dismisses the alert.
type phoneWDA struct {
	t   *testing.T
	url string

	mu           sync.Mutex
	alert        string // SpringBoard's alert text; "" for none
	buttons      []string
	raise        string
	raiseButtons []string
	returns      int  // how many times the alert comes back after a dismissal
	failText     bool // /alert/text fails, as when the accessibility connection drops
	targeted     bool
	calls        []string
	settings     []map[string]interface{}
	alertQueries []string // the class chains that looked for an alert's button
	pressing     string   // the button the last such query found
	dismissed    []string
}

// clickAlertButton is the click on the alert button the phone finds.
const clickAlertButton = "POST /element/alert-button/click"

// phoneAlertChain is the query the phone answers for button on the alert
// titled title. The strings here need no quoting, so %q is enough.
func phoneAlertChain(title, button string) string {
	return fmt.Sprintf("**/XCUIElementTypeAlert[`label == %q OR name == %q`]/**/XCUIElementTypeButton[`label == %q`]", title, title, button)
}

func newPhoneWDA(t *testing.T) *phoneWDA {
	t.Helper()
	p := &phoneWDA{t: t}
	server := httptest.NewServer(http.HandlerFunc(p.serve))
	t.Cleanup(server.Close)
	p.url = server.URL
	return p
}

// with changes what the phone does from the next call on.
func (p *phoneWDA) with(change func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	change()
}

func (p *phoneWDA) serve(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	path := strings.TrimPrefix(r.URL.Path, "/session/s1")
	call := r.Method + " " + path
	p.calls = append(p.calls, call)
	var body map[string]interface{}
	if raw, _ := io.ReadAll(r.Body); len(raw) > 0 {
		_ = json.Unmarshal(raw, &body)
	}
	answer := func(value interface{}) { writeWDA(w, http.StatusOK, map[string]interface{}{"value": value}) }
	fail := func(status int, code, message string) {
		writeWDA(w, status, map[string]interface{}{"value": map[string]interface{}{"error": code, "message": message}})
	}
	noAlert := func() {
		fail(http.StatusNotFound, "no such alert", "An attempt was made to operate on a modal dialog when one was not open")
	}

	switch call {
	case "POST /session":
		answer(map[string]interface{}{"sessionId": "s1", "capabilities": map[string]interface{}{}})
	case "POST /appium/settings":
		settings, _ := body["settings"].(map[string]interface{})
		p.settings = append(p.settings, settings)
		if app, ok := settings["defaultActiveApplication"]; ok {
			p.targeted = app == springBoardApp
		}
		answer(settings)
	case "POST /elements":
		chain, _ := body["value"].(string)
		if !strings.HasPrefix(chain, "**/XCUIElementTypeAlert[") {
			fail(http.StatusNotFound, "no such element", "An element could not be located on the page")
			return
		}
		p.alertQueries = append(p.alertQueries, chain)
		p.pressing = ""
		if title, _, _ := strings.Cut(p.alert, "\n"); p.targeted && p.alert != "" {
			for _, b := range p.buttons {
				if chain == phoneAlertChain(title, b) {
					p.pressing = b
					answer([]interface{}{map[string]interface{}{"ELEMENT": "alert-button"}})
					return
				}
			}
		}
		answer([]interface{}{})
	case "POST /element":
		fail(http.StatusNotFound, "no such element", "An element could not be located on the page")
	case "GET /source":
		connected := ""
		if p.alert == "" {
			connected = `<XCUIElementTypeStaticText type="XCUIElementTypeStaticText" name="Connected" label="Connected" enabled="true" visible="true" x="50" y="200" width="290" height="40"/>`
		}
		answer(`<?xml version="1.0" encoding="UTF-8"?><AppiumAUT>` +
			`<XCUIElementTypeApplication type="XCUIElementTypeApplication" name="Vyra" label="Vyra" enabled="true" visible="true" x="0" y="0" width="390" height="844">` +
			`<XCUIElementTypeButton type="XCUIElementTypeButton" name="call" label="Call" enabled="true" visible="true" x="50" y="100" width="290" height="50"/>` +
			connected + `</XCUIElementTypeApplication></AppiumAUT>`)
	case "POST /wda/tap":
		if p.raise != "" {
			p.alert, p.buttons = p.raise, p.raiseButtons
		}
		answer(nil)
	case "GET /wda/activeAppInfo":
		bundleID := "com.example.app"
		if p.targeted {
			bundleID = springBoardApp
		}
		answer(map[string]interface{}{"bundleId": bundleID, "pid": 1, "name": ""})
	case "GET /alert/text":
		switch {
		case p.failText:
			fail(http.StatusInternalServerError, "unknown error", "the accessibility connection was lost")
		case p.alert == "":
			noAlert()
		default:
			answer(p.alert)
		}
	case "GET /wda/alert/buttons":
		if p.alert == "" {
			noAlert()
			return
		}
		answer(p.buttons)
	case clickAlertButton:
		if p.alert == "" || p.pressing == "" {
			fail(http.StatusNotFound, "stale element reference", "The element identified by \"alert-button\" is either not present or it has expired from the internal cache. Try to find it again")
			return
		}
		p.dismissed = append(p.dismissed, p.pressing)
		if p.returns > 0 {
			p.returns--
		} else {
			p.alert, p.buttons = "", nil
		}
		answer(nil)
	default:
		fail(http.StatusNotFound, "unknown command", "Unhandled endpoint: "+call)
	}
}

func writeWDA(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// seen returns the calls the phone got, the settings it was sent and the
// alert buttons tapped.
func (p *phoneWDA) seen() (calls []string, settings []map[string]interface{}, dismissed []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.calls), slices.Clone(p.settings), slices.Clone(p.dismissed)
}

// queries returns the class chains that looked for an alert's button.
func (p *phoneWDA) queries() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.alertQueries)
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

// callsAfterLast returns the calls after the last one that is call.
func callsAfterLast(calls []string, call string) []string {
	for i := len(calls) - 1; i >= 0; i-- {
		if calls[i] == call {
			return calls[i+1:]
		}
	}
	return calls
}

// phoneRun is what running one flow on the phone gave.
type phoneRun struct {
	result  FlowResult
	detail  report.FlowDetail
	printed string
}

// runOnPhone runs a flow of steps on the WDA driver against phone.
func runOnPhone(t *testing.T, phone *phoneWDA, steps ...flow.Step) phoneRun {
	t.Helper()
	u, err := url.Parse(phone.url)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.ParseUint(u.Port(), 10, 16)
	if err != nil {
		t.Fatal(err)
	}
	driver := wda.NewDriver(wda.NewClient(uint16(port)),
		&core.PlatformInfo{Platform: "ios", DeviceID: "phone", ScreenWidth: 390, ScreenHeight: 844}, "")
	out := t.TempDir()
	runner := New(driver, RunnerConfig{
		OutputDir: out,
		Artifacts: ArtifactNever,
		Device:    report.Device{ID: "phone", Platform: "ios"},
	})
	var run phoneRun
	var result *RunResult
	run.printed = captureStdout(t, func() {
		result, err = runner.Run(context.Background(), []flow.Flow{{
			SourcePath: "call.yaml",
			Config:     flow.Config{Name: "call", AppID: "com.example.app"},
			Steps:      steps,
		}})
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	run.result = result.FlowResults[0]
	_, details, err := report.ReadReport(out)
	if err != nil {
		t.Fatalf("ReadReport: %v", err)
	}
	run.detail = details[0]
	if calls, _, _ := phone.seen(); count(calls, "POST /alert/dismiss") > 0 {
		t.Errorf("/alert/dismiss sent %d times: it taps whatever alert is up when it arrives", count(calls, "POST /alert/dismiss"))
	}
	return run
}

// captureStdout returns what run prints, which is where the console output goes.
func captureStdout(t *testing.T, run func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	printed := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		printed <- string(b)
	}()
	stdout := os.Stdout
	os.Stdout = w
	func() {
		defer func() {
			os.Stdout = stdout
			_ = w.Close()
		}()
		run()
	}()
	return <-printed
}

func tapCall() flow.Step {
	return &flow.TapOnStep{BaseStep: flow.BaseStep{StepType: flow.StepTapOn, TimeoutMs: 300}, Selector: flow.Selector{Text: "Call"}}
}

func seeConnected(optional bool) flow.Step {
	return &flow.AssertVisibleStep{
		BaseStep: flow.BaseStep{StepType: flow.StepAssertVisible, TimeoutMs: 300, Optional: optional},
		Selector: flow.Selector{Text: "Connected"},
	}
}

const connectedDesc = `assertVisible: text="Connected"`

// checkCalls are the calls only the system alert check makes.
var checkCalls = []string{"GET /wda/activeAppInfo", "GET /alert/text", "GET /wda/alert/buttons", clickAlertButton}

func TestSystemAlertsOff_ARunSendsWDANothingNew(t *testing.T) {
	phone := newPhoneWDA(t)
	phone.with(func() { phone.alert, phone.buttons = facetimeAlert, []string{"Go to App Store", "Cancel"} })

	run := runOnPhone(t, phone, seeConnected(false))

	if run.result.Status != report.StatusFailed {
		t.Fatalf("status = %v, want failed: nothing dismissed the alert", run.result.Status)
	}
	calls, settings, _ := phone.seen()
	for _, c := range checkCalls {
		if n := count(calls, c); n > 0 {
			t.Errorf("%s sent %d times without MAESTRO_WDA_DISMISS_SYSTEM_ALERTS", c, n)
		}
	}
	for _, s := range settings {
		if _, ok := s["respectSystemAlerts"]; ok {
			t.Errorf("settings %v sent without the switch", s)
		}
		if _, ok := s["defaultActiveApplication"]; ok {
			t.Errorf("settings %v sent without the switch", s)
		}
	}
	if q := phone.queries(); len(q) > 0 {
		t.Errorf("alert buttons queried without the switch: %q", q)
	}
	if strings.Contains(run.result.Error, "system alert") || strings.Contains(run.printed, "system alert") {
		t.Errorf("error %q / output %q mention a system alert without the switch", run.result.Error, run.printed)
	}
	if len(run.detail.SystemAlerts) > 0 {
		t.Errorf("report has system alerts without the switch: %v", run.detail.SystemAlerts)
	}
}

func TestNoSystemAlert_OnlyTheCheckRuns(t *testing.T) {
	t.Setenv("MAESTRO_WDA_DISMISS_SYSTEM_ALERTS", "1")
	phone := newPhoneWDA(t)
	missing := &flow.AssertVisibleStep{
		BaseStep: flow.BaseStep{StepType: flow.StepAssertVisible, TimeoutMs: 300},
		Selector: flow.Selector{Text: "Hang Up"},
	}

	run := runOnPhone(t, phone, missing)

	if run.result.Status != report.StatusFailed || strings.Contains(run.result.Error, "system alert") {
		t.Fatalf("status %v, error %q: want the lookup's own failure", run.result.Status, run.result.Error)
	}
	calls, _, _ := phone.seen()
	// Once as the flow starts, once for the failed lookup.
	if n := count(calls, "GET /alert/text"); n != 2 {
		t.Errorf("the check read the alert %d times, want 2", n)
	}
	if n := count(calls, "GET /wda/alert/buttons") + len(phone.queries()) + count(calls, clickAlertButton); n != 0 {
		t.Errorf("with no alert up, the check read buttons or tapped: %v", calls)
	}
	if after := callsAfterLast(calls, "GET /alert/text"); slices.Contains(after, "GET /source") {
		t.Errorf("the step ran again with no alert dismissed: %v", after)
	}
}

func TestFaceTimeAlert_IsDismissedAndTheStepRunsAgain(t *testing.T) {
	t.Setenv("MAESTRO_WDA_DISMISS_SYSTEM_ALERTS", "1")
	phone := newPhoneWDA(t)
	phone.with(func() { phone.raise, phone.raiseButtons = facetimeAlert, []string{"Go to App Store", "Cancel"} })

	run := runOnPhone(t, phone, tapCall(), seeConnected(false))

	if run.result.Status != report.StatusPassed {
		t.Fatalf("status = %v (%s), want passed once the alert is gone", run.result.Status, run.result.Error)
	}
	calls, settings, dismissed := phone.seen()
	if !slices.Equal(dismissed, []string{"Cancel"}) {
		t.Errorf("tapped %v, want Cancel once", dismissed)
	}
	if after := callsAfterLast(calls, clickAlertButton); !slices.Contains(after, "GET /source") {
		t.Errorf("the step did not run again after the dismissal: %v", after)
	}
	if q, want := phone.queries(), phoneAlertChain("FaceTime App Required", "Cancel"); !slices.Equal(q, []string{want}) {
		t.Errorf("queried %q, want only %q", q, want)
	}
	if last := settings[len(settings)-1]; last["defaultActiveApplication"] != "auto" || last["respectSystemAlerts"] != false {
		t.Errorf("last settings = %v, want the app targeted again", last)
	}
	want := `⚠ dismissed a system alert "FaceTime App Required" with "Cancel"`
	if !strings.Contains(run.printed, want) {
		t.Errorf("output %q does not have %q", run.printed, want)
	}
	if len(run.detail.SystemAlerts) != 1 {
		t.Fatalf("flow report system alerts = %v, want one", run.detail.SystemAlerts)
	}
	got := run.detail.SystemAlerts[0]
	if got.Title != "FaceTime App Required" || got.Button != "Cancel" || got.Step != connectedDesc || got.Time.IsZero() {
		t.Errorf("recorded %+v, want FaceTime App Required, Cancel, for %s", got, connectedDesc)
	}
	if cmds := run.detail.Commands; len(cmds[1].SystemAlerts) != 1 || len(cmds[0].SystemAlerts) != 0 {
		t.Errorf("command system alerts = %v and %v, want the dismissal on the assertVisible only", cmds[0].SystemAlerts, cmds[1].SystemAlerts)
	}
}

func TestDenyListedAlert_IsLeftAloneAndNamedInTheError(t *testing.T) {
	t.Setenv("MAESTRO_WDA_DISMISS_SYSTEM_ALERTS", "1")
	phone := newPhoneWDA(t)
	phone.with(func() { phone.raise, phone.raiseButtons = trustAlert, []string{"Trust", "Don’t Trust"} })

	run := runOnPhone(t, phone, tapCall(), seeConnected(false))

	if run.result.Status != report.StatusFailed {
		t.Fatalf("status = %v, want failed", run.result.Status)
	}
	note := ` (a system alert is covering the app: "Trust This Computer?", buttons: "Trust", "Don’t Trust")`
	if !strings.HasSuffix(run.result.Error, note) {
		t.Errorf("error = %q, want it to end with %q", run.result.Error, note)
	}
	if e := run.detail.Commands[1].Error; e == nil || !strings.HasSuffix(e.Message, note) {
		t.Errorf("command error = %+v, want the note", e)
	}
	if _, _, dismissed := phone.seen(); len(dismissed) > 0 {
		t.Errorf("tapped %v on a deny-listed alert", dismissed)
	}
	if len(run.detail.SystemAlerts) > 0 || strings.Contains(run.printed, "dismissed a system alert") {
		t.Errorf("a dismissal was reported for an alert left alone: %v", run.detail.SystemAlerts)
	}
}

func TestAlertWithoutAClosingButton_IsLeftAlone(t *testing.T) {
	t.Setenv("MAESTRO_WDA_DISMISS_SYSTEM_ALERTS", "1")
	phone := newPhoneWDA(t)
	phone.with(func() { phone.raise, phone.raiseButtons = facetimeAlert, []string{"Go to App Store", "Open"} })

	run := runOnPhone(t, phone, tapCall(), seeConnected(false))

	note := ` (a system alert is covering the app: "FaceTime App Required", buttons: "Go to App Store", "Open")`
	if run.result.Status != report.StatusFailed || !strings.HasSuffix(run.result.Error, note) {
		t.Errorf("status %v, error %q; want failed, ending with %q", run.result.Status, run.result.Error, note)
	}
	if _, _, dismissed := phone.seen(); len(dismissed) > 0 {
		t.Errorf("tapped %v, which does more than close the alert", dismissed)
	}
}

func TestAlertUpAsTheFlowStarts_IsDismissedBeforeTheFirstStep(t *testing.T) {
	t.Setenv("MAESTRO_WDA_DISMISS_SYSTEM_ALERTS", "1")
	phone := newPhoneWDA(t)
	phone.with(func() { phone.alert, phone.buttons = facetimeAlert, []string{"Go to App Store", "Cancel"} })

	run := runOnPhone(t, phone, seeConnected(false))

	if run.result.Status != report.StatusPassed {
		t.Fatalf("status = %v (%s), want passed", run.result.Status, run.result.Error)
	}
	calls, _, dismissed := phone.seen()
	if !slices.Equal(dismissed, []string{"Cancel"}) {
		t.Errorf("tapped %v, want Cancel once", dismissed)
	}
	if first := slices.Index(calls, "GET /source"); first < slices.Index(calls, clickAlertButton) {
		t.Errorf("the first step looked at the screen before the alert was dismissed: %v", calls)
	}
	if len(run.detail.SystemAlerts) != 1 || run.detail.SystemAlerts[0].Step != "" {
		t.Errorf("flow report system alerts = %+v, want one from the flow's start", run.detail.SystemAlerts)
	}
	if len(run.detail.Commands[0].SystemAlerts) > 0 {
		t.Errorf("the flow-start dismissal is on a command: %v", run.detail.Commands[0].SystemAlerts)
	}
}

func TestSystemAlertDismissals_StopAtThreeAFlow(t *testing.T) {
	t.Setenv("MAESTRO_WDA_DISMISS_SYSTEM_ALERTS", "1")
	phone := newPhoneWDA(t)
	// An alert that comes straight back every time it is dismissed.
	phone.with(func() {
		phone.alert, phone.buttons = facetimeAlert, []string{"Cancel"}
		phone.returns = 100
	})

	run := runOnPhone(t, phone, seeConnected(true), seeConnected(true), seeConnected(true), seeConnected(true))

	calls, _, dismissed := phone.seen()
	if len(dismissed) != maxSystemAlertDismissals {
		t.Errorf("dismissed %d times, want %d", len(dismissed), maxSystemAlertDismissals)
	}
	if n := count(calls, "GET /wda/activeAppInfo"); n != maxSystemAlertDismissals {
		t.Errorf("looked %d times, want no look after the %dth dismissal", n, maxSystemAlertDismissals)
	}
	if len(run.detail.SystemAlerts) != maxSystemAlertDismissals {
		t.Errorf("flow report has %d system alerts, want %d", len(run.detail.SystemAlerts), maxSystemAlertDismissals)
	}
	if strings.Count(run.printed, "dismissed a system alert") != maxSystemAlertDismissals {
		t.Errorf("output %q, want %d warnings", run.printed, maxSystemAlertDismissals)
	}
}

func TestAppTargeting_IsRestoredWhenTheCheckFails(t *testing.T) {
	t.Setenv("MAESTRO_WDA_DISMISS_SYSTEM_ALERTS", "1")
	phone := newPhoneWDA(t)
	phone.with(func() {
		phone.raise, phone.raiseButtons = facetimeAlert, []string{"Cancel"}
		phone.failText = true
	})

	run := runOnPhone(t, phone, tapCall(), seeConnected(false))

	if run.result.Status != report.StatusFailed || strings.Contains(run.result.Error, "system alert") {
		t.Errorf("status %v, error %q: want the lookup's own failure", run.result.Status, run.result.Error)
	}
	calls, settings, dismissed := phone.seen()
	if len(dismissed) > 0 {
		t.Errorf("tapped %v on an alert that could not be read", dismissed)
	}
	if last := settings[len(settings)-1]; last["defaultActiveApplication"] != "auto" || last["respectSystemAlerts"] != false {
		t.Errorf("last settings = %v, want the app targeted again", last)
	}
	if after := callsAfterLast(calls, "GET /alert/text"); !slices.Contains(after, "POST /appium/settings") {
		t.Errorf("no settings call after the failed read: %v", after)
	}
}

func TestNestedStep_RunsAgainAfterADismissedAlert(t *testing.T) {
	t.Setenv("MAESTRO_WDA_DISMISS_SYSTEM_ALERTS", "1")
	phone := newPhoneWDA(t)
	phone.with(func() { phone.raise, phone.raiseButtons = facetimeAlert, []string{"Cancel"} })

	run := runOnPhone(t, phone, &flow.RunFlowStep{
		BaseStep: flow.BaseStep{StepType: flow.StepRunFlow},
		Steps:    []flow.Step{tapCall(), seeConnected(false)},
	})

	if run.result.Status != report.StatusPassed {
		t.Fatalf("status = %v (%s), want passed once the alert is gone", run.result.Status, run.result.Error)
	}
	runFlow := run.detail.Commands[0]
	if len(runFlow.SubCommands) != 2 || len(runFlow.SubCommands[1].SystemAlerts) != 1 {
		t.Fatalf("sub-commands = %+v, want the dismissal on the assertVisible", runFlow.SubCommands)
	}
	if len(runFlow.SystemAlerts) > 0 {
		t.Errorf("the runFlow command also has the dismissal: %v", runFlow.SystemAlerts)
	}
	if len(run.detail.SystemAlerts) != 1 || run.detail.SystemAlerts[0].Step != connectedDesc {
		t.Errorf("flow report system alerts = %+v, want the one for %s", run.detail.SystemAlerts, connectedDesc)
	}
}

func TestNestedStep_ErrorNamesAnAlertLeftAlone(t *testing.T) {
	t.Setenv("MAESTRO_WDA_DISMISS_SYSTEM_ALERTS", "1")
	phone := newPhoneWDA(t)
	phone.with(func() { phone.raise, phone.raiseButtons = trustAlert, []string{"Trust", "Don’t Trust"} })

	run := runOnPhone(t, phone, &flow.RunFlowStep{
		BaseStep: flow.BaseStep{StepType: flow.StepRunFlow},
		Steps:    []flow.Step{tapCall(), seeConnected(false)},
	})

	note := `(a system alert is covering the app: "Trust This Computer?", buttons: "Trust", "Don’t Trust")`
	sub := run.detail.Commands[0].SubCommands
	if len(sub) != 2 || sub[1].Error == nil || !strings.HasSuffix(sub[1].Error.Message, note) {
		t.Errorf("sub-commands = %+v, want the assertVisible's error to end with %q", sub, note)
	}
	if !strings.Contains(run.result.Error, note) {
		t.Errorf("flow error = %q, want the note carried up", run.result.Error)
	}
}

// A lookup that failed because its runFlow ran out of time was not covered
// by anything, and a stopped run is not to do more on the device.
func TestRunFlowOutOfTime_StartsNoCheck(t *testing.T) {
	t.Setenv("MAESTRO_WDA_DISMISS_SYSTEM_ALERTS", "1")
	phone := newPhoneWDA(t)
	phone.with(func() { phone.raise, phone.raiseButtons = facetimeAlert, []string{"Cancel"} })

	run := runOnPhone(t, phone, &flow.RunFlowStep{
		BaseStep: flow.BaseStep{StepType: flow.StepRunFlow, TimeoutMs: 150},
		Steps:    []flow.Step{tapCall(), seeConnected(false)},
	})

	if run.result.Status != report.StatusFailed {
		t.Fatalf("status = %v, want failed", run.result.Status)
	}
	calls, _, dismissed := phone.seen()
	if n := count(calls, "GET /wda/activeAppInfo"); n != 1 || len(dismissed) > 0 {
		t.Errorf("looked %d times and tapped %v; want only the flow-start look", n, dismissed)
	}
}

func TestFailedLookup(t *testing.T) {
	failed := func(msg string, err string) *core.CommandResult {
		return &core.CommandResult{Success: false, Message: msg, Error: &testError{msg: err}}
	}
	sel := flow.Selector{Text: "Connected"}
	cases := []struct {
		name   string
		step   flow.Step
		result *core.CommandResult
		want   bool
	}{
		{"tapOn not found", &flow.TapOnStep{Selector: sel}, failed(`Element not found: text="Connected"`, "element not found via WDA"), true},
		{"assertVisible not visible", &flow.AssertVisibleStep{Selector: sel}, failed(`Element not visible: text="Connected"`, "context deadline exceeded"), true},
		{"extendedWaitUntil visible", &flow.WaitUntilStep{Visible: &sel}, failed(`Element 'text="Connected"' not visible within 17s`, "context deadline exceeded"), true},
		{"scrollUntilVisible", &flow.ScrollUntilVisibleStep{Element: sel}, failed(`Element not found: text="Connected"`, "element not found after scrolling"), true},
		{"inputText into a target", &flow.InputTextStep{Text: "hi", Selector: sel}, failed(`Element not found: text="Connected"`, "element not found via WDA"), true},
		{"copyTextFrom", &flow.CopyTextFromStep{Selector: sel}, failed(`Element not found: text="Connected"`, "element not found via WDA"), true},
		{"said only by the error", &flow.TapOnStep{Selector: sel}, failed("", "element exists but is not visible on screen (bounds outside viewport)"), true},
		{"passed", &flow.TapOnStep{Selector: sel}, &core.CommandResult{Success: true, Message: "Element not found, skipped"}, false},
		{"another failure", &flow.TapOnStep{Selector: sel}, failed("Tap failed", "connection refused"), false},
		{"assertNotVisible", &flow.AssertNotVisibleStep{Selector: sel}, failed(`Could not check that text="Connected" is not visible: boom`, "boom"), false},
		{"extendedWaitUntil notVisible", &flow.WaitUntilStep{NotVisible: &sel}, failed(`Could not check that 'text="Connected"' is not visible within 5s: boom`, "boom"), false},
		{"runFlow", &flow.RunFlowStep{}, failed(`Element not found: text="Connected"`, "element not found via WDA"), false},
		{"retry", &flow.RetryStep{}, failed("Retry failed after 2 attempts", "element not found via WDA"), false},
		{"repeat", &flow.RepeatStep{}, failed(`Element not found: text="Connected"`, "element not found via WDA"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := failedLookup(c.step, c.result); got != c.want {
				t.Errorf("failedLookup = %v, want %v", got, c.want)
			}
		})
	}
}

func TestSystemAlertNote(t *testing.T) {
	cases := []struct {
		alert *core.SystemAlert
		want  string
	}{
		{nil, ""},
		{&core.SystemAlert{Title: "FaceTime App Required", Buttons: []string{"Cancel"}, Dismissed: "Cancel"}, ""},
		{&core.SystemAlert{Title: "Trust This Computer?", Buttons: []string{"Trust", "Don’t Trust"}},
			` (a system alert is covering the app: "Trust This Computer?", buttons: "Trust", "Don’t Trust")`},
		{&core.SystemAlert{Title: "No SIM"}, ` (a system alert is covering the app: "No SIM")`},
	}
	for _, c := range cases {
		if got := systemAlertNote(c.alert); got != c.want {
			t.Errorf("systemAlertNote(%+v) = %q, want %q", c.alert, got, c.want)
		}
	}
}
