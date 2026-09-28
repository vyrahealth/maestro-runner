package cli

import (
	"testing"

	"github.com/urfave/cli/v2"
)

// An unset --condition-timeout has to reach the executor as 0, not as the
// 1000 ms the engine uses by default: MAESTRO_PARITY_TIMEOUTS gives when:/while:
// checks Maestro's budget only when no condition timeout was configured, and
// a flag that always carried 1000 would look configured.
func TestConditionTimeoutFlag_UnsetReadsAsZero(t *testing.T) {
	var flag *cli.IntFlag
	for _, f := range testCommand.Flags {
		if intFlag, ok := f.(*cli.IntFlag); ok && intFlag.Name == "condition-timeout" {
			flag = intFlag
		}
	}
	if flag == nil {
		t.Fatal("--condition-timeout is not a test flag")
	}
	if flag.Value != 0 {
		t.Errorf("unset value = %d, want 0 so the executor can tell it was not set", flag.Value)
	}
	if flag.DefaultText != "1000" {
		t.Errorf("help default = %q, want the engine's 1000", flag.DefaultText)
	}
}
