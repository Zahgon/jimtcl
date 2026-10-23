/* Jim - A small embeddable Tcl interpreter: the script, subst and list parsers.
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

import "github.com/msteveb/jimtcl/internal/u8"

// Token types produced by the parsers. The script, subst and expression
// parsers all share the same context, so the expression token types live
// next to the script ones (C: JIM_TT_*).
const (
	ttNone         = 0  // no token returned
	ttStr          = 1  // simple string
	ttEsc          = 2  // string that needs escape conversion
	ttVar          = 3  // variable substitution
	ttDictSugar    = 4  // syntax sugar for [dict get], $foo(bar)
	ttCmd          = 5  // command substitution
	ttSep          = 6  // word separator (white space)
	ttEOL          = 7  // line separator
	ttEOF          = 8  // end of script
	ttLine         = 9  // start-of-line token; arg is the number of words, -ve if {*}
	ttWord         = 10 // start-of-word token; arg is the number of tokens, -ve if {*}
	ttSubexprStart = 11
	ttSubexprEnd   = 12
	ttSubexprComma = 13
	ttExprInt      = 14
	ttExprDouble   = 15
	ttExprBoolean  = 16
	ttExprSugar    = 17 // $(expression)
	ttExprOp       = 20 // operator token types start here
)

// tokenIsSep reports whether a token type ends a word.
func tokenIsSep(t int) bool { return t >= ttSep && t <= ttEOF }

// parser is the parser context shared by the script, subst, list and
// expression parsers (C: struct JimParserCtx).
type parser struct {
	src     []byte // the whole program text
	p       int    // index of the point we are parsing
	linenr  int    // current line number
	tstart  int    // the returned token is src[tstart:tend+1]
	tend    int
	tline   int  // line number of the returned token
	tt      int  // token type
	eof     bool // set once the end of the program is reached
	inquote bool
	comment bool // the next char may start a comment
	// Details of any missing quote, brace or bracket: ' ' when the program is
	// complete, else the character that is missing and the line it started on.
	missingCh   byte
	missingLine int
	// errmsg carries the message of a failed expression parse, the one parser
	// error the C reports with a text of its own.
	errmsg string
}

// newParser initialises a parser context for the program text prg, whose
// first line is numbered linenr (C: JimParserInit).
func newParser(prg []byte, linenr int) *parser {
	return &parser{
		src:         prg,
		tt:          ttNone,
		linenr:      linenr,
		comment:     true,
		missingCh:   ' ',
		missingLine: linenr,
	}
}

// rem returns the number of unparsed bytes.
func (p *parser) rem() int { return len(p.src) - p.p }

// at returns the byte at offset n from the current position, or 0 past the
// end of the program. The C parser relies on the NUL terminator of the
// string representation in the same way.
func (p *parser) at(n int) byte {
	if p.p+n < len(p.src) && p.p+n >= 0 {
		return p.src[p.p+n]
	}
	return 0
}

// cur returns the byte at the current position, or 0 at the end.
func (p *parser) cur() byte { return p.at(0) }

// token returns the text of the current token. The end is clamped to the
// buffer because the expression parser leaves tend one past the last byte when
// it reaches the end of the expression.
func (p *parser) token() []byte {
	end := min(p.tend+1, len(p.src))
	if end <= p.tstart {
		return nil
	}
	return p.src[p.tstart:end]
}

/* ---------------------------------------------------------------------------
 * The script parser
 * ------------------------------------------------------------------------ */

// parseScript parses the next token of a Tcl script.
func (p *parser) parseScript() {
	for {
		if p.rem() == 0 {
			p.tstart = p.p
			p.tend = p.p - 1
			p.tline = p.linenr
			p.tt = ttEOL
			if p.inquote {
				p.missingCh = '"'
			}
			p.eof = true
			return
		}
		switch p.cur() {
		case '\\':
			if p.at(1) == '\n' && !p.inquote {
				p.parseSep()
				return
			}
			p.comment = false
			p.parseStr()
			return
		case ' ', '\t', '\r', '\f':
			if !p.inquote {
				p.parseSep()
				return
			}
			p.comment = false
			p.parseStr()
			return
		case '\n', ';':
			p.comment = true
			if !p.inquote {
				p.parseEol()
				return
			}
			p.parseStr()
			return
		case '[':
			p.comment = false
			p.parseCmd()
			return
		case '$':
			p.comment = false
			if !p.parseVar() {
				/* An orphan $. Create as a separate token */
				p.tstart = p.p
				p.tend = p.p
				p.p++
				p.tt = ttEsc
			}
			return
		case '#':
			if p.comment {
				p.parseComment()
				continue
			}
			p.parseStr()
			return
		default:
			p.comment = false
			p.parseStr()
			return
		}
	}
}

// parseSep parses a run of word separators, including backslash-newline
// continuations.
func (p *parser) parseSep() {
	p.tstart = p.p
	p.tline = p.linenr
	for u8.IsSpace(p.cur()) || (p.cur() == '\\' && p.at(1) == '\n') {
		if p.cur() == '\n' {
			break
		}
		if p.cur() == '\\' {
			p.p++
			p.linenr++
		}
		p.p++
	}
	p.tend = p.p - 1
	p.tt = ttSep
}

// parseEol parses a run of end-of-command separators.
func (p *parser) parseEol() {
	p.tstart = p.p
	p.tline = p.linenr
	for u8.IsSpace(p.cur()) || p.cur() == ';' {
		if p.cur() == '\n' {
			p.linenr++
		}
		p.p++
	}
	p.tend = p.p - 1
	p.tt = ttEOL
}

/*
 * Here are the rules for parsing:
 * {braced expression}
 * - Count open and closing braces
 * - Backslash escapes meaning of braces but doesn't remove the backslash
 *
 * "quoted expression"
 * - Unescaped double quote terminates the expression
 * - Backslash escapes next char
 * - [commands brackets] are counted/nested
 * - command rules apply within [brackets], not quoting rules
 *
 * [command expression]
 * - Count open and closing brackets
 * - Backslash escapes next char
 * - [commands brackets] are counted/nested
 * - "quoted expressions" are parsed according to quoting rules
 * - {braced expressions} are parsed according to brace rules
 *
 * For everything, backslash escapes the next char, newline increments the
 * current line.
 */

// parseSubBrace positions the parser at the end of a braced expression
// starting at the current position, setting tend and possibly the missing
// character.
func (p *parser) parseSubBrace() {
	level := 1

	p.p++ /* skip the brace */
	for p.rem() != 0 {
		switch p.cur() {
		case '\\':
			if p.rem() > 1 {
				p.p++
				if p.cur() == '\n' {
					p.linenr++
				}
			}
		case '{':
			level++
		case '}':
			level--
			if level == 0 {
				p.tend = p.p - 1
				p.p++
				return
			}
		case '\n':
			p.linenr++
		}
		p.p++
	}
	p.missingCh = '{'
	p.missingLine = p.tline
	p.tend = p.p - 1
}

// parseSubQuote positions the parser at the end of a quoted expression
// starting at the current position and returns the token type: ttEsc when the
// text needs substitution, ttStr otherwise.
func (p *parser) parseSubQuote() int {
	tt := ttStr
	line := p.tline

	p.p++ /* skip the quote */
	for p.rem() != 0 {
		switch p.cur() {
		case '\\':
			if p.rem() > 1 {
				p.p++
				if p.cur() == '\n' {
					p.linenr++
				}
				tt = ttEsc
			}
		case '"':
			p.tend = p.p - 1
			p.p++
			return tt
		case '[':
			p.parseSubCmd()
			tt = ttEsc
			continue
		case '\n':
			p.linenr++
		case '$':
			tt = ttEsc
		}
		p.p++
	}
	p.missingCh = '"'
	p.missingLine = line
	p.tend = p.p - 1
	return tt
}

// parseSubCmd positions the parser at the end of a [command] expression
// starting at the current position.
func (p *parser) parseSubCmd() {
	level := 1
	startofword := true
	line := p.tline

	p.p++ /* skip the bracket */
	for p.rem() != 0 {
		switch p.cur() {
		case '\\':
			if p.rem() > 1 {
				p.p++
				if p.cur() == '\n' {
					p.linenr++
				}
			}
		case '[':
			level++
		case ']':
			level--
			if level == 0 {
				p.tend = p.p - 1
				p.p++
				return
			}
		case '"':
			if startofword {
				p.parseSubQuote()
				if p.missingCh == '"' {
					return
				}
				continue
			}
		case '{':
			p.parseSubBrace()
			startofword = false
			continue
		case '\n':
			p.linenr++
		}
		startofword = u8.IsSpace(p.cur())
		p.p++
	}
	p.missingCh = '['
	p.missingLine = line
	p.tend = p.p - 1
}

// parseBrace parses a braced word.
func (p *parser) parseBrace() {
	p.tstart = p.p + 1
	p.tline = p.linenr
	p.tt = ttStr
	p.parseSubBrace()
}

// parseCmd parses a command substitution.
func (p *parser) parseCmd() {
	p.tstart = p.p + 1
	p.tline = p.linenr
	p.tt = ttCmd
	p.parseSubCmd()
}

// parseQuote parses a quoted word.
func (p *parser) parseQuote() {
	p.tstart = p.p + 1
	p.tline = p.linenr
	p.tt = p.parseSubQuote()
}

// parseVar parses a variable reference, reporting false for an orphan '$'
// which the caller must then treat as an ordinary string.
func (p *parser) parseVar() bool {
	p.p++ /* skip the $ */

	p.tstart = p.p
	p.tt = ttVar
	p.tline = p.linenr

	if p.cur() == '{' {
		p.p++
		p.tstart = p.p
		for p.rem() != 0 && p.cur() != '}' {
			if p.cur() == '\n' {
				p.linenr++
			}
			p.p++
		}
		p.tend = p.p - 1
		if p.rem() != 0 {
			p.p++
		}
	} else {
		for {
			/* Skip double colon, but not single colon! */
			if p.at(0) == ':' && p.at(1) == ':' {
				for p.cur() == ':' {
					p.p++
				}
				continue
			}
			/* Note that any char >= 0x80 must be part of a utf-8 char.
			 * We consider all unicode points outside of ASCII as letters.
			 */
			if u8.IsAlnum(p.cur()) || p.cur() == '_' || p.cur() >= 0x80 {
				p.p++
				continue
			}
			break
		}
		/* Parse [dict get] syntax sugar. */
		if p.cur() == '(' {
			count := 1
			paren := -1

			p.tt = ttDictSugar

			for count != 0 && p.rem() != 0 {
				p.p++
				if p.cur() == '\\' && p.rem() >= 1 {
					p.p++
				} else if p.cur() == '(' {
					count++
				} else if p.cur() == ')' {
					paren = p.p
					count--
				}
			}
			if count == 0 {
				p.p++
			} else if paren >= 0 {
				/* Did not find a matching paren. Back up */
				p.p = paren + 1
			}
			if p.src[p.tstart] == '(' {
				p.tt = ttExprSugar
			}
		}
		p.tend = p.p - 1
	}
	/* Check if we parsed just the '$' character. That's not a variable so
	 * report failure to tell the state machine to consider this '$' just a
	 * string. */
	if p.tstart == p.p {
		p.p--
		return false
	}
	return true
}

// parseStr parses a plain string token, stopping at a substitution or at a
// word boundary.
func (p *parser) parseStr() {
	if p.tt == ttSep || p.tt == ttEOL || p.tt == ttNone || p.tt == ttStr {
		/* Starting a new word */
		if p.cur() == '{' {
			p.parseBrace()
			return
		}
		if p.cur() == '"' {
			p.inquote = true
			p.p++
			/* In case the end quote is missing */
			p.missingLine = p.tline
		}
	}
	p.tstart = p.p
	p.tline = p.linenr
	for {
		if p.rem() == 0 {
			if p.inquote {
				p.missingCh = '"'
			}
			p.tend = p.p - 1
			p.tt = ttEsc
			return
		}
		switch p.cur() {
		case '\\':
			if !p.inquote && p.at(1) == '\n' {
				p.tend = p.p - 1
				p.tt = ttEsc
				return
			}
			if p.rem() >= 2 {
				if p.at(1) == '\n' {
					p.linenr++
				}
				p.p++
			} else if p.rem() == 1 {
				/* End of script with trailing backslash */
				p.missingCh = '\\'
			}
		case '(', ')':
			/* If the following token is not '$' just keep going */
			if p.cur() == '(' && p.rem() > 1 && p.at(1) != '$' {
				break
			}
			/* Only need a separate ')' token if the previous was a var */
			if p.cur() == '(' || p.tt == ttVar {
				if p.p == p.tstart {
					/* At the start of the token, so just return this char */
					p.p++
				}
				p.tend = p.p - 1
				p.tt = ttEsc
				return
			}
		case '$', '[':
			p.tend = p.p - 1
			p.tt = ttEsc
			return
		case ' ', '\t', '\n', '\r', '\f', ';':
			if !p.inquote {
				p.tend = p.p - 1
				p.tt = ttEsc
				return
			} else if p.cur() == '\n' {
				p.linenr++
			}
		case '"':
			if p.inquote {
				p.tend = p.p - 1
				p.tt = ttEsc
				p.p++
				p.inquote = false
				return
			}
		}
		p.p++
	}
}

// parseComment skips a comment up to and including the end of the line.
func (p *parser) parseComment() {
	for p.rem() != 0 {
		if p.cur() == '\\' {
			p.p++
			if p.rem() == 0 {
				p.missingCh = '\\'
				return
			}
			if p.cur() == '\n' {
				p.linenr++
			}
		} else if p.cur() == '\n' {
			p.p++
			p.linenr++
			break
		}
		p.p++
	}
}

/* ---------------------------------------------------------------------------
 * Backslash escapes
 * ------------------------------------------------------------------------ */

// xdigitval returns the value of a hexadecimal digit, or -1.
func xdigitval(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

// odigitval returns the value of an octal digit, or -1.
func odigitval(c byte) int {
	if c >= '0' && c <= '7' {
		return int(c - '0')
	}
	return -1
}

// escape performs Tcl backslash substitution on s. The result is never longer
// than the source (C: JimEscape).
func escape(s []byte) []byte {
	dest := make([]byte, 0, len(s))
	// at returns the byte at i, or 0 past the end: the C implementation reads
	// the NUL terminator of the string representation in the same way.
	at := func(i int) byte {
		if i < len(s) {
			return s[i]
		}
		return 0
	}
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			dest = append(dest, s[i])
			continue
		}
		switch at(i + 1) {
		case 'a':
			dest = append(dest, 0x7)
			i++
		case 'b':
			dest = append(dest, 0x8)
			i++
		case 'f':
			dest = append(dest, 0xc)
			i++
		case 'n':
			dest = append(dest, 0xa)
			i++
		case 'r':
			dest = append(dest, 0xd)
			i++
		case 't':
			dest = append(dest, 0x9)
			i++
		case 'u', 'U', 'x':
			/* A unicode or hex sequence.
			 * \x expects 1-2 hex chars and converts to a single byte.
			 * \u expects 1-4 hex chars and converts to utf-8.
			 * \U expects 1-8 hex chars and converts to utf-8.
			 * \u{NNN} supports 1-6 hex chars and converts to utf-8.
			 * An invalid sequence means simply the escaped char.
			 */
			val := 0
			maxchars := 2
			i++
			if at(i) == 'U' {
				maxchars = 8
			} else if at(i) == 'u' {
				if at(i+1) == '{' {
					maxchars = 6
					i++
				} else {
					maxchars = 4
				}
			}
			k := 0
			for ; k < maxchars; k++ {
				c := xdigitval(at(i + k + 1))
				if c == -1 {
					break
				}
				val = (val << 4) | c
			}
			/* The \u{nnn} syntax supports up to 21 bit codepoints. */
			if at(i) == '{' {
				if k == 0 || val > 0x1fffff || at(i+k+1) != '}' {
					/* Back up */
					i--
					k = 0
				} else {
					/* Skip the closing brace */
					k++
				}
			}
			if k != 0 {
				if at(i) == 'x' {
					dest = append(dest, byte(val))
				} else {
					dest = append(dest, u8.FromUnicode(val)...)
				}
				i += k
				break
			}
			/* Not a valid codepoint, just an escaped char */
			dest = append(dest, at(i))
		case 'v':
			dest = append(dest, 0xb)
			i++
		case 0:
			dest = append(dest, '\\')
			i++
		case '\n':
			/* Replace all spaces and tabs after backslash newline with a
			 * single space */
			dest = append(dest, ' ')
			for {
				i++
				if at(i+1) != ' ' && at(i+1) != '\t' {
					break
				}
			}
		case '0', '1', '2', '3', '4', '5', '6', '7':
			val := odigitval(at(i + 1))
			c := odigitval(at(i + 2))
			if c == -1 {
				dest = append(dest, byte(val))
				i++
				break
			}
			val = val*8 + c
			c = odigitval(at(i + 3))
			if c == -1 {
				dest = append(dest, byte(val))
				i += 2
				break
			}
			val = val*8 + c
			dest = append(dest, byte(val))
			i += 3
		default:
			dest = append(dest, at(i+1))
			i++
		}
	}
	return dest
}

// parserGetTokenObj returns the current token as a new object, converting
// backslash escapes when the token is of type ttEsc.
//
// Note that after the conversion, tokens that are grouped with braces in the
// source code are always recognisable from the identical string obtained in a
// different way, from the type. For example "{*}$a" returns "*" as a token of
// type ttStr, while "*$a" returns "*" as a token of type ttEsc.
func (i *Interp) parserGetTokenObj(p *parser) *Obj {
	tok := p.token()
	if p.tt != ttEsc {
		return i.NewStringObjBytes(tok)
	}
	return i.NewStringObjBytes(escape(tok))
}

/* ---------------------------------------------------------------------------
 * The list parser
 * ------------------------------------------------------------------------ */

// parseList parses the next token of a Tcl list.
func (p *parser) parseList() {
	if u8.IsSpace(p.cur()) {
		p.parseListSep()
		return
	}
	switch p.cur() {
	case '"':
		p.parseListQuote()
		return
	case '{':
		p.parseBrace()
		return
	default:
		if p.rem() != 0 {
			p.parseListStr()
			return
		}
	}

	p.tstart = p.p
	p.tend = p.p
	p.tline = p.linenr
	p.tt = ttEOL
	p.eof = true
}

// parseListSep parses a run of list element separators.
func (p *parser) parseListSep() {
	p.tstart = p.p
	p.tline = p.linenr
	for u8.IsSpace(p.cur()) {
		if p.cur() == '\n' {
			p.linenr++
		}
		p.p++
	}
	p.tend = p.p - 1
	p.tt = ttSep
}

// parseListQuote parses a quoted list element.
func (p *parser) parseListQuote() {
	p.p++

	p.tstart = p.p
	p.tline = p.linenr
	p.tt = ttStr

	for p.rem() != 0 {
		switch p.cur() {
		case '\\':
			p.tt = ttEsc
			if p.rem() == 1 {
				/* Trailing backslash. The C decrements the remaining length
				 * without advancing, which ends the parse on the next call. */
				p.tend = p.p
				p.p++
				return
			}
			p.p++
		case '\n':
			p.linenr++
		case '"':
			p.tend = p.p - 1
			p.p++
			return
		}
		p.p++
	}

	p.tend = p.p - 1
}

// parseListStr parses an unquoted list element.
func (p *parser) parseListStr() {
	p.tstart = p.p
	p.tline = p.linenr
	p.tt = ttStr

	for p.rem() != 0 {
		if u8.IsSpace(p.cur()) {
			p.tend = p.p - 1
			return
		}
		if p.cur() == '\\' {
			if p.rem() == 1 {
				/* Trailing backslash; see parseListQuote */
				p.tend = p.p
				p.p++
				return
			}
			p.tt = ttEsc
			p.p++
		}
		p.p++
	}
	p.tend = p.p - 1
}

// parseListObjs splits a list string into element objects, processing
// backslash escapes, braces and quotes as the list parser does. Like the C's
// JimParseList it reports no error for an unterminated brace or quote: the
// element simply ends at the end of the string. Only the script parser
// (script.go's parseCheckMissing) turns a missing close-brace into an error.
func (i *Interp) parseListObjs(src []byte, startLine int) ([]*Obj, []int) {
	var elems []*Obj
	var lines []int
	p := newParser(src, startLine)
	for {
		p.parseList()
		if p.tt == ttEOL {
			break
		}
		if p.tt == ttSep {
			continue
		}
		elems = append(elems, i.parserGetTokenObj(p))
		lines = append(lines, p.tline)
	}
	return elems, lines
}
