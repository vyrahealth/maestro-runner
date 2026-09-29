package cli

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	wdadriver "github.com/devicelab-dev/maestro-runner/pkg/driver/wda"
)

// attachUDID is a made-up UDID; the CLI derives the WDA port from it.
const attachUDID = "00000000-0000000000A77AC4"

func fakeDeviceInfo(t *testing.T) {
	t.Helper()
	old := getIOSDeviceInfoFn
	getIOSDeviceInfoFn = func(string) (*iosDeviceInfo, error) {
		return &iosDeviceInfo{Name: "iPhone", OSVersion: "27.0"}, nil
	}
	t.Cleanup(func() { getIOSDeviceInfoFn = old })
}

// With MAESTRO_WDA_ATTACH the CLI uses the WDA already answering on the device's port: it does not
// read the bound port as "device in use", builds and starts nothing (xcodebuild is not on a CI
// machine, so a build would fail the call), and its cleanup is safe with no runner.
func TestCreateIOSDriverAttachesToTheWDAOnTheDevicePort(t *testing.T) {
	t.Setenv("MAESTRO_WDA_ATTACH", "1")
	fakeDeviceInfo(t)
	port := wdadriver.PortFromUDID(attachUDID)
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Skipf("port %d is taken here: %v", port, err)
	}
	var mu sync.Mutex
	var asked []string
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked = append(asked, r.URL.Path)
		mu.Unlock()
		if r.URL.Path == "/status" {
			_, _ = w.Write([]byte(`{"value":{"ready":true},"sessionId":null}`))
			return
		}
		_, _ = w.Write([]byte(`{"value":{"width":414,"height":896}}`))
	}), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	driver, cleanup, err := CreateIOSDriver(&RunConfig{Devices: []string{attachUDID}, NoAppInstall: true, AppID: "ai.example.app"})
	if err != nil {
		t.Fatalf("CreateIOSDriver: %v", err)
	}
	if driver == nil || cleanup == nil {
		t.Fatal("no driver or no cleanup")
	}
	cleanup() // no runner was started, so there is nothing to stop, and nothing may panic

	mu.Lock()
	defer mu.Unlock()
	if len(asked) == 0 || asked[0] != "/status" {
		t.Fatalf("the WDA on port %d was asked %q, want /status first", port, asked)
	}
}

func TestCreateIOSDriverAttachFailsWhenNoWDAAnswers(t *testing.T) {
	t.Setenv("MAESTRO_WDA_ATTACH", "1")
	fakeDeviceInfo(t)
	old := wdadriver.AttachTimeout
	wdadriver.AttachTimeout = 200 * time.Millisecond
	t.Cleanup(func() { wdadriver.AttachTimeout = old })
	port := wdadriver.PortFromUDID(attachUDID)
	if ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port)); err != nil {
		t.Skipf("port %d is taken here: %v", port, err)
	} else {
		_ = ln.Close() // nothing listens there now
	}

	_, _, err := CreateIOSDriver(&RunConfig{Devices: []string{attachUDID}, NoAppInstall: true, AppID: "ai.example.app"})
	if err == nil {
		t.Fatal("CreateIOSDriver attached with no WDA answering")
	}
	if !strings.Contains(err.Error(), "MAESTRO_WDA_ATTACH") || !strings.Contains(err.Error(), fmt.Sprint(port)) {
		t.Fatalf("the error does not say the attach found no WDA on port %d: %v", port, err)
	}
}
