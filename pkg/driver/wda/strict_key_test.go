package wda

import (
	"net/http"
	"testing"

	"github.com/devicelab-dev/maestro-runner/pkg/flow"
)

// With the switch a key-named tapOn is an element lookup like any other:
// with no element labelled Delete it fails, even with the keyboard up,
// instead of pressing backspace. Maestro has no key fallback in tapOn.
func TestStrictTapOnKeyNameNeverPressesTheKey(t *testing.T) {
	var sentKeys bool
	server := keyboardKeyServer(true, http.StatusOK, &sentKeys)
	defer server.Close()

	step := &flow.TapOnStep{BaseStep: flow.BaseStep{TimeoutMs: 300}, Selector: flow.Selector{Text: "Delete"}}
	if res := createTestDriver(server).tapOn(step); !res.Success || !sentKeys {
		t.Fatalf("setup: without the switch the key is pressed: %s", res.Message)
	}

	t.Setenv("MAESTRO_STRICT_SELECTORS", "1")
	sentKeys = false
	res := createTestDriver(server).tapOn(step)
	if res.Success {
		t.Errorf("tapOn Delete passed with no element labelled Delete: %s", res.Message)
	}
	if sentKeys {
		t.Error("tapOn pressed a key; with the switch it only taps elements")
	}
}
