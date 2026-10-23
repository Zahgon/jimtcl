/* Jim Tcl 0.84 ported from C to Go: glob-style pattern matching.
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

import "github.com/msteveb/jimtcl/internal/u8"

// Flags for charsetMatch(), in addition to CaseSens and NoCase.
const (
	charsetGlob = 0 // [string match]: '^' and a leading ']' are ordinary
	charsetScan = 2 // [scan]: '^' negates the set and a leading ']' is literal
)

// charsetMatch matches the code point c against the character set that starts
// at pat[p], e.g. the "^a-z\xc2\xb5" of "[^a-zµ]" (C: JimCharsetMatch).
// plen is the number of bytes from p to the end of the pattern; trailing
// characters after the set are ignored.
//
// It returns the index of the closing ']', or the index of the end of the
// pattern when the ']' is missing, together with whether c matched.
//
// Note that '^' only negates the set with charsetScan, which [scan] passes but
// [string match] does not: in a glob pattern "[^ab]" is a set of three
// characters, '^', 'a' and 'b'.
func charsetMatch(pat []byte, p, plen, c, flags int) (int, bool) {
	not := false
	match := false
	nocase := flags&NoCase != 0
	if nocase {
		c = u8.Upper(c)
	}

	// first replaces the C's goto into the middle of the loop: a ']' straight
	// after the optional '^' is a member of the set rather than its end.
	first := false
	if flags&charsetScan != 0 {
		if u8.ByteAt(pat, p) == '^' {
			not = true
			p++
			plen--
		}
		if u8.ByteAt(pat, p) == ']' {
			first = true
		}
	}

	for first || (plen != 0 && u8.ByteAt(pat, p) != ']') {
		var pchar int
		// A backslash matches itself: the set syntax has no escapes, so "[\\]"
		// is the one-character set holding a backslash and the ']' ends it.
		if first || u8.ByteAt(pat, p) == '\\' {
			first = false
			var n int
			pchar, n = u8.ToUnicodeCase(u8.Slice(pat, p), nocase)
			p += n
			plen -= n
		} else {
			start, n := u8.ToUnicodeCase(u8.Slice(pat, p), nocase)
			p += n
			plen -= n
			if u8.ByteAt(pat, p) == '-' && plen > 1 {
				// A range, a-z. Reversed ranges such as z-a also match.
				end, en := u8.ToUnicodeCase(u8.Slice(pat, p+1), nocase)
				n = 1 + en
				p += n
				plen -= n
				if (c >= start && c <= end) || (c >= end && c <= start) {
					match = true
				}
				continue
			}
			pchar = start
		}

		if pchar == c {
			match = true
		}
	}
	if not {
		match = !match
	}
	return p, match
}

// globMatch reports whether the glob pattern matches s, honouring '*', '?',
// '[...]' and '\' escaping (C: JimGlobMatch).
func globMatch(pattern, s []byte, nocase bool) bool {
	return globMatchAt(pattern, 0, len(pattern), s, 0, len(s), nocase)
}

// globMatchAt is JimGlobMatch() with the pattern and string positions kept as
// offsets, since the C advances pointers while decrementing the remaining
// lengths. Note that the remaining string length can go negative, when the
// pattern asks for a character that is not there, and that the C relies on that
// to keep the loop going; the positions are always read through u8.ByteAt() and
// u8.Slice(), which stand in for the NUL terminator.
func globMatchAt(pat []byte, p, plen int, str []byte, sp, slen int, nocase bool) bool {
	for plen != 0 {
		switch u8.ByteAt(pat, p) {
		case '*':
			for u8.ByteAt(pat, p+1) == '*' && plen != 0 {
				p++
				plen--
			}
			p++
			plen--
			if plen == 0 {
				return true // match
			}
			for slen != 0 {
				// Does the remaining pattern match anywhere from here on?
				if globMatchAt(pat, p, plen, str, sp, slen, nocase) {
					return true // match
				}
				_, n := u8.ToUnicode(u8.Slice(str, sp))
				sp += n
				slen -= n
			}
			return false // no match

		case '?':
			_, n := u8.ToUnicode(u8.Slice(str, sp))
			sp += n
			slen -= n

		case '[':
			c, n := u8.ToUnicode(u8.Slice(str, sp))
			sp += n
			slen -= n
			np, ok := charsetMatch(pat, p+1, plen-1, c, boolFlag(nocase, NoCase)|charsetGlob)
			if !ok {
				return false
			}
			plen -= np - p
			p = np
			if plen == 0 {
				// Ran out of pattern (no ']')
				continue
			}

		case '\\':
			if u8.ByteAt(pat, p+1) != 0 {
				p++
				plen--
			}
			fallthrough

		default:
			c, n := u8.ToUnicodeCase(u8.Slice(str, sp), nocase)
			sp += n
			slen -= n
			pchar, _ := u8.ToUnicodeCase(u8.Slice(pat, p), nocase)
			if pchar != c {
				return false
			}
		}

		_, n := u8.ToUnicodeCase(u8.Slice(pat, p), nocase)
		p += n
		plen -= n
		if slen == 0 {
			for u8.ByteAt(pat, p) == '*' && plen != 0 {
				p++
				plen--
			}
			break
		}
	}
	return plen == 0 && slen == 0
}

// boolFlag returns flag when b is set, so that a Go bool can be passed where
// the C passes a flag word.
func boolFlag(b bool, flag int) int {
	if b {
		return flag
	}
	return 0
}
