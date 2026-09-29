package wda

import (
	"context"
	"fmt"
	"os/exec"
	"sync"
	"time"

	goios "github.com/danielpaulus/go-ios/ios"
	"github.com/danielpaulus/go-ios/ios/installationproxy"
	"github.com/danielpaulus/go-ios/ios/zipconduit"
)

// A real device's app is removed and put back with xcrun devicectl on a Mac. A host without
// Xcode, such as the Linux machine a phone is plugged into, has no devicectl, and go-ios does the
// same over usbmuxd: the installation proxy uninstalls and the zip conduit installs. Both are
// lockdown services, so neither needs the iOS 17+ tunnel. They are variables so tests can
// replace them.
var (
	devicectlAvailable = hasDevicectl
	devicectlRun       = func(args ...string) ([]byte, error) {
		return exec.Command("xcrun", append([]string{"devicectl"}, args...)...).CombinedOutput()
	}
	goiosUninstall = uninstallWithGoIOS
	goiosInstall   = installWithGoIOS
)

var (
	devicectlOnce sync.Once
	devicectlOK   bool
)

// hasDevicectl reports, once per process, whether `xcrun devicectl` works on this host (macOS
// 14 with Xcode 15 or later).
func hasDevicectl() bool {
	devicectlOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		devicectlOK = exec.CommandContext(ctx, "xcrun", "devicectl", "--version").Run() == nil
	})
	return devicectlOK
}

// goiosAppTimeout bounds a go-ios uninstall or install: the install service can take the
// connection and never answer (seen on iOS 26), and a clearState that hangs hangs its flow.
const goiosAppTimeout = 5 * time.Minute

func uninstallWithGoIOS(udid, bundleID string) error {
	entry, err := goios.GetDevice(udid)
	if err != nil {
		return fmt.Errorf("device %s not found: %w", udid, err)
	}
	conn, err := installationproxy.New(entry)
	if err != nil {
		return fmt.Errorf("connect to the installation proxy: %w", err)
	}
	defer conn.Close()
	return withAppTimeout(func() error { return conn.Uninstall(bundleID) })
}

func installWithGoIOS(udid, appFile string) error {
	entry, err := goios.GetDevice(udid)
	if err != nil {
		return fmt.Errorf("device %s not found: %w", udid, err)
	}
	conn, err := zipconduit.New(entry)
	if err != nil {
		return fmt.Errorf("connect to the install service: %w", err)
	}
	return withAppTimeout(func() error { return conn.SendFile(appFile) })
}

func withAppTimeout(f func() error) error {
	done := make(chan error, 1)
	go func() { done <- f() }()
	select {
	case err := <-done:
		return err
	case <-time.After(goiosAppTimeout):
		return fmt.Errorf("timed out after %v", goiosAppTimeout)
	}
}
