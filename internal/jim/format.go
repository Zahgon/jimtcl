/* Jim Tcl 0.84 ported from C to Go: the internals of the format command.
 *
 * Based on code originally from Tcl 8.5:
 * Copyright (c) 1995-1997 Sun Microsystems, Inc.
 * Copyright (c) 1999 by Scriptics Corporation.
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

/* The C builds every numeric field with sprintf(), so [format] inherits the C
 * library's rules rather than Go's: a default precision of 6 for %e/%f/%g (Go
 * would print the shortest unique representation), "inf"/"nan" rather than
 * "+Inf"/"NaN", the '+' and ' ' flags ignored for the unsigned conversions,
 * and no "0x" prefix on a "%#x" of zero. numFormatWide() and numFormatDouble()
 * below reproduce that.
 */

// The limits the C puts on a single field: a numeric field is built in a
// buffer of this size plus the width and the precision.
const (
	integerSpace  = 24
	maxFloatWidth = 320
)

// numSpec is a conversion specification, the equivalent of the sprintf()
// format that jim-format.c assembles in its `spec` buffer.
type numSpec struct {
	minus, zero, plus, space, alt bool
	width, prec                   int
	hasPrec                       bool
	conv                          byte
}

// numFormatChar decodes the character at pos, treating the end of the format
// string as the terminating NUL the C sees there.
func numFormatChar(s []byte, pos int) (ch, step int) {
	if pos >= len(s) {
		return 0, 1
	}
	return u8.ToUnicode(s[pos:])
}

// numUtf8Index returns the byte offset of the given character index
// (C: utf8_index).
func numUtf8Index(s []byte, charIndex int) int {
	n := 0
	for ; charIndex > 0 && n < len(s); charIndex-- {
		_, step := u8.ToUnicode(s[n:])
		n += step
	}
	return n
}

// numStrtol reads the decimal number at pos the way the C's strtol() does,
// saturating at LONG_MAX, and returns it together with the offset just past
// it. The precision of a conversion is parsed with strtol(), so an
// over-long precision saturates instead of wrapping.
func numStrtol(s []byte, pos int) (int64, int) {
	var v int64
	for ; pos < len(s) && u8.IsDigit(s[pos]); pos++ {
		d := int64(s[pos] - '0')
		if v > (math.MaxInt64-d)/10 {
			v = math.MaxInt64
			for ; pos < len(s) && u8.IsDigit(s[pos]); pos++ {
			}
			break
		}
		v = v*10 + d
	}
	return v, pos
}

// numStrtoul reads the decimal number at pos the way the C's strtoul() does,
// saturating at ULONG_MAX, and returns it together with the offset just past
// it. The C stores the width in a long, so an overflowing width comes out
// negative.
func numStrtoul(s []byte, pos int) (int64, int) {
	var v uint64
	overflow := false
	for ; pos < len(s) && u8.IsDigit(s[pos]); pos++ {
		d := uint64(s[pos] - '0')
		if v > math.MaxUint64/10 || v == math.MaxUint64/10 && d > math.MaxUint64%10 {
			overflow = true
		}
		v = v*10 + d
	}
	if overflow {
		v = math.MaxUint64
	}
	return int64(v), pos
}

// numPad surrounds a formatted field with its padding. zeroOK says whether the
// '0' flag applies: the C library ignores it for an integer conversion with an
// explicit precision, and for infinities and NaNs.
func numPad(sp numSpec, prefix, body string, zeroOK bool) string {
	n := sp.width - len(prefix) - len(body)
	switch {
	case n <= 0:
		return prefix + body
	case sp.minus:
		return prefix + body + strings.Repeat(" ", n)
	case sp.zero && zeroOK:
		return prefix + strings.Repeat("0", n) + body
	}
	return strings.Repeat(" ", n) + prefix + body
}

// numFormatWide formats an integer conversion the way the C library does.
func numFormatWide(sp numSpec, w int64) string {
	var prefix, digits string
	switch sp.conv {
	case 'd':
		u := uint64(w)
		switch {
		case w < 0:
			u, prefix = -u, "-"
		case sp.plus:
			prefix = "+"
		case sp.space:
			prefix = " "
		}
		digits = strconv.FormatUint(u, 10)
	case 'u':
		digits = strconv.FormatUint(uint64(w), 10)
	case 'o':
		digits = strconv.FormatUint(uint64(w), 8)
	case 'x', 'X':
		digits = strconv.FormatUint(uint64(w), 16)
		if sp.conv == 'X' {
			digits = strings.ToUpper(digits)
		}
	}

	// A precision is a minimum number of digits; "%.0d" of zero is empty.
	if sp.hasPrec && sp.prec == 0 && w == 0 {
		digits = ""
	}
	if n := sp.prec - len(digits); n > 0 {
		digits = strings.Repeat("0", n) + digits
	}
	switch sp.conv {
	case 'o':
		if sp.alt && !strings.HasPrefix(digits, "0") {
			digits = "0" + digits
		}
	case 'x', 'X':
		if sp.alt && w != 0 {
			prefix = "0" + string(sp.conv)
		}
	}
	return numPad(sp, prefix, digits, !sp.hasPrec)
}

// numTrimFraction removes the trailing zeros of a fractional part, and the
// decimal point with them, as %g does when the '#' flag is absent.
func numTrimFraction(mantissa string) string {
	if strings.IndexByte(mantissa, '.') < 0 {
		return mantissa
	}
	return strings.TrimSuffix(strings.TrimRight(mantissa, "0"), ".")
}

// numFormatExp formats the magnitude in the style of %e; alt keeps the decimal
// point even at precision 0, and trim removes trailing fraction zeros for %g.
func numFormatExp(a float64, prec int, alt, trim bool) string {
	s := strconv.FormatFloat(a, 'e', prec, 64)
	e := strings.IndexByte(s, 'e')
	mantissa, exp := s[:e], s[e:]
	switch {
	case trim:
		mantissa = numTrimFraction(mantissa)
	case alt && prec == 0:
		mantissa += "."
	}
	return mantissa + exp
}

// numDecExp returns the decimal exponent of the magnitude once rounded to prec
// significant digits, which is what decides between the %e and %f styles of %g.
func numDecExp(a float64, prec int) int {
	s := strconv.FormatFloat(a, 'e', prec-1, 64)
	x, _ := strconv.Atoi(s[strings.IndexByte(s, 'e')+1:])
	return x
}

// numFormatDouble formats a floating-point conversion the way the C library
// does.
func numFormatDouble(sp numSpec, d float64) string {
	upper := sp.conv == 'E' || sp.conv == 'G'

	var prefix string
	switch {
	case math.Signbit(d):
		prefix = "-"
	case sp.plus:
		prefix = "+"
	case sp.space:
		prefix = " "
	}

	if math.IsNaN(d) || math.IsInf(d, 0) {
		body := "inf"
		if math.IsNaN(d) {
			body = "nan"
		}
		if upper {
			body = strings.ToUpper(body)
		}
		// The C library pads infinities and NaNs with spaces, never zeros.
		return numPad(sp, prefix, body, false)
	}

	prec := 6 // the C library's default precision
	if sp.hasPrec {
		prec = sp.prec
	}
	a := math.Abs(d)

	var body string
	switch sp.conv {
	case 'f':
		body = strconv.FormatFloat(a, 'f', prec, 64)
		if prec == 0 && sp.alt {
			body += "."
		}
	case 'e', 'E':
		body = numFormatExp(a, prec, sp.alt, false)
	case 'g', 'G':
		// %g uses the %e style when the exponent is below -4 or at least the
		// precision, and strips trailing zeros unless '#' is given. (glibc
		// has a quirk here that is not reproduced: when rounding to the
		// precision carries into a new decimal exponent, it loses the
		// mantissa's trailing zeros, printing "%#g" of 999999.5 as "1.e+06".)
		p := prec
		if p == 0 {
			p = 1
		}
		if x := numDecExp(a, p); x < -4 || x >= p {
			body = numFormatExp(a, p-1, sp.alt, !sp.alt)
		} else {
			body = strconv.FormatFloat(a, 'f', p-1-x, 64)
			if sp.alt {
				if p-1-x == 0 {
					body += "."
				}
			} else {
				body = numTrimFraction(body)
			}
		}
	}
	if upper {
		body = strings.ToUpper(body)
	}
	return numPad(sp, prefix, body, true)
}

// FormatString applies the printf-like format in fmtObj to argv
// (C: Jim_FormatString). It returns nil and an error result on failure.
func (i *Interp) FormatString(fmtObj *Obj, argv []*Obj) (*Obj, int) {
	const mixedXPG = "cannot mix \"%\" and \"%n$\" conversion specifiers"
	badIndex := [2]string{
		"not enough arguments for all format specifiers",
		"\"%n$\" argument index out of range",
	}

	format := fmtObj.Bytes()
	var result []byte
	objIndex, gotXpg, gotSequential := 0, false, false
	span, numBytes := 0, 0

	pos := 0
	for pos < len(format) {
		ch, step := numFormatChar(format, pos)
		pos += step
		if ch != '%' {
			numBytes += step
			continue
		}
		if numBytes > 0 {
			result = append(result, format[span:span+numBytes]...)
			numBytes = 0
		}

		// Step 0. Handle the special case of an escaped format marker (%%).
		ch, step = numFormatChar(format, pos)
		if ch == '%' {
			span, numBytes = pos, step
			pos += step
			continue
		}

		// Step 1. XPG3 position specifier.
		newXpg := false
		if ch < 0x80 && u8.IsDigit(byte(ch)) {
			position, end := numStrtoul(format, pos)
			if end < len(format) && format[end] == '$' {
				newXpg = true
				objIndex = int(position) - 1
				pos = end + 1
				ch, step = numFormatChar(format, pos)
			}
		}
		if newXpg {
			if gotSequential {
				i.SetResultString(mixedXPG)
				return nil, ERR
			}
			gotXpg = true
		} else {
			if gotXpg {
				i.SetResultString(mixedXPG)
				return nil, ERR
			}
			gotSequential = true
		}
		which := 0
		if gotXpg {
			which = 1
		}
		if objIndex < 0 || objIndex >= len(argv) {
			i.SetResultString(badIndex[which])
			return nil, ERR
		}

		// Step 2. The set of flags. The C stops after five of them.
		var sp numSpec
		pad := byte(' ')
		specLen := 1
		for {
			switch ch {
			case '-':
				sp.minus = true
			case '0':
				sp.zero = true
				pad = '0'
			case '+':
				sp.plus = true
			case ' ':
				sp.space = true
			case '#':
				sp.alt = true
			}
			if ch != '-' && ch != '0' && ch != '+' && ch != ' ' && ch != '#' {
				break
			}
			specLen++
			pos += step
			ch, step = numFormatChar(format, pos)
			if specLen > 5 {
				break
			}
		}

		// Step 3. Minimum field width.
		width := int64(0)
		if ch < 0x80 && u8.IsDigit(byte(ch)) {
			width, pos = numStrtoul(format, pos)
			ch, step = numFormatChar(format, pos)
		} else if ch == '*' {
			if objIndex >= len(argv)-1 {
				i.SetResultString(badIndex[which])
				return nil, ERR
			}
			w, rc := i.GetWide(argv[objIndex])
			if rc != OK {
				return nil, ERR
			}
			width = w
			if width < 0 {
				width = -width
				sp.minus = true
			}
			objIndex++
			pos += step
			ch, step = numFormatChar(format, pos)
		}

		// Step 4. Precision.
		gotPrecision, precision := false, int64(0)
		if ch == '.' {
			gotPrecision = true
			pos += step
			ch, step = numFormatChar(format, pos)
		}
		if ch < 0x80 && u8.IsDigit(byte(ch)) {
			precision, pos = numStrtol(format, pos)
			ch, step = numFormatChar(format, pos)
		} else if ch == '*' {
			if objIndex >= len(argv)-1 {
				i.SetResultString(badIndex[which])
				return nil, ERR
			}
			p, rc := i.GetWide(argv[objIndex])
			if rc != OK {
				return nil, ERR
			}
			if precision = p; precision < 0 {
				precision = 0
			}
			objIndex++
			pos += step
			ch, step = numFormatChar(format, pos)
		}

		// Step 5. Length modifier. All non-short integers are wide, so "l"
		// and "ll" are accepted and ignored.
		useShort := false
		if ch == 'h' {
			useShort = true
			pos += step
			ch, step = numFormatChar(format, pos)
		} else if ch == 'l' {
			pos += step
			ch, step = numFormatChar(format, pos)
			if ch == 'l' {
				pos += step
				ch, step = numFormatChar(format, pos)
			}
		}
		pos += step
		span = pos

		// Step 6. The actual conversion character.
		if ch == 'i' {
			ch = 'd'
		}
		// Only a precision introduced by '.' reaches the sprintf() spec; a
		// bare number after a '*' width is consumed and then ignored.
		sp.width, sp.hasPrec = int(width), gotPrecision
		if gotPrecision {
			sp.prec = int(precision)
		}
		if width < 0 {
			// An overflowing width ends up negative; the C renders it into the
			// sprintf() spec with "%ld", where the '-' becomes a left-justify
			// flag and the digits the width.
			sp.minus, sp.width = true, int(-width)
		}

		var formattedBuf []byte
		var formattedChars int

		switch ch {
		case 0:
			i.SetResultString("format string ended in middle of field specifier")
			return nil, ERR

		case 's':
			buf := argv[objIndex].Bytes()
			formattedChars = i.Utf8Length(argv[objIndex])
			if gotPrecision && precision < int64(formattedChars) {
				formattedChars = int(precision)
				buf = buf[:numUtf8Index(buf, formattedChars)]
			}
			formattedBuf = buf

		case 'c':
			code, rc := i.GetWide(argv[objIndex])
			if rc != OK {
				return nil, ERR
			}
			// The C passes the wide value to an unsigned parameter.
			formattedBuf = u8.FromUnicode(int(uint32(code)))
			formattedChars = 1

		case 'b':
			w, rc := i.GetWide(argv[objIndex])
			if rc != OK {
				return nil, ERR
			}
			// Width, precision and the flags are not implemented for binary;
			// only the common padding below applies.
			u := uint64(w)
			for n := 63; n >= 0; n-- {
				switch {
				case u&(1<<uint(n)) != 0:
					formattedBuf = append(formattedBuf, '1')
				case len(formattedBuf) > 0 || n == 0:
					formattedBuf = append(formattedBuf, '0')
				}
			}
			formattedChars = len(formattedBuf)

		case 'e', 'E', 'f', 'g', 'G', 'd', 'u', 'o', 'x', 'X':
			doubleType := ch == 'e' || ch == 'E' || ch == 'f' || ch == 'g' || ch == 'G'
			sp.conv = byte(ch)

			var d float64
			var w int64
			length := int64(integerSpace)
			if doubleType {
				var rc int
				if d, rc = i.GetDouble(argv[objIndex]); rc != OK {
					return nil, ERR
				}
				length = maxFloatWidth
			} else {
				var rc int
				if w, rc = i.GetWide(argv[objIndex]); rc != OK {
					return nil, ERR
				}
				if useShort {
					if ch == 'd' {
						w = int64(int16(w))
					} else {
						w = int64(uint16(w))
					}
				}
			}

			// Put some reasonable limits on the field size.
			if width > 10000 || length > 10000 || precision > 10000 {
				i.SetResultString("format too long")
				return nil, ERR
			}
			if width > length {
				length = width
			}
			if gotPrecision {
				length += precision
			}

			var s string
			if doubleType {
				s = numFormatDouble(sp, d)
			} else {
				s = numFormatWide(sp, w)
			}
			// The C formats into a buffer of length+1 bytes, so anything
			// longer than that is truncated by snprintf().
			if int64(len(s)) > length {
				s = s[:length]
			}
			formattedBuf = []byte(s)
			formattedChars = len(formattedBuf)

		default:
			// The C stores only the low byte of the character here.
			return nil, i.Errorf("bad field specifier \"%s\"", string([]byte{byte(ch)}))
		}

		if !sp.minus {
			for ; formattedChars < int(width); formattedChars++ {
				result = append(result, pad)
			}
		}
		result = append(result, formattedBuf...)
		for ; formattedChars < int(width); formattedChars++ {
			result = append(result, pad)
		}

		if gotSequential {
			objIndex++
		}
	}
	if numBytes > 0 {
		result = append(result, format[span:span+numBytes]...)
	}
	return i.NewStringObjBytes(result), OK
}

// formatCoreCommand implements [format] (C: Jim_FormatCoreCommand).
func formatCoreCommand(i *Interp, argv []*Obj) int {
	o, rc := i.FormatString(argv[1], argv[2:])
	if o == nil {
		return rc
	}
	i.SetResult(o)
	return OK
}

func registerFormatCommands(i *Interp) {
	i.RegisterCmd("format", "formatString ?arg arg ...?", 1, -1, formatCoreCommand, 0)
	i.RegisterCmd("scan", "string format ?varName varName ...?", 2, -1, scanCoreCommand, 0)
}
