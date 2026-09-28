package wda

import "os"

// permissionAlertsOnly reports whether MAESTRO_WDA_PERMISSION_ALERTS_ONLY is set.
//
// Without it, a launchApp that allows (or denies) every permission turns on
// WebDriverAgent's defaultAlertAction. WDA's alert monitor then checks every
// active app every two seconds, the app under test included, and taps a
// matching button on ANY alert it finds, or the alert's last button when none
// matches. So an in-app "Delete?" dialog gets its Delete tapped, and an "Open
// in <app>?" prompt its Open, racing the flow's own tap. Maestro never taps an
// in-app alert: on the Simulator it grants permissions before the launch, and
// its iOS runner taps only the notification prompt (SystemPermissionHelper).
//
// With the switch, the monitor is driven by WDA's autoClickAlertSelector
// instead, which taps only a button matching the selector and never falls back
// to another one: the permission prompts' Allow (or Don't Allow) buttons. OK
// stays in the allow selector because some permission prompts use it, so an
// in-app alert whose button is OK is still tapped. Every other alert is left to
// the flow, as in Maestro.
func permissionAlertsOnly() bool {
	return os.Getenv("MAESTRO_WDA_PERMISSION_ALERTS_ONLY") != ""
}

// permissionAlertSelector is the autoClickAlertSelector for an alert action:
// the button a permission prompt uses to allow ("accept") or to refuse
// ("dismiss"), and "" (monitor off) for anything else. iOS writes "Don’t
// Allow" with a typographic apostrophe, so the refusal matches on its start.
func permissionAlertSelector(alertAction string) string {
	switch alertAction {
	case "accept":
		return "**/XCUIElementTypeButton[`label BEGINSWITH[c] 'Allow' OR label ==[c] 'OK'`]"
	case "dismiss":
		return "**/XCUIElementTypeButton[`label BEGINSWITH[c] 'Don'`]"
	}
	return ""
}
