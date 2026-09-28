package executor

import (
	"context"
	"testing"
	"time"

	"github.com/devicelab-dev/maestro-runner/pkg/flow"
)

func TestExecuteWait_Duration(t *testing.T) {
	fr := &FlowRunner{ctx: context.Background()}

	start := time.Now()
	res := fr.executeWait(&flow.WaitStep{DurationMs: 60})
	elapsed := time.Since(start)

	if !res.Success {
		t.Fatalf("expected success, got: %v", res.Error)
	}
	if elapsed < 55*time.Millisecond {
		t.Errorf("returned after %v, expected to wait ~60ms", elapsed)
	}
}

func TestExecuteWait_ZeroIsNoop(t *testing.T) {
	fr := &FlowRunner{ctx: context.Background()}
	start := time.Now()
	res := fr.executeWait(&flow.WaitStep{DurationMs: 0})
	if !res.Success {
		t.Fatalf("expected success for zero wait, got: %v", res.Error)
	}
	if time.Since(start) > 20*time.Millisecond {
		t.Error("zero wait should return immediately")
	}
}

func TestExecuteWait_ContextCancellationStopsEarly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	fr := &FlowRunner{ctx: ctx}

	// Cancel shortly after the wait begins; a 10s wait must return well before
	// its full duration.
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	res := fr.executeWait(&flow.WaitStep{DurationMs: 10000})
	elapsed := time.Since(start)

	if res.Success {
		t.Error("expected failure when the run is cancelled mid-wait")
	}
	if elapsed > time.Second {
		t.Errorf("cancelled wait took %v, expected to stop promptly", elapsed)
	}
}
