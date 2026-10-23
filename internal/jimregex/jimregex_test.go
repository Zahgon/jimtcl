/* Jim Tcl 0.84 ported from C to Go: tests for Jim's regular expression engine.
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions
 * are met:
 *
 * 1. Redistributions of source code must retain the above copyright
 *    notice, this list of conditions and the following disclaimer.
 * 2. Redistributions in binary form must reproduce the above
 *    copyright notice, this list of conditions and the following
 *    disclaimer in the documentation and/or other materials
 *    provided with the distribution.
 *
 * THIS SOFTWARE IS PROVIDED BY THE JIM TCL PROJECT ``AS IS'' AND ANY
 * EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO,
 * THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A
 * PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE
 * JIM TCL PROJECT OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT,
 * INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES
 * (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
 * OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
 * HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT,
 * STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE)
 * ARISING IN ANY WAY OUT OF THE USE OF THIS SOFTWARE, EVEN IF
 * ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 *
 * The views and conclusions contained in the software and documentation
 * are those of the authors and should not be interpreted as representing
 * official policies, either expressed or implied, of the Jim Tcl Project.
 */

package jimregex

import (
	"fmt"
	"github.com/msteveb/jimtcl/internal/u8"
	"strings"
	"testing"
)

// format renders the result of Exec the way the test table spells it: one
// "start-end" byte range per group, "-" for a group that did not match and an
// empty string when the expression did not match at all.
func format(pmatch []int, matched bool) string {
	if !matched {
		return ""
	}
	parts := make([]string, 0, len(pmatch)/2)
	for i := 0; i < len(pmatch); i += 2 {
		if pmatch[i] == -1 {
			parts = append(parts, "-")
		} else {
			parts = append(parts, fmt.Sprintf("%d-%d", pmatch[i], pmatch[i+1]))
		}
	}
	return strings.Join(parts, " ")
}

// The expected values come from the original jimsh:
// regexp -indices -inline ?-option? -- pattern string
var execTests = []struct {
	pat   string
	s     string
	flags int
	want  string
}{
	{"a.c", "xabcx", 0, "1-4"},
	{"a.c", "abd", 0, ""},
	{"(a+)(b*)", "aaab", 0, "0-4 0-3 3-4"},
	{"(a)(b)?", "a", 0, "0-1 0-1 -"},
	{"^abc", "xabc", 0, ""},
	{"abc$", "abcx", 0, ""},
	{"^$", "", 0, "0-0"},
	{"a|b|c", "zbc", 0, "1-2"},
	{"(a|b)+", "xabba", 0, "1-5 4-5"},
	{"a*", "aaa", 0, "0-3"},
	{"a*?", "aaa", 0, "0-0"},
	{"a+?", "aaa", 0, "0-1"},
	{"ba*?c", "baaac", 0, "0-5"},
	{"a{2,3}", "aaaa", 0, "0-3"},
	{"a{2,3}?", "aaaa", 0, "0-2"},
	{"(ab){2}", "ababab", 0, "0-4 2-4"},
	{"(ab){2,3}?", "ababab", 0, "0-4 2-4"},
	{"x{99}", "x", 0, ""},
	{"[abc]+", "xcabz", 0, "1-4"},
	{"[^abc]+", "abxyzc", 0, "2-5"},
	{"[]a]+", "a]b", 0, "0-2"},
	{"[-a]+", "-ab", 0, "0-2"},
	{"[[:upper:]]+", "aBCd", 0, "1-3"},
	{"[[:xdigit:]]+", "zzfa0z", 0, "2-5"},
	{"[[:punct:]]+", "ab,.!cd", 0, "2-5"},
	{"[[:alpha:]0-9]+", ".a0.", 0, "1-3"},
	{`[\d]+`, "ab12", 0, "2-4"},
	{`[\w]+`, ".a_1.", 0, "1-4"},
	{"[z-a]+", "xyz", 0, "0-3"},
	{`\d+`, "x123", 0, "1-4"},
	{`\D+`, "12ab34", 0, "2-4"},
	{`\w+`, "-ab_1-", 0, "1-5"},
	{`\s+`, "a b", 0, "1-2"},
	{`\S+`, " ab ", 0, "1-3"},
	{`a\Z`, "ba", 0, "1-2"},
	{`\Ab`, "ab", 0, ""},
	{`\<ab\>`, "xx ab yy", 0, "3-5"},
	{`\mabc\M`, "x abc y", 0, "2-5"},
	{`\x41`, "A", 0, "0-1"},
	{`\u0041`, "A", 0, "0-1"},
	{`\u{41}`, "A", 0, "0-1"},
	{`\.`, "a.b", 0, "1-2"},
	{`\*`, "a*b", 0, "1-2"},
	{`\q`, "q", 0, "0-1"},
	{"abc", "ABC", ICase, "0-3"},
	{"[a-c]+", "ABC", ICase, "0-3"},
	{"[[:upper:]]+", "abc", ICase, "0-3"},
	{"[[:lower:]]+", "ABC", ICase, ""},
	{"(a)(A)", "aA", ICase, "0-2 0-1 1-2"},
	{"a.c", "a\nc", 0, "0-3"},
	{"a.c", "a\nc", NewlineStop, ""},
	{"^b", "a\nb", NewlineAnchor, "2-3"},
	{"b$", "b\na", Newline, "0-1"},
	{"(?:a)(b)", "ab", 0, "0-2 1-2"},
	{"(?:a|b)+", "ab", 0, "0-2"},
	{"(a)|(b)", "b", 0, "0-1 - 0-1"},
	{"((a)(b))+", "abab", 0, "0-4 2-4 2-3 3-4"},
	{"(a|ab)(c|bcd)", "abcd", 0, "0-4 0-1 1-4"},
	{"(a*)(a*)", "aaa", 0, "0-3 0-3 3-3"},
	{"a b c", "abc", Expanded, "0-3"},
	{"a*bcd", "xxbcd", 0, "2-5"},
	{"x*hello", "say hello", 0, "4-9"},

	// Multi-byte characters: the offsets are byte offsets, and -nocase maps
	// the whole of the basic multilingual plane.
	{".", "ébc", 0, "0-2"},
	{"..", "ébc", 0, "0-3"},
	{"[^é]+", "ééab", 0, "4-6"},
	{"é{2}", "ééé", 0, "0-4"},
	{"É", "é", ICase, "0-2"},
	{"[a-é]", "É", ICase, "0-2"},
	{".", "\xc3", 0, "0-1"},
}

func TestExec(t *testing.T) {
	for _, tc := range execTests {
		re, err := Compile(tc.pat, Extended|tc.flags)
		if err != nil {
			t.Errorf("Compile(%q, %d): %v", tc.pat, tc.flags, err)
			continue
		}
		if got := format(re.Exec(tc.s, 0, false)); got != tc.want {
			t.Errorf("Exec(%q, %q, %d) = %q, want %q", tc.pat, tc.s, tc.flags, got, tc.want)
		}
	}
}

// TestExecOffset covers matching from an offset, where the subject is taken to
// start at the offset: [regexp -start] relies on that.
func TestExecOffset(t *testing.T) {
	tests := []struct {
		pat    string
		s      string
		start  int
		notbol bool
		want   string
	}{
		{"a", "aaa", 1, true, "1-2"},
		{"^a", "aaa", 1, false, "1-2"},
		{"^a", "aaa", 1, true, ""},
		{`\Aa`, "aaa", 1, false, "1-2"},
		{`\Aa`, "aaa", 1, true, ""},
		{".", "ébc", 2, true, "2-3"},
		{"a", "aaa", 3, true, ""},
	}
	for _, tc := range tests {
		re, err := Compile(tc.pat, Extended)
		if err != nil {
			t.Errorf("Compile(%q): %v", tc.pat, err)
			continue
		}
		if got := format(re.Exec(tc.s, tc.start, tc.notbol)); got != tc.want {
			t.Errorf("Exec(%q, %q, %d, %v) = %q, want %q", tc.pat, tc.s, tc.start, tc.notbol, got, tc.want)
		}
	}
}

// TestCompileError checks the error texts of jim_regerror(), which [regexp]
// reports as "couldn't compile regular expression pattern: ...".
func TestCompileError(t *testing.T) {
	tests := []struct{ pat, want string }{
		{"a(", "parentheses () not balanced"},
		{")", "parentheses () not balanced"},
		{"(", "parentheses () not balanced"},
		{"a{2", "braces {} not balanced"},
		{"a{2,3", "braces {} not balanced"},
		{"a{2,1}", "invalid repetition count(s)"},
		{"a{100}", "invalid repetition count(s)"},
		{"a{2,100}", "invalid repetition count(s)"},
		{"*a", "count follows nothing"},
		{"+a", "count follows nothing"},
		{"?a", "count follows nothing"},
		{"a**", "nested count"},
		{"a{2}{3}", "nested count"},
		{"(a*)*", "*+ of empty atom"},
		{"()*", "*+ of empty atom"},
		{"[a", "brackets [] not balanced"},
		{"[]", "brackets [] not balanced"},
		{"[[:alpha:]", "brackets [] not balanced"},
		{`[\x00]`, "contains null char"},
		{`[a\`, "invalid escape \\ sequence"},
		{strings.Repeat("(", 100) + "a" + strings.Repeat(")", 100), "too big"},
	}
	for _, tc := range tests {
		_, err := Compile(tc.pat, Extended)
		if err == nil {
			t.Errorf("Compile(%q): no error, want %q", tc.pat, tc.want)
			continue
		}
		if err.Error() != tc.want {
			t.Errorf("Compile(%q): error %q, want %q", tc.pat, err, tc.want)
		}
	}
}

func TestMaxSub(t *testing.T) {
	tests := []struct {
		pat  string
		want int
	}{
		{"abc", 0},
		{"(a)(b)", 2},
		{"(?:a)(b)", 1},
		{"((a))", 2},
	}
	for _, tc := range tests {
		re, err := Compile(tc.pat, Extended)
		if err != nil {
			t.Errorf("Compile(%q): %v", tc.pat, err)
			continue
		}
		if got := re.MaxSub(); got != tc.want {
			t.Errorf("MaxSub(%q) = %d, want %d", tc.pat, got, tc.want)
		}
	}
}

// TestUtf8Upper checks the case folding used by ICase against the table the C
// generates from UnicodeData.txt.
func TestUtf8Upper(t *testing.T) {
	tests := [][2]int{
		{'a', 'A'}, {'A', 'A'}, {'1', '1'}, {0xe9, 0xc9}, {0xc9, 0xc9},
		{0xdf, 0xdf}, {0xb5, 0x39c}, {0x131, 'I'}, {0x1c5, 0x1c4},
		{0x345, 0x345}, {0x2170, 0x2170}, {0x24d0, 0x24d0}, {0x10428, 0x10428},
	}
	for _, tc := range tests {
		if got := u8.Upper(tc[0]); got != tc[1] {
			t.Errorf("u8.Upper(%#x) = %#x, want %#x", tc[0], got, tc[1])
		}
	}
}
