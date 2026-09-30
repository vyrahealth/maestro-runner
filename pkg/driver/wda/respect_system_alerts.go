package wda

import "os"

// respectSystemAlerts reports whether the session reads SpringBoard while SpringBoard shows an
// alert over the app under test (MAESTRO_WDA_RESPECT_SYSTEM_ALERTS), so that lookups see the
// alert the way Maestro's do.
//
// WebDriverAgent reads one application at a time. Without the switch that is the app under
// test, and a permission prompt the app raises, such as "“App” Would Like to Send You
// Notifications", is drawn by SpringBoard and is in no page source and no query: a flow that
// answers the prompt itself waits for its text until it times out, with the prompt on the
// screen. With it, the session is created with WDA's respectSystemAlerts setting on. WDA then
// reads SpringBoard whenever the app under test is in the foreground and SpringBoard shows an
// alert (FBSession.m, activeApplication), and the app again once the alert has gone. A
// defaultActiveApplication in the foreground (MAESTRO_WDA_DEFAULT_ACTIVE_APP) still comes first.
//
// While such an alert is up, a lookup for one of the app's own elements fails as it would with
// the alert covering them. So the switch is for flows that answer the prompts themselves; a
// launchApp that grants permissions has WDA's alert monitor answer them instead.
func respectSystemAlerts() bool {
	return os.Getenv("MAESTRO_WDA_RESPECT_SYSTEM_ALERTS") != ""
}
