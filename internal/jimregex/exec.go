/* Jim Tcl 0.84 ported from C to Go: the regular expression matcher.
 *
 * regexec and friends, ported from jimregexp.c.
 *
 * Copyright (c) 1986 by University of Toronto, written by Henry Spencer.
 * Copyright 2010 Steve Bennett <steveb@workware.net.au>
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

import "github.com/msteveb/jimtcl/internal/u8"

// matcher holds the state of one match attempt (the "working state - exec"
// fields of the C's regex_t).
type matcher struct {
	re     *Regexp
	s      string // the subject
	start  int    // where the match started: all offsets are computed from here
	input  int    // current input position (C: reginput)
	bol    int    // beginning of input, for ^ check (C: regbol)
	notbol bool   // the REG_NOTBOL exec flag
	pmatch []int  // submatch byte offsets: start, end, start, end, ...
}

// at returns the subject byte at i, or 0 at and past the end of the subject.
// Jim passes a NUL terminated string to the C, so an embedded NUL ends the
// subject here too.
func (m *matcher) at(i int) int { return int(u8.ByteAt(m.s, i)) }

// Exec matches the compiled expression against s, starting at the byte offset
// start, and returns the byte offsets of the whole match and of each capture
// group as start, end pairs; an unmatched group has both offsets set to -1.
//
// The subject is taken to begin at start, so "^" and "\A" match there and the
// returned offsets are absolute offsets into s. notbol is the REG_NOTBOL exec
// flag: the start of the subject is not the beginning of a line.
func (re *Regexp) Exec(s string, start int, notbol bool) ([]int, bool) {
	if len(re.program) == 0 || re.program[0] != regMagic {
		return nil, false
	}
	if start < 0 {
		start = 0
	} else if start > len(s) {
		start = len(s)
	}

	m := &matcher{re: re, s: s, start: start, notbol: notbol}
	m.pmatch = make([]int, 2*(re.nsub+1))

	// Must clear out the embedded repeat counts of REPX and REPXMIN opcodes.
	// The C keeps the current count inside the program itself, so a compiled
	// expression cannot be matched against two strings at once.
	for scan := operand(1); scan+4 < len(re.program); scan += re.regopsize(scan) {
		op := re.program[scan]
		if op == opEnd {
			break
		}
		if op == opRepX || op == opRepXMin {
			re.program[scan+4] = 0
		}
	}

	// If there is a "must appear" string, look for it.
	if re.regmust != 0 {
		found := false
		for p := start; ; p++ {
			p = m.strFind(p, re.program[re.regmust])
			if p < 0 {
				break
			}
			if m.prefixCmp(re.regmust, re.regmlen, p) >= 0 {
				found = true
				break
			}
		}
		if !found {
			return nil, false // not present
		}
	}

	// Mark beginning of line for ^ .
	m.bol = start

	// Simplest case: an anchored match need be tried only once (maybe per line).
	if re.reganch != 0 {
		// This is an anchored search; if it is not at a BOL, possibly skip to
		// the next line.
		try := !notbol
		pos := start
		for {
			if try {
				if m.regtry(pos) {
					return m.pmatch, true
				}
				if m.at(pos) == 0 {
					return nil, false
				}
			}
			try = true
			if re.cflags&NewlineAnchor != 0 {
				// Try the next anchor?
				if nl := u8.IndexByte(s, pos, '\n'); nl >= 0 {
					pos = nl + 1
					m.bol = pos
					continue
				}
			}
			return nil, false
		}
	}

	// Messy cases: unanchored match.
	if re.regstart != 0 {
		// We know what char it must start with.
		for p := start; ; p++ {
			p = m.strFind(p, re.regstart)
			if p < 0 {
				break
			}
			if m.regtry(p) {
				return m.pmatch, true
			}
		}
	} else {
		// We don't -- general case.
		for p := start; ; {
			if m.regtry(p) {
				return m.pmatch, true
			}
			if m.at(p) == 0 {
				break
			}
			_, n := u8.ToUnicode(u8.Slice(s, p))
			p += n
		}
	}

	// Failure.
	return nil, false
}

// regtry tries a match at a specific point (C: regtry).
func (m *matcher) regtry(pos int) bool {
	m.input = pos

	for i := range m.pmatch {
		m.pmatch[i] = -1
	}
	if m.regmatch(1) {
		m.pmatch[0] = pos
		m.pmatch[1] = m.input
		return true
	}
	return false
}

// prefixCmp returns the number of bytes matched if the program string at prog
// of proglen characters is a prefix of the subject at pos, or -1
// (C: prefix_cmp).
func (m *matcher) prefixCmp(prog, proglen, pos int) int {
	nocase := m.re.cflags&ICase != 0
	s := pos
	for proglen > 0 && m.at(s) != 0 {
		ch, n := m.toUnicodeCase(s, nocase)
		if ch != m.re.program[prog] {
			return -1
		}
		prog++
		s += n
		proglen--
	}
	if proglen == 0 {
		return s - pos
	}
	return -1
}

// toUnicodeCase extracts the next code point of the subject at i, converting
// it to upper case if upper is set (C: reg_utf8_tounicode_case).
func (m *matcher) toUnicodeCase(i int, upper bool) (uc, n int) {
	uc, n = u8.ToUnicode(u8.Slice(m.s, i))
	if upper {
		uc = u8.Upper(uc)
	}
	return uc, n
}

// regRangeFind reports whether c is in the set of ranges at p, which are
// stored as length, start pairs and terminated by a zero length
// (C: reg_range_find).
func (re *Regexp) regRangeFind(p, c int) bool {
	for p+1 < len(re.program) && re.program[p] != 0 {
		if c >= re.program[p+1] && c <= re.program[p]+re.program[p+1]-1 {
			return true
		}
		p += 2
	}
	return false
}

// strFind returns the byte position at or after pos where the character c
// occurs in the subject, or -1 (C: str_find). With ICase the subject is
// upper-cased as it is scanned, so c is upper-cased too.
func (m *matcher) strFind(pos, c int) int {
	nocase := m.re.cflags&ICase != 0
	if nocase {
		c = u8.Upper(c)
	}
	for m.at(pos) != 0 {
		ch, n := m.toUnicodeCase(pos, nocase)
		if c == ch {
			return pos
		}
		pos += n
	}
	return -1
}

// isEOL reports whether ch is an end-of-line char. In NewlineStop mode \n is
// considered EOL in addition to the end of the string (C: reg_iseol).
func (re *Regexp) isEOL(ch int) bool {
	if re.cflags&NewlineStop != 0 {
		return ch == 0 || ch == '\n'
	}
	return ch == 0
}

// regmatchsimplerepeat matches a REP or REPMIN node, whose operand is simple
// enough to be matched with regrepeat (C: regmatchsimplerepeat).
func (m *matcher) regmatchsimplerepeat(scan int, matchmin bool) bool {
	re := m.re
	nextch := 0

	max := re.program[scan+2]
	min := re.program[scan+3]
	next := re.regnext(scan)

	// Lookahead to avoid useless match attempts when we know what character
	// comes next.
	if re.op(next) == opExactly {
		nextch = re.program[operand(next)]
	}
	save := m.input
	no := m.regrepeat(scan+5, max)
	if no < min {
		return false
	}
	if matchmin {
		// from min up to no
		max = no
		no = min
	}
	// else from no down to min
	for {
		if matchmin {
			if no > max {
				break
			}
		} else if no < min {
			break
		}
		m.input = save + u8.ByteIndex(u8.Slice(m.s, save), no)
		c, _ := m.toUnicodeCase(m.input, re.cflags&ICase != 0)
		// If it could work, try it.
		if re.isEOL(nextch) || c == nextch {
			if m.regmatch(next) {
				return true
			}
		}
		if matchmin {
			// Couldn't or didn't, add one more
			no++
		} else {
			// Couldn't or didn't -- back up.
			no--
		}
	}
	return false
}

// regmatchrepeat matches a REPX or REPXMIN node by recursion, keeping the
// current count in the program (C: regmatchrepeat).
func (m *matcher) regmatchrepeat(scan int, matchmin bool) bool {
	re := m.re
	max := re.program[scan+2]
	min := re.program[scan+3]

	// Have we reached min?
	if re.program[scan+4] < min {
		// No, so get another one
		re.program[scan+4]++
		if m.regmatch(scan + 5) {
			return true
		}
		re.program[scan+4]--
		return false
	}
	if re.program[scan+4] > max {
		return false
	}

	if matchmin {
		// minimal, so try other branch first
		if m.regmatch(re.regnext(scan)) {
			return true
		}
		// No, so try one more
		re.program[scan+4]++
		if m.regmatch(scan + 5) {
			return true
		}
		re.program[scan+4]--
		return false
	}
	// maximal, so try this branch again
	if re.program[scan+4] < max {
		re.program[scan+4]++
		if m.regmatch(scan + 5) {
			return true
		}
		re.program[scan+4]--
	}
	// At this point we are at max with no match. Try the other branch
	return m.regmatch(re.regnext(scan))
}

// regmatch is the main matching routine (C: regmatch).
//
// Conceptually the strategy is simple: check to see whether the current node
// matches, call self recursively to see whether the rest matches, and then act
// accordingly. In practice we make some effort to avoid recursion, in
// particular by going through "ordinary" nodes by a loop instead.
func (m *matcher) regmatch(prog int) bool {
	re := m.re
	scan := prog

	for scan != 0 {
		next := re.regnext(scan)
		c, n := m.toUnicodeCase(m.input, re.cflags&ICase != 0)

		switch op := re.op(scan); op {
		case opBOLX:
			if m.notbol {
				return false
			}
			fallthrough
		case opBOL:
			if m.input != m.bol {
				return false
			}
		case opEOLX:
			// For EOLX, only match the real end of the string, not a newline
			if c != 0 {
				return false
			}
		case opEOL:
			if !re.isEOL(c) {
				return false
			}
		case opWordA:
			// Must be looking at a letter, digit, or _
			if !u8.IsAlnum(byte(c)) && c != '_' {
				return false
			}
			// Prev must be BOL or nonword
			if m.input > m.bol && (u8.IsAlnum(byte(m.at(m.input-1))) || m.at(m.input-1) == '_') {
				return false
			}
		case opWordZ:
			// Can't match at BOL
			if m.input <= m.bol {
				return false
			}
			// Current must be EOL or nonword
			if !re.isEOL(c) && (u8.IsAlnum(byte(c)) || c == '_') {
				return false
			}
			// Previous must be word
			if prev := m.at(m.input - 1); !u8.IsAlnum(byte(prev)) && prev != '_' {
				return false
			}
		case opAny:
			if re.isEOL(c) {
				return false
			}
			m.input += n
		case opExactly:
			opnd := operand(scan)
			slen := m.prefixCmp(opnd, re.strIntLen(opnd), m.input)
			if slen < 0 {
				return false
			}
			m.input += slen
		case opAnyOf:
			if re.isEOL(c) || !re.regRangeFind(operand(scan), c) {
				return false
			}
			m.input += n
		case opAnyBut:
			if re.isEOL(c) || re.regRangeFind(operand(scan), c) {
				return false
			}
			m.input += n
		case opNothing, opBack:
		case opBranch:
			if re.op(next) != opBranch {
				// No choice.
				next = operand(scan) // avoid recursion
			} else {
				for scan != 0 && re.op(scan) == opBranch {
					save := m.input
					if m.regmatch(operand(scan)) {
						return true
					}
					m.input = save
					scan = re.regnext(scan)
				}
				return false
			}
		case opRep, opRepMin:
			return m.regmatchsimplerepeat(scan, op == opRepMin)
		case opRepX, opRepXMin:
			return m.regmatchrepeat(scan, op == opRepXMin)
		case opEnd:
			return true // success!
		case opOpenNC, opCloseNC:
			return m.regmatch(next)
		default:
			if op >= opOpen+1 && op < opCloseEnd {
				// The capture is recorded on the way back, once the rest of
				// the expression has matched, and only if it is not set yet:
				// for a repeated group that keeps the last iteration.
				save := m.input
				if m.regmatch(next) {
					if op < opClose {
						if no := 2 * (op - opOpen); no < len(m.pmatch) && m.pmatch[no] == -1 {
							m.pmatch[no] = save
						}
					} else {
						if no := 2*(op-opClose) + 1; no < len(m.pmatch) && m.pmatch[no] == -1 {
							m.pmatch[no] = save
						}
					}
					return true
				}
				// Restore input position after failure
				m.input = save
				return false
			}
			// Corrupted program: the C returns an error code here, which its
			// callers cannot distinguish from a match.
			return false
		}

		scan = next
	}

	// We get here only if there's trouble -- normally "case END" is the
	// terminating point.
	return false
}

// regrepeat repeatedly matches something simple and reports how many times it
// matched (C: regrepeat).
func (m *matcher) regrepeat(p, max int) int {
	re := m.re
	nocase := re.cflags&ICase != 0
	count := 0
	scan := m.input
	opnd := operand(p)

	switch re.op(p) {
	case opAny:
		for !re.isEOL(m.at(scan)) && count < max {
			count++
			scan += u8.CharLen(byte(m.at(scan)))
		}
	case opExactly:
		for count < max {
			ch, n := m.toUnicodeCase(scan, nocase)
			if re.program[opnd] != ch {
				break
			}
			count++
			scan += n
		}
	case opAnyOf:
		for count < max {
			ch, n := m.toUnicodeCase(scan, nocase)
			if re.isEOL(ch) || !re.regRangeFind(opnd, ch) {
				break
			}
			count++
			scan += n
		}
	case opAnyBut:
		for count < max {
			ch, n := m.toUnicodeCase(scan, nocase)
			if re.isEOL(ch) || re.regRangeFind(opnd, ch) {
				break
			}
			count++
			scan += n
		}
	}
	m.input = scan

	return count
}
