package wda

import (
	"fmt"
	"os"
	"strings"

	"github.com/devicelab-dev/maestro-runner/pkg/core"
	"github.com/devicelab-dev/maestro-runner/pkg/logger"
)

// dismissSystemAlerts reports whether MAESTRO_WDA_DISMISS_SYSTEM_ALERTS is set.
//
// An alert iOS shows over the app, such as "FaceTime App Required" when the
// app opens a link to a deleted app, is SpringBoard's. Lookups read only the
// app's view tree, so the runner never sees it, and it takes every touch: the
// taps land on the alert, the app never moves on, and a later lookup fails. It
// stays up after the flow and takes the next flows' touches too.
//
// With the switch, the executor asks the driver to look for such an alert at
// the start of each flow and when a step fails to find its element, and at no
// other time. The driver taps only a button that just closes the alert
// (systemAlertButtons), and never one on an alert about trust, passwords,
// payments and the like (systemAlertRefusals). WDA's alert monitor cannot be
// given this job: it acts on the app's own alerts too, and would tap Cancel on
// the app's confirmation dialogs, racing the flow.
func dismissSystemAlerts() bool {
	return os.Getenv("MAESTRO_WDA_DISMISS_SYSTEM_ALERTS") != ""
}

// systemAlertButtons are the only buttons a system alert is dismissed with,
// in the order they are preferred. Each closes the alert and decides nothing,
// so tapping it changes nothing on the device or in an account. A button
// counts only when its whole label is one of these, ignoring case: "Cancel
// Subscription" is not "Cancel".
var systemAlertButtons = []string{"Cancel", "Not Now", "Close", "Later", "Dismiss", "Remind Me Later", "Ignore"}

// systemAlertRefusals leave a system alert alone, whatever its buttons, when
// its title or message contains one of them, ignoring case. On such an alert
// even Cancel is a decision for a person: trusting a computer, a password or
// Apple Account prompt, a payment or purchase, deleting or erasing, an update
// or an install. "allow" keeps the permission prompts ("Allow “Vyra” to use
// your location?") out: they belong to the permission handling (WDA's alert
// monitor, MAESTRO_WDA_PERMISSION_ALERTS_ONLY), which knows whether the flow
// grants them.
var systemAlertRefusals = []string{
	"trust", "password", "passcode", "apple id", "apple account", "sign in", "allow",
	"pay", "purchase", "buy", "subscribe", "delete", "erase", "update", "install",
}

// systemAlertButton is the button to dismiss a system alert with, given its
// text and its buttons' labels, or "" and why it is left alone. The label is
// returned as the alert has it, since WDA taps the button whose label is
// exactly that.
func systemAlertButton(text string, buttons []string) (string, string) {
	lower := strings.ToLower(text)
	for _, word := range systemAlertRefusals {
		if strings.Contains(lower, word) {
			return "", fmt.Sprintf("its text has %q", word)
		}
	}
	for _, want := range systemAlertButtons {
		for _, label := range buttons {
			if strings.EqualFold(strings.TrimSpace(label), want) {
				return label, ""
			}
		}
	}
	return "", "none of its buttons only closes it"
}

// springBoardBundleID is SpringBoard, the process that shows the alerts no
// app owns.
const springBoardBundleID = "com.apple.springboard"

// DismissSystemAlert implements core.SystemAlertDismisser. Without
// MAESTRO_WDA_DISMISS_SYSTEM_ALERTS, or without a session, it returns nil and
// sends nothing to WDA.
//
// WDA's alert calls find the alert in SpringBoard first and then in the
// session's active application when that is another app (FBAlert.m,
// alertElementFromApplication:). So while the session reads the app under
// test, an in-app dialog is reported as the alert whenever SpringBoard shows
// none. For the look, the session reads SpringBoard instead: its active
// application (FBSession.m, activeApplication) is defaultActiveApplication's
// app when that app is in the foreground, and with respectSystemAlerts it is
// SpringBoard while SpringBoard shows an alert over the app under test. No
// alert is read unless /wda/activeAppInfo then names SpringBoard, since one
// found otherwise could be the app's own. The tap goes to the alert the
// decision was made on (tapSystemAlertButton). The session goes back to the
// app under test afterwards, even when the look fails.
func (d *Driver) DismissSystemAlert() (*core.SystemAlert, error) {
	if !dismissSystemAlerts() || !d.client.HasSession() {
		return nil, nil
	}

	// Deferred before the settings are sent: a request that fails may still
	// have reached WDA.
	defer d.restoreAppTargeting()
	if err := d.client.UpdateSettings(map[string]interface{}{
		"defaultActiveApplication": springBoardBundleID,
		"respectSystemAlerts":      true,
	}); err != nil {
		return nil, fmt.Errorf("could not point WDA at SpringBoard: %w", err)
	}
	app, err := d.client.ActiveAppBundleID()
	if err != nil {
		return nil, fmt.Errorf("could not read which app WDA reads: %w", err)
	}
	if app != springBoardBundleID {
		// SpringBoard shows no alert over the app under test, and is not
		// reported in the foreground either.
		logger.Debug("system alert check: WDA reads %s, not SpringBoard, so it sees no system alert", app)
		return nil, nil
	}

	text, err := d.client.AlertText()
	if isNoAlert(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("could not read the system alert: %w", err)
	}
	alert := &core.SystemAlert{Title: firstLine(text), Text: text}
	alert.Buttons, err = d.client.AlertButtons()
	if isNoAlert(err) {
		return nil, nil // it went away between the two reads
	}
	if err != nil {
		return alert, fmt.Errorf("could not read the system alert's buttons: %w", err)
	}

	button, why := systemAlertButton(text, alert.Buttons)
	if button == "" {
		logger.Info("system alert %q (buttons %q) left alone: %s", alert.Title, alert.Buttons, why)
		return alert, nil
	}
	return d.tapSystemAlertButton(alert, button)
}

// tapSystemAlertButton dismisses alert with button, tapping that alert's
// button and no other. WDA works out the active application again for every
// call, and /alert/dismiss taps whichever alert is up when it arrives: had
// SpringBoard's alert closed by then, it could tap Cancel on the app's own
// dialog. So the alert is read again and must still have the title the
// decision was made on, one class chain query then finds the button on an
// alert of that title (alertButtonChain), exactly one must match, and the tap
// is a click on that element. What binds the tap is the title and the
// button's label, looked up in the app WDA reads, which the check has
// confirmed is SpringBoard: an app's own alert could match only by having
// that same title and button. A found element stays bound to the application
// it was found in, and a click first resolves it there (FBElementCache.m,
// elementForUUID:checkStaleness:), so if the alert is gone by then the click
// fails as a stale element and taps nothing.
//
// It returns nil when the alert closed or changed before the tap, and the
// alert with an empty Dismissed when the button could not be told apart.
func (d *Driver) tapSystemAlertButton(alert *core.SystemAlert, button string) (*core.SystemAlert, error) {
	text, err := d.client.AlertText()
	if isNoAlert(err) {
		logger.Debug("system alert %q closed before it could be dismissed", alert.Title)
		return nil, nil
	}
	if err != nil {
		return alert, fmt.Errorf("could not read the system alert again: %w", err)
	}
	if now := firstLine(text); now != alert.Title {
		logger.Debug("system alert %q changed to %q before it could be dismissed", alert.Title, now)
		return nil, nil
	}

	chain := alertButtonChain(alert.Title, button)
	ids, err := d.client.FindElements("class chain", chain)
	if err != nil {
		return alert, fmt.Errorf("could not look for %q on the system alert: %w", button, err)
	}
	if len(ids) != 1 {
		logger.Debug("system alert %q: %d elements match %s, so nothing is tapped", alert.Title, len(ids), chain)
		return alert, nil
	}
	if err := d.client.ElementClick(ids[0]); err != nil {
		return alert, fmt.Errorf("could not tap %q on the system alert: %w", button, err)
	}
	alert.Dismissed = button
	return alert, nil
}

// alertButtonChain is the class chain for the button labelled button on the
// alert titled title: WDA matches the alert by its label or name and the
// button by its label, each exactly.
func alertButtonChain(title, button string) string {
	return fmt.Sprintf("**/XCUIElementTypeAlert[`label == %s OR name == %s`]/**/XCUIElementTypeButton[`label == %s`]",
		chainString(title), chainString(title), chainString(button))
}

// chainString quotes s for a predicate inside a class chain. The predicate is
// an NSPredicate, whose double-quoted strings take a backslash before a
// backslash or a double quote, and WDA's parser takes two backticks inside a
// predicate for one (FBClassChainQueryParser.m).
func chainString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "`", "``")
	return `"` + s + `"`
}

// restoreAppTargeting points the session back at the app under test, as the
// runner set it up: at MAESTRO_WDA_DEFAULT_ACTIVE_APP's app when that is set
// (CreateSession), at WDA's own choice ("auto", FBDefaultApplicationAuto)
// otherwise, and with respectSystemAlerts off, WDA's default, which nothing
// else here turns on. It is sent a second time when the first fails, since
// until it lands every lookup could read SpringBoard.
func (d *Driver) restoreAppTargeting() {
	app := os.Getenv("MAESTRO_WDA_DEFAULT_ACTIVE_APP")
	if app == "" {
		app = "auto"
	}
	settings := map[string]interface{}{
		"defaultActiveApplication": app,
		"respectSystemAlerts":      false,
	}
	err := d.client.UpdateSettings(settings)
	if err != nil {
		err = d.client.UpdateSettings(settings)
	}
	if err != nil {
		logger.Warn("system alert check: could not point WDA back at the app under test, so lookups may read SpringBoard: %v", err)
	}
}

// isNoAlert reports whether err is WDA saying that no alert is open: W3C's
// "no such alert", whose message WDA words as "An attempt was made to
// operate on a modal dialog when one was not open" (FBCommandStatus.m).
func isNoAlert(err error) bool {
	if err == nil {
		return false
	}
	m := err.Error()
	return strings.Contains(m, "no such alert") || strings.Contains(m, "when one was not open")
}

// firstLine is an alert's title: WDA gives its texts a line each, title first.
func firstLine(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	return strings.TrimSpace(line)
}
