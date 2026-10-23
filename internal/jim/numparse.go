/* Jim Tcl 0.84 ported from C to Go: parsing numbers from strings.
 *
 * Copyright 2005 Salvatore Sanfilippo <antirez@invece.org>
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
	"github.com/msteveb/jimtcl/internal/u8"
	"math"
	"strconv"
	"strings"
)

/* The C implementation leaves the actual work to the C library's strtoull()
 * and strtod(), so the accepted syntax is glibc's, not Go's: leading white
 * space, an optional sign, glibc's "inf"/"infinity"/"nan(chars)" spellings,
 * hexadecimal floats, a wrap-around (rather than clamping) conversion of
 * values that do not fit, and an end pointer that marks how much was
 * consumed. numStrtoull() and numStrtod() below reproduce that, returning the
 * offset of the first unconsumed byte, which is 0 when nothing was converted
 * (C: endptr == str).
 */

// cErrnoRange mirrors errno == ERANGE as the conversions observe it. In C,
// errno is a process-wide global that strtoull() and strtod() set on overflow
// and never clear; the only place Jim resets it is Jim_StringToDouble(). Since
// SetIntFromAny() consults it when deciding whether to report "Integer value
// too big to be represented", the flag has to survive between conversions.
var cErrnoRange bool

// numCStr truncates at the first NUL byte, since the C conversions are handed
// a NUL-terminated string and never look past it.
func numCStr(s string) string {
	if n := strings.IndexByte(s, 0); n >= 0 {
		return s[:n]
	}
	return s
}

// numCheckConversion reports whether a conversion consumed something and left
// nothing but white space behind (C: JimCheckConversion).
func numCheckConversion(s string, end int) bool {
	if len(s) == 0 || end == 0 {
		return false
	}
	for ; end < len(s); end++ {
		if !u8.IsSpace(s[end]) {
			return false
		}
	}
	return true
}

// numberBase parses the front of a number to determine its sign and base and
// returns the offset at which to start parsing in that base (C: JimNumberBase).
// base is zero when the string carries no base indicator.
func numberBase(s string) (idx, base, sign int) {
	at := func(n int) byte {
		if n < len(s) {
			return s[n]
		}
		return 0 // the C sees the terminating NUL here
	}

	i := 0
	for u8.IsSpace(at(i)) {
		i++
	}
	sign = 1
	if at(i) == '-' {
		sign = -1
		i++
	} else if at(i) == '+' {
		i++
	}

	if at(i) != '0' {
		return 0, 0, sign
	}
	switch at(i + 1) {
	case 'x', 'X':
		base = 16
	case 'o', 'O':
		base = 8
	case 'b', 'B':
		base = 2
	case 'd', 'D':
		base = 10
	default:
		return 0, 0, sign
	}
	i += 2
	// Ensure that (e.g.) 0x-5 fails to parse: a sign or space after the
	// prefix makes the whole string be parsed in the default base instead.
	if c := at(i); c != '-' && c != '+' && !u8.IsSpace(c) {
		return i, base, sign
	}
	return 0, 0, sign
}

// cStrtoull is the C library's strtoull(): optional white space, an optional
// sign, an optional base prefix, then digits. The second result is the offset
// of the first unconsumed byte, or 0 if no conversion was performed.
func cStrtoull(s string, base int) (uint64, int) {
	i := 0
	for i < len(s) && u8.IsSpace(s[i]) {
		i++
	}
	neg := false
	if i < len(s) && (s[i] == '-' || s[i] == '+') {
		neg = s[i] == '-'
		i++
	}
	if base == 16 && i+1 < len(s) && s[i] == '0' && s[i+1]|0x20 == 'x' {
		i += 2
	} else if base == 0 {
		switch {
		case i+1 < len(s) && s[i] == '0' && s[i+1]|0x20 == 'x':
			i += 2
			base = 16
		case i < len(s) && s[i] == '0':
			base = 8
		default:
			base = 10
		}
	}

	start := i
	var v uint64
	overflow := false
	cutoff, cutlim := math.MaxUint64/uint64(base), math.MaxUint64%uint64(base)
	for i < len(s) {
		d := u8.DigitVal(s[i])
		if d < 0 || d >= base {
			break
		}
		if v > cutoff || v == cutoff && uint64(d) > cutlim {
			overflow = true
		}
		v = v*uint64(base) + uint64(d)
		i++
	}

	if i == start {
		// Special case in the C library: "0x" not followed by a hex digit
		// is not an error; the leading '0' is converted and the end pointer
		// is left on the 'x'.
		if start >= 2 && s[start-1]|0x20 == 'x' && s[start-2] == '0' {
			return 0, start - 1
		}
		return 0, 0
	}
	if overflow {
		cErrnoRange = true
		return math.MaxUint64, i
	}
	if neg {
		v = -v
	}
	return v, i
}

// numStrtoull converts a number as per strtoull(..., 0) except that leading
// zeros do not imply octal: decimal is assumed unless the number begins with
// 0x, 0o, 0b or 0d (C: jim_strtoull).
func numStrtoull(s string) (int64, int) {
	i, base, sign := numberBase(s)
	if base != 0 {
		if v, end := cStrtoull(s[i:], base); end != 0 {
			return int64(v) * int64(sign), i + end
		}
	}
	v, end := cStrtoull(s, 10)
	return int64(v), end
}

// numStrtod is the C library's strtod(). The second result is the offset of
// the first unconsumed byte, or 0 if no conversion was performed.
func numStrtod(s string) (float64, int) {
	hasPrefix := func(p int, word string) bool {
		return len(s)-p >= len(word) && strings.EqualFold(s[p:p+len(word)], word)
	}

	i := 0
	for i < len(s) && u8.IsSpace(s[i]) {
		i++
	}
	start := i
	neg := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}

	sign := 1
	if neg {
		sign = -1
	}
	switch {
	case hasPrefix(i, "infinity"):
		return math.Inf(sign), i + 8
	case hasPrefix(i, "inf"):
		return math.Inf(sign), i + 3
	case hasPrefix(i, "nan"):
		i += 3
		// An optional parenthesised n-char-sequence belongs to the NaN.
		if i < len(s) && s[i] == '(' {
			j := i + 1
			for j < len(s) && (u8.IsAlnum(s[j]) || s[j] == '_') {
				j++
			}
			if j < len(s) && s[j] == ')' {
				i = j + 1
			}
		}
		return math.Copysign(math.NaN(), float64(sign)), i
	}

	if i+1 < len(s) && s[i] == '0' && s[i+1]|0x20 == 'x' {
		j := i + 2
		mant := j
		digits, nonzero := 0, false
		scanHex := func() {
			for j < len(s) && u8.IsHexDigit(s[j]) {
				nonzero = nonzero || s[j] != '0'
				j++
				digits++
			}
		}
		scanHex()
		if j < len(s) && s[j] == '.' {
			j++
			scanHex()
		}
		if digits == 0 {
			// As for strtoull(), "0x" with no hex digit converts just the '0'.
			return 0, i + 1
		}
		// The C library makes the binary exponent optional; Go requires it.
		mantEnd, exp := j, "p0"
		if j < len(s) && s[j]|0x20 == 'p' {
			k := j + 1
			if k < len(s) && (s[k] == '+' || s[k] == '-') {
				k++
			}
			d := k
			for k < len(s) && u8.IsDigit(s[k]) {
				k++
			}
			if k > d {
				exp, j = s[j:k], k
			}
		}
		lit := s[start:i] + "0x" + strings.TrimSuffix(s[mant:mantEnd], ".") + exp
		return numParseFloat(lit, nonzero), j
	}

	j := i
	digits, nonzero := 0, false
	scanDec := func() {
		for j < len(s) && u8.IsDigit(s[j]) {
			nonzero = nonzero || s[j] != '0'
			j++
			digits++
		}
	}
	scanDec()
	if j < len(s) && s[j] == '.' {
		j++
		scanDec()
	}
	if digits == 0 {
		return 0, 0
	}
	if j < len(s) && s[j]|0x20 == 'e' {
		k := j + 1
		if k < len(s) && (s[k] == '+' || s[k] == '-') {
			k++
		}
		d := k
		for k < len(s) && u8.IsDigit(s[k]) {
			k++
		}
		if k > d {
			j = k
		}
	}
	return numParseFloat(s[start:j], nonzero), j
}

// numParseFloat converts a syntactically valid number, recording ERANGE for a
// result that overflows to infinity or underflows out of the normal range, as
// the C library does. nonzero says whether the mantissa had a non-zero digit,
// which is what distinguishes an underflow from a plain zero.
func numParseFloat(s string, nonzero bool) float64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		cErrnoRange = true
		return v
	}
	if nonzero && math.Abs(v) < 2.2250738585072014e-308 {
		cErrnoRange = true
	}
	return v
}

// stringToWide parses an integer in the given base (0 = auto), reporting
// whether it parsed and whether it overflowed (C: Jim_StringToWide). As in the
// C, errno is not cleared first, so rangeErr may have been set by an earlier
// conversion.
func stringToWide(s string, base int) (val int64, ok bool, rangeErr bool) {
	s = numCStr(s)
	var end int
	if base != 0 {
		var u uint64
		u, end = cStrtoull(s, base)
		val = int64(u)
	} else {
		val, end = numStrtoull(s)
	}
	return val, numCheckConversion(s, end), cErrnoRange
}

// stringToDouble parses a floating-point number as the C implementation does
// (C: Jim_StringToDouble).
func stringToDouble(s string) (float64, bool) {
	s = numCStr(s)
	cErrnoRange = false // the C clears errno so callers can detect underflow
	val, end := numStrtod(s)
	return val, numCheckConversion(s, end)
}
