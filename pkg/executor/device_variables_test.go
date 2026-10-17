package executor

import (
	"context"
	"sync"
	"testing"

	"github.com/devicelab-dev/maestro-runner/pkg/core"
	"github.com/devicelab-dev/maestro-runner/pkg/flow"
	"github.com/devicelab-dev/maestro-runner/pkg/report"
)

// Every flow gets DEVICE_UDID, the device it runs on (#187): the worker's own
// device under --parallel, else the run's device, else the driver's. A value
// the user set is kept.
func TestSetDeviceVariables(t *testing.T) {
	newRunner := func(cfg RunnerConfig) *FlowRunner {
		fr := &FlowRunner{
			script: NewScriptEngine(),
			driver: &mockDriver{platformFunc: func() *core.PlatformInfo {
				return &core.PlatformInfo{Platform: "android", DeviceID: "from-driver"}
			}},
			config: cfg,
			flow:   flow.Flow{SourcePath: "/flows/login.yaml"},
		}
		fr.script.SetVariables(cfg.Env)
		return fr
	}
	for _, tc := range []struct {
		name string
		cfg  RunnerConfig
		want string
	}{
		{"worker's own device", RunnerConfig{DeviceInfo: &report.Device{ID: "emulator-5556"}, Device: report.Device{ID: "emulator-5554"}}, "emulator-5556"},
		{"run's device", RunnerConfig{Device: report.Device{ID: "11171JEC200939"}}, "11171JEC200939"},
		{"driver's device", RunnerConfig{}, "from-driver"},
		{"user's value kept", RunnerConfig{Device: report.Device{ID: "emulator-5554"}, Env: map[string]string{"DEVICE_UDID": "mine"}}, "mine"},
	} {
		fr := newRunner(tc.cfg)
		fr.setDeviceVariables()
		if got := fr.script.GetVariable("DEVICE_UDID"); got != tc.want {
			t.Errorf("%s: DEVICE_UDID = %q, want %q", tc.name, got, tc.want)
		}
		fr.script.Close()
	}
}

// Under --parallel each device keeps its own DEVICE_UDID for every flow it
// runs, so a per-device launch argument never mixes devices (#187).
func TestParallelWorkersGetTheirOwnDeviceUDID(t *testing.T) {
	var mu sync.Mutex
	seen := map[string][]string{}
	driverFor := func(dev string) *mockDriver {
		return &mockDriver{
			platformFunc: func() *core.PlatformInfo { return &core.PlatformInfo{Platform: "android", DeviceID: dev} },
			executeFunc: func(step flow.Step) *core.CommandResult {
				if s, ok := step.(*flow.InputTextStep); ok {
					mu.Lock()
					seen[dev] = append(seen[dev], s.Text)
					mu.Unlock()
				}
				return &core.CommandResult{Success: true}
			},
		}
	}
	var flows []flow.Flow
	for i := 0; i < 6; i++ {
		flows = append(flows, flow.Flow{
			SourcePath: "flow.yaml",
			Config:     flow.Config{Name: "f" + string(rune('a'+i))},
			Steps:      []flow.Step{&flow.InputTextStep{Text: "${DEVICE_UDID}"}},
		})
	}
	workers := []DeviceWorker{
		{ID: 0, DeviceID: "dev-a", Driver: driverFor("dev-a"), Cleanup: func() {}},
		{ID: 1, DeviceID: "dev-b", Driver: driverFor("dev-b"), Cleanup: func() {}},
	}
	pr := NewParallelRunner(workers, RunnerConfig{
		OutputDir: t.TempDir(), Artifacts: ArtifactNever,
		Device: report.Device{Name: "2 devices", Platform: "android"}, App: report.App{ID: "com.test"},
		RunnerVersion: "test", DriverName: "mock",
	})
	if _, err := pr.Run(context.Background(), flows); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	total := 0
	for dev, values := range seen {
		for _, v := range values {
			if v != dev {
				t.Errorf("a flow on %s got DEVICE_UDID %q", dev, v)
			}
		}
		total += len(values)
	}
	if total != len(flows) {
		t.Errorf("got %d values, want one per flow (%d): %v", total, len(flows), seen)
	}
}
