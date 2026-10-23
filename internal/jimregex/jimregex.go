/* Jim Tcl 0.84 ported from C to Go: Jim's own regular expression engine.
 *
 * regcomp and regexec, ported from jimregexp.c.
 *
 * Copyright (c) 1986 by University of Toronto, written by Henry Spencer.
 * Copyright 2010 Steve Bennett <steveb@workware.net.au>
 *
 * This is an altered version of Spencer's regexp package: the C was altered by
 * John Gilmore, James A. Woods, Christopher Seiwald and finally by Steve
 * Bennett, who removed the static state and added Tcl ARE compatibility
 * (counted repetitions, UTF-8, character classes, shorthand character classes
 * and non-capturing parentheses); this is a translation of that C into Go.
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

// Package jimregex implements the regular expression engine of Jim Tcl: a
// backtracking matcher with Tcl's "advanced regular expression" syntax.
package jimregex

import (
	"strings"

	"github.com/msteveb/jimtcl/internal/u8"
)

// Compile flags, as in jimregexp.h. Extended is accepted for compatibility
// with POSIX regcomp(): this engine is always "extended".
const (
	Extended      = 0
	ICase         = 2
	NewlineAnchor = 4
	NewlineStop   = 8
	Newline       = NewlineAnchor | NewlineStop
	NotBOL        = 16
	Expanded      = 32
)

// An arbitrary limit, but this seems enough. Must be less than 1000.
const maxParen = 100

// Opcodes. Each node is one word of opcode followed by one word of "next"
// pointer (a positive offset from the opcode), possibly followed by an operand.
const (
	opEnd     = 0  // no   End of program.
	opBOL     = 1  // no   Match "" at beginning of line.
	opEOL     = 2  // no   Match "" at end of line.
	opAny     = 3  // no   Match any one character.
	opAnyOf   = 4  // str  Match any character in this string.
	opAnyBut  = 5  // str  Match any character not in this string.
	opBranch  = 6  // node Match this alternative, or the next...
	opBack    = 7  // no   Match "", "next" ptr points backward.
	opExactly = 8  // str  Match this string.
	opNothing = 9  // no   Match empty string.
	opRep     = 10 // max,min Match this (simple) thing [min,max] times.
	opRepMin  = 11 // max,min As REP, but a minimal match.
	opRepX    = 12 // max,min Match this (complex) thing [min,max] times.
	opRepXMin = 13 // max,min As REPX, but a minimal match.
	opBOLX    = 14 // no   Match "" at beginning of input.
	opEOLX    = 15 // no   Match "" at end of input.
	opWordA   = 16 // no   Match "" at wordchar, where prev is nonword.
	opWordZ   = 17 // no   Match "" at nonwordchar, where prev is word.

	opOpenNC = 1000 // no   Non-capturing parentheses - must be OPEN-1.
	opOpen   = 1001 // no   Mark this point in input as start of #n.

	opCloseNC = 2000 // no   Non-capturing parentheses - must be CLOSE-1.
	opClose   = 2001 // no   Analogous to OPEN.

	opCloseEnd = opClose + maxParen
)

// The first word of the compiled program is this magic number, so that program
// offsets are never 0 and 0 can be used as a "no node" marker.
const regMagic = 0xFADED00D

// Flags passed up and down the parser.
const (
	flagWorst    = 0 // worst case
	flagHasWidth = 1 // known never to match null string
	flagSimple   = 2 // simple enough to be a REP operand
	flagSPStart  = 4 // starts with * or +
)

const maxRepCount = 1000000

// Characters with a special meaning anywhere in a pattern. Note that the
// repetition characters are not here: str_is_mult() handles those.
const metaChars = "^$.[()|"

// Error is a compile error; its text is the one jim_regerror() produces.
type Error int

// Error codes, in the order of jimregexp.h so that they index errorStrings.
// Some of them cannot occur in the Go port (there is no allocation failure and
// no null or corrupted program) but the numbering is kept intact.
const (
	errNoError Error = iota
	errNoMatch
	errBadPat
	errNullArgument
	errUnknown
	errTooBig
	errNoMem
	errTooManyParen
	errUnmatchedParen
	errUnmatchedBraces
	errBadCount
	errJunkOnEnd
	errOperandCouldBeEmpty
	errNestedCount
	errInternal
	errCountFollowsNothing
	errInvalidEscape
	errCorrupted
	errNullChar
	errUnmatchedBracket
	errNum
)

var errorStrings = [errNum]string{
	"success",
	"no match",
	"bad pattern",
	"null argument",
	"unknown error",
	"too big",
	"out of memory",
	"too many ()",
	"parentheses () not balanced",
	"braces {} not balanced",
	"invalid repetition count(s)",
	"extra characters",
	"*+ of empty atom",
	"nested count",
	"internal error",
	"count follows nothing",
	"invalid escape \\ sequence",
	"corrupted program",
	"contains null char",
	"brackets [] not balanced",
}

func (e Error) Error() string {
	if e < 0 || e >= errNum {
		return "Bad error code"
	}
	return errorStrings[e]
}

// Regexp is a compiled regular expression (C: regex_t).
//
// The "internal use only" fields regstart, reganch, regmust and regmlen pass
// information from the compile phase to the execute phase that permits very
// fast decisions on suitable starting points for a match.
type Regexp struct {
	nsub   int // number of parenthesized subexpressions
	cflags int // flags used when compiling

	regstart int // char that must begin a match; 0 if none obvious
	reganch  int // is the match anchored (at beginning-of-line only)?
	regmust  int // offset of a string the match must include, or 0
	regmlen  int // length of the regmust string

	program []int // the compiled program

	// working state of the parser
	pat string // the pattern being parsed
	pp  int    // input-scan position (C: regparse)
	err Error
}

// MaxSub returns the number of parenthesized subexpressions in the pattern
// (C: re_nsub).
func (re *Regexp) MaxSub() int { return re.nsub }

// op returns the opcode of the node at p (C: OP).
func (re *Regexp) op(p int) int {
	if p < 0 || p >= len(re.program) {
		return opEnd
	}
	return re.program[p]
}

// operand returns the offset of the operand of the node at p (C: OPERAND).
func operand(p int) int { return p + 2 }

// b returns the pattern byte at i, or 0 at and past the end of the pattern,
// which is where the C finds the NUL terminator.
func (re *Regexp) b(i int) int { return int(u8.ByteAt(re.pat, i)) }

// cur returns the byte at the scan position.
func (re *Regexp) cur() int { return re.b(re.pp) }

// strIsMult reports whether the pattern at i is '*', '+', '?' or {n...} where
// n must be a number (C: str_is_mult).
func (re *Regexp) strIsMult(i int) bool {
	switch re.b(i) {
	case '*', '+', '?':
		return true
	case '{':
		return u8.IsDigit(byte(re.b(i + 1)))
	}
	return false
}

// strIntLen returns the length of the NUL-terminated sequence of program words
// at p (C: str_int_len).
func (re *Regexp) strIntLen(p int) int {
	n := 0
	for p+n < len(re.program) && re.program[p+n] != 0 {
		n++
	}
	return n
}

// expandedNewPattern removes white space and comments from a pattern compiled
// with Expanded (C: reg_expanded_new_pattern).
//
// The C sets an "escape" flag after a backslash but then continues the loop
// without consuming anything, so the flag only costs an iteration: backslash
// escapes are not honoured in expanded mode, and "\ " drops the space.
func expandedNewPattern(exp string) string {
	d := make([]byte, 0, len(exp))
	for s := 0; s < len(exp); {
		switch c := exp[s]; {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\f' || c == '\v':
			s++
		case c == '#':
			// skip comments to end of line
			for s++; s < len(exp) && exp[s] != '\n'; s++ {
			}
		default:
			d = append(d, c)
			s++
		}
	}
	return string(d)
}

// Compile compiles a regular expression into internal code (C: jim_regcomp).
//
// Unlike the C, which compiles the pattern twice, the program simply grows as
// it is emitted.
func Compile(pattern string, cflags int) (*Regexp, error) {
	re := &Regexp{cflags: cflags}

	exp := pattern
	if cflags&Expanded != 0 {
		exp = expandedNewPattern(exp)
	}
	re.pat = exp
	re.program = make([]int, 0, (len(exp)+1)*5)

	// Note that since we store a magic value as the first item in the program,
	// program offsets will never be 0.
	re.regc(regMagic)
	_, flags, ok := re.reg(false)
	if !ok {
		return nil, re.err
	}

	// Small enough for pointer-storage convention?
	if re.nsub >= maxParen {
		return nil, errTooBig
	}

	// Dig out information for optimizations.
	scan := 1 // first BRANCH
	if re.op(re.regnext(scan)) == opEnd {
		// Only one top-level choice.
		scan = operand(scan)

		// Starting-point info.
		if re.op(scan) == opExactly {
			re.regstart = re.program[operand(scan)]
		} else if re.op(scan) == opBOL {
			re.reganch++
		}

		// If there's something expensive in the r.e., find the longest literal
		// string that must appear and make it the regmust. Resolve ties in
		// favour of later strings, since the regstart check works with the
		// beginning of the r.e. and avoiding duplication strengthens checking.
		if flags&flagSPStart != 0 {
			longest := 0
			length := 0
			for ; scan != 0; scan = re.regnext(scan) {
				if re.op(scan) == opExactly {
					if plen := re.strIntLen(operand(scan)); plen >= length {
						longest = operand(scan)
						length = plen
					}
				}
			}
			re.regmust = longest
			re.regmlen = length
		}
	}

	re.pat = ""
	return re, nil
}

// reg parses a regular expression, i.e. the main body or a parenthesized
// thing; the caller must absorb the opening parenthesis (C: reg).
func (re *Regexp) reg(paren bool) (ret, flagp int, ok bool) {
	flagp = flagHasWidth // tentatively
	parno := 0

	// Make an OPEN node, if parenthesized.
	if paren {
		if re.b(re.pp) == '?' && re.b(re.pp+1) == ':' {
			// non-capturing paren
			re.pp += 2
			parno = -1
		} else {
			re.nsub++
			parno = re.nsub
		}
		ret = re.regnode(opOpen + parno)
	}

	// Pick up the branches, linking them together.
	br, flags, ok := re.regbranch()
	if !ok {
		return 0, flagp, false
	}
	if ret != 0 {
		re.regtail(ret, br) // OPEN -> first
	} else {
		ret = br
	}
	if flags&flagHasWidth == 0 {
		flagp &^= flagHasWidth
	}
	flagp |= flags & flagSPStart
	for re.cur() == '|' {
		re.pp++
		br, flags, ok = re.regbranch()
		if !ok {
			return 0, flagp, false
		}
		re.regtail(ret, br) // BRANCH -> BRANCH
		if flags&flagHasWidth == 0 {
			flagp &^= flagHasWidth
		}
		flagp |= flags & flagSPStart
	}

	// Make a closing node, and hook it on the end.
	ender := opEnd
	if paren {
		ender = opClose + parno
	}
	ender = re.regnode(ender)
	re.regtail(ret, ender)

	// Hook the tails of the branches to the closing node.
	for br = ret; br != 0; br = re.regnext(br) {
		re.regoptail(br, ender)
	}

	// Check for proper termination.
	if paren {
		c := re.cur()
		re.pp++
		if c != ')' {
			re.err = errUnmatchedParen
			return 0, flagp, false
		}
	} else if re.cur() != 0 {
		if re.cur() == ')' {
			re.err = errUnmatchedParen
		} else {
			re.err = errJunkOnEnd
		}
		return 0, flagp, false
	}

	return ret, flagp, true
}

// regbranch parses one alternative of an | operator, implementing the
// concatenation operator (C: regbranch).
func (re *Regexp) regbranch() (ret, flagp int, ok bool) {
	flagp = flagWorst // tentatively

	ret = re.regnode(opBranch)
	chain := 0
	for re.cur() != 0 && re.cur() != ')' && re.cur() != '|' {
		latest, flags, ok := re.regpiece()
		if !ok {
			return 0, flagp, false
		}
		flagp |= flags & flagHasWidth
		if chain == 0 {
			// First piece.
			flagp |= flags & flagSPStart
		} else {
			re.regtail(chain, latest)
		}
		chain = latest
	}
	if chain == 0 {
		// Loop ran zero times.
		re.regnode(opNothing)
	}

	return ret, flagp, true
}

// strtoul parses an unsigned decimal number at i the way C's strtoul() does:
// leading white space and a sign are allowed, the value saturates at
// ULONG_MAX, and storing it in the C's int min/max truncates it. If there are
// no digits, end is the starting position.
func (re *Regexp) strtoul(i int) (val, end int) {
	p := i
	for u8.IsSpace(byte(re.b(p))) {
		p++
	}
	neg := false
	if c := re.b(p); c == '+' || c == '-' {
		neg = c == '-'
		p++
	}
	if !u8.IsDigit(byte(re.b(p))) {
		return 0, i
	}
	var v uint64
	for u8.IsDigit(byte(re.b(p))) {
		d := uint64(re.b(p) - '0')
		if v > (^uint64(0)-d)/10 {
			v = ^uint64(0)
		} else {
			v = v*10 + d
		}
		p++
	}
	if neg {
		v = -v
	}
	return int(int32(uint32(v))), p
}

// regpiece parses something followed by a possible repetition count
// (C: regpiece).
func (re *Regexp) regpiece() (ret, flagp int, ok bool) {
	ret, flags, ok := re.regatom()
	if !ok {
		return 0, flagp, false
	}

	if !re.strIsMult(re.pp) {
		return ret, flags, true
	}

	op := re.cur()
	if flags&flagHasWidth == 0 && op != '?' {
		re.err = errOperandCouldBeEmpty
		return 0, flagp, false
	}

	var min, max int

	// Handle braces (counted repetition) by expansion
	if op == '{' {
		var end int
		min, end = re.strtoul(re.pp + 1)
		if end == re.pp+1 {
			re.err = errBadCount
			return 0, flagp, false
		}
		switch re.b(end) {
		case '}':
			max = min
		case 0:
			re.err = errUnmatchedBraces
			return 0, flagp, false
		default:
			re.pp = end
			max, end = re.strtoul(re.pp + 1)
			if re.b(end) != '}' {
				re.err = errUnmatchedBraces
				return 0, flagp, false
			}
		}
		if end == re.pp+1 {
			max = maxRepCount
		} else if max < min || max >= 100 {
			re.err = errBadCount
			return 0, flagp, false
		}
		if min >= 100 {
			re.err = errBadCount
			return 0, flagp, false
		}

		brace := u8.IndexByte(re.pat, re.pp, '}')
		if brace < 0 {
			re.err = errUnmatchedBraces
			return 0, flagp, false
		}
		re.pp = brace
	} else {
		if op == '+' {
			min = 1
		}
		max = maxRepCount
		if op == '?' {
			max = 1
		}
	}

	var next int
	if re.b(re.pp+1) == '?' {
		re.pp++
		if flags&flagSimple != 0 {
			next = re.reginsert(opRepMin, 5, ret)
		} else {
			next = re.reginsert(opRepXMin, 5, ret)
		}
	} else if flags&flagSimple != 0 {
		next = re.reginsert(opRep, 5, ret)
	} else {
		next = re.reginsert(opRepX, 5, ret)
	}
	re.program[ret+2] = max
	re.program[ret+3] = min
	re.program[ret+4] = 0

	flagp = flagWorst | flagSPStart
	if min != 0 {
		flagp = flagWorst | flagHasWidth
	}

	if flags&flagSimple == 0 {
		back := re.regnode(opBack)
		re.regtail(back, ret)
		re.regtail(next, back)
	}

	re.pp++
	if re.strIsMult(re.pp) {
		re.err = errNestedCount
		return 0, flagp, false
	}

	return ret, flagp, true
}

// regAddrange adds all characters in the inclusive range between lower and
// upper, handling a swapped range (C: reg_addrange).
func (re *Regexp) regAddrange(lower, upper int) {
	if lower > upper {
		lower, upper = upper, lower
	}
	// Add a range as length, start
	re.regc(upper - lower + 1)
	re.regc(lower)
}

// regAddrangeStr adds a literal string as a set of single character ranges
// (C: reg_addrange_str).
func (re *Regexp) regAddrangeStr(str string) {
	for i := 0; i < len(str); i++ {
		re.regAddrange(int(str[i]), int(str[i]))
	}
}

// regToUnicodeCase extracts the next code point at i, converting it to upper
// case if upper is set (C: reg_utf8_tounicode_case).
func (re *Regexp) regToUnicodeCase(i int, upper bool) (uc, n int) {
	uc, n = u8.ToUnicode(u8.Slice(re.pat, i))
	if upper {
		uc = u8.Upper(uc)
	}
	return uc, n
}

// hexdigitval converts a hex digit to its value, or -1 (C: hexdigitval).
func hexdigitval(c int) int {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	}
	return -1
}

// parseHex parses up to n hex digits at i and returns the value and the number
// of digits parsed; if there are no hex digits it returns 0, 0 (C: parse_hex).
func (re *Regexp) parseHex(i, n int) (val, digits int) {
	for k := 0; k < n; k++ {
		c := hexdigitval(re.b(i + k))
		if c == -1 {
			break
		}
		val = (val << 4) | c
		digits++
	}
	if digits == 0 {
		return 0, 0
	}
	return val, digits
}

// regDecodeEscape decodes the escape sequence that follows a backslash at i
// and returns the character and the number of bytes consumed
// (C: reg_decode_escape).
func (re *Regexp) regDecodeEscape(i int) (ch, used int) {
	s := i
	ch = re.b(s)
	s++

	switch ch {
	case 'b':
		ch = '\b'
	case 'e':
		ch = 27
	case 'f':
		ch = '\f'
	case 'n':
		ch = '\n'
	case 'r':
		ch = '\r'
	case 't':
		ch = '\t'
	case 'v':
		ch = '\v'
	case 'u':
		if re.b(s) == '{' {
			// Expect \u{NNNN}
			val, n := re.parseHex(s+1, 6)
			if n > 0 && re.b(s+n+1) == '}' && val >= 0 && val <= 0x1fffff {
				ch = val
				s += n + 2
			} else {
				// Invalid, so just treat as an escaped 'u'
				ch = 'u'
			}
		} else if val, n := re.parseHex(s, 4); n > 0 {
			ch = val
			s += n
		}
	case 'U':
		if val, n := re.parseHex(s, 8); n > 0 {
			ch = val
			s += n
		}
	case 'x':
		if val, n := re.parseHex(s, 2); n > 0 {
			ch = val
			s += n
		}
	case 0:
		s--
		ch = '\\'
	}
	return ch, s - i
}

// The POSIX character classes, in the order of the C's enum.
const (
	ccAlpha = iota
	ccAlnum
	ccSpace
	ccBlank
	ccUpper
	ccLower
	ccDigit
	ccXdigit
	ccCntrl
	ccGraph
	ccPrint
	ccPunct
	ccNum
)

var characterClass = [ccNum]string{
	":alpha:", ":alnum:", ":space:", ":blank:", ":upper:", ":lower:",
	":digit:", ":xdigit:", ":cntrl:", ":graph:", ":print:", ":punct:",
}

// addCharClass emits the ranges of the given character class.
func (re *Regexp) addCharClass(cc int) {
	switch cc {
	case ccAlnum:
		re.regAddrange('0', '9')
		fallthrough
	case ccAlpha:
		// With ICase the subject is upper-cased before matching, so only the
		// upper-case range is needed.
		if re.cflags&ICase == 0 {
			re.regAddrange('a', 'z')
		}
		re.regAddrange('A', 'Z')
	case ccSpace:
		re.regAddrangeStr(" \t\r\n\f\v")
	case ccBlank:
		re.regAddrangeStr(" \t")
	case ccUpper:
		re.regAddrange('A', 'Z')
	case ccLower:
		re.regAddrange('a', 'z')
	case ccXdigit:
		re.regAddrange('a', 'f')
		re.regAddrange('A', 'F')
		fallthrough
	case ccDigit:
		re.regAddrange('0', '9')
	case ccCntrl:
		re.regAddrange(0, 31)
		re.regAddrange(127, 127)
	case ccPrint:
		re.regAddrange(' ', '~')
	case ccGraph:
		re.regAddrange('!', '~')
	case ccPunct:
		re.regAddrange('!', '/')
		re.regAddrange(':', '@')
		re.regAddrange('[', '`')
		re.regAddrange('{', '~')
	}
}

// regbracket parses a bracket expression; the scan position is just past the
// opening '[' (the "case '['" of C's regatom).
func (re *Regexp) regbracket() (ret, flagp int, ok bool) {
	nocase := re.cflags&ICase != 0
	pattern := re.pp

	if re.b(pattern) == '^' {
		// Complement of range.
		ret = re.regnode(opAnyBut)
		pattern++
	} else {
		ret = re.regnode(opAnyOf)
	}

	// Special case. If the first char is ']' or '-', it is part of the set
	if c := re.b(pattern); c == ']' || c == '-' {
		re.regAddrange(c, c)
		pattern++
	}

	for re.b(pattern) != ']' {
		if re.b(pattern) == 0 {
			re.err = errUnmatchedBracket
			return 0, flagp, false
		}

		start, n := re.regToUnicodeCase(pattern, nocase)
		pattern += n
		if start == '\\' {
			// First check for class shorthand escapes
			cc := -1
			switch re.b(pattern) {
			case 's':
				pattern++
				cc = ccSpace
			case 'd':
				pattern++
				cc = ccDigit
			case 'w':
				pattern++
				re.regAddrange('_', '_')
				cc = ccAlnum
			}
			if cc >= 0 {
				re.addCharClass(cc)
				continue
			}
			var used int
			start, used = re.regDecodeEscape(pattern)
			pattern += used
			if start == 0 {
				re.err = errNullChar
				return 0, flagp, false
			}
			if start == '\\' && re.b(pattern) == 0 {
				re.err = errInvalidEscape
				return 0, flagp, false
			}
		}
		if re.b(pattern) == '-' && re.b(pattern+1) != 0 && re.b(pattern+1) != ']' {
			var end int
			// skip '-'
			_, n = u8.ToUnicode(u8.Slice(re.pat, pattern))
			pattern += n
			end, n = re.regToUnicodeCase(pattern, nocase)
			pattern += n
			if end == '\\' {
				var used int
				end, used = re.regDecodeEscape(pattern)
				pattern += used
				if end == 0 {
					re.err = errNullChar
					return 0, flagp, false
				}
				if end == '\\' && re.b(pattern) == 0 {
					re.err = errInvalidEscape
					return 0, flagp, false
				}
			}

			re.regAddrange(start, end)
			continue
		}
		if start == '[' && re.b(pattern) == ':' {
			cc := 0
			for ; cc < ccNum; cc++ {
				name := characterClass[cc]
				if strings.HasPrefix(re.pat[min(pattern, len(re.pat)):], name) {
					if re.b(pattern+len(name)) != ']' {
						re.err = errUnmatchedBracket
						return 0, flagp, false
					}
					// Found a character class
					pattern += len(name) + 1
					break
				}
			}
			if cc != ccNum {
				re.addCharClass(cc)
				continue
			}
		}
		// Not a range, so just add the char
		re.regAddrange(start, start)
	}
	re.regc(0)

	if re.b(pattern) != 0 {
		pattern++
	}
	re.pp = pattern

	return ret, flagHasWidth | flagSimple, true
}

// regatom parses the lowest level of the grammar (C: regatom).
//
// Optimization: it gobbles an entire sequence of ordinary characters so that
// they can be turned into a single node.
func (re *Regexp) regatom() (ret, flagp int, ok bool) {
	nocase := re.cflags&ICase != 0

	ch, n := re.regToUnicodeCase(re.pp, nocase)
	flagp = flagWorst // tentatively
	re.pp += n

	switch ch {
	case '^':
		ret = re.regnode(opBOL)
	case '$':
		ret = re.regnode(opEOL)
	case '.':
		ret = re.regnode(opAny)
		flagp |= flagHasWidth | flagSimple
	case '[':
		return re.regbracket()
	case '(':
		var flags int
		ret, flags, ok = re.reg(true)
		if !ok {
			return 0, flagp, false
		}
		flagp |= flags & (flagHasWidth | flagSPStart)
	case 0, '|', ')':
		// Supposed to be caught earlier.
		re.err = errInternal
		return 0, flagp, false
	case '\\':
		esc := re.b(re.pp)
		re.pp++
		switch esc {
		case 0:
			re.err = errInvalidEscape
			return 0, flagp, false
		case 'A':
			ret = re.regnode(opBOLX)
		case 'Z':
			ret = re.regnode(opEOLX)
		case '<', 'm':
			ret = re.regnode(opWordA)
		case '>', 'M':
			ret = re.regnode(opWordZ)
		case 'd', 'D':
			if esc == 'd' {
				ret = re.regnode(opAnyOf)
			} else {
				ret = re.regnode(opAnyBut)
			}
			re.regAddrange('0', '9')
			re.regc(0)
			flagp |= flagHasWidth | flagSimple
		case 'w', 'W':
			if esc == 'w' {
				ret = re.regnode(opAnyOf)
			} else {
				ret = re.regnode(opAnyBut)
			}
			if re.cflags&ICase == 0 {
				re.regAddrange('a', 'z')
			}
			re.regAddrange('A', 'Z')
			re.regAddrange('0', '9')
			re.regAddrange('_', '_')
			re.regc(0)
			flagp |= flagHasWidth | flagSimple
		case 's', 'S':
			if esc == 's' {
				ret = re.regnode(opAnyOf)
			} else {
				ret = re.regnode(opAnyBut)
			}
			re.regAddrangeStr(" \t\r\n\f\v")
			re.regc(0)
			flagp |= flagHasWidth | flagSimple
		default:
			// Handle general quoted chars in the exact-match routine, backing
			// up to include the backslash.
			re.pp--
			return re.regexactly(n)
		}
	default:
		return re.regexactly(n)
	}

	return ret, flagp, true
}

// regexactly encodes a string of characters to be matched exactly (the
// "de_fault" case of C's regatom). n is the length of the first character, to
// which the scan position is backed up.
func (re *Regexp) regexactly(n int) (ret, flagp int, ok bool) {
	added := 0

	// Back up to pick up the first char of interest
	re.pp -= n

	if re.strIsMult(re.pp) {
		re.err = errCountFollowsNothing
		return 0, flagWorst, false
	}

	ret = re.regnode(opExactly)

	// Note that a META operator such as ? or * consumes the preceding char.
	// Thus we must be careful to look ahead by 2 and add the last char as its
	// own EXACTLY if necessary.
	for re.cur() != 0 && strings.IndexByte(metaChars, byte(re.cur())) < 0 && !re.strIsMult(re.pp) {
		ch, n := re.regToUnicodeCase(re.pp, re.cflags&ICase != 0)
		if ch == '\\' && re.b(re.pp+n) != 0 {
			// Non-trailing backslash. Is this a special escape, or a regular
			// escape?
			if strings.IndexByte("<>mMwWdDsSAZ", byte(re.b(re.pp+n))) >= 0 {
				// A special escape. All done with EXACTLY
				break
			}
			// Decode it. Note that we add the length for the escape sequence
			// to the length for the backslash so we can skip the entire
			// sequence, or not as required.
			var used int
			ch, used = re.regDecodeEscape(re.pp + n)
			n += used
			if ch == 0 {
				re.err = errNullChar
				return 0, flagWorst, false
			}
		}

		// Now we have one char 'ch' of length 'n'. Check to see if the
		// following char is a MULT.
		if re.strIsMult(re.pp + n) {
			// Yes. But do we already have some EXACTLY chars?
			if added != 0 {
				// Yes, so return what we have and pick up the current char
				// next time around.
				break
			}
			// No, so add this single char and finish
			re.regc(ch)
			added++
			re.pp += n
			break
		}

		// No, so just add this char normally
		re.regc(ch)
		added++
		re.pp += n
	}
	re.regc(0)

	flagp = flagWorst | flagHasWidth
	if added == 1 {
		flagp |= flagSimple
	}
	return ret, flagp, true
}

// regnode emits a node and returns its location (C: regnode).
func (re *Regexp) regnode(op int) int {
	// The OP followed by a next pointer
	re.program = append(re.program, op, 0)
	return len(re.program) - 2
}

// regc emits one word of code (C: regc).
func (re *Regexp) regc(b int) {
	re.program = append(re.program, b)
}

// reginsert inserts an operator in front of an already-emitted operand,
// relocating the operand, and returns its new location (C: reginsert).
func (re *Regexp) reginsert(op, size, opnd int) int {
	re.program = append(re.program, make([]int, size)...)
	copy(re.program[opnd+size:], re.program[opnd:])
	for i := opnd; i < opnd+size; i++ {
		re.program[i] = 0
	}
	re.program[opnd] = op

	return opnd + size
}

// regtail sets the next-pointer at the end of a node chain (C: regtail).
func (re *Regexp) regtail(p, val int) {
	// Find last node.
	scan := p
	for {
		temp := re.regnext(scan)
		if temp == 0 {
			break
		}
		scan = temp
	}

	offset := val - scan
	if re.op(scan) == opBack {
		offset = scan - val
	}
	re.program[scan+1] = offset
}

// regoptail is regtail on the operand of the node at p; a no-op if the node
// has no operand (C: regoptail).
func (re *Regexp) regoptail(p, val int) {
	// "Operandless" and "op != BRANCH" are synonymous in practice.
	if p != 0 && re.op(p) == opBranch {
		re.regtail(operand(p), val)
	}
}

// regnext digs the "next" pointer out of the node at p (C: regnext).
func (re *Regexp) regnext(p int) int {
	if p+1 >= len(re.program) {
		return 0
	}
	offset := re.program[p+1]
	if offset == 0 {
		return 0
	}
	if re.op(p) == opBack {
		return p - offset
	}
	return p + offset
}

// regopsize returns the size in words of the opcode and operands at p
// (C: regopsize).
func (re *Regexp) regopsize(p int) int {
	// Almost all opcodes are 2 words, but some are more
	switch re.op(p) {
	case opRep, opRepMin, opRepX, opRepXMin:
		return 5
	case opAnyOf, opAnyBut, opExactly:
		s := p + 2
		for s < len(re.program) && re.program[s] != 0 {
			s++
		}
		return s + 1 - p
	}
	return 2
}
