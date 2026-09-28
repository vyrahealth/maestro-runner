package executor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devicelab-dev/maestro-runner/pkg/core"
	"github.com/devicelab-dev/maestro-runner/pkg/flow"
	"github.com/devicelab-dev/maestro-runner/pkg/report"
)

// Maestro reads maxRetries as retries after the first run, not as attempts:
// `(maxRetries ?: 1).coerceAtMost(3)`, then `while (attempt <= maxRetries)`
// from 0 (Orchestra.kt:934-955).
func TestRetryAttempts_CountsRetriesAsMaestroDoes(t *testing.T) {
	se := NewScriptEngine()
	defer se.Close()
	se.SetVariable("TWO", "2")
	fr := &FlowRunner{script: se}

	for _, tc := range []struct {
		maxRetries string
		want       int
	}{
		{"", 2},             // unset: one retry
		{"0", 1},            // no retry, one run
		{"1", 2},            // one retry
		{"3", 4},            // the most there can be
		{"10", 4},           // capped at three retries
		{"-1", 0},           // the loop never starts
		{"${TWO}", 3},       // an expression is evaluated first
		{"${1 + 1}", 3},     // and so is arithmetic
		{"${UNSET_VAR}", 2}, // evaluates to nothing: one retry
		{"five", 2},         // not an integer: one retry, not an error
		{"1.5", 2},          // toIntOrNull rejects a decimal
	} {
		if got := fr.retryAttempts(tc.maxRetries); got != tc.want {
			t.Errorf("maxRetries %q: %d attempts, want %d", tc.maxRetries, got, tc.want)
		}
	}
}

// failingTaps is a driver whose taps fail the first failures times and then
// pass; every other step passes. It counts the taps.
func failingTaps(failures int, taps *int) *mockDriver {
	return &mockDriver{executeFunc: func(step flow.Step) *core.CommandResult {
		if _, ok := step.(*flow.TapOnStep); ok {
			*taps++
			if *taps <= failures {
				return &core.CommandResult{Success: false, Error: &testError{msg: "not there yet"}}
			}
		}
		return &core.CommandResult{Success: true}
	}}
}

func retryAroundTap(maxRetries string) *flow.RetryStep {
	return &flow.RetryStep{
		BaseStep:   flow.BaseStep{StepType: flow.StepRetry},
		MaxRetries: maxRetries,
		Steps:      []flow.Step{&flow.TapOnStep{BaseStep: flow.BaseStep{StepType: flow.StepTapOn}}},
	}
}

// `maxRetries: 1` ran the commands once and never retried.
func TestRetry_MaxRetriesOneRetriesOnce(t *testing.T) {
	taps := 0
	result := runOneFlow(t, failingTaps(1, &taps), flow.Flow{
		SourcePath: "test.yaml",
		Config:     flow.Config{Name: "retry once"},
		Steps:      []flow.Step{retryAroundTap("1")},
	})
	if result.Status != report.StatusPassed {
		t.Errorf("status = %v, want passed: the retry should run the tap a second time", result.Status)
	}
	if taps != 2 {
		t.Errorf("tap ran %d times, want 2", taps)
	}
}

func TestRetry_AttemptsWhenEveryRunFails(t *testing.T) {
	for _, tc := range []struct {
		maxRetries string
		taps       int
		status     report.Status
	}{
		{"", 2, report.StatusFailed},
		{"0", 1, report.StatusFailed},
		{"1", 2, report.StatusFailed},
		{"10", 4, report.StatusFailed},
		// A negative maxRetries runs nothing, and Maestro's command then
		// completes without an error.
		{"-1", 0, report.StatusPassed},
	} {
		taps := 0
		result := runOneFlow(t, failingTaps(1000, &taps), flow.Flow{
			SourcePath: "test.yaml",
			Config:     flow.Config{Name: "retry " + tc.maxRetries},
			Steps:      []flow.Step{retryAroundTap(tc.maxRetries)},
		})
		if taps != tc.taps {
			t.Errorf("maxRetries %q: tap ran %d times, want %d", tc.maxRetries, taps, tc.taps)
		}
		if result.Status != tc.status {
			t.Errorf("maxRetries %q: status = %v, want %v", tc.maxRetries, result.Status, tc.status)
		}
	}
}

// The file form counts the same way.
func TestRetry_FileFormRetriesOnce(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tap.yaml"), []byte("appId: com.example.app\n---\n- tapOn: OK\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	taps := 0
	result := runOneFlow(t, failingTaps(1000, &taps), flow.Flow{
		SourcePath: filepath.Join(dir, "main.yaml"),
		Config:     flow.Config{Name: "retry file"},
		Steps: []flow.Step{&flow.RetryStep{
			BaseStep:   flow.BaseStep{StepType: flow.StepRetry},
			MaxRetries: "1",
			File:       "tap.yaml",
		}},
	})
	if taps != 2 {
		t.Errorf("tap ran %d times, want 2 (one run and one retry)", taps)
	}
	if result.Status != report.StatusFailed {
		t.Errorf("status = %v, want failed", result.Status)
	}
}
