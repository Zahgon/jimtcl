/* Jim Tcl 0.84 ported from C to Go: the string object and its operations.
 *
 * Copyright 2005 Salvatore Sanfilippo <antirez@invece.org>
 * Copyright 2005 Clemens Hintze <c.hintze@gmx.net>
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
	"strings"
)

// strIntMax is the C INT_MAX, the range the index objects are clamped to. Go's int
// is 64 bits wide, so the places where the C relies on 32-bit int arithmetic
// are done in int32 below.
const strIntMax = math.MaxInt32

// strBuf is the stand-in for the maxLength field of the C string internal
// representation: it records that an object owns the spare capacity of its byte
// slice, so that repeated appends can grow the buffer in place the way
// StringAppendString() does with realloc(). The owner pointer matters because
// Obj.dup() shares the slice: the duplicate does not own the buffer and so
// reallocates on its first append, which is what DupStringInternalRep() does by
// setting maxLength to the string length.
type strBuf struct{ owner *Obj }

// setStringFromAny converts an object to the canonical string representation:
// the string rep is generated if needed and any other internal representation
// is dropped (C: SetStringFromAny). An object that already holds only a string
// keeps its cached character length.
func (i *Interp) setStringFromAny(o *Obj) {
	if o.kind != kindNone {
		o.Bytes()
		o.freeIntRep()
		// Don't know the utf-8 length yet
		o.charLen = -1
	}
}

// Utf8Length returns the length of the object's string in characters, which
// differs from the byte length for a UTF-8 string (C: Jim_Utf8Length).
func (i *Interp) Utf8Length(o *Obj) int {
	i.setStringFromAny(o)
	if o.charLen < 0 {
		o.charLen = u8.Strlen(o.Bytes())
	}
	return o.charLen
}

// NewStringObjUtf8 returns a new object holding the first charlen characters of
// s and remembers that character length (C: Jim_NewStringObjUtf8).
//
// The byte length is found with u8.Index(), which steps over invalid sequences
// differently from the decoder, so the remembered character length is not
// always u8.Strlen() of the result; the C has the same property.
func (i *Interp) NewStringObjUtf8(s []byte, charlen int) *Obj {
	o := i.NewStringObjBytes(strRangeBytes(s, 0, u8.Index(s, charlen)))
	o.charLen = charlen
	return o
}

// appendBytes is the low-level string append (C: StringAppendString).
func (o *Obj) appendBytes(b []byte) {
	if o.charLen >= 0 {
		// Update the utf-8 char length
		o.charLen += u8.Strlen(b)
	}
	sb, owned := o.aux.(*strBuf)
	if !owned || sb.owner != o || cap(o.bytes) < len(o.bytes)+len(b) {
		// The C doubles the requested size and never asks for less than 7 bytes
		needlen := (len(o.bytes) + len(b)) * 2
		if needlen < 7 {
			needlen = 7
		}
		grown := make([]byte, len(o.bytes), needlen)
		copy(grown, o.bytes)
		o.bytes = grown
		o.aux = &strBuf{owner: o}
	}
	o.bytes = append(o.bytes, b...)
	o.strValid = true
}

// AppendString appends to an object the caller owns, invalidating its
// representation as Jim_AppendString() does.
func (i *Interp) AppendString(o *Obj, s string) {
	i.setStringFromAny(o)
	o.appendBytes([]byte(s))
}

// AppendObj appends another object's string representation (C: Jim_AppendObj).
func (i *Interp) AppendObj(o, app *Obj) {
	i.setStringFromAny(o)
	o.appendBytes(app.Bytes())
}

// AppendStrings appends several strings in turn (C: Jim_AppendStrings).
func (i *Interp) AppendStrings(o *Obj, strs ...string) {
	i.setStringFromAny(o)
	for _, s := range strs {
		i.AppendString(o, s)
	}
}

// StringEqObj reports whether two objects have the same string representation
// (C: Jim_StringEqObj).
func (i *Interp) StringEqObj(a, b *Obj) bool {
	if a == b {
		return true
	}
	sa, sb := a.Bytes(), b.Bytes()
	if len(sa) != len(sb) {
		return false
	}
	for n := range sa {
		if sa[n] != sb[n] {
			return false
		}
	}
	return true
}

// StringMatchObj reports whether the glob pattern matches the object's string
// (C: Jim_StringMatchObj). Embedded nulls are not supported in either.
func (i *Interp) StringMatchObj(pattern, s *Obj, nocase bool) bool {
	return globMatch(pattern.Bytes(), s.Bytes(), nocase)
}

// StringCompareObj compares two objects as UTF-8 strings
// (C: Jim_StringCompareObj).
func (i *Interp) StringCompareObj(a, b *Obj, nocase bool) int {
	sa, la := a.Bytes(), i.Utf8Length(a)
	sb, lb := b.Bytes(), i.Utf8Length(b)
	return strCompareUtf8(sa, la, sb, lb, nocase)
}

// stringCompareUtf8 compares two whole UTF-8 strings, returning -1, 0 or 1.
func stringCompareUtf8(a, b []byte, nocase bool) int {
	return strCompareUtf8(a, u8.Strlen(a), b, u8.Strlen(b), nocase)
}

// strCompareUtf8 compares the first l1 characters of a with the first l2
// characters of b (C: JimStringCompareUtf8). The lengths are character lengths,
// not byte lengths, which is how [string compare -length] truncates.
func strCompareUtf8(a []byte, l1 int, b []byte, l2 int, nocase bool) int {
	minlen := l1
	if l2 < l1 {
		minlen = l2
	}
	ai, bi := 0, 0
	for ; minlen > 0; minlen-- {
		c1, n1 := u8.ToUnicodeCase(u8.Slice(a, ai), nocase)
		c2, n2 := u8.ToUnicodeCase(u8.Slice(b, bi), nocase)
		ai += n1
		bi += n2
		if c1 != c2 {
			return strSign(c1 - c2)
		}
	}
	// Equal to this point, so the shorter string is less
	switch {
	case l1 < l2:
		return -1
	case l1 > l2:
		return 1
	}
	return 0
}

// strSign returns the sign of n (C: JimSign).
func strSign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}

// strBytesEqual compares n bytes of a and b, reading past the end as the NUL
// that terminates a C string, which is what the memcmp() calls of
// JimStringFirst() and JimStringLast() do when the needle is longer than what
// is left of the haystack.
func strBytesEqual(a []byte, ai int, b []byte, bi, n int) bool {
	for k := 0; k < n; k++ {
		if u8.ByteAt(a, ai+k) != u8.ByteAt(b, bi+k) {
			return false
		}
	}
	return true
}

// stringFirst returns the character index of the first occurrence of s1 in s2
// at or after character index idx, or -1 (C: JimStringFirst). The lengths are
// character lengths.
func stringFirst(s1 []byte, l1 int, s2 []byte, l2, idx int) int {
	if l1 == 0 || l2 == 0 || l1 > l2 || idx > l2 {
		return -1
	}
	if idx < 0 {
		idx = 0
	}
	pos := u8.Index(s2, idx)
	// The needle length is measured with u8.Index(), so for a needle holding
	// an invalid sequence it can exceed the needle's byte length and then never
	// compare equal.
	l1bytelen := u8.Index(s1, l1)

	for n := idx; n <= l2-l1; n++ {
		if strBytesEqual(s2, pos, s1, 0, l1bytelen) {
			return n
		}
		_, w := u8.ToUnicode(u8.Slice(s2, pos))
		pos += w
	}
	return -1
}

// stringLast returns the byte index of the last occurrence of s1 in the first
// l2 bytes of s2, or -1 (C: JimStringLast). The lengths are byte lengths.
func stringLast(s1 []byte, l1 int, s2 []byte, l2 int) int {
	if l1 == 0 || l2 == 0 || l1 > l2 {
		return -1
	}
	for p := l2 - 1; p >= 0; p-- {
		if u8.ByteAt(s2, p) == u8.ByteAt(s1, 0) && strBytesEqual(s1, 0, s2, p, l1) {
			return p
		}
	}
	return -1
}

// stringLastUtf8 is stringLast() with character lengths and a character result
// (C: JimStringLastUtf8).
func stringLastUtf8(s1 []byte, l1 int, s2 []byte, l2 int) int {
	n := stringLast(s1, u8.Index(s1, l1), s2, u8.Index(s2, l2))
	if n > 0 {
		n = u8.Strlen(s2[:min(n, len(s2))])
	}
	return n
}

/* ---------------------------------------------------------------------------
 * Ranges
 * ------------------------------------------------------------------------ */

// strRelToAbsIndex turns an index as returned by Jim_GetIndex() into an absolute
// index into a structure of the given length (C: JimRelToAbsIndex). The result
// may be negative or beyond the end.
func strRelToAbsIndex(length, idx int) int {
	if idx < 0 && idx > -strIntMax {
		return int(int32(length) + int32(idx))
	}
	return idx
}

// strRelToAbsRange clamps a pair of absolute indices to a range that addresses
// valid elements, returning the new first and last index and the range length
// (C: JimRelToAbsRange).
//
// The arithmetic is done in int32 because the C does it in int and wraps for
// extreme indices; [string range abc -2147483647 2147483647] returns the whole
// string only because of that wrapping.
func strRelToAbsRange(length, first, last int) (int, int, int) {
	f, l := int32(first), int32(last)
	var rangeLen int32

	if f > l {
		rangeLen = 0
	} else {
		rangeLen = l - f + 1
		if rangeLen != 0 {
			if f < 0 {
				rangeLen += f
				f = 0
			}
			if int(l) >= length {
				rangeLen -= l - int32(length-1)
				l = int32(length - 1)
			}
		}
	}
	if rangeLen < 0 {
		rangeLen = 0
	}
	return int(f), int(l), int(rangeLen)
}

// strRangeBytes returns the rangeLen bytes of str starting at first, as the C
// reads them out of a NUL-terminated string: a range that reaches one byte past
// the end picks up the terminator, which is observable for a string ending in a
// truncated UTF-8 sequence, e.g. [string replace "\xc3(" 0 0] is a single null
// byte. Reading further than the terminator is undefined in the C, so the
// result stops there.
func strRangeBytes(str []byte, first, rangeLen int) []byte {
	if rangeLen <= 0 || first < 0 || first > len(str) {
		return nil
	}
	if first+rangeLen > len(str) {
		out := make([]byte, 0, len(str)-first+1)
		return append(append(out, str[first:]...), 0)
	}
	return str[first : first+rangeLen]
}

// stringGetRange converts Tcl-style first/last index objects into absolute
// indices into a structure of the given length (C: JimStringGetRange).
func (i *Interp) stringGetRange(firstObj, lastObj *Obj, length int) (first, last, rangeLen, rc int) {
	first, rc = i.GetIndex(firstObj)
	if rc != OK {
		return 0, 0, 0, ERR
	}
	last, rc = i.GetIndex(lastObj)
	if rc != OK {
		return 0, 0, 0, ERR
	}
	first = strRelToAbsIndex(length, first)
	last = strRelToAbsIndex(length, last)
	first, last, rangeLen = strRelToAbsRange(length, first, last)
	return first, last, rangeLen, OK
}

// StringByteRangeObj returns the given byte range of an object's string
// (C: Jim_StringByteRangeObj).
func (i *Interp) StringByteRangeObj(s, first, last *Obj) (*Obj, int) {
	str := s.Bytes()
	bytelen := len(str)

	f, _, rangeLen, rc := i.stringGetRange(first, last, bytelen)
	if rc != OK {
		return nil, ERR
	}
	if f == 0 && rangeLen == bytelen {
		return s, OK
	}
	return i.NewStringObjBytes(strRangeBytes(str, f, rangeLen)), OK
}

// StringRangeObj returns the given character range of an object's string
// (C: Jim_StringRangeObj).
func (i *Interp) StringRangeObj(s, first, last *Obj) (*Obj, int) {
	str := s.Bytes()
	bytelen := len(str)
	length := i.Utf8Length(s)

	f, _, rangeLen, rc := i.stringGetRange(first, last, length)
	if rc != OK {
		return nil, ERR
	}
	if f == 0 && rangeLen == length {
		return s, OK
	}
	if length == bytelen {
		// ASCII optimisation
		return i.NewStringObjBytes(strRangeBytes(str, f, rangeLen)), OK
	}
	return i.NewStringObjUtf8(u8.Slice(str, u8.ByteIndex(str, f)), rangeLen), OK
}

// stringReplaceObj replaces the given character range of a string with newStr,
// which may be nil to delete the range (C: JimStringReplaceObj).
func (i *Interp) stringReplaceObj(s, first, last, newStr *Obj) (*Obj, int) {
	length := i.Utf8Length(s)

	f, l, _, rc := i.stringGetRange(first, last, length)
	if rc != OK {
		return nil, ERR
	}
	if l < f {
		return s, OK
	}

	str := s.Bytes()

	// Before part
	o := i.NewStringObjUtf8(str, f)

	// Replacement
	if newStr != nil {
		i.AppendObj(o, newStr)
	}

	// After part. The C passes the remaining *character* count where
	// Jim_AppendString() wants a byte count, so the tail of a string holding
	// multi-byte characters is cut short, and may even be cut in the middle of
	// a character: [string replace abcde\u00e9 1 1 X] gives "aXcd\xc3".
	tail := strRangeBytes(str, u8.ByteIndex(str, l+1), length-l-1)
	o.appendBytes(tail)

	return o, OK
}

/* ---------------------------------------------------------------------------
 * Case conversion
 * ------------------------------------------------------------------------ */

// strCopyUpperLower maps every character of s to upper or lower case
// (C: JimStrCopyUpperLower). Like the C it stops at the first null byte, which
// is why [string toupper "a\x00b"] is just "A".
func strCopyUpperLower(dst []byte, s []byte, upper bool) []byte {
	for n := 0; n < len(s) && s[n] != 0; {
		c, l := u8.ToUnicode(s[n:])
		n += l
		if upper {
			c = u8.Upper(c)
		} else {
			c = u8.Lower(c)
		}
		dst = append(dst, u8.FromUnicode(c)...)
	}
	return dst
}

// stringToLower returns the string with every character in lower case
// (C: JimStringToLower).
func (i *Interp) stringToLower(s *Obj) *Obj {
	str := s.Bytes()
	return i.NewStringObjBytes(strCopyUpperLower(make([]byte, 0, len(str)), str, false))
}

// stringToUpper returns the string with every character in upper case
// (C: JimStringToUpper).
func (i *Interp) stringToUpper(s *Obj) *Obj {
	str := s.Bytes()
	return i.NewStringObjBytes(strCopyUpperLower(make([]byte, 0, len(str)), str, true))
}

// stringToTitle returns the string with its first character in title case and
// the rest in lower case (C: JimStringToTitle).
func (i *Interp) stringToTitle(s *Obj) *Obj {
	str := s.Bytes()
	c, l := u8.ToUnicode(str)
	buf := u8.FromUnicode(u8.Title(c))
	buf = strCopyUpperLower(buf, u8.Slice(str, l), false)
	// The C result is built as a C string, so it ends at the first null byte:
	// an empty input title-cases the null terminator and yields "".
	for n, b := range buf {
		if b == 0 {
			return i.NewStringObjBytes(buf[:n])
		}
	}
	return i.NewStringObjBytes(buf)
}

/* ---------------------------------------------------------------------------
 * Trimming
 * ------------------------------------------------------------------------ */

// strDefaultTrimChars is the set trimmed by [string trim] with no trimchars. The C
// measures it with sizeof(), so the terminating null is part of the set and a
// null byte is trimmed too.
var strDefaultTrimChars = []byte(" \t\n\r\x00")

// strFindTrimLeft returns the offset of the first character of str that is not in
// trimChars, or the length of str (C: JimFindTrimLeft).
func strFindTrimLeft(str, trimChars []byte) int {
	n := 0
	for n < len(str) {
		c, w := u8.ToUnicode(str[n:])
		if u8.Memchr(trimChars, c) < 0 {
			// Not a trim char, so stop
			break
		}
		n += w
	}
	return n
}

// strFindTrimRight returns the offset just past the last character of str that is
// not in trimChars, or -1 when every character is trimmed
// (C: JimFindTrimRight).
func strFindTrimRight(str, trimChars []byte) int {
	pos := len(str)
	length := len(str)

	for length != 0 {
		n := u8.PrevLen(str, pos, length)
		length -= n
		pos -= n

		c, w := u8.ToUnicode(str[pos:])
		if u8.Memchr(trimChars, c) < 0 {
			return pos + w
		}
	}
	return -1
}

// stringTrimLeft trims characters from the left of a string object
// (C: JimStringTrimLeft).
func (i *Interp) stringTrimLeft(s, trimChars *Obj) *Obj {
	str := s.Bytes()
	trim := strDefaultTrimChars
	if trimChars != nil {
		trim = trimChars.Bytes()
	}

	n := strFindTrimLeft(str, trim)
	if n == 0 {
		return s
	}
	return i.NewStringObjBytes(str[n:])
}

// stringTrimRight trims characters from the right of a string object
// (C: JimStringTrimRight).
func (i *Interp) stringTrimRight(s, trimChars *Obj) *Obj {
	trim := strDefaultTrimChars
	if trimChars != nil {
		trim = trimChars.Bytes()
	}

	i.setStringFromAny(s)
	str := s.Bytes()

	nontrim := strFindTrimRight(str, trim)
	if nontrim < 0 {
		// All trim, so return a zero-length string
		return i.NewStringObj("")
	}
	if nontrim == len(str) {
		// All non-trim, so return the original object
		return s
	}
	return i.NewStringObjBytes(str[:nontrim])
}

// stringTrim trims characters from both sides of a string object
// (C: JimStringTrim).
func (i *Interp) stringTrim(s, trimChars *Obj) *Obj {
	return i.stringTrimRight(i.stringTrimLeft(s, trimChars), trimChars)
}

/* ---------------------------------------------------------------------------
 * Character classes and immediate comparison
 * ------------------------------------------------------------------------ */

// The character classes of [string is], in the order of the C table: the index
// is what Jim_GetEnum() returns, and the error message is built from the names.
var strClassNames = []string{
	"integer", "alpha", "alnum", "ascii", "digit",
	"double", "lower", "upper", "space", "xdigit",
	"control", "print", "graph", "punct", "boolean",
}

const (
	strIsInteger = iota
	strIsAlpha
	strIsAlnum
	strIsASCII
	strIsDigit
	strIsDouble
	strIsLower
	strIsUpper
	strIsSpace
	strIsXdigit
	strIsControl
	strIsPrint
	strIsGraph
	strIsPunct
	strIsBoolean
)

// The ctype predicates of the "C" locale, which is the only locale jimsh runs
// in: every byte of 0x80 and above fails each of them.
func strIsAlphaByte(c byte) bool { return c|0x20 >= 'a' && c|0x20 <= 'z' }
func strIsDigitByte(c byte) bool { return c >= '0' && c <= '9' }
func strIsAlnumByte(c byte) bool { return strIsAlphaByte(c) || strIsDigitByte(c) }
func strIsLowerByte(c byte) bool { return c >= 'a' && c <= 'z' }
func strIsUpperByte(c byte) bool { return c >= 'A' && c <= 'Z' }
func strIsPrintByte(c byte) bool { return c >= 0x20 && c < 0x7f }
func strIsGraphByte(c byte) bool { return c > 0x20 && c < 0x7f }
func strIsCntrlByte(c byte) bool { return c < 0x20 || c == 0x7f }
func strIsPunctByte(c byte) bool { return strIsGraphByte(c) && !strIsAlnumByte(c) }
func strIsASCIIByte(c byte) bool { return c&0x80 == 0 }

func strIsXdigitByte(c byte) bool {
	return strIsDigitByte(c) || (c|0x20 >= 'a' && c|0x20 <= 'f')
}

// stringIs implements the [string is] character-class checks (C: JimStringIs).
func (i *Interp) stringIs(s, class *Obj, strict bool) int {
	strclass, rc := i.GetEnum(class, strClassNames, "class", ErrMsg|EnumAbbrev)
	if rc != OK {
		return ERR
	}

	str := s.Bytes()
	if len(str) == 0 {
		// An empty string is a member of every class unless -strict is given
		i.SetResultBool(!strict)
		return OK
	}

	var isclass func(byte) bool
	switch strclass {
	case strIsInteger:
		_, ok := i.GetWideNoErr(s)
		i.SetResultBool(ok)
		return OK
	case strIsDouble:
		// Jim_GetDouble() may accept an out-of-range value such as 1e400, but
		// then strtod() has set errno to ERANGE, which the C rejects here
		d, drc := i.GetDouble(s)
		i.SetResultBool(drc == OK && !strDoubleRangeErr(s.String(), d))
		return OK
	case strIsBoolean:
		_, brc := i.GetBoolean(s)
		i.SetResultBool(brc == OK)
		return OK
	case strIsAlpha:
		isclass = strIsAlphaByte
	case strIsAlnum:
		isclass = strIsAlnumByte
	case strIsASCII:
		isclass = strIsASCIIByte
	case strIsDigit:
		isclass = strIsDigitByte
	case strIsLower:
		isclass = strIsLowerByte
	case strIsUpper:
		isclass = strIsUpperByte
	case strIsSpace:
		isclass = u8.IsSpace
	case strIsXdigit:
		isclass = strIsXdigitByte
	case strIsControl:
		isclass = strIsCntrlByte
	case strIsPrint:
		isclass = strIsPrintByte
	case strIsGraph:
		isclass = strIsGraphByte
	case strIsPunct:
		isclass = strIsPunctByte
	default:
		return ERR
	}

	// The classes above are tested byte by byte, not character by character
	for _, c := range str {
		if !isclass(c) {
			i.SetResultBool(false)
			return OK
		}
	}
	i.SetResultBool(true)
	return OK
}

// CompareStringImmediate reports whether an object's string representation is
// the given literal (C: Jim_CompareStringImmediate). The C also caches the
// matching literal pointer in the object, which is only an optimisation.
func (i *Interp) CompareStringImmediate(o *Obj, s string) bool {
	return o.String() == s
}

// strMinNormalDouble is DBL_MIN, the smallest double that is not subnormal.
const strMinNormalDouble = 2.2250738585072014e-308

// strDoubleRangeErr reports whether converting s to the double d is the
// "errno == ERANGE" that [string is double] rejects.
//
// strtod() flags a range error when it overflows to an infinity, when it
// underflows to zero and when the result is only representable as a subnormal,
// so "1e400", "1e-400" and "1e-320" are all rejected while the literals "inf"
// and "nan" are accepted.
func strDoubleRangeErr(s string, d float64) bool {
	if math.IsInf(d, 0) {
		return !strIsInfLiteral(s)
	}
	if d != 0 {
		return math.Abs(d) < strMinNormalDouble
	}
	return strHasNonzeroDigit(s)
}

// strIsInfLiteral reports whether s spells an infinity, which strtod() converts
// without a range error.
func strIsInfLiteral(s string) bool {
	s = strings.TrimLeft(s, " \t\n\v\f\r")
	s = strings.TrimLeft(s, "+-")
	return len(s) >= 3 && strings.EqualFold(s[:3], "inf")
}

// strHasNonzeroDigit reports whether the significand of a floating-point
// literal holds a non-zero digit, which tells a true zero from an underflow.
func strHasNonzeroDigit(s string) bool {
	s = strings.TrimLeft(s, " \t\n\v\f\r+-")
	hex := false
	if len(s) > 1 && s[0] == '0' && (s[1]|0x20) == 'x' {
		hex, s = true, s[2:]
	}
	for n := 0; n < len(s); n++ {
		c := s[n] | 0x20
		if hex && c == 'p' || !hex && c == 'e' {
			return false // start of the exponent
		}
		if s[n] >= '1' && s[n] <= '9' || hex && c >= 'a' && c <= 'f' {
			return true
		}
	}
	return false
}
