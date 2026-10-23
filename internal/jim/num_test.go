/* Jim Tcl 0.84 ported from C to Go: tests for the number area.
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
	"math"
	"strings"
	"testing"
)

// numTestInterp returns an interpreter without NewInterp(), which registers
// every built-in command and so needs the whole interpreter. Nothing in this
// area looks at interpreter state beyond the result object.
func numTestInterp() *Interp { return &Interp{} }

// expected values below are taken from the original jimsh build.

func TestFormatWideAndDouble(t *testing.T) {
	for _, c := range []struct {
		in   int64
		want string
	}{
		{0, "0"}, {1, "1"}, {-1, "-1"}, {math.MaxInt64, "9223372036854775807"},
		{math.MinInt64, "-9223372036854775808"},
	} {
		if got := formatWide(c.in); got != c.want {
			t.Errorf("formatWide(%d) = %q, want %q", c.in, got, c.want)
		}
	}

	negZero := math.Copysign(0, -1)
	for _, c := range []struct {
		in   float64
		want string
	}{
		{1, "1.0"}, {0, "0.0"}, {negZero, "-0.0"}, {0.1, "0.1"},
		{1.0 / 3, "0.333333333333"}, {1e22, "1e+22"}, {1e-7, "1e-07"},
		{1e30, "1e+30"}, {1e11, "100000000000.0"}, {1e12, "1e+12"},
		{math.Inf(1), "Inf"}, {math.Inf(-1), "-Inf"}, {math.NaN(), "NaN"},
	} {
		if got := formatDouble(c.in); got != c.want {
			t.Errorf("formatDouble(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestStringToWide(t *testing.T) {
	for _, c := range []struct {
		in   string
		base int
		want int64
		ok   bool
	}{
		{"42", 0, 42, true}, {"-42", 0, -42, true}, {"+42", 0, 42, true},
		{" 42 ", 0, 42, true}, {"", 0, 0, false}, {"  ", 0, 0, false},
		{"42abc", 0, 0, false}, {"3.0", 0, 0, false},
		{"0x10", 0, 16, true}, {"0X10", 0, 16, true}, {"-0x10", 0, -16, true},
		{"0o17", 0, 15, true}, {"0b1011", 0, 11, true}, {"0d99", 0, 99, true},
		{"010", 0, 10, true}, {"0x-5", 0, 0, false}, {"0x", 0, 0, false},
		{"0x0x10", 0, 16, true}, {"  -0b11  ", 0, -3, true},
		{"9223372036854775807", 0, math.MaxInt64, true},
		{"9223372036854775808", 0, math.MinInt64, true},
		{"18446744073709551615", 0, -1, true},
		{"99999999999999999999", 0, -1, true},
		{"-99999999999999999999", 0, -1, true},
		{"-9223372036854775809", 0, math.MaxInt64, true},
		{"-0x10000000000000000", 0, 1, true},
		{"0x10", 10, 0, false}, {"ff", 16, 255, true}, {"0x10", 16, 16, true},
	} {
		got, ok, _ := stringToWide(c.in, c.base)
		if ok != c.ok || ok && got != c.want {
			t.Errorf("stringToWide(%q, %d) = %d, %v, want %d, %v", c.in, c.base, got, ok, c.want, c.ok)
		}
	}
}

func TestStringToDouble(t *testing.T) {
	for _, c := range []struct {
		in   string
		want float64
		ok   bool
	}{
		{"1.5", 1.5, true}, {".5", 0.5, true}, {"5.", 5, true}, {"+.5", 0.5, true},
		{" 1.5 ", 1.5, true}, {"1.5e", 0, false}, {"1_0", 0, false},
		{"0x10", 16, true}, {"0x1p3", 8, true}, {"0X1.8P1", 3, true}, {"0x", 0, false},
		{"inf", math.Inf(1), true}, {"INF", math.Inf(1), true},
		{"infinity", math.Inf(1), true}, {"-inf", math.Inf(-1), true},
		{"1e400", math.Inf(1), true}, {"1e-400", 0, true},
		{"0b101", 0, false}, {"0o17", 0, false}, {"099", 99, true},
	} {
		got, ok := stringToDouble(c.in)
		if ok != c.ok || ok && got != c.want {
			t.Errorf("stringToDouble(%q) = %v, %v, want %v, %v", c.in, got, ok, c.want, c.ok)
		}
	}
	if v, ok := stringToDouble("nan"); !ok || !math.IsNaN(v) {
		t.Errorf("stringToDouble(\"nan\") = %v, %v", v, ok)
	}
}

// numTestFormat runs [format] through the interpreter API, which is the only
// way to reach the XPG3 "%n$" specifiers without the engine's substitutions.
func numTestFormat(t *testing.T, i *Interp, format string, args ...string) string {
	t.Helper()
	argv := make([]*Obj, len(args))
	for n, a := range args {
		argv[n] = i.NewStringObj(a)
	}
	o, rc := i.FormatString(i.NewStringObj(format), argv)
	if rc != OK {
		return "ERR: " + i.Result().String()
	}
	return o.String()
}

func TestFormatString(t *testing.T) {
	i := numTestInterp()
	for _, c := range []struct {
		format string
		args   []string
		want   string
	}{
		{"%d", []string{"3"}, "3"},
		{"%d", []string{"0x10"}, "16"},
		{"%x", []string{"-1"}, "ffffffffffffffff"},
		{"%X", []string{"-1"}, "FFFFFFFFFFFFFFFF"},
		{"%o", []string{"-1"}, "1777777777777777777777"},
		{"%u", []string{"-1"}, "18446744073709551615"},
		{"%#x", []string{"26"}, "0x1a"},
		{"%#x", []string{"0"}, "0"},
		{"%#o", []string{"8"}, "010"},
		{"%.0d", []string{"0"}, ""},
		{"%.5d", []string{"-42"}, "-00042"},
		{"%05.3d", []string{"7"}, "  007"},
		{"%-+ 0#5d", []string{"7"}, "+7   "},
		{"%b", []string{"10"}, "1010"},
		{"%08b", []string{"5"}, "00000101"},
		{"%-8b", []string{"5"}, "101     "},
		{"%c", []string{"955"}, "λ"},
		{"%5c", []string{"955"}, "    λ"},
		{"%5s", []string{"é"}, "    é"},
		{"%.1s", []string{"éabc"}, "é"},
		{"%05s", []string{"ab"}, "000ab"},
		{"%f", []string{"1.5"}, "1.500000"},
		{"%e", []string{"3"}, "3.000000e+00"},
		{"%g", []string{"1000000"}, "1e+06"},
		{"%G", []string{"1e-22"}, "1E-22"},
		{"%.17g", []string{"0.1"}, "0.10000000000000001"},
		{"%#.0e", []string{"1.0"}, "1.e+00"},
		{"%#g", []string{"0"}, "0.00000"},
		{"%#.0f", []string{"1.0"}, "1."},
		{"%08.2f", []string{"-1.5"}, "-0001.50"},
		{"%010f", []string{"1e400"}, "       inf"},
		{"%f", []string{"-1e400"}, "-inf"},
		{"%hd", []string{"65537"}, "1"},
		{"%hu", []string{"-1"}, "65535"},
		{"%*d", []string{"5", "42"}, "   42"},
		{"%*d", []string{"-5", "42"}, "42   "},
		{"%.*f", []string{"2", "3.14159"}, "3.14"},
		{"%*5d", []string{"3", "7"}, "  7"},
		{"%%", nil, "%"},
		{"%%d", nil, "%d"},
		{"abc", nil, "abc"},
		{"", nil, ""},
		{"%s%s", []string{"a", "b"}, "ab"},
		// XPG3 position specifiers
		{"%2$s", []string{"a", "b"}, "b"},
		{"%1$s%1$s", []string{"a"}, "aa"},
		{"%2$s %1$s", []string{"a", "b"}, "b a"},
		{"%2$5s", []string{"a", "b"}, "    b"},
		// error cases
		{"%3$s", []string{"a", "b"}, "ERR: \"%n$\" argument index out of range"},
		{"%0$s", []string{"a"}, "ERR: \"%n$\" argument index out of range"},
		{"%s %1$s", []string{"a"}, "ERR: cannot mix \"%\" and \"%n$\" conversion specifiers"},
		{"%1$s %s", []string{"a"}, "ERR: cannot mix \"%\" and \"%n$\" conversion specifiers"},
		{"%z", []string{"1"}, "ERR: bad field specifier \"z\""},
		{"%s", nil, "ERR: not enough arguments for all format specifiers"},
		{"%d %d", []string{"1"}, "ERR: not enough arguments for all format specifiers"},
		{"%5", nil, "ERR: not enough arguments for all format specifiers"},
		{"%", []string{"1"}, "ERR: format string ended in middle of field specifier"},
		{"%10001d", []string{"1"}, "ERR: format too long"},
		{"%.10001f", []string{"1"}, "ERR: format too long"},
		{"%hhd", []string{"300"}, "ERR: bad field specifier \"h\""},
		{"%llld", []string{"5"}, "ERR: bad field specifier \"l\""},
		{"%d", []string{"abc"}, "ERR: expected integer but got \"abc\""},
		{"%f", []string{"abc"}, "ERR: expected floating-point number but got \"abc\""},
	} {
		if got := numTestFormat(t, i, c.format, c.args...); got != c.want {
			t.Errorf("format %q %v = %q, want %q", c.format, c.args, got, c.want)
		}
	}
}

func TestIndexAndReturnCode(t *testing.T) {
	i := numTestInterp()
	for _, c := range []struct {
		in   string
		want int
		ok   bool
	}{
		{"0", 0, true}, {"3", 3, true}, {"-1", -maxIndex, true},
		{"end", -1, true}, {"end-1", -2, true}, {"end+2", maxIndex, true},
		{"endx", 0, false}, {"2147483648", 0, false},
	} {
		// A non-numeric index is an expression, which the expr area evaluates.
		got, rc := i.GetIndex(i.NewStringObj(c.in))
		if (rc == OK) != c.ok || rc == OK && got != c.want {
			t.Errorf("GetIndex(%q) = %d, %d, want %d, ok %v", c.in, got, rc, c.want, c.ok)
		}
	}

	// The string representation of an index object round-trips.
	for _, c := range []struct {
		idx  int
		want string
	}{{-1, "end"}, {-2, "end-1"}, {3, "3"}, {-maxIndex, "-2147483647"}} {
		if got := i.NewIndexObj(c.idx).String(); got != c.want {
			t.Errorf("NewIndexObj(%d) = %q, want %q", c.idx, got, c.want)
		}
	}

	for _, c := range []struct {
		code int
		want string
	}{{0, "ok"}, {1, "error"}, {7, "eval"}, {8, "?"}, {-1, "?"}} {
		if got := ReturnCodeName(c.code); got != c.want {
			t.Errorf("ReturnCodeName(%d) = %q, want %q", c.code, got, c.want)
		}
	}
	for _, c := range []struct {
		in   string
		want int
		ok   bool
	}{{"ok", 0, true}, {"error", 1, true}, {"5", 5, true}, {"-3", -3, true}, {"bogus", 0, false}} {
		got, rc := i.GetReturnCode(i.NewStringObj(c.in))
		if (rc == OK) != c.ok || rc == OK && got != c.want {
			t.Errorf("GetReturnCode(%q) = %d, %d, want %d, ok %v", c.in, got, rc, c.want, c.ok)
		}
	}
}

func TestGetBooleanAndStringRepIsKept(t *testing.T) {
	i := numTestInterp()
	for _, c := range []struct {
		in   string
		want bool
		ok   bool
	}{
		{"1", true, true}, {"true", true, true}, {"yes", true, true}, {"on", true, true},
		{"0", false, true}, {"false", false, true}, {"no", false, true}, {"off", false, true},
		{"2", false, false}, {"True", false, false}, {"", false, false},
	} {
		got, rc := i.GetBoolean(i.NewStringObj(c.in))
		if (rc == OK) != c.ok || rc == OK && got != c.want {
			t.Errorf("GetBoolean(%q) = %v, %d, want %v, ok %v", c.in, got, rc, c.want, c.ok)
		}
	}

	// Converting to a number keeps the original string representation, which
	// the C relies on: "set x 1.00; expr {$x}" still prints 1.00.
	o := i.NewStringObj("1.00")
	if d, rc := i.GetDouble(o); rc != OK || d != 1 {
		t.Fatalf("GetDouble(1.00) = %v, %d", d, rc)
	}
	if got := o.String(); got != "1.00" {
		t.Errorf("string rep after GetDouble = %q, want 1.00", got)
	}
	// "1.00" is a plain double, not a coerced one, so it is not an integer.
	if _, rc := i.GetWide(o); rc != ERR || i.Result().String() != `expected integer but got "1.00"` {
		t.Errorf("GetWide(1.00) = %d, %q", rc, i.Result().String())
	}

	// An integer used as a double becomes a coerced double: it keeps the
	// integer string representation and converts back without loss.
	c := i.NewStringObj("010")
	if d, rc := i.GetDouble(c); rc != OK || d != 10 {
		t.Fatalf("GetDouble(010) = %v, %d", d, rc)
	}
	if w, rc := i.GetWide(c); rc != OK || w != 10 || c.String() != "010" {
		t.Errorf("GetWide of coerced double = %v, %d, %q", w, rc, c.String())
	}
}

// numTestScan runs a scan through the interpreter API; the character-class
// conversions cannot be written as a script here because the test engine
// substitutes inside brackets.
func numTestScan(t *testing.T, i *Interp, str, format string) string {
	t.Helper()
	f := i.scanFmtOf(i.NewStringObj(format))
	if f.err != "" {
		return "ERR: " + f.err
	}
	values, status := i.scanValues(i.NewStringObj(str), f)
	switch status {
	case scanEOF:
		return "EOF"
	case scanFailed:
		return "ERR"
	}
	out := make([]string, len(values))
	for n, o := range values {
		out[n] = o.String()
	}
	return strings.Join(out, "|")
}

func TestScanString(t *testing.T) {
	i := numTestInterp()
	for _, c := range []struct {
		str, format, want string
	}{
		{"0x10", "%x", "16"},
		{"10", "%x", "16"},
		{"abc", "%c", "97"},
		{"éx", "%c", "233"},
		{"10 20", "%d %d", "10|20"},
		{"hello world", "%s %s", "hello|world"},
		{"   42", "%d", "42"},
		{"", "%d", "EOF"},
		{"abc", "%d", ""},
		{"42abc", "%d%s", "42|abc"},
		{"12345", "%2d%2d", "12|34"},
		{"0x10", "%i", "16"},
		{"017", "%i", "17"},
		{"017", "%o", "15"},
		{"5.5", "%f", "5.5"},
		{"1.5e3", "%f", "1500.0"},
		{"1e400", "%f", "Inf"},
		{"inf", "%f", "Inf"},
		{"abc", "%n%s%n", "0||"},
		{"abc", "%s%n", "abc|3"},
		{"abc", "abc", ""},
		{"abc", "x%d", ""},
		{"a b c", "%s%*s%s", "a|c"},
		{"%", "%%", ""},
		{"5", "%-5d", "5"},
		// character classes
		{"foo", "%[a-z]", "foo"},
		{"a1b", "%[a-z]%d%[a-z]", "a|1|b"},
		{"foo=42", "%[a-z]=%d", "foo|42"},
		{"a-c", `%[a\-c]`, "a-c"},
		{"A", "%[c-a]", ""},
		{"abc", "%[^x]", "abc"},
		{"]abc", "%[]a-c]", "]abc"},
		{"abcx", "%[a-c]", "abc"},
		// malformed formats
		{"a", "%[", "ERR: unmatched [ in format string"},
		{"a", "%[^]", "ERR: unmatched [ in format string"},
		{"x", "%q", "ERR: bad scan conversion character"},
		{"x", "%hh d", "ERR: bad scan conversion character"},
		{"5", "%-5c", "ERR: field width may not be specified in %c conversion"},
		{"5", "%lu", "ERR: unsigned wide not supported"},
		{"x", "%h", "ERR: missing scan conversion character"},
		{"x", "%s%1$s", "ERR: cannot mix \"%\" and \"%n$\" conversion specifiers"},
		{"x", "%1$s%1$s", "ERR: variable is assigned by multiple \"%n$\" conversion specifiers"},
		// XPG3 positions
		{"a b", "%2$s%1$s", "b|a"},
		{"1 2 3", "%3$d%2$d%1$d", "3|2|1"},
	} {
		if got := numTestScan(t, i, c.str, c.format); got != c.want {
			t.Errorf("scan %q %q = %q, want %q", c.str, c.format, got, c.want)
		}
	}
}

func TestIntegerTooBig(t *testing.T) {
	i := numTestInterp()
	// errno is a global in the C and only Jim_StringToDouble() resets it.
	stringToDouble("0")

	// An out-of-range integer wraps round like strtoull() does, and leaves
	// ERANGE set.
	if w, rc := i.GetWide(i.NewStringObj("99999999999999999999")); rc != OK || w != -1 {
		t.Fatalf("GetWide(99999999999999999999) = %d, %d", w, rc)
	}
	// With ERANGE still set, a value of exactly WIDE_MAX is rejected.
	_, rc := i.GetWide(i.NewStringObj("9223372036854775807"))
	if rc != ERR || i.Result().String() != "Integer value too big to be represented" {
		t.Errorf("GetWide(WIDE_MAX) after overflow = %d, %q", rc, i.Result().String())
	}
	// Converting a double clears errno again.
	if _, rc := i.GetDouble(i.NewStringObj("1.5")); rc != OK {
		t.Fatalf("GetDouble(1.5) failed")
	}
	if w, rc := i.GetWide(i.NewStringObj("9223372036854775807")); rc != OK || w != math.MaxInt64 {
		t.Errorf("GetWide(WIDE_MAX) = %d, %d, want %d, OK", w, rc, int64(math.MaxInt64))
	}
}
