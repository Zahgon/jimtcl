/* Jim Tcl 0.84 ported from C to Go: tests for the string area.
 *
 * Copyright 2008 Steve Bennett <steveb@workware.net.au>
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

package jim

import (
	"testing"

	"github.com/msteveb/jimtcl/internal/u8"
)

// newStringTestInterp returns an interpreter holding no state, which is all the
// string functions need: none of them look at the command table or the call
// frames.
func newStringTestInterp() *Interp { return &Interp{} }

func TestUtf8ToUnicode(t *testing.T) {
	tests := []struct {
		in string
		uc int
		n  int
	}{
		{"", 0, 1},
		{"a", 'a', 1},
		{"é", 0xe9, 2},
		{"中", 0x4e2d, 3},
		{"\U0001f600", 0x1f600, 4},
		{"\x7f", 0x7f, 1},
		{"\x80", 0x80, 1},     // a stray continuation byte is its own character
		{"\xbf", 0xbf, 1},     // likewise
		{"\xc3", 0xc3, 1},     // truncated two-byte sequence
		{"\xc3(", 0xc3, 1},    // bad continuation byte
		{"\xc0\x80", 0xc0, 1}, // overlong encoding of NUL
		{"\xe0\x80\x80", 0xe0, 1},
		{"\xe0\xa0", 0xe0, 1},
		{"\xf0\x80\x80\x80", 0xf0, 1},
		{"\xf0\x9f", 0xf0, 1},
		{"\xff", 0xff, 1},
		{"\xf8\x80\x80\x80", 0xf8, 1}, // five-byte lead byte is not supported
	}
	for _, tc := range tests {
		uc, n := u8.ToUnicode([]byte(tc.in))
		if uc != tc.uc || n != tc.n {
			t.Errorf("u8.ToUnicode(%q) = %#x, %d; want %#x, %d", tc.in, uc, n, tc.uc, tc.n)
		}
	}
}

func TestUtf8FromUnicode(t *testing.T) {
	tests := []struct {
		uc   int
		want string
	}{
		{0, "\x00"},
		{'a', "a"},
		{0x7f, "\x7f"},
		{0x80, "\u0080"},
		{0x7ff, "߿"},
		{0x800, "ࠀ"},
		{0xffff, "￿"},
		{0x1f600, "\U0001f600"},
		{0x1fffff, "\xf7\xbf\xbf\xbf"},
		{0x200041, "\xf0\x80\x81\x81"}, // silently truncated to 21 bits
	}
	for _, tc := range tests {
		if got := string(u8.FromUnicode(tc.uc)); got != tc.want {
			t.Errorf("u8.FromUnicode(%#x) = %q; want %q", tc.uc, got, tc.want)
		}
	}
}

func TestUtf8Lengths(t *testing.T) {
	tests := []struct {
		in           string
		strlen, idx1 int
	}{
		{"", 0, 1},
		{"abc", 3, 1},
		{"éé", 2, 2},
		{"a中b", 3, 1},
		{"\U0001f600", 1, 4},
		// An invalid lead byte counts as one character but u8.Index() still
		// steps over the number of bytes the sequence would have had.
		{"\xe0", 1, 3},
		{"\xc3", 1, 2},
		{"\x80", 1, 1},
	}
	for _, tc := range tests {
		if got := u8.Strlen([]byte(tc.in)); got != tc.strlen {
			t.Errorf("u8.Strlen(%q) = %d; want %d", tc.in, got, tc.strlen)
		}
		if got := u8.Index([]byte(tc.in), 1); got != tc.idx1 {
			t.Errorf("u8.Index(%q, 1) = %d; want %d", tc.in, got, tc.idx1)
		}
	}
	if got := u8.Index([]byte("ab"), 0); got != 0 {
		t.Errorf("u8.Index(ab, 0) = %d; want 0", got)
	}
	if got := u8.PrevLen([]byte("aé"), 3, 3); got != 2 {
		t.Errorf("u8.PrevLen = %d; want 2", got)
	}
}

func TestUnicodeCase(t *testing.T) {
	// Expected values taken from [string toupper/tolower/totitle] of the
	// original jimsh, which maps only the Basic Multilingual Plane.
	tests := []struct{ cp, upper, lower, title int }{
		{'a', 'A', 'a', 'A'},
		{'A', 'A', 'a', 'A'},
		{'9', '9', '9', '9'},
		{0x00df, 0x00df, 0x00df, 0x00df}, // sharp s has no single-char upper case
		{0x00e9, 0x00c9, 0x00e9, 0x00c9},
		{0x01c4, 0x01c4, 0x01c6, 0x01c5},
		{0x01c5, 0x01c4, 0x01c6, 0x01c5},
		{0x01f3, 0x01f1, 0x01f3, 0x01f2},
		{0x0131, 0x0049, 0x0131, 0x0049},
		{0x0130, 0x0130, 0x0069, 0x0130},
		{0x017f, 0x0053, 0x017f, 0x0053},
		{0x00b5, 0x039c, 0x00b5, 0x039c},
		{0x212b, 0x212b, 0x00e5, 0x212b},
		{0x10d0, 0x1c90, 0x10d0, 0x10d0}, // title case is the code point itself
		{0x4e2d, 0x4e2d, 0x4e2d, 0x4e2d},
		{0x1f600, 0x1f600, 0x1f600, 0x1f600}, // above U+FFFF: unchanged
	}
	for _, tc := range tests {
		if got := u8.Upper(tc.cp); got != tc.upper {
			t.Errorf("u8.Upper(%#x) = %#x; want %#x", tc.cp, got, tc.upper)
		}
		if got := u8.Lower(tc.cp); got != tc.lower {
			t.Errorf("u8.Lower(%#x) = %#x; want %#x", tc.cp, got, tc.lower)
		}
		if got := u8.Title(tc.cp); got != tc.title {
			t.Errorf("u8.Title(%#x) = %#x; want %#x", tc.cp, got, tc.title)
		}
	}
}

func TestGlobMatch(t *testing.T) {
	// Expected values taken from [string match ?-nocase?] of the original.
	tests := []struct {
		pattern, s     string
		want, wantCase bool
	}{
		{"", "", true, true},
		{"", "a", false, false},
		{"*", "", true, true},
		{"*", "abc", true, true},
		{"?", "", false, false},
		{"?", "a", true, true},
		{"?*", "", true, true}, // the C keeps matching with a negative length
		{"*?", "", false, false},
		{"**", "", true, true},
		{"a*", "abc", true, true},
		{"*a", "bca", true, true},
		{"a?c", "abc", true, true},
		{"**a", "ba", true, true},
		{"a**", "ab", true, true},
		{"A*", "abc", false, true},
		{"[abc]", "b", true, true},
		{"[a-c]", "b", true, true},
		{"[c-a]", "b", true, true}, // reversed ranges match too
		{"[^ab]", "^", true, true}, // '^' is not special in a glob charset
		{"[^ab]", "c", false, false},
		{"[]a]", "a", false, false}, // a leading ']' ends the set
		{`[a\]]`, "a", false, false},
		{`[\]`, `\`, true, true}, // a backslash in a set matches itself
		{"[", "a", false, false},
		{"[a", "a", true, true},
		{"a[bc]d", "abcd", false, false},
		{"[]", "]", false, false},
		{`\*`, "*", true, true},
		{`\*`, "a", false, false},
		{`a\`, "a", false, false},
		{"[é]", "é", true, true},
		{"[à-ÿ]x", "éx", true, true},
		{"É", "é", false, true},
	}
	for _, tc := range tests {
		if got := globMatch([]byte(tc.pattern), []byte(tc.s), false); got != tc.want {
			t.Errorf("globMatch(%q, %q) = %v; want %v", tc.pattern, tc.s, got, tc.want)
		}
		if got := globMatch([]byte(tc.pattern), []byte(tc.s), true); got != tc.wantCase {
			t.Errorf("globMatch(%q, %q, nocase) = %v; want %v", tc.pattern, tc.s, got, tc.wantCase)
		}
	}
}

func TestStringCompareUtf8(t *testing.T) {
	tests := []struct {
		a, b   string
		nocase bool
		want   int
	}{
		{"", "", false, 0},
		{"", "a", false, -1},
		{"a", "", false, 1},
		{"abc", "abc", false, 0},
		{"abc", "abd", false, -1},
		{"abd", "abc", false, 1},
		{"ABC", "abc", false, -1},
		{"ABC", "abc", true, 0},
		{"é", "É", false, 1},
		{"é", "É", true, 0},
		// Comparison is per character, so a two-byte character is greater than
		// any one-byte one even though its first byte would compare smaller.
		{"é", "\x7f", false, 1},
		{"ab", "abc", false, -1},
	}
	for _, tc := range tests {
		if got := stringCompareUtf8([]byte(tc.a), []byte(tc.b), tc.nocase); got != tc.want {
			t.Errorf("stringCompareUtf8(%q, %q, %v) = %d; want %d", tc.a, tc.b, tc.nocase, got, tc.want)
		}
	}
	// [string compare -length 2 abc abd] truncates to characters, not bytes
	if got := strCompareUtf8([]byte("abc"), 2, []byte("abd"), 2, false); got != 0 {
		t.Errorf("strCompareUtf8 truncated = %d; want 0", got)
	}
}

func TestStringFirstLast(t *testing.T) {
	first := func(needle, haystack string, idx int) int {
		n, h := []byte(needle), []byte(haystack)
		return stringFirst(n, u8.Strlen(n), h, u8.Strlen(h), idx)
	}
	last := func(needle, haystack string, idx int) int {
		n, h := []byte(needle), []byte(haystack)
		return stringLastUtf8(n, u8.Strlen(n), h, idx)
	}
	tests := []struct {
		needle, haystack string
		idx, wantFirst   int
	}{
		{"", "abc", 0, -1},
		{"a", "", 0, -1},
		{"a", "abcabc", 0, 0},
		{"a", "abcabc", 3, 3},
		{"a", "abcabc", 7, -1},
		{"bc", "abcabc", 0, 1},
		{"abcd", "abc", 0, -1},
		{"é", "aéb", 0, 1},
		{"b", "aéb", 0, 2},
		// The needle's byte length is measured with u8.Index(), so an invalid
		// sequence can never be found.
		{"\xe0", "abc\xe0def", 0, -1},
		{"\xc3", "ab\xc3", 0, 2},
	}
	for _, tc := range tests {
		if got := first(tc.needle, tc.haystack, tc.idx); got != tc.wantFirst {
			t.Errorf("first(%q, %q, %d) = %d; want %d", tc.needle, tc.haystack, tc.idx, got, tc.wantFirst)
		}
	}
	lastTests := []struct {
		needle, haystack string
		idx, want        int
	}{
		{"a", "abcabc", 6, 3},
		{"a", "abcabc", 3, 0},
		{"a", "abcabc", 0, -1},
		{"", "abc", 3, -1},
		{"é", "aébé", 4, 3},
		{"\xe0", "a\xe0b", 3, -1},
	}
	for _, tc := range lastTests {
		if got := last(tc.needle, tc.haystack, tc.idx); got != tc.want {
			t.Errorf("last(%q, %q, %d) = %d; want %d", tc.needle, tc.haystack, tc.idx, got, tc.want)
		}
	}
}

func TestRelToAbsRange(t *testing.T) {
	tests := []struct {
		length, first, last       int
		wantFirst, wantLast, want int
	}{
		{3, 0, 2, 0, 2, 3},
		{3, 1, 1, 1, 1, 1},
		{3, 2, 1, 2, 1, 0},
		{3, -2, 1, 0, 1, 2},
		{3, 1, 9, 1, 2, 2},
		{3, -5, -2, 0, -2, 0},
		// The C computes the range length in a 32-bit int, which wraps twice
		// here and leaves the whole string selected.
		{3, -strIntMax, strIntMax, 0, 2, 3},
	}
	for _, tc := range tests {
		f, l, n := strRelToAbsRange(tc.length, tc.first, tc.last)
		if f != tc.wantFirst || l != tc.wantLast || n != tc.want {
			t.Errorf("strRelToAbsRange(%d, %d, %d) = %d, %d, %d; want %d, %d, %d",
				tc.length, tc.first, tc.last, f, l, n, tc.wantFirst, tc.wantLast, tc.want)
		}
	}
	if got := strRelToAbsIndex(3, -1); got != 2 {
		t.Errorf("strRelToAbsIndex(3, -1) = %d; want 2", got)
	}
	if got := strRelToAbsIndex(3, -strIntMax); got != -strIntMax {
		t.Errorf("strRelToAbsIndex(3, -INT_MAX) = %d; want -INT_MAX", got)
	}
}

func TestStringCaseConversion(t *testing.T) {
	i := newStringTestInterp()
	tests := []struct{ in, lower, upper, title string }{
		{"", "", "", ""},
		{"aBcD", "abcd", "ABCD", "Abcd"},
		{"éa", "éa", "ÉA", "Éa"},
		{"ß", "ß", "ß", "ß"},
		{"ǅx", "ǆx", "ǄX", "ǅx"},
		{"ა", "ა", "Ა", "ა"},
		// The C builds the result as a C string, so it stops at the first null
		{"a\x00b", "a", "A", "A"},
		{"\x00ab", "", "", ""},
		// An invalid byte decodes as its own code point and is then written back
		// as UTF-8, so case conversion lengthens the string
		{"\xe0z", "\u00e0z", "\u00c0Z", "\u00c0z"},
	}
	for _, tc := range tests {
		o := i.NewStringObj(tc.in)
		if got := i.stringToLower(o).String(); got != tc.lower {
			t.Errorf("tolower(%q) = %q; want %q", tc.in, got, tc.lower)
		}
		if got := i.stringToUpper(o).String(); got != tc.upper {
			t.Errorf("toupper(%q) = %q; want %q", tc.in, got, tc.upper)
		}
		if got := i.stringToTitle(o).String(); got != tc.title {
			t.Errorf("totitle(%q) = %q; want %q", tc.in, got, tc.title)
		}
	}
}

func TestStringTrim(t *testing.T) {
	i := newStringTestInterp()
	tests := []struct {
		in, chars         string
		useChars          bool
		left, right, both string
	}{
		{" \tabc\n ", "", false, "abc\n ", " \tabc", "abc"},
		{"xxaxx", "x", true, "axx", "xxa", "a"},
		{"xxx", "x", true, "", "", ""},
		{"abc", "", true, "abc", "abc", "abc"},
		{"éaé", "é", true, "aé", "éa", "a"},
		{"a\x00", "", false, "a\x00", "a", "a"}, // the default set includes the null
	}
	for _, tc := range tests {
		var chars *Obj
		if tc.useChars {
			chars = i.NewStringObj(tc.chars)
		}
		o := i.NewStringObj(tc.in)
		if got := i.stringTrimLeft(o, chars).String(); got != tc.left {
			t.Errorf("trimleft(%q) = %q; want %q", tc.in, got, tc.left)
		}
		if got := i.stringTrimRight(o, chars).String(); got != tc.right {
			t.Errorf("trimright(%q) = %q; want %q", tc.in, got, tc.right)
		}
		if got := i.stringTrim(o, chars).String(); got != tc.both {
			t.Errorf("trim(%q) = %q; want %q", tc.in, got, tc.both)
		}
	}
}

func TestNewStringObjUtf8(t *testing.T) {
	i := newStringTestInterp()
	tests := []struct {
		in      string
		charlen int
		want    string
	}{
		{"abcde", 0, ""},
		{"abcde", 3, "abc"},
		{"\u00e9\u00e9", 1, "\u00e9"},
		{"\u00e9\u00e9", 2, "\u00e9\u00e9"},
		{"\xc3(", 1, "\xc3("}, // u8.Index() says the sequence is two bytes long
		// u8.Index() steps over a truncated sequence as if it were complete,
		// so the C copies the null terminator into the result
		{"\xe0", 1, "\xe0\x00"},
		{"\xe0\xa0", 1, "\xe0\xa0\x00"},
	}
	for _, tc := range tests {
		o := i.NewStringObjUtf8([]byte(tc.in), tc.charlen)
		if got := o.String(); got != tc.want {
			t.Errorf("NewStringObjUtf8(%q, %d) = %q; want %q", tc.in, tc.charlen, got, tc.want)
		}
		if o.charLen != tc.charlen {
			t.Errorf("NewStringObjUtf8(%q, %d) charLen = %d", tc.in, tc.charlen, o.charLen)
		}
	}
	rangeTests := []struct {
		in              string
		first, rangeLen int
		want            string
	}{
		{"abc", 0, 3, "abc"},
		{"abc", 1, 2, "bc"},
		{"abc", 1, 0, ""},
		{"abc", 5, 2, ""},     // first past the end reads nothing
		{"abc", 3, 1, "\x00"}, // first at the end reads the terminator
		{"abc", 2, 4, "c\x00"},
	}
	for _, tc := range rangeTests {
		if got := string(strRangeBytes([]byte(tc.in), tc.first, tc.rangeLen)); got != tc.want {
			t.Errorf("strRangeBytes(%q, %d, %d) = %q; want %q", tc.in, tc.first, tc.rangeLen, got, tc.want)
		}
	}
}

func TestStringAppendAndLength(t *testing.T) {
	i := newStringTestInterp()
	o := i.NewStringObj("é")
	if got := i.Utf8Length(o); got != 1 {
		t.Errorf("Utf8Length = %d; want 1", got)
	}
	i.AppendString(o, "ab")
	if got, want := o.String(), "éab"; got != want {
		t.Errorf("after AppendString = %q; want %q", got, want)
	}
	if got := i.Utf8Length(o); got != 3 {
		t.Errorf("Utf8Length after append = %d; want 3", got)
	}
	// A duplicate must not share the spare capacity of the original's buffer
	dup := o.dup()
	i.AppendString(dup, "X")
	i.AppendString(o, "Y")
	if got, want := dup.String(), "éabX"; got != want {
		t.Errorf("dup = %q; want %q", got, want)
	}
	if got, want := o.String(), "éabY"; got != want {
		t.Errorf("orig = %q; want %q", got, want)
	}
	if !i.StringEqObj(i.NewStringObj("abc"), i.NewStringObj("abc")) {
		t.Error("StringEqObj(abc, abc) = false")
	}
	if i.StringEqObj(i.NewStringObj("abc"), i.NewStringObj("abd")) {
		t.Error("StringEqObj(abc, abd) = true")
	}
}

func TestStringClassPredicates(t *testing.T) {
	// The "C" locale predicates: nothing above 0x7f belongs to a class.
	tests := []struct {
		c                                      byte
		alpha, digit, space, xdigit, punct, pr bool
	}{
		{'a', true, false, false, true, false, true},
		{'z', true, false, false, false, false, true},
		{'A', true, false, false, true, false, true},
		{'5', false, true, false, true, false, true},
		{' ', false, false, true, false, false, true},
		{'\t', false, false, true, false, false, false},
		{'\v', false, false, true, false, false, false},
		{'.', false, false, false, false, true, true},
		{'\x00', false, false, false, false, false, false},
		{'\x7f', false, false, false, false, false, false},
		{0xe9, false, false, false, false, false, false},
	}
	for _, tc := range tests {
		for _, p := range []struct {
			name string
			fn   func(byte) bool
			want bool
		}{
			{"alpha", strIsAlphaByte, tc.alpha},
			{"digit", strIsDigitByte, tc.digit},
			{"space", u8.IsSpace, tc.space},
			{"xdigit", strIsXdigitByte, tc.xdigit},
			{"punct", strIsPunctByte, tc.punct},
			{"print", strIsPrintByte, tc.pr},
		} {
			if got := p.fn(tc.c); got != p.want {
				t.Errorf("%s(%#x) = %v; want %v", p.name, tc.c, got, p.want)
			}
		}
	}
}
