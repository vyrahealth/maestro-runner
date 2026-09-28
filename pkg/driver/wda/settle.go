package wda

import (
	"bytes"
	"time"

	"github.com/devicelab-dev/maestro-runner/pkg/logger"
)

// WaitUntilScreenIsStatic waits up to timeoutMs for two screenshots in a row
// to be the same, and reports whether they were. It is how Maestro waits for
// the screen on iOS (IOSDriver.kt:490-507): its runner compares the SHA-256 of
// two screenshots (ScreenDiffHandler.swift:16-21), so any difference at all is
// a screen still changing, and the bytes are compared here the same way. Each
// screenshot is compared with the one before it, so a still screen costs two.
// A screenshot that fails ends the wait, since nothing more can be learned.
func (d *Driver) WaitUntilScreenIsStatic(timeoutMs int) bool {
	deadline := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
	prev, err := d.client.Screenshot()
	for err == nil && time.Now().Before(deadline) {
		var next []byte
		next, err = d.client.Screenshot()
		if err == nil && len(next) > 0 && bytes.Equal(prev, next) {
			return true
		}
		prev = next
	}
	if err != nil {
		logger.Debug("[wda] screen settle: screenshot failed: %v", err)
	}
	return false
}
