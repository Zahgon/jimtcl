/*
 * UTF-8 utility functions, ported from utf8.c.
 *
 * (c) 2010-2016 Steve Bennett <steveb@workware.net.au>
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

// Package u8 is the interpreter's UTF-8 layer: the port of the original's
// utf8.c together with the case-mapping tables generated from UnicodeData.txt.
// The interpreter and the regular expression engine both use it, the one on
// byte slices and the other on strings, so every function is generic over the
// two.
package u8

// Bytes is the constraint of the text types the functions below accept: a
// string (used by the regular expression engine, where slicing is free) or a
// byte slice (used by the interpreter's string objects).
type Bytes interface {
	~string | ~[]byte
}

// maxUtf8Len is the longest sequence produced by utf8FromUnicode: Jim supports
// code points up to 2^21-1 only.
const maxUtf8Len = 4

// ByteAt returns s[idx], or 0 past the end. The C implementation works on
// NUL-terminated strings and relies on reading the terminator: the decoder
// rejects a truncated sequence because the NUL is not a continuation byte, and
// Index() treats the terminator as a one-byte character. Every such read is
// reproduced through this helper.
func ByteAt[T Bytes](s T, idx int) byte {
	if idx < 0 || idx >= len(s) {
		return 0
	}
	return s[idx]
}

// Slice returns s[idx:], or an empty result when idx is at or past the end,
// which is what the C sees once a pointer has walked onto the NUL terminator.
func Slice[T Bytes](s T, idx int) T {
	if idx >= len(s) {
		var empty T
		return empty
	}
	return s[idx:]
}

// FromUnicode encodes a code point as UTF-8 (C: utf8_fromunicode).
//
// Code points above 0x1fffff are silently truncated to 21 bits, exactly as the
// C does, which is why e.g. 0x200041 encodes as U+41 would not: the top bits
// are simply masked away.
func FromUnicode(uc int) []byte {
	u := uint(uc)
	switch {
	case u <= 0x7f:
		return []byte{byte(u)}
	case u <= 0x7ff:
		return []byte{byte(0xc0 | (u&0x7c0)>>6), byte(0x80 | u&0x3f)}
	case u <= 0xffff:
		return []byte{byte(0xe0 | (u&0xf000)>>12), byte(0x80 | (u&0xfc0)>>6), byte(0x80 | u&0x3f)}
	default:
		return []byte{byte(0xf0 | (u&0x1c0000)>>18), byte(0x80 | (u&0x3f000)>>12),
			byte(0x80 | (u&0xfc0)>>6), byte(0x80 | u&0x3f)}
	}
}

// CharLen returns the length of the UTF-8 sequence starting with byte c,
// 1 for a byte that cannot start a sequence (C: utf8_charlen).
func CharLen(c byte) int {
	switch {
	case c&0x80 == 0:
		return 1
	case c&0xe0 == 0xc0:
		return 2
	case c&0xf0 == 0xe0:
		return 3
	case c&0xf8 == 0xf0:
		return 4
	}
	// Invalid sequence, so treat it as a single byte
	return 1
}

// ToUnicode decodes the first UTF-8 sequence of s and returns the code
// point and the number of bytes consumed (C: utf8_tounicode, with the results
// in the opposite order).
//
// Anything that is not a well formed, non-overlong sequence is returned as the
// single leading byte: that is how Jim keeps arbitrary binary data in strings.
// An empty slice decodes as the NUL that terminates the C string, i.e. (0, 1),
// so callers must check the remaining length before advancing.
func ToUnicode[T Bytes](s T) (uc int, n int) {
	c0 := ByteAt(s, 0)
	switch {
	case c0 < 0xc0:
		return int(c0), 1
	case c0 < 0xe0:
		if ByteAt(s, 1)&0xc0 == 0x80 {
			uc = int(c0&^0xc0)<<6 | int(ByteAt(s, 1)&^0x80)
			if uc >= 0x80 {
				return uc, 2
			}
		}
	case c0 < 0xf0:
		if ByteAt(s, 1)&0xc0 == 0x80 && ByteAt(s, 2)&0xc0 == 0x80 {
			uc = int(c0&^0xe0)<<12 | int(ByteAt(s, 1)&^0x80)<<6 | int(ByteAt(s, 2)&^0x80)
			if uc >= 0x800 {
				return uc, 3
			}
		}
	case c0 < 0xf8:
		if ByteAt(s, 1)&0xc0 == 0x80 && ByteAt(s, 2)&0xc0 == 0x80 && ByteAt(s, 3)&0xc0 == 0x80 {
			uc = int(c0&^0xf0)<<18 | int(ByteAt(s, 1)&^0x80)<<12 |
				int(ByteAt(s, 2)&^0x80)<<6 | int(ByteAt(s, 3)&^0x80)
			if uc >= 0x10000 {
				return uc, 4
			}
		}
	}
	// Invalid sequence, so just return the byte
	return int(c0), 1
}

// ToUnicodeCase decodes one code point and folds it to upper case when
// upper is set (C: utf8_tounicode_case).
func ToUnicodeCase[T Bytes](s T, upper bool) (uc int, n int) {
	uc, n = ToUnicode(s)
	if upper {
		uc = Upper(uc)
	}
	return uc, n
}

// Strlen returns the number of characters in s (C: utf8_strlen). Bytes that
// are not part of a valid sequence count as one character each.
func Strlen[T Bytes](s T) int {
	charlen := 0
	for n := 0; n < len(s); {
		_, l := ToUnicode(s[n:])
		charlen++
		n += l
	}
	return charlen
}

// Index returns the byte offset of character charindex in s
// (C: utf8_index).
//
// Note that it steps with CharLen() rather than the decoder, so for a
// string holding an invalid sequence the result may disagree with Strlen()
// and may even run past the end of s; the C does the same and relies on the NUL
// terminator. A negative charindex gives 0, where the C loops about 2^32 times
// and walks off the end of the string.
func Index[T Bytes](s T, charindex int) int {
	idx := 0
	for ; charindex > 0; charindex-- {
		if idx >= len(s) {
			// Past the end only the NUL terminator is left, which counts as a
			// one-byte character; the C walks on into whatever follows it.
			return idx + charindex
		}
		idx += CharLen(s[idx])
	}
	return idx
}

// utf8ByteIndex is Index() clamped to the length of s, for the places where
// the C would index past the NUL terminator.
func ByteIndex[T Bytes](s T, charindex int) int {
	if idx := Index(s, charindex); idx < len(s) {
		return idx
	}
	return len(s)
}

// PrevLen returns how many bytes before s[pos] the previous UTF-8 sequence
// starts, looking back at most length bytes, which must be greater than zero
// (C: utf8_prev_len). If no start byte is found it returns length.
func PrevLen[T Bytes](s T, pos, length int) int {
	n := 1
	for length--; length != 0; length-- {
		if s[pos-n]&0x80 == 0 || s[pos-n]&0xc0 == 0xc0 {
			break
		}
		n++
	}
	return n
}

// Memchr returns the offset of the first character c in s, or -1
// (C: utf8_memchr).
func Memchr[T Bytes](s T, c int) int {
	for n := 0; n < len(s); {
		sc, l := ToUnicode(s[n:])
		if sc == c {
			return n
		}
		n += l
	}
	return -1
}

/* ---------------------------------------------------------------------------
 * Case mapping
 * ------------------------------------------------------------------------ */

// Casemap is one entry of a generated case mapping table: a code point and the
// code point of its other case (C: struct Casemap).
type Casemap struct {
	code    uint16
	altcode uint16
}

// MapCase looks ch up in a generated mapping table. Only the Basic
// Multilingual Plane is mapped; anything above U+FFFF is returned unchanged.
func MapCase(mapping []Casemap, ch int) int {
	if ch > 0xffff || ch < 0 {
		return ch
	}
	lo, hi := 0, len(mapping)
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		switch code := int(mapping[mid].code); {
		case code == ch:
			return int(mapping[mid].altcode)
		case code < ch:
			lo = mid + 1
		default:
			hi = mid
		}
	}
	return ch
}

// unicodeUpper returns the upper-case variant of a code point
// (C: utf8_upper).
func Upper(uc int) int {
	if IsASCII(uc) {
		return ASCIIUpper(uc)
	}
	return MapCase(unicodeCaseMappingUpper, uc)
}

// unicodeLower returns the lower-case variant of a code point
// (C: utf8_lower).
func Lower(uc int) int {
	if IsASCII(uc) {
		return ASCIILower(uc)
	}
	return MapCase(unicodeCaseMappingLower, uc)
}

// unicodeTitle returns the title-case variant of a code point, falling back to
// the upper-case variant (C: utf8_title).
//
// The generated table stores 0 for a code point whose title case is itself: the
// entry exists only to stop the fall back to upper case, which is how e.g.
// U+10D0 GEORGIAN LETTER AN is left alone by [string totitle].
func Title(uc int) int {
	if !IsASCII(uc) {
		if newch := MapCase(unicodeCaseMappingTitle, uc); newch != uc {
			if newch != 0 {
				return newch
			}
			return uc
		}
	}
	return Upper(uc)
}

// IsASCII reports whether c is a 7-bit character (C: isascii/jim_isascii).
func IsASCII(c int) bool { return c&^0x7f == 0 }

// strASCIIUpper and strASCIILower are toupper()/tolower() in the C locale, which is
// the only locale jimsh ever runs in.
func ASCIIUpper(c int) int {
	if c >= 'a' && c <= 'z' {
		return c - ('a' - 'A')
	}
	return c
}

func ASCIILower(c int) int {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}

/* The C-locale character tests the interpreter and the regular expression
 * engine share. The C truncates the code point to a byte first (its UCHAR()
 * macro), so these take a byte. */

// IsSpace reports whether c is a space character in the C locale.
func IsSpace(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\v', '\f', '\r':
		return true
	}
	return false
}

// IsDigit reports whether c is a decimal digit.
func IsDigit(c byte) bool { return c >= '0' && c <= '9' }

// IsAlpha reports whether c is an ASCII letter.
func IsAlpha(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

// IsAlnum reports whether c is an ASCII letter or digit.
func IsAlnum(c byte) bool { return IsAlpha(c) || IsDigit(c) }

// IndexByte returns the index of the byte c in s at or after i, stopping at the
// end of the string or at an embedded NUL (C: strchr), or -1.
func IndexByte[T Bytes](s T, i int, c byte) int {
	for ; i < len(s); i++ {
		if s[i] == c {
			return i
		}
		if s[i] == 0 {
			break
		}
	}
	return -1
}

// IsHexDigit reports whether c is a hexadecimal digit.
func IsHexDigit(c byte) bool { return IsDigit(c) || (c|0x20) >= 'a' && (c|0x20) <= 'f' }

// DigitVal returns the value of c as a digit in bases up to 36, or -1.
func DigitVal(c byte) int {
	switch {
	case IsDigit(c):
		return int(c - '0')
	case (c|0x20) >= 'a' && (c|0x20) <= 'z':
		return int(c|0x20-'a') + 10
	}
	return -1
}
