package core

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestMatchSelectorText(t *testing.T) {
	cases := []struct {
		pattern string
		values  []string
		want    bool
	}{
		{"sign in", []string{"Sign In"}, true},                           // plain: case-insensitive, whole
		{"Sign", []string{"Please Sign In"}, false},                      // plain: never a substring (Maestro)
		{"Example", []string{"Examples"}, false},                         // plain: whole value only
		{"line two", []string{"line\ntwo"}, true},                        // newline read as space
		{"Passwords.*", []string{"Passwords"}, true},                     // regex: whole value
		{"Passwords.*", []string{"Import Passwords from Google"}, false}, // regex: not a substring
		{"(let's get started!|x)", []string{"Let's get started!"}, true}, // regex: case-insensitive
		{"DDG.", []string{"Not DDG."}, false},                            // dotted plain: whole match only
		{"Protections.activated!", []string{"Protections activated!"}, true},
		{"a.*b", []string{"a\nb"}, true},        // dot matches newline
		{"([", []string{"has ([ in it"}, false}, // invalid regex: literal, whole
		{"([", []string{"(["}, true},
		{"", []string{"x"}, false},
		{"x", []string{""}, false},
	}
	for _, c := range cases {
		if got := MatchSelectorText(c.pattern, c.values...); got != c.want {
			t.Errorf("MatchSelectorText(%q, %q) = %v, want %v", c.pattern, c.values, got, c.want)
		}
	}
}

// The #188 / #178 cases: plain text matches a whole value, never part of one.
func TestMatchesTextMaestro(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
		values  []string
		want    bool
	}{
		{"substring of a longer label", "Open", []string{"Talk · Open"}, false},
		{"case ignored", "open", []string{"Open"}, true},
		{"partial match written as regex", ".*Open.*", []string{"Talk · Open"}, true},
		{"prefix regex", "Inbox.*", []string{"Inbox (3)"}, true},
		{"literal equality when $ would anchor", "$7.50", []string{"$7.50"}, true},
		{"literal equality is case-sensitive", "$7.50A", []string{"$7.50a"}, false},
		{"invalid regex is literal", "(", []string{"("}, true},
		{"invalid regex literal is whole", "(", []string{"a ( b"}, false},
		{"invalid regex literal ignores case", "Tap (here", []string{"tap (HERE"}, true},
		{"content-desc label is not a substring", "English", []string{"", "Language: English"}, false},
		{"long content-desc", "Jawy", []string{"Jawy. All nodes connected. Nodes"}, false},
		{"dot is any char but whole", "DDG.", []string{"Not DDG."}, false},
		{"dot is any char", "DDG.", []string{"DDG!"}, true},
		{"newline read as space", "Sign in", []string{"Sign\nin"}, true},
		{"dot matches newline", "Sign.in", []string{"Sign\nin"}, true},
		{"multiline anchors", "a$\n^b", []string{"a\nb"}, true},
		{"any of the values", "Email", []string{"", "Email", ""}, true},
		{"hint value", "Email", []string{"", "", "email"}, true},
		{"alternation is grouped", "Yes|No", []string{"Nope"}, false},
		{"alternation", "Yes|No", []string{"no"}, true},
		{"unicode case", "ÉCOLE", []string{"école"}, true},
		{"no values", "x", nil, false},
	}
	for _, c := range cases {
		if got := MatchesTextMaestro(c.pattern, c.values...); got != c.want {
			t.Errorf("%s: MatchesTextMaestro(%q, %q) = %v, want %v", c.name, c.pattern, c.values, got, c.want)
		}
	}
}

func TestMatchesIDMaestro(t *testing.T) {
	cases := []struct {
		pattern, id string
		want        bool
	}{
		{"login", "com.app:id/login", true},
		{"login", "com.app:id/login_button", false},
		{"LOGIN", "com.app:id/login", true},
		{"com.app:id/login", "com.app:id/login", true},
		{"login.*", "com.app:id/login_button", true},
		{".*button", "com.app:id/login_button", true},
		{"id/login", "com.app:id/login", false},
		{"omnibarTextInput|inputField", "com.app:id/inputField", true},
		{"(", "(", true},
		{"login", "", false},
	}
	for _, c := range cases {
		if got := MatchesIDMaestro(c.pattern, c.id); got != c.want {
			t.Errorf("MatchesIDMaestro(%q, %q) = %v, want %v", c.pattern, c.id, got, c.want)
		}
	}
}

func TestSelectorRegexSource(t *testing.T) {
	for pattern, want := range map[string]string{
		"Open":      "Open",
		".*Open.*":  ".*Open.*",
		"$7.50":     "$7.50",
		"(":         `\(`,
		"a(?=b)":    `a\(\?=b\)`, // RE2 has no lookahead: literal
		"Tap (here": `Tap \(here`,
	} {
		if got := SelectorRegexSource(pattern); got != want {
			t.Errorf("SelectorRegexSource(%q) = %q, want %q", pattern, got, want)
		}
	}
}

func TestUiAutomatorRegexes(t *testing.T) {
	if got := UiAutomatorTextRegex("Open", true); got != "(?ims)(?:Open)" {
		t.Errorf("text ignoring case = %q", got)
	}
	if got := UiAutomatorTextRegex("Open", false); got != "(?ms)(?:Open)" {
		t.Errorf("text in own case = %q", got)
	}
	if got := UiAutomatorTextRegex("(", true); got != `(?ims)(?:\()` {
		t.Errorf("invalid regex = %q", got)
	}
	if got := UiAutomatorIDRegex("login"); got != "(?ims)(?:.*/)?(?:login)" {
		t.Errorf("id = %q", got)
	}
	// The Java patterns are whole-match; check the Go reading agrees.
	re := regexp.MustCompile(`\A` + UiAutomatorIDRegex("login") + `\z`)
	if !re.MatchString("com.app:id/login") || re.MatchString("com.app:id/login_button") {
		t.Error("id regex should match the id after the package prefix, whole")
	}
	re = regexp.MustCompile(`\A` + UiAutomatorTextRegex("Open", true) + `\z`)
	if !re.MatchString("open") || re.MatchString("Talk · Open") {
		t.Error("text regex should match whole, ignoring case")
	}
}

func TestUiAutomatorIDRegexDropsOuterAnchors(t *testing.T) {
	re := regexp.MustCompile(`\A` + UiAutomatorIDRegex(`^auth\.login$`) + `\z`)
	if !re.MatchString("com.app:id/auth.login") {
		t.Error("an anchored id should match after the package prefix")
	}
	if got := UiAutomatorIDRegex(`price\$`); got != `(?ims)(?:.*/)?(?:price\$)` {
		t.Errorf("an escaped $ must stay, got %q", got)
	}
}

func TestUiSelectorTextTiers(t *testing.T) {
	want := [][]string{
		{`.text("Open")`, `.description("Open")`},
		{`.textMatches("(?ims)(?:Open)")`, `.descriptionMatches("(?ims)(?:Open)")`, `.hintMatches("(?ims)(?:Open)")`},
	}
	if got := UiSelectorTextTiers("Open", true); !reflect.DeepEqual(got, want) {
		t.Errorf("plain text tiers:\n got %q\nwant %q", got, want)
	}
	// Regex syntax adds the own-case tier between the literal and the
	// ignore-case one (#151); no hint without withHint.
	want = [][]string{
		{`.text("^SIGN OUT$")`, `.description("^SIGN OUT$")`},
		{`.textMatches("(?ms)(?:^SIGN OUT$)")`, `.descriptionMatches("(?ms)(?:^SIGN OUT$)")`},
		{`.textMatches("(?ims)(?:^SIGN OUT$)")`, `.descriptionMatches("(?ims)(?:^SIGN OUT$)")`},
	}
	if got := UiSelectorTextTiers("^SIGN OUT$", false); !reflect.DeepEqual(got, want) {
		t.Errorf("regex tiers:\n got %q\nwant %q", got, want)
	}
	// Quotes are escaped for the UiSelector string; an invalid regex is quoted.
	got := UiSelectorTextTiers(`Say "hi" (`, false)
	if got[0][0] != `.text("Say \"hi\" (")` || got[2][0] != `.textMatches("(?ims)(?:Say \"hi\" \()")` {
		t.Errorf("escaping wrong: %q", got)
	}
	for _, tier := range UiSelectorTextTiers("Open", true) {
		for _, q := range tier {
			if strings.Contains(q, "Contains(") || strings.Contains(q, ".*") {
				t.Errorf("plain text must not produce a partial query: %s", q)
			}
		}
	}
	if UiSelectorTextTiers("", true) != nil {
		t.Error("empty text should give no tiers")
	}
}

func TestUiSelectorIDTiers(t *testing.T) {
	want := [][]string{
		{`.resourceId("login")`},
		{`.resourceIdMatches("(?ims)(?:.*/)?(?:login)")`},
	}
	if got := UiSelectorIDTiers("login"); !reflect.DeepEqual(got, want) {
		t.Errorf("id tiers:\n got %q\nwant %q", got, want)
	}
	if UiSelectorIDTiers("") != nil {
		t.Error("empty id should give no tiers")
	}
}

func TestIsPlainSelectorText(t *testing.T) {
	for text, want := range map[string]bool{
		"Sign In": true, "DDG.": true, "Mr. Smith": true, "": false,
		"$7.50": false, "Inbox (3)": false, ".*Open.*": false, `a\.b`: false, "a|b": false,
	} {
		if got := IsPlainSelectorText(text); got != want {
			t.Errorf("IsPlainSelectorText(%q) = %v, want %v", text, got, want)
		}
	}
}

func TestMatchSelectorTextExactCase(t *testing.T) {
	if !MatchSelectorTextExactCase("^SIGN OUT$", "SIGN OUT") {
		t.Error("regex in its own case should match")
	}
	if MatchSelectorTextExactCase("^SIGN OUT$", "Sign out") {
		t.Error("regex in another case should not match exactly")
	}
	if !MatchSelectorTextExactCase("Login", "Login") || MatchSelectorTextExactCase("Login", "login") {
		t.Error("plain exact-case match wrong")
	}
	if MatchSelectorTextExactCase("([", "([") {
		t.Error("invalid regex should not match")
	}
}

func TestMatchSelectorID(t *testing.T) {
	cases := []struct {
		pattern, id string
		want        bool
	}{
		{"Flatlist", "FlatList", true},
		{`^auth\.login$`, "com.app:id/auth.login", true},
		{"login", "login_button", false},
		{"login.*", "login_button", true},
		{"Field_Password", "Field_PasswordName", false},
		{"field_password", "Field_Password", true},
		{"login_button", "com.app:id/login_button", true},
		{"([", "([", true},
		{"([", "a([b", false},
		{"x", "", false},
		{"", "x", false},
	}
	for _, c := range cases {
		if got := MatchSelectorID(c.pattern, c.id); got != c.want {
			t.Errorf("MatchSelectorID(%q, %q) = %v, want %v", c.pattern, c.id, got, c.want)
		}
	}
}

func TestLooksLikeRegex(t *testing.T) {
	for text, want := range map[string]bool{
		"plain": false, "Mr. Smith": false, "a.*": true, "^start": true, "end$": true,
		"a|b": true, `\.`: true, `a\b`: false, "mid^dle": false, "(x)": true,
	} {
		if got := LooksLikeRegex(text); got != want {
			t.Errorf("LooksLikeRegex(%q) = %v, want %v", text, got, want)
		}
	}
}

func TestLiteralNeedles(t *testing.T) {
	cases := map[string][]string{
		"Sign In":            {"sign", "in"},
		"line\ntwo":          {"line", "two"},
		"Welcome back.*":     {"welcome", "back"},
		`Total: \$10`:        {"total:", "$10"},
		"colou?r scheme":     {"r", "scheme"},
		"a|b":                nil,
		"(?i)hello world":    {"hello", "world"},
		"café?s menu":        {"s", "menu"},
		"x{100}yz tail":      {"yz", "tail"},
		"x.*":                nil,
		"":                   nil,
		"Terms of Service.+": {"terms", "of", "service"},
	}
	for pattern, want := range cases {
		if got := LiteralNeedles(pattern); !reflect.DeepEqual(got, want) {
			t.Errorf("LiteralNeedles(%q) = %q, want %q", pattern, got, want)
		}
	}
}
