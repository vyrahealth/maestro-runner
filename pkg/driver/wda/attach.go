package wda

import (
	"fmt"
	"os"
	"time"
)

// AttachExisting reports whether MAESTRO_WDA_ATTACH is set: a WebDriverAgent already runs on
// the device, started by another tool (go-ios on the machine the phone is plugged into, for
// one), and 127.0.0.1:<port> reaches it. The CLI then installs, builds, starts and stops no WDA,
// and uses the running one as it is. Without Xcode on the host, this is how a run reaches a
// physical iPhone at all: the runner starts WDA only through xcodebuild.
func AttachExisting() bool {
	return os.Getenv("MAESTRO_WDA_ATTACH") != ""
}

// AttachTimeout is how long an attaching run waits for the running WDA to answer.
const AttachTimeout = 30 * time.Second

// attachPollInterval is how often WaitForRunning asks; a variable so tests can shorten it.
var attachPollInterval = time.Second

// WaitForRunning waits until the WDA behind c answers /status, or timeout passes. A WDA that
// was just started takes a few seconds to listen, so one failed probe is not an answer.
func WaitForRunning(c *Client, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		_, err := c.Status()
		if err == nil {
			return nil
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("no WDA answers at %s after %v: %w", c.baseURL, timeout, err)
		}
		time.Sleep(attachPollInterval)
	}
}
