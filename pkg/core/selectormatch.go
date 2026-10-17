package core

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Selector text and id matching with Maestro's semantics, shared so drivers
// that receive raw candidates from a device agent all decide alike.
//
// Text: every text selector is a regex that must match a whole value,
// case-insensitive, with "." matching line breaks and ^/$ matching at them
// (Maestro compiles with IGNORE_CASE, DOT_MATCHES_ALL and MULTILINE and calls
// Regex.matches: Filters.kt textMatches). Plain text included, so "Open" does
// not match "Talk · Open" and "Example" does not match "Examples" (#188); a
// partial match has to be written, as ".*Open.*". A value also matches when it
// equals the pattern exactly, which finds "$7.50" although its "$" anchors as
// a regex, and each value is tried again with its line breaks read as spaces.
// A pattern that does not compile is quoted and matched the same way, as
// Maestro's toRegexSafe does.
//
// ID: the same regex options, matched whole against the resource id or
// against the part after its last '/', so "login" finds "com.app:id/login"
// but not "com.app:id/login_button" (Filters.kt idMatches).

// MatchSelectorText reports whether pattern matches any of the values.
func MatchSelectorText(pattern string, values ...string) bool {
	if pattern == "" {
		return false
	}
	re := wholeRegex(pattern)
	for _, v := range values {
		if v == "" {
			continue
		}
		flat := strings.ReplaceAll(v, "\n", " ")
		if re.MatchString(v) || re.MatchString(flat) || v == pattern || flat == pattern {
			return true
		}
	}
	return false
}

// MatchSelectorTextExactCase reports whether pattern matches a value in its
// own case, so of several matches the one written as in the selector wins.
func MatchSelectorTextExactCase(pattern string, values ...string) bool {
	if LooksLikeRegex(pattern) {
		re, err := regexp.Compile(`(?ms)\A(?:` + pattern + `)\z`)
		if err != nil {
			return false
		}
		for _, v := range values {
			if v != "" && (re.MatchString(v) || re.MatchString(strings.ReplaceAll(v, "\n", " "))) {
				return true
			}
		}
		return false
	}
	for _, v := range values {
		if v == pattern || strings.ReplaceAll(v, "\n", " ") == pattern {
			return true
		}
	}
	return false
}

// MatchSelectorID reports whether an id selector matches identifier, as
// Maestro's idMatches does: a case-insensitive regex over the whole id, or
// over the part after its last '/'. An invalid regex matches literally. A
// substring match let `id: Field_Password` hit DDG's Field_PasswordName and
// type the password into the title.
func MatchSelectorID(pattern, identifier string) bool {
	if pattern == "" || identifier == "" {
		return false
	}
	re := wholeRegex(pattern)
	return re.MatchString(identifier) || re.MatchString(identifier[strings.LastIndex(identifier, "/")+1:])
}

// MatchesTextMaestro reports whether a text selector matches any of an
// element's values (text, hint, accessibility text) under Maestro's rule.
// It is MatchSelectorText, named for the page-source matchers that share it.
func MatchesTextMaestro(pattern string, values ...string) bool {
	return MatchSelectorText(pattern, values...)
}

// MatchesIDMaestro reports whether an id selector matches id under Maestro's
// rule. It is MatchSelectorID, named for the page-source matchers.
func MatchesIDMaestro(pattern, id string) bool {
	return MatchSelectorID(pattern, id)
}

// wholeRegex compiles pattern to match a whole value with Maestro's options,
// quoted when it is not a valid regex.
func wholeRegex(pattern string) *regexp.Regexp {
	return regexp.MustCompile(`(?ims)\A(?:` + SelectorRegexSource(pattern) + `)\z`)
}

// SelectorRegexSource returns the regex a selector pattern stands for: the
// pattern itself or, when it does not compile, the pattern quoted so that it
// matches literally (Maestro's toRegexSafe falls back to Regex.escape).
//
// Validity is judged by Go's RE2. A pattern RE2 rejects but Java accepts, such
// as a lookahead or a backreference, is taken as literal text.
func SelectorRegexSource(pattern string) string {
	if _, err := regexp.Compile(`(?ims)\A(?:` + pattern + `)\z`); err != nil {
		return regexp.QuoteMeta(pattern)
	}
	return pattern
}

// UiAutomatorTextRegex returns the Java regex for a UiSelector textMatches /
// descriptionMatches query that matches a text selector as Maestro does.
// UiAutomator's *Matches calls Matcher.matches(), which already needs the
// whole value, so no anchors are added. With ignoreCase false it matches only
// in the pattern's own case, for trying that first (#151).
//
// Only the i, m and s flags are used: Android's regex engine is ICU, whose
// inline flags do not include Java's u. The result still needs escaping for
// the UiSelector string literal. The exact-equality fallback is not expressed
// (callers add a .text("...") query for it), nor is the line-breaks-as-spaces
// reading, which only the page-source matcher applies.
func UiAutomatorTextRegex(pattern string, ignoreCase bool) string {
	flags := `(?ms)`
	if ignoreCase {
		flags = `(?ims)`
	}
	return flags + `(?:` + SelectorRegexSource(pattern) + `)`
}

// UiAutomatorIDRegex returns the Java regex for a UiSelector resourceIdMatches
// query that matches an id selector as Maestro does: whole, ignoring case,
// with or without the package prefix. The prefix is anything up to a '/',
// which for a pattern that itself holds a '/' is looser than Maestro's split
// at the last one. A leading ^ and trailing $ are dropped, as they add
// nothing to a whole match and a ^ could not match after the prefix: `^login$`
// still finds "com.app:id/login".
func UiAutomatorIDRegex(pattern string) string {
	src := SelectorRegexSource(pattern)
	src = strings.TrimPrefix(src, "^")
	if strings.HasSuffix(src, "$") && !strings.HasSuffix(src, `\$`) {
		src = src[:len(src)-1]
	}
	return `(?ims)(?:.*/)?(?:` + src + `)`
}

// UiSelectorTextTiers returns the UiSelector method chains that find a text
// selector's element natively, as tiers tried in order; the queries in a tier
// are equally good. Every query matches whole, as Maestro does, so none can
// land on an element whose text merely contains the selector (#188):
//
//  1. text or description equal to the selector as written — Maestro's
//     literal fallback, which finds "$7.50" whose "$" anchors as a regex;
//  2. the selector as a regex in its own case, so `^SIGN OUT$` prefers the
//     "SIGN OUT" button over a "Sign out" row (#151) — left out for text
//     with no regex syntax at all, where it would repeat tier 1;
//  3. the selector as a regex ignoring case, as Maestro matches.
//
// withHint adds hintMatches, a DeviceLab-agent extension over an EditText's
// placeholder. A value matching only with its line breaks read as spaces is
// not found here; the page-source matcher finds it.
func UiSelectorTextTiers(text string, withHint bool) [][]string {
	if text == "" {
		return nil
	}
	lit := EscapeUiSelectorString(text)
	regexTier := func(ignoreCase bool) []string {
		re := EscapeUiSelectorString(UiAutomatorTextRegex(text, ignoreCase))
		tier := []string{`.textMatches("` + re + `")`, `.descriptionMatches("` + re + `")`}
		if withHint {
			tier = append(tier, `.hintMatches("`+re+`")`)
		}
		return tier
	}
	tiers := [][]string{{`.text("` + lit + `")`, `.description("` + lit + `")`}}
	if strings.ContainsAny(text, `.\^$*+?()[]{}|`) {
		tiers = append(tiers, regexTier(false))
	}
	return append(tiers, regexTier(true))
}

// UiSelectorIDTiers returns the UiSelector method chains that find an id
// selector's element natively: the id exactly as written first, then the
// Maestro match (UiAutomatorIDRegex). The exact query goes first because a
// resourceIdMatches that finds nothing on screen can make UiAutomator scroll
// a lazy list internally and return an element it happened to land on.
func UiSelectorIDTiers(id string) [][]string {
	if id == "" {
		return nil
	}
	return [][]string{
		{`.resourceId("` + EscapeUiSelectorString(id) + `")`},
		{`.resourceIdMatches("` + EscapeUiSelectorString(UiAutomatorIDRegex(id)) + `")`},
	}
}

// EscapeUiSelectorString escapes s for a double-quoted UiSelector string
// argument. Only quotes are escaped: backslashes pass through, so regex
// escapes such as \d and \. reach the regex engine unchanged.
func EscapeUiSelectorString(s string) string {
	return strings.ReplaceAll(s, `"`, `\"`)
}

// IsPlainSelectorText reports whether a text selector has no regex syntax
// other than '.', so that as a regex it matches every value equal to it
// ignoring case. A driver can look such text up with a native
// case-insensitive equality query and leave anything else to the page source.
func IsPlainSelectorText(text string) bool {
	return text != "" && !strings.ContainsAny(text, `\^$*+?()[]{}|`)
}

// LooksLikeRegex reports whether text uses regex syntax beyond a lone dot.
func LooksLikeRegex(text string) bool {
	for i := 0; i < len(text); i++ {
		c := text[i]
		if i > 0 && text[i-1] == '\\' {
			switch c {
			case '.', '*', '+', '?', '[', ']', '{', '}', '|', '(', ')', '^', '$', '\\':
				return true
			}
			continue
		}
		switch c {
		case '.':
			if i+1 < len(text) && (text[i+1] == '*' || text[i+1] == '+' || text[i+1] == '?') {
				return true
			}
		case '*', '+', '?', '[', ']', '{', '}', '|', '(', ')':
			return true
		case '^':
			if i == 0 {
				return true
			}
		case '$':
			if i == len(text)-1 {
				return true
			}
		}
	}
	return false
}

// LiteralNeedles returns lowercase substrings every match of pattern must
// contain, for coarse filtering on a device: the words of the pattern when
// plain, otherwise the words of its longest regex-free run (nil when there is
// none worth using). Words, not the phrase, because a match may break a line
// where the pattern has a space.
func LiteralNeedles(pattern string) []string {
	if pattern == "" {
		return nil
	}
	if !LooksLikeRegex(pattern) && !strings.Contains(pattern, ".") {
		return strings.Fields(strings.ToLower(pattern))
	}
	if strings.ContainsAny(pattern, "|[") {
		// Alternation and classes make any run optional or unknown.
		return nil
	}
	best, cur := "", []byte{}
	flush := func() {
		if len(cur) > len(best) {
			best = string(cur)
		}
		cur = cur[:0]
	}
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch {
		case c == '\\' && i+1 < len(pattern):
			i++
			if n := pattern[i]; (n >= 'a' && n <= 'z') || (n >= 'A' && n <= 'Z') || (n >= '0' && n <= '9') {
				flush() // \d, \s, \w…: a class, not a literal
			} else {
				cur = append(cur, n) // an escaped metacharacter is itself
			}
		case c == '*' || c == '?' || c == '{':
			// The preceding character is optional (a {n,m} count may be 0).
			if _, size := utf8.DecodeLastRune(cur); size > 0 {
				cur = cur[:len(cur)-size]
			}
			flush()
			if c == '{' {
				i = skipTo(pattern, i, '}')
			}
		case c == '(' && i+1 < len(pattern) && pattern[i+1] == '?':
			// (?i), (?:…), (?P<name>…): skip the group's head, or all of it.
			flush()
			i = skipTo(pattern, i, ')')
		case strings.IndexByte(".+}()^$", c) >= 0:
			flush()
		default:
			cur = append(cur, c)
		}
	}
	flush()
	if len(strings.TrimSpace(best)) < 3 {
		return nil
	}
	return strings.Fields(strings.ToLower(best))
}

// skipTo returns the index of the first end at or after i (the last index
// when there is none).
func skipTo(s string, i int, end byte) int {
	if j := strings.IndexByte(s[i:], end); j >= 0 {
		return i + j
	}
	return len(s) - 1
}
