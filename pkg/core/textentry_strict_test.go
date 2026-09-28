package core

import (
	"strings"
	"testing"
)

// A field whose value the app rewrites on every input, as a unit formatter does.
type formattingField struct {
	value   string
	clears  int
	retypes int
}

func (f *formattingField) Text() (string, error) { return f.value, nil }
func (f *formattingField) Clear() error          { f.clears++; f.value = ""; return nil }
func (f *formattingField) Input(s string) error {
	f.retypes++
	f.value += s + " kg"
	return nil
}

const typedEmail = "qa+signup-1790573666347@example.com"

func TestConfirmTypedText_ExtraCharacterIsLeftAloneByDefault(t *testing.T) {
	t.Setenv("MAESTRO_STRICT_TYPING", "")
	f := &recordingField{value: typedEmail + "@"}
	if note := ConfirmTypedText(f, typedEmail, "", nil); note != "" {
		t.Fatalf("note %q, want none: without the switch a longer value is the app's", note)
	}
	if f.value != typedEmail+"@" || len(f.typed) != 0 {
		t.Fatalf("field %q retyped %d times, want it untouched", f.value, len(f.typed))
	}
}

func TestConfirmTypedText_StrictRetypesAnExtraCharacter(t *testing.T) {
	t.Setenv("MAESTRO_STRICT_TYPING", "1")
	f := &recordingField{value: typedEmail + "@"}
	note := ConfirmTypedText(f, typedEmail, "", nil)
	if f.value != typedEmail {
		t.Fatalf("field holds %q after the retype, want %q", f.value, typedEmail)
	}
	if !strings.Contains(note, "held more than was typed") {
		t.Errorf("note %q, want it to say the field held more than was typed", note)
	}
}

func TestConfirmTypedText_StrictRetypesOnceWhenTheAppRewritesTheField(t *testing.T) {
	t.Setenv("MAESTRO_STRICT_TYPING", "1")
	f := &formattingField{value: "72.5 kg"}
	note := ConfirmTypedText(f, "72.5", "", nil)
	if f.clears != 1 || f.retypes != 1 {
		t.Fatalf("cleared %d and retyped %d times, want exactly one of each", f.clears, f.retypes)
	}
	if !strings.Contains(note, "still differs") {
		t.Errorf("note %q, want a warning that the field still differs", note)
	}
}

func TestConfirmTypedText_StrictLeavesALandedValueAndASecureFieldAlone(t *testing.T) {
	t.Setenv("MAESTRO_STRICT_TYPING", "1")
	for _, tt := range []struct{ name, typed, after string }{
		{"landed", typedEmail, typedEmail},
		{"secure field", "secret", "••••••"},
		{"hint that did not change", "hello", "Username"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			before := tt.after
			if tt.name != "hint that did not change" {
				before = ""
			}
			f := &recordingField{value: tt.after}
			if note := ConfirmTypedText(f, tt.typed, before, nil); note != "" || len(f.typed) != 0 {
				t.Fatalf("note %q and %d retypes, want neither", note, len(f.typed))
			}
		})
	}
}
