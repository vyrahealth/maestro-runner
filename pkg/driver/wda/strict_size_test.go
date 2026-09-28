package wda

import (
	"testing"

	"github.com/devicelab-dev/maestro-runner/pkg/core"
	"github.com/devicelab-dev/maestro-runner/pkg/flow"
)

// A size selector without a tolerance must match exactly, as in Maestro
// (Filters.kt:146); the runner allows 5 points either way.
func TestStrictSizeHasNoDefaultTolerance(t *testing.T) {
	e := &ParsedElement{Bounds: core.Bounds{Width: 102, Height: 40}}
	if !matchesSelector(e, flow.Selector{Width: 100}) {
		t.Fatal("setup: without the switch 102 is within 5 of 100")
	}
	t.Setenv("MAESTRO_STRICT_SELECTORS", "1")
	if matchesSelector(e, flow.Selector{Width: 100}) {
		t.Error("with the switch width 100 must not match 102")
	}
	if !matchesSelector(e, flow.Selector{Width: 100, Tolerance: 2}) {
		t.Error("an explicit tolerance still applies")
	}
}
