package wda

import "os"

// Lookup timeouts with MAESTRO_PARITY_TIMEOUTS set: Maestro's own. Maestro
// deducts the time since the last command from them (Orchestra.kt:1767-1770);
// that is not ported.
const (
	// maestroFindTimeout is Maestro's wait for a required element
	// (Orchestra.kt:136). Its wait for an optional one (Orchestra.kt:137) is
	// OptionalFindTimeout's 7 seconds already.
	maestroFindTimeout = 17000
	// notVisibleTimeout is assertNotVisible's own default without the switch.
	notVisibleTimeout = 5000
)

// parityTimeouts reports whether lookups wait as long by default as
// Maestro's do (MAESTRO_PARITY_TIMEOUTS).
func parityTimeouts() bool {
	return os.Getenv("MAESTRO_PARITY_TIMEOUTS") != ""
}

// requiredFindTimeoutMs is the default wait for a required element.
func requiredFindTimeoutMs() int {
	if parityTimeouts() {
		return maestroFindTimeout
	}
	return DefaultFindTimeout
}

// notVisibleTimeoutMs is how long assertNotVisible waits by default for an
// element to go. Maestro gives it the full lookup timeout (Orchestra.kt:514,
// 1067).
func notVisibleTimeoutMs() int {
	if parityTimeouts() {
		return maestroFindTimeout
	}
	return notVisibleTimeout
}

// assertionOptional is the optional flag an assertion's lookup uses for its
// timeout. Maestro waits the full lookup timeout for an assertion, optional
// or not (Orchestra.kt:513-523, with no timeout set on the command).
func assertionOptional(optional bool) bool {
	return optional && !parityTimeouts()
}
