package core

import (
	"strings"
	"testing"
	"time"
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
	quickTypedReads(t)
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
	quickTypedReads(t)
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
	quickTypedReads(t)
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

// quickTypedReads shortens the strict read-back's waits for a test.
func quickTypedReads(t *testing.T) {
	t.Helper()
	wait, limit := typedReadWait, typedReadLimit
	typedReadWait, typedReadLimit = time.Millisecond, 20*time.Millisecond
	t.Cleanup(func() { typedReadWait, typedReadLimit = wait, limit })
}

// lateField is a field the keyboard adds to late: it reads as typed, and from
// the second read after typing it holds late on the end as well. With again,
// that happens after a retype too.
type lateField struct {
	value   string
	late    string
	again   bool
	pending bool
	reads   int // since the last typing
	total   int
	inputs  int
}

func (f *lateField) Text() (string, error) {
	f.total++
	f.reads++
	if f.pending && f.reads == 2 {
		f.value += f.late
		f.pending = false
	}
	return f.value, nil
}
func (f *lateField) Clear() error { f.value = ""; return nil }
func (f *lateField) Input(s string) error {
	f.inputs++
	f.value += s
	f.reads, f.pending = 0, f.again
	return nil
}

// The phone's case: the email reads back as typed, and the "@" the keyboard
// adds comes after. A strict read-back waits for two reads that agree, so it
// sees the "@" and retypes; the retype is read the same way and holds.
func TestConfirmTypedText_StrictWaitsForALateCharacter(t *testing.T) {
	t.Setenv("MAESTRO_STRICT_TYPING", "1")
	quickTypedReads(t)
	f := &lateField{value: typedEmail, late: "@", pending: true}
	note := ConfirmTypedText(f, typedEmail, "", nil)
	if f.value != typedEmail || f.inputs != 1 {
		t.Fatalf("field holds %q after %d retypes, want %q after one", f.value, f.inputs, typedEmail)
	}
	if !strings.Contains(note, "held more than was typed") {
		t.Errorf("note %q, want it to say the field held more than was typed", note)
	}
	// Three reads until two agree, then two for the retype.
	if f.total != 5 {
		t.Errorf("reads = %d, want 5", f.total)
	}
}

// When the keyboard does it again after the retype, the second read-back
// waits as well and sees it: reported, not failed.
func TestConfirmTypedText_StrictReadsTheRetypeTheSameWay(t *testing.T) {
	t.Setenv("MAESTRO_STRICT_TYPING", "1")
	quickTypedReads(t)
	f := &lateField{value: typedEmail, late: "@", again: true, pending: true}
	note := ConfirmTypedText(f, typedEmail, "", nil)
	if !strings.Contains(note, "still differs") {
		t.Errorf("note %q, want a warning that the field still differs after the retype", note)
	}
	if f.inputs != 1 {
		t.Errorf("retyped %d times, want once", f.inputs)
	}
}

// Without the switch the read-back is a single read, with no wait, and the
// late "@" is not seen: the default is unchanged.
func TestConfirmTypedText_ReadBackIsOneReadByDefault(t *testing.T) {
	t.Setenv("MAESTRO_STRICT_TYPING", "")
	f := &lateField{value: typedEmail, late: "@", pending: true}
	start := time.Now()
	if note := ConfirmTypedText(f, typedEmail, "", nil); note != "" {
		t.Fatalf("note %q, want none", note)
	}
	if f.total != 1 || f.inputs != 0 {
		t.Errorf("%d reads and %d retypes, want one read and no retype", f.total, f.inputs)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("took %v, want no wait", elapsed)
	}
}

// changingField reads differently every time.
type changingField struct{ reads int }

func (f *changingField) Text() (string, error) {
	f.reads++
	return strings.Repeat("x", f.reads), nil
}
func (f *changingField) Clear() error         { return nil }
func (f *changingField) Input(s string) error { return nil }

// A field that never holds still is judged on its last read once the limit
// is up, rather than waited on for ever.
func TestReadBackGivesUpOnAFieldThatKeepsChanging(t *testing.T) {
	t.Setenv("MAESTRO_STRICT_TYPING", "1")
	quickTypedReads(t)
	f := &changingField{}
	start := time.Now()
	text, err := readBack(f)
	if err != nil || text != strings.Repeat("x", f.reads) {
		t.Fatalf("read %q (%v), want the last of %d reads", text, err, f.reads)
	}
	if elapsed := time.Since(start); elapsed < typedReadLimit || elapsed > time.Second {
		t.Errorf("gave up after %v, want about %v", elapsed, typedReadLimit)
	}
}
