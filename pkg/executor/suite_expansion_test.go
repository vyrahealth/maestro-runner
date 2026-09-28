package executor

import (
	"testing"

	"github.com/devicelab-dev/maestro-runner/pkg/flow"
)

// A flow made only of runFlow steps, as a Maestro suite writes one: sign in, then
// run the body in that session.
func runFlowOnlyFlow() flow.Flow {
	return flow.Flow{
		SourcePath: "/flows/checkout.yaml",
		Config:     flow.Config{AppID: "com.example.app"},
		Steps: []flow.Step{
			&flow.RunFlowStep{
				BaseStep: flow.BaseStep{StepType: flow.StepRunFlow},
				File:     "../subflows/sign-in.yaml",
				Env:      map[string]string{"API_BASE_URL": "${API_BASE_URL}"},
			},
			&flow.RunFlowStep{
				BaseStep: flow.BaseStep{StepType: flow.StepRunFlow},
				File:     "../subflows/checkout-body.yaml",
			},
		},
	}
}

func TestFlowsToRun_ExpandsASuiteByDefault(t *testing.T) {
	t.Setenv("MAESTRO_NO_SUITE_EXPANSION", "")
	got := flowsToRun([]flow.Flow{runFlowOnlyFlow()})
	if len(got) != 2 {
		t.Fatalf("expanded into %d flows, want one per runFlow (2)", len(got))
	}
}

func TestFlowsToRun_NoSuiteExpansion_KeepsTheFlowWhole(t *testing.T) {
	t.Setenv("MAESTRO_NO_SUITE_EXPANSION", "1")
	in := runFlowOnlyFlow()
	got := flowsToRun([]flow.Flow{in})
	if len(got) != 1 {
		t.Fatalf("got %d flows, want the one flow as written", len(got))
	}
	if got[0].SourcePath != in.SourcePath || len(got[0].Steps) != len(in.Steps) {
		t.Fatalf("the flow was changed: %q with %d steps, want %q with %d",
			got[0].SourcePath, len(got[0].Steps), in.SourcePath, len(in.Steps))
	}
}
