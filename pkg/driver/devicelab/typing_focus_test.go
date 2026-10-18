package devicelab

import (
	"testing"

	"github.com/devicelab-dev/maestro-runner/pkg/core"
)

// focusIsOn accepts the tapped field itself, a field inside a tapped wrapper,
// and a field that holds the tapped element's centre; a field elsewhere on the
// screen (the one that kept focus when the tap did nothing) is refused (#189).
func TestFocusIsOn(t *testing.T) {
	field := core.Bounds{X: 40, Y: 900, Width: 1000, Height: 120}
	for _, tc := range []struct {
		name    string
		focused *core.ElementInfo
		tapped  core.Bounds
		want    bool
	}{
		{"the tapped field", &core.ElementInfo{Bounds: field}, field, true},
		{"input inside a tapped wrapper", &core.ElementInfo{Bounds: core.Bounds{X: 60, Y: 920, Width: 900, Height: 80}}, core.Bounds{X: 0, Y: 880, Width: 1080, Height: 160}, true},
		{"tapped label inside the field", &core.ElementInfo{Bounds: field}, core.Bounds{X: 60, Y: 930, Width: 200, Height: 50}, true},
		{"previous field kept focus", &core.ElementInfo{Bounds: core.Bounds{X: 40, Y: 300, Width: 1000, Height: 120}}, field, false},
		{"unknown focused bounds", &core.ElementInfo{}, field, true},
		{"nothing focused", nil, field, false},
	} {
		if got := focusIsOn(tc.focused, tc.tapped); got != tc.want {
			t.Errorf("%s: focusIsOn = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// Only a text field is tapped a second time: a tap on a label or a button
// that moves focus elsewhere is not repeated.
func TestIsTextField(t *testing.T) {
	for class, want := range map[string]bool{
		"android.widget.EditText":                          true,
		"com.facebook.react.views.textinput.ReactEditText": true,
		"android.widget.AutoCompleteTextView":              true,
		"android.widget.TextView":                          false,
		"android.widget.Button":                            false,
		"":                                                 false,
	} {
		if got := isTextField(class); got != want {
			t.Errorf("isTextField(%q) = %v, want %v", class, got, want)
		}
	}
}
