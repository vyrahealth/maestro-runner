package executor

import (
	"context"
	"testing"
	"time"

	"github.com/devicelab-dev/maestro-runner/pkg/core"
	"github.com/devicelab-dev/maestro-runner/pkg/flow"
	"github.com/devicelab-dev/maestro-runner/pkg/report"
)

// slowStep is how long the "slow" assertVisible below takes. A budget read
// right after a step that changed the device is at least 7000 minus a little
// scheduling noise; one read after the slow step is at most 7000 minus this.
const slowStep = 700 * time.Millisecond

func TestConditionBudgetMs_ShrinksFromTheLastInteraction(t *testing.T) {
	fr := &FlowRunner{lastInteraction: time.Now().Add(-2 * time.Second)}
	if got := fr.conditionBudgetMs(); got > 5000 || got < 4500 {
		t.Errorf("2 s after the last interaction the budget is %d ms, want about 5000", got)
	}

	// Nothing left: one look, never 0, which the drivers read as their default.
	fr.lastInteraction = time.Now().Add(-10 * time.Second)
	if got := fr.conditionBudgetMs(); got != 1 {
		t.Errorf("spent budget = %d ms, want 1", got)
	}
}

func TestCheckCondition_BudgetReplacesTheDefault(t *testing.T) {
	se := NewScriptEngine()
	defer se.Close()
	se.conditionBudget = func() int { return 4321 }

	if got := captureVisibleTimeout(t, se, flow.Condition{Visible: &flow.Selector{Text: "Nope"}}); got != 4321 {
		t.Errorf("visible check timeout = %d, want the budget 4321", got)
	}
	// An explicit timeout still wins.
	if got := captureVisibleTimeout(t, se, flow.Condition{Visible: &flow.Selector{Text: "Nope"}, Timeout: 250}); got != 250 {
		t.Errorf("visible check timeout = %d, want the condition's own 250", got)
	}

	var notVisible int
	driver := &mockDriver{executeFunc: func(step flow.Step) *core.CommandResult {
		if s, ok := step.(*flow.AssertNotVisibleStep); ok {
			notVisible = s.TimeoutMs
		}
		return &core.CommandResult{Success: true}
	}}
	se.CheckCondition(context.Background(), flow.Condition{NotVisible: &flow.Selector{Text: "Gone"}}, driver)
	if notVisible != 4321 {
		t.Errorf("notVisible check timeout = %d, want the budget 4321", notVisible)
	}
}

func budgetTap(text string, optional bool) *flow.TapOnStep {
	return &flow.TapOnStep{
		BaseStep: flow.BaseStep{StepType: flow.StepTapOn, Optional: optional},
		Selector: flow.Selector{Text: text},
	}
}

func budgetAssert(text string) *flow.AssertVisibleStep {
	return &flow.AssertVisibleStep{
		BaseStep: flow.BaseStep{StepType: flow.StepAssertVisible},
		Selector: flow.Selector{Text: text},
	}
}

// whenCond is a runFlow that runs only when "cond" is visible, which it never is.
func whenCond() *flow.RunFlowStep {
	return &flow.RunFlowStep{
		BaseStep: flow.BaseStep{StepType: flow.StepRunFlow},
		When:     &flow.Condition{Visible: &flow.Selector{Text: "cond"}},
		Steps:    []flow.Step{budgetTap("inside", false)},
	}
}

// conditionTimeouts runs steps as one flow and returns the timeout of every
// check of "cond". The driver takes slowStep to find "slow", fails a tap on
// "missing", and passes everything else.
func conditionTimeouts(t *testing.T, conditionTimeout int, steps ...flow.Step) []int {
	t.Helper()
	var got []int
	driver := &mockDriver{executeFunc: func(step flow.Step) *core.CommandResult {
		switch s := step.(type) {
		case *flow.AssertVisibleStep:
			switch s.Selector.Text {
			case "cond":
				got = append(got, s.TimeoutMs)
				return &core.CommandResult{Success: false}
			case "slow":
				time.Sleep(slowStep)
			}
		case *flow.TapOnStep:
			if s.Selector.Text == "missing" {
				return &core.CommandResult{Success: false, Error: &testError{msg: "not found"}}
			}
		}
		return &core.CommandResult{Success: true}
	}}
	runner := New(driver, RunnerConfig{
		OutputDir:        t.TempDir(),
		Artifacts:        ArtifactNever,
		Device:           report.Device{ID: "test", Platform: "ios"},
		ConditionTimeout: conditionTimeout,
	})
	result, err := runner.Run(context.Background(), []flow.Flow{{
		SourcePath: "test.yaml",
		Config:     flow.Config{Name: "condition budget"},
		Steps:      steps,
	}})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Status != report.StatusPassed {
		t.Fatalf("status = %v, want passed", result.Status)
	}
	if len(got) == 0 {
		t.Fatal("the condition was never checked")
	}
	return got
}

func wantFresh(t *testing.T, what string, ms int) {
	t.Helper()
	if ms > maestroOptionalLookupMs || ms < maestroOptionalLookupMs-int((slowStep-100*time.Millisecond).Milliseconds()) {
		t.Errorf("%s: budget %d ms, want about %d (the clock restarted just before)", what, ms, maestroOptionalLookupMs)
	}
}

func wantSpent(t *testing.T, what string, ms int) {
	t.Helper()
	if ms > maestroOptionalLookupMs-int(slowStep.Milliseconds()) || ms < 1000 {
		t.Errorf("%s: budget %d ms, want at most %d (the slow step counted against it)",
			what, ms, maestroOptionalLookupMs-int(slowStep.Milliseconds()))
	}
}

func TestConditionBudget_OffByDefault(t *testing.T) {
	t.Setenv("MAESTRO_PARITY_TIMEOUTS", "")
	if got := conditionTimeouts(t, 0, budgetTap("A", false), whenCond()); got[0] != defaultConditionTimeoutMs {
		t.Errorf("timeout = %d, want the runner's %d", got[0], defaultConditionTimeoutMs)
	}
}

func TestConditionBudget_ExplicitConditionTimeoutWins(t *testing.T) {
	t.Setenv("MAESTRO_PARITY_TIMEOUTS", "1")
	if got := conditionTimeouts(t, 2500, budgetTap("A", false), whenCond()); got[0] != 2500 {
		t.Errorf("timeout = %d, want the configured 2500", got[0])
	}
}

func TestConditionBudget_RestartsAfterAStepThatChangesTheDevice(t *testing.T) {
	t.Setenv("MAESTRO_PARITY_TIMEOUTS", "1")

	got := conditionTimeouts(t, 0, budgetTap("A", false), whenCond())
	wantFresh(t, "right after a tap", got[0])

	got = conditionTimeouts(t, 0, budgetAssert("slow"), budgetTap("A", false), whenCond())
	wantFresh(t, "a tap after the slow assertion", got[0])
}

func TestConditionBudget_AnAssertionDoesNotRestartIt(t *testing.T) {
	t.Setenv("MAESTRO_PARITY_TIMEOUTS", "1")
	got := conditionTimeouts(t, 0, budgetTap("A", false), budgetAssert("slow"), whenCond())
	wantSpent(t, "an assertion after the tap", got[0])
}

// The clock starts with the flow, not at the first step that changes the device.
func TestConditionBudget_StartsWithTheFlow(t *testing.T) {
	t.Setenv("MAESTRO_PARITY_TIMEOUTS", "1")
	repeat := &flow.RepeatStep{
		BaseStep: flow.BaseStep{StepType: flow.StepRepeat},
		While:    flow.Condition{Visible: &flow.Selector{Text: "cond"}},
		Steps:    []flow.Step{budgetTap("A", false)},
	}
	got := conditionTimeouts(t, 0, budgetAssert("slow"), repeat)
	wantSpent(t, "a while: check with no interaction yet", got[0])
}

// A failed step changes nothing, optional or not: Maestro's command throws
// before the clock is reset.
func TestConditionBudget_AFailedStepDoesNotRestartIt(t *testing.T) {
	t.Setenv("MAESTRO_PARITY_TIMEOUTS", "1")
	got := conditionTimeouts(t, 0, budgetTap("A", false), budgetAssert("slow"), budgetTap("missing", true), whenCond())
	wantSpent(t, "after a failed optional tap", got[0])
}

// A runFlow or retry that changed the device restarts the clock when it ends,
// so the slow assertion inside it does not count.
func TestConditionBudget_CompoundStepRestartsItWhenItEnds(t *testing.T) {
	t.Setenv("MAESTRO_PARITY_TIMEOUTS", "1")

	runFlow := &flow.RunFlowStep{
		BaseStep: flow.BaseStep{StepType: flow.StepRunFlow},
		Steps:    []flow.Step{budgetTap("A", false), budgetAssert("slow")},
	}
	got := conditionTimeouts(t, 0, runFlow, whenCond())
	wantFresh(t, "after a runFlow with a tap", got[0])

	retry := &flow.RetryStep{
		BaseStep: flow.BaseStep{StepType: flow.StepRetry},
		Steps:    []flow.Step{budgetTap("A", false), budgetAssert("slow")},
	}
	got = conditionTimeouts(t, 0, retry, whenCond())
	wantFresh(t, "after a retry with a tap", got[0])

	// One with no step that changed the device leaves the clock alone.
	assertsOnly := &flow.RunFlowStep{
		BaseStep: flow.BaseStep{StepType: flow.StepRunFlow},
		Steps:    []flow.Step{budgetAssert("slow")},
	}
	got = conditionTimeouts(t, 0, budgetTap("A", false), assertsOnly, whenCond())
	wantSpent(t, "after a runFlow of assertions", got[0])
}
