package wda

import (
	"testing"
	"time"

	"github.com/devicelab-dev/maestro-runner/pkg/flow"
)

// Without the switch the defaults stay as they were: 12 s for a required
// element, 7 s for an optional one, 5 s for assertNotVisible.
func TestLookupTimeoutDefaults(t *testing.T) {
	d := &Driver{}
	if got := d.calculateTimeout(false, 0); got != 12*time.Second {
		t.Errorf("required = %v, want 12s", got)
	}
	if got := d.calculateTimeout(true, 0); got != 7*time.Second {
		t.Errorf("optional = %v, want 7s", got)
	}
	if got := notVisibleTimeoutMs(); got != 5000 {
		t.Errorf("assertNotVisible = %dms, want 5000", got)
	}
	if !assertionOptional(true) {
		t.Error("an optional assertion keeps the optional timeout without the switch")
	}
}

// With MAESTRO_PARITY_TIMEOUTS the defaults are Maestro's: 17 s required and
// 7 s optional (Orchestra.kt:136-137), 17 s for assertNotVisible and for an
// assertion whether optional or not (Orchestra.kt:513-523).
func TestLookupTimeoutsParity(t *testing.T) {
	t.Setenv("MAESTRO_PARITY_TIMEOUTS", "1")
	d := &Driver{}
	if got := d.calculateTimeout(false, 0); got != 17*time.Second {
		t.Errorf("required = %v, want 17s", got)
	}
	if got := d.calculateTimeout(true, 0); got != 7*time.Second {
		t.Errorf("optional = %v, want 7s", got)
	}
	if got := notVisibleTimeoutMs(); got != 17000 {
		t.Errorf("assertNotVisible = %dms, want 17000", got)
	}
	if assertionOptional(true) {
		t.Error("an optional assertion waits the full lookup timeout in Maestro")
	}

	// A step's own timeout, and one set for the run, still win.
	if got := d.calculateTimeout(false, 1500); got != 1500*time.Millisecond {
		t.Errorf("step timeout = %v, want 1.5s", got)
	}
	d.SetFindTimeout(9000)
	if got := d.calculateTimeout(false, 0); got != 9*time.Second {
		t.Errorf("configured timeout = %v, want 9s", got)
	}
}

// An optional assertVisible waits for the required timeout with the switch,
// and for the optional one without it.
func TestOptionalAssertVisibleWaitsFullTimeoutWithParity(t *testing.T) {
	server := sourceServer(t, emptyScreenSource)
	defer server.Close()

	elapsed := func() time.Duration {
		d := createTestDriver(server)
		d.SetFindTimeout(600)
		d.SetOptionalFindTimeout(100)
		start := time.Now()
		res := d.assertVisible(&flow.AssertVisibleStep{
			BaseStep: flow.BaseStep{Optional: true},
			Selector: flow.Selector{Text: "Absent"},
		})
		if res.Success {
			t.Fatal("an absent element should not be visible")
		}
		return time.Since(start)
	}

	if got := elapsed(); got >= 600*time.Millisecond {
		t.Errorf("without the switch an optional assertion waited %v, want about 100ms", got)
	}
	t.Setenv("MAESTRO_PARITY_TIMEOUTS", "1")
	if got := elapsed(); got < 600*time.Millisecond {
		t.Errorf("with the switch an optional assertion waited %v, want the full 600ms", got)
	}
}
