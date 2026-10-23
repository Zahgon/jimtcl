/* Jim - A small embeddable Tcl interpreter: compiled script and subst objects.
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

import "bytes"

// scriptToken is one token of a compiled script: a type and an object which
// may be specialised by the commands operating on it.
type scriptToken struct {
	objPtr *Obj
	typ    int
}

// script is the compiled form of a script object (C: struct ScriptObj).
//
// It is an array of tokens including a pre-computed representation of the
// command length and arguments. For example the script
//
//	puts hello
//	set $i $x$y [foo]BAR
//
// compiles to
//
//	LIN 2 / ESC puts / ESC hello
//	LIN 4 / ESC set / VAR i / WRD 2 / VAR x / VAR y / WRD 2 / CMD foo / ESC BAR
//
// The WRD token is omitted for the common case of a single token word. If a
// word begins with {*} the word token count is negative instead.
//
// The same structure represents "subst" objects, in which case there are no
// LIN and WRD tokens, SEP and EOL tokens are kept as-is, and substFlags
// records the flags the string was compiled with so that the object can be
// recompiled when [subst] is called with different options.
type script struct {
	token      []scriptToken
	fileName   *Obj
	substFlags int
	firstLine  int  // line number of the first line
	lineNr     int  // error line number, if any
	missing    byte // missing char if the script failed to parse, else ' ' or '\\'
}

// scriptLine is the internal representation of the compact token that records
// the number of words and the source line of one line of a script.
type scriptLine struct {
	argc int
	line int
}

// newScriptLineObj creates the token that records one parsed script line.
func (i *Interp) newScriptLineObj(argc, line int) *Obj {
	objPtr := i.NewStringObj("")
	objPtr.kind = kindScriptLine
	objPtr.aux = scriptLine{argc: argc, line: line}
	return objPtr
}

// parseToken is a token produced by the parser, pointing into the string
// representation of the script being compiled.
type parseToken struct {
	text []byte
	typ  int
	line int
}

// countWordTokens counts the adjoining non-separator tokens starting at
// tokenlist[n]. The result is negative if the first token is the expansion
// operator, in which case the count does not include that token.
func countWordTokens(s *script, tokenlist []parseToken, n int) int {
	expand := 1
	count := 0

	/* Is the first word {*} or {expand}? */
	if tokenlist[n].typ == ttStr && !tokenIsSep(tokenlist[n+1].typ) {
		tok := tokenlist[n].text
		if (len(tok) == 1 && tok[0] == '*') || string(tok) == "expand" {
			/* Create an expand token */
			expand = -1
			n++
		} else if s.missing == ' ' {
			/* This is an "extra characters after close-brace" error.
			 * Report the first error */
			s.missing = '}'
			s.lineNr = tokenlist[n+1].line
		}
	}

	/* Now count non-separator words */
	for !tokenIsSep(tokenlist[n].typ) {
		n++
		count++
	}

	return count * expand
}

// makeScriptObj creates the object for one token of a script or subst object.
func (i *Interp) makeScriptObj(t parseToken) *Obj {
	if t.typ == ttEsc && bytes.IndexByte(t.text, '\\') >= 0 {
		/* Convert backslash escapes; the result is never longer than the
		 * original. */
		return i.NewStringObjBytes(escape(t.text))
	}
	/* Note that for strict Tcl compatibility a ttStr token should replace
	 * <backslash><newline><whitespace> with a single space. */
	return i.NewStringObjBytes(t.text)
}

// scriptObjAddTokens turns a token list into the token array of a script,
// discarding unnecessary tokens and inserting LINE and WORD tokens as
// required. It also records the line number of the first token.
func (i *Interp) scriptObjAddTokens(s *script, tokenlist []parseToken) {
	/* May need up to one extra script token for each EOL in the worst case */
	count := len(tokenlist)
	for n := range tokenlist {
		if tokenlist[n].typ == ttEOL {
			count++
		}
	}
	linenr := tokenlist[0].line
	s.firstLine = linenr

	token := make([]scriptToken, count)
	/* The first token for the current command */
	linefirst := 0
	next := 1
	/* Number of words so far for the current command */
	lineargs := 0

	for n := 0; n < len(tokenlist); {
		/* Skip any leading separators */
		for tokenlist[n].typ == ttSep {
			n++
		}

		wordtokens := countWordTokens(s, tokenlist, n)

		if wordtokens == 0 {
			/* None, so at end of line */
			if lineargs != 0 {
				token[linefirst].typ = ttLine
				token[linefirst].objPtr = i.newScriptLineObj(lineargs, linenr)

				/* Reset for new line */
				lineargs = 0
				linefirst = next
				next++
			}
			n++
			continue
		} else if wordtokens != 1 {
			/* More than 1, or {*}, so insert a WORD token */
			token[next] = scriptToken{typ: ttWord, objPtr: i.NewIntObj(int64(wordtokens))}
			next++
			if wordtokens < 0 {
				/* Skip the expand token */
				n++
				wordtokens = -wordtokens - 1
				lineargs--
			}
		}

		if lineargs == 0 {
			/* First real token on the line, so record the line number */
			linenr = tokenlist[n].line
		}
		lineargs++

		/* Add each non-separator word token to the line */
		for ; wordtokens > 0; wordtokens-- {
			t := tokenlist[n]
			n++
			objPtr := i.makeScriptObj(t)
			/* Every object is initially a string of type 'source', but the
			 * internal type may be specialised while the script runs. */
			objPtr.SetSourceInfo(s.fileName, t.line)
			token[next] = scriptToken{typ: t.typ, objPtr: objPtr}
			next++
		}
	}

	if lineargs == 0 {
		next--
	}
	s.token = token[:next]
}

// substObjAddTokens is the equivalent of scriptObjAddTokens for subst objects:
// every token is kept as-is.
func (i *Interp) substObjAddTokens(s *script, tokenlist []parseToken) {
	s.token = make([]scriptToken, len(tokenlist))
	for n := range tokenlist {
		s.token[n] = scriptToken{typ: tokenlist[n].typ, objPtr: i.makeScriptObj(tokenlist[n])}
	}
}

// ScriptIsComplete parses the given object to determine whether it represents
// a complete script, and returns the state character: ' ' for a complete
// script, '{', '[' or '"' when a terminator is missing and '\\' for a trailing
// backslash.
//
// A script with extra characters after a close brace is reported as complete
// with a state character of '}'; evaluating it gives the error "extra
// characters after close-brace".
func (i *Interp) ScriptIsComplete(o *Obj) (bool, byte) {
	s := i.getScript(o)
	return s.missing == ' ' || s.missing == '}', s.missing
}

// parseCheckMissing sets an appropriate error message for a missing script or
// expression terminator, and returns ERR if ch represents an unmatched
// character. A trailing backslash is not considered an error.
func (i *Interp) parseCheckMissing(ch byte) int {
	var msg string

	switch ch {
	case '\\', ' ':
		return OK
	case '[':
		msg = "unmatched \"[\""
	case '{':
		msg = "missing close-brace"
	case '}':
		msg = "extra characters after close-brace"
	default:
		msg = "missing quote"
	}

	i.SetResultString(msg)
	return ERR
}

// getSourceInfo returns the file name and line number recorded for an object,
// or the empty file name and line 1 when it has none (C: Jim_GetSourceInfo).
func (i *Interp) getSourceInfo(o *Obj) (*Obj, int) {
	if o.fileName != nil {
		return o.fileName, o.lineNr
	}
	if o.kind == kindScript {
		s := i.getScript(o)
		return s.fileName, s.firstLine
	}
	return i.emptyObj, 1
}

// setScriptFromAny takes the string representation of an object as a Tcl
// script and generates the pre-parsed internal representation. Note that the
// object is converted even if the script does not parse; the caller is
// expected to check script.missing with parseCheckMissing.
func (i *Interp) setScriptFromAny(o *Obj) {
	scriptText := o.Bytes()

	/* Try to get information about filename / line number */
	fileNameObj, line := i.getSourceInfo(o)

	/* Initially parse the script into tokens */
	var tokenlist []parseToken
	p := newParser(scriptText, line)
	for !p.eof {
		p.parseScript()
		tokenlist = append(tokenlist, parseToken{text: p.token(), typ: p.tt, line: p.tline})
	}
	/* Add a final EOF token */
	tokenlist = append(tokenlist, parseToken{typ: ttEOF})

	s := &script{fileName: fileNameObj, missing: p.missingCh, lineNr: p.missingLine}
	i.scriptObjAddTokens(s, tokenlist)

	o.freeIntRep()
	o.kind = kindScript
	o.aux = s
}

// getScript returns the compiled script for an object. If there is any
// possibility that the script is not valid, call parseCheckMissing() on
// script.missing to check.
func (i *Interp) getScript(o *Obj) *script {
	if o == i.emptyObj {
		/* Avoid converting emptyObj to a script; use nullScriptObj instead */
		o = i.nullScriptObj
	}

	if o.kind != kindScript || o.aux.(*script).substFlags != 0 {
		i.setScriptFromAny(o)
	}

	return o.aux.(*script)
}

/* ---------------------------------------------------------------------------
 * Subst objects
 * ------------------------------------------------------------------------ */

// parseSubst parses the next token of a [subst] argument.
func (p *parser) parseSubst(flags int) {
	p.tstart = p.p
	p.tline = p.linenr

	if p.rem() == 0 {
		p.tend = p.p
		p.tt = ttEOL
		p.eof = true
		return
	}
	if p.cur() == '[' && flags&SubstNoCmd == 0 {
		p.parseCmd()
		return
	}
	if p.cur() == '$' && flags&SubstNoVar == 0 {
		if p.parseVar() {
			return
		}
		/* Not a var, so treat as a string */
		p.tstart = p.p
		/* Skip this $ */
		p.p++
	}
	for p.rem() != 0 {
		if p.cur() == '$' && flags&SubstNoVar == 0 {
			break
		}
		if p.cur() == '[' && flags&SubstNoCmd == 0 {
			break
		}
		if p.cur() == '\\' && p.rem() > 1 {
			p.p++
		}
		p.p++
	}
	p.tend = p.p - 1
	if flags&SubstNoEsc != 0 {
		p.tt = ttStr
	} else {
		p.tt = ttEsc
	}
}

// setSubstFromAny takes the string representation of an object as a Tcl
// string in which to perform substitution, and generates the pre-parsed
// internal representation.
func (i *Interp) setSubstFromAny(o *Obj, flags int) {
	scriptText := o.Bytes()

	var tokenlist []parseToken
	p := newParser(scriptText, 1)
	for {
		p.parseSubst(flags)
		if p.eof {
			/* Note that subst doesn't need the EOL token */
			break
		}
		tokenlist = append(tokenlist, parseToken{text: p.token(), typ: p.tt, line: p.tline})
	}

	s := &script{substFlags: flags, fileName: i.emptyObj}
	i.substObjAddTokens(s, tokenlist)

	o.freeIntRep()
	o.kind = kindScript
	o.aux = s
}

// getSubst returns the compiled subst object for the given flags, recompiling
// it if it was compiled with different flags.
func (i *Interp) getSubst(o *Obj, flags int) *script {
	if o.kind != kindScript || o.aux.(*script).substFlags != flags {
		i.setSubstFromAny(o, flags)
	}
	return o.aux.(*script)
}

// SubstObj performs command, variable and backslash substitution on the given
// object and returns the result.
func (i *Interp) SubstObj(o *Obj, flags int) (*Obj, int) {
	s := i.getSubst(o, flags)

	res := i.interpolateTokens(s.token, flags)
	if res == nil {
		return nil, ERR
	}
	return res, OK
}
