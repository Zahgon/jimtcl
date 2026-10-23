/*
 * UTF-8 utility tests. The expected values are those of the C implementation
 * of Jim Tcl 0.84 (utf8.c and the tables generated from UnicodeData.txt).
 *
 * Copyright 2010-2016 Steve Bennett <steveb@workware.net.au>
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

package u8

import "testing"

func TestToUnicode(t *testing.T) {
	tests := []struct {
		s  string
		uc int
		n  int
	}{
		{"a", 'a', 1},
		{"é", 0xe9, 2},
		{"日", 0x65e5, 3},
		{"\U0001f600", 0x1f600, 4},
		{"", 0, 1},
		{"\xe9", 0xe9, 1},           // an invalid sequence is one byte
		{"\xc3", 0xc3, 1},           // truncated
		{"\xc0\x80", 0xc0, 1},       // overlong
		{"\xed\xa0\x80", 0xd800, 3}, // a surrogate is accepted, as in the C
	}
	for _, tc := range tests {
		uc, n := ToUnicode(tc.s)
		if uc != tc.uc || n != tc.n {
			t.Errorf("ToUnicode(%q) = %#x, %d; want %#x, %d", tc.s, uc, n, tc.uc, tc.n)
		}
		if bc, bn := ToUnicode([]byte(tc.s)); bc != uc || bn != n {
			t.Errorf("ToUnicode([]byte(%q)) = %#x, %d; want the string result", tc.s, bc, bn)
		}
	}
}

func TestFromUnicode(t *testing.T) {
	tests := []struct {
		uc int
		s  string
	}{
		{'a', "a"}, {0xe9, "é"}, {0x65e5, "日"}, {0x1f600, "\U0001f600"},
		// Code points above 0x1fffff are truncated to 21 bits, as in the C.
		{0x200041, "\xf0\x80\x81\x81"},
	}
	for _, tc := range tests {
		if got := string(FromUnicode(tc.uc)); got != tc.s {
			t.Errorf("FromUnicode(%#x) = %q, want %q", tc.uc, got, tc.s)
		}
	}
}

func TestCase(t *testing.T) {
	tests := []struct{ in, upper, lower, title int }{
		{'a', 'A', 'a', 'A'},
		{'A', 'A', 'a', 'A'},
		{0xe9, 0xc9, 0xe9, 0xc9},             // e acute
		{0xdf, 0xdf, 0xdf, 0xdf},             // sharp s has no simple upper case
		{0xb5, 0x39c, 0xb5, 0x39c},           // micro sign maps to capital mu
		{0x1f600, 0x1f600, 0x1f600, 0x1f600}, // outside the table
	}
	for _, tc := range tests {
		if got := Upper(tc.in); got != tc.upper {
			t.Errorf("Upper(%#x) = %#x, want %#x", tc.in, got, tc.upper)
		}
		if got := Lower(tc.in); got != tc.lower {
			t.Errorf("Lower(%#x) = %#x, want %#x", tc.in, got, tc.lower)
		}
		if got := Title(tc.in); got != tc.title {
			t.Errorf("Title(%#x) = %#x, want %#x", tc.in, got, tc.title)
		}
	}
}

func TestStrlenAndIndex(t *testing.T) {
	const s = "aé日z"
	if got := Strlen(s); got != 4 {
		t.Errorf("Strlen(%q) = %d, want 4", s, got)
	}
	for charindex, want := range map[int]int{0: 0, 1: 1, 2: 3, 3: 6, 4: 7} {
		if got := ByteIndex(s, charindex); got != want {
			t.Errorf("ByteIndex(%q, %d) = %d, want %d", s, charindex, got, want)
		}
	}
	// Past the end the C walks one byte per character; ByteIndex clamps.
	if got, want := Index(s, 6), 9; got != want {
		t.Errorf("Index(%q, 6) = %d, want %d", s, got, want)
	}
	if got, want := ByteIndex(s, 6), len(s); got != want {
		t.Errorf("ByteIndex(%q, 6) = %d, want %d", s, got, want)
	}
}

func TestByteAtAndSlice(t *testing.T) {
	if got := ByteAt("ab", 5); got != 0 {
		t.Errorf("ByteAt past the end = %q, want NUL", got)
	}
	if got := string(Slice([]byte("abc"), 5)); got != "" {
		t.Errorf("Slice past the end = %q, want empty", got)
	}
	if got := Slice("abc", 1); got != "bc" {
		t.Errorf("Slice(%q, 1) = %q, want \"bc\"", "abc", got)
	}
}
