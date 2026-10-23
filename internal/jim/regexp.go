/* Jim Tcl 0.84 ported from C to Go: the regexp and regsub commands.
 *
 * Copyright 2008 Steve Bennett <steveb@workware.net.au>
 *
 * Based on code originally from Tcl 6.7, copyright 1987-1991 Regents of the
 * University of California.
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
	"strings"

	"github.com/msteveb/jimtcl/internal/jimregex"
)

// cmdUsageErr is the C's JIM_USAGE: a command proc returns it to ask the
// interpreter to build the "wrong # args" message from the command's usage
// string.
const cmdUsageErr = -1

// maxSubMatches is the number of submatches regsub makes available to its
// replacement string and to -command (C: MAX_SUB_MATCHES).
const maxSubMatches = 50

// regexpRep is the internal representation of an object holding a compiled
// regular expression (C: regexpObjType), together with the flags it was
// compiled with.
type regexpRep struct {
	flags int
	re    *jimregex.Regexp
}

// setRegexpFromAny returns the compiled form of the pattern in objPtr,
// compiling it and caching it in the object if needed (C: SetRegexpFromAny).
func (i *Interp) setRegexpFromAny(objPtr *Obj, flags int) *jimregex.Regexp {
	// Check if the object is already an uptodate compiled regexp
	if objPtr.kind == kindRegexp {
		if rep, ok := objPtr.aux.(*regexpRep); ok && rep.flags == flags {
			return rep.re
		}
	}

	// Not a regexp or the flags do not match, so compile the string rep
	re, err := jimregex.Compile(objPtr.String(), jimregex.Extended|flags)
	if err != nil {
		i.Errorf("couldn't compile regular expression pattern: %s", err.Error())
		return nil
	}

	objPtr.freeIntRep()
	objPtr.kind = kindRegexp
	objPtr.aux = &regexpRep{flags: flags, re: re}

	return re
}

/* ---------------------------------------------------------------------------
 * UTF-8 helpers with the signatures the C commands use
 * ------------------------------------------------------------------------ */

// regexpUtf8Strlen returns the number of characters in the first bytelen bytes
// of s (C: utf8_strlen). Like the C it decodes whole sequences, so a sequence
// that straddles bytelen counts as one character.
func regexpUtf8Strlen(s string, bytelen int) int {
	charlen := 0
	for n := 0; bytelen > 0; charlen++ {
		_, l := u8.ToUnicode(u8.Slice(s, n))
		n += l
		bytelen -= l
	}
	return charlen
}

// regexpCString returns the part of s before the first NUL byte, which is all
// that the C sees when it appends a string by pointer (Jim_AppendString with
// a length of -1).
func regexpCString(s string) string {
	if n := strings.IndexByte(s, 0); n >= 0 {
		return s[:n]
	}
	return s
}

/* ---------------------------------------------------------------------------
 * The regexp command
 * ------------------------------------------------------------------------ */

// regexpCmd implements [regexp] (C: Jim_RegexpCmd).
func regexpCmd(i *Interp, argv []*Obj) int {
	const (
		optIndices = iota
		optNocase
		optLine
		optLinestop
		optLineanchor
		optAll
		optInline
		optStart
		optExpanded
		optEnd
	)
	options := []string{
		"-indices", "-nocase", "-line", "-linestop", "-lineanchor", "-all", "-inline", "-start", "-expanded", "--",
	}

	var (
		indices    bool
		all        bool
		inline     bool
		lineanchor bool
		notbol     bool
	)
	// offset is the -start option as a character index, and then the character
	// offset of the subject for -indices.
	offset := 0
	regcompFlags := 0

	n := 1
	for ; n < len(argv); n++ {
		if !strings.HasPrefix(argv[n].String(), "-") {
			break
		}
		option, rc := i.GetEnum(argv[n], options, "option", ErrMsg|EnumAbbrev)
		if rc != OK {
			return ERR
		}
		if option == optEnd {
			n++
			break
		}
		switch option {
		case optIndices:
			indices = true
		case optNocase:
			regcompFlags |= jimregex.ICase
		case optLine:
			regcompFlags |= jimregex.Newline
			lineanchor = true
		case optLinestop:
			regcompFlags |= jimregex.NewlineStop
		case optLineanchor:
			regcompFlags |= jimregex.NewlineAnchor
			lineanchor = true
		case optAll:
			all = true
		case optInline:
			inline = true
		case optStart:
			n++
			if n == len(argv) {
				return cmdUsageErr
			}
			idx, rc := i.GetIndex(argv[n])
			if rc != OK {
				return ERR
			}
			offset = idx
		case optExpanded:
			regcompFlags |= jimregex.Expanded
		}
	}
	if len(argv)-n < 2 {
		return cmdUsageErr
	}

	regex := i.setRegexpFromAny(argv[n], regcompFlags)
	if regex == nil {
		return ERR
	}

	pattern := argv[n].String()
	source := argv[n+1].String()

	numVars := len(argv) - n - 2

	if inline {
		if numVars != 0 {
			i.SetResultString("regexp match variables not allowed when using -inline")
			return ERR
		}
		numVars = regex.MaxSub() + 1
	}

	// If an offset has been specified, adjust for that now.
	// If it points past the end of the string, point to the terminating null.
	// Note that the C compares the character offset against the byte length.
	base := 0
	if offset != 0 {
		if offset < 0 {
			offset += len(source) + 1
		}
		if offset > len(source) {
			base = len(source)
		} else if offset > 0 {
			base = u8.ByteIndex(source, offset)
		}
		notbol = true
	}

	var resultListObj *Obj
	if inline {
		resultListObj = i.NewListObj(nil)
	}

	numMatches := 0
	result := OK
	for {
		pmatch, matched := regex.Exec(source, base, notbol)
		if !matched {
			break
		}

		numMatches++

		// We used to not assign vars for -all if not -inline, since we can't
		// really assign capture groups for multiple matches, but Tcl does
		// this, just setting the last value for each capture group, so we will
		// do the same for compatibility.
		//
		// If additional variable names have been specified, return index
		// information in those variables.
		for j := 0; j < numVars; j++ {
			so, eo := -1, -1
			if 2*j+1 < len(pmatch) {
				so, eo = pmatch[2*j], pmatch[2*j+1]
			}

			var resultObj *Obj
			switch {
			case indices:
				resultObj = i.NewListObj(nil)
				if so == -1 {
					i.ListAppendElement(resultObj, i.NewIntObj(-1))
					i.ListAppendElement(resultObj, i.NewIntObj(-1))
				} else {
					// The match offsets are byte offsets. We need char offsets
					soChar := offset + regexpUtf8Strlen(source[base:], so-base)
					eoChar := offset + regexpUtf8Strlen(source[base:], eo-base)
					i.ListAppendElement(resultObj, i.NewIntObj(int64(soChar)))
					i.ListAppendElement(resultObj, i.NewIntObj(int64(eoChar-1)))
				}
			case so == -1:
				resultObj = i.NewStringObj("")
			default:
				resultObj = i.NewStringObj(source[so:eo])
			}

			if inline {
				i.ListAppendElement(resultListObj, resultObj)
			} else {
				// And now set the result variable
				result = i.SetVariable(argv[n+2+j], resultObj)
				if result != OK {
					break
				}
			}
		}

		if !all || (u8.ByteAt(pattern, 0) == '^' && !lineanchor) || u8.ByteAt(source, base) == 0 {
			break
		}
		if pmatch[1] != base {
			offset += regexpUtf8Strlen(source[base:], pmatch[1]-base)
			base = pmatch[1]
		} else {
			// A zero length match at the start of the subject advances by one
			// byte, not by one character
			base++
			offset++
		}
		if u8.ByteAt(source, base) == 0 {
			break
		}
		notbol = true
	}

	if result == OK {
		if inline {
			i.SetResult(resultListObj)
		} else {
			i.SetResultInt(int64(numMatches))
		}
	}
	return result
}

/* ---------------------------------------------------------------------------
 * The regsub command
 * ------------------------------------------------------------------------ */

// regsubReplace appends the replacement string to b, substituting "&" and
// "\0".."\9" with the matched submatches of source. This is the subSpec
// handling of C's Jim_RegsubCmd.
func regsubReplace(b *strings.Builder, replace, source string, pmatch []int) {
	for j := 0; j < len(replace); j++ {
		idx := -1
		c := replace[j]

		switch {
		case c == '&':
			idx = 0
		case c == '\\':
			j++
			c = u8.ByteAt(replace, j)
			switch {
			case c >= '0' && c <= '9':
				idx = int(c - '0')
			case c == '\\' || c == '&':
				b.WriteByte(c)
				continue
			case j == len(replace):
				// A trailing backslash is replaced with a backslash
				b.WriteByte(replace[j-1])
				continue
			default:
				// Otherwise keep the literal backslash and the following char
				b.WriteString(replace[j-1 : j+1])
				continue
			}
		default:
			b.WriteByte(c)
			continue
		}

		if idx < maxSubMatches && 2*idx+1 < len(pmatch) && pmatch[2*idx] != -1 && pmatch[2*idx+1] != -1 {
			b.WriteString(source[pmatch[2*idx]:pmatch[2*idx+1]])
		}
	}
}

// regsubCmd implements [regsub] (C: Jim_RegsubCmd).
func regsubCmd(i *Interp, argv []*Obj) int {
	const (
		optNocase = iota
		optLine
		optLinestop
		optLineanchor
		optAll
		optStart
		optCommand
		optExpanded
		optEnd
	)
	options := []string{
		"-nocase", "-line", "-linestop", "-lineanchor", "-all", "-start", "-command", "-expanded", "--",
	}

	var (
		all     bool
		command bool
		notbol  bool
	)
	offset := 0
	regcompFlags := 0

	n := 1
	for ; n < len(argv); n++ {
		if !strings.HasPrefix(argv[n].String(), "-") {
			break
		}
		option, rc := i.GetEnum(argv[n], options, "option", ErrMsg|EnumAbbrev)
		if rc != OK {
			return ERR
		}
		if option == optEnd {
			n++
			break
		}
		switch option {
		case optNocase:
			regcompFlags |= jimregex.ICase
		case optLine:
			regcompFlags |= jimregex.Newline
		case optLinestop:
			regcompFlags |= jimregex.NewlineStop
		case optLineanchor:
			regcompFlags |= jimregex.NewlineAnchor
		case optAll:
			all = true
		case optStart:
			n++
			if n == len(argv) {
				return cmdUsageErr
			}
			idx, rc := i.GetIndex(argv[n])
			if rc != OK {
				return ERR
			}
			offset = idx
		case optCommand:
			command = true
		case optExpanded:
			regcompFlags |= jimregex.Expanded
		}
	}
	if len(argv)-n != 3 && len(argv)-n != 4 {
		return cmdUsageErr
	}

	// Need to ensure that this is unshared, so just duplicate it always
	regex := i.setRegexpFromAny(argv[n].dup(), regcompFlags)
	if regex == nil {
		return ERR
	}
	pattern := argv[n].String()

	source := argv[n+1].String()
	var cmdPrefix *Obj
	replace := ""
	if command {
		cmdPrefix = argv[n+2]
		if i.ListLength(cmdPrefix) == 0 {
			i.SetResultString("command prefix must be a list of at least one element")
			return ERR
		}
	} else {
		replace = argv[n+2].String()
	}

	// Create the result string
	var b strings.Builder
	result := OK
	numMatches := 0

	// If an offset has been specified, adjust for that now.
	// If it points past the end of the string, point to the terminating null.
	if offset != 0 {
		if offset < 0 {
			offset += len(source) + 1
		}
		if offset > len(source) {
			offset = len(source)
		} else if offset < 0 {
			offset = 0
		}
	}
	// Convert from character offset to byte offset
	offset = u8.ByteIndex(source, offset)

	// Copy the part before -start
	b.WriteString(source[:offset])

	// The following loop is to handle multiple matches within the same source
	// string; each iteration handles one match and its corresponding
	// substitution. If "-all" hasn't been specified then the loop body only
	// gets executed once.
	//
	// To match Tcl, an empty pattern does not match at the end of the string.
	remain := len(source) - offset
	p := offset
	for remain != 0 || u8.ByteAt(pattern, 0) != 0 {
		pmatch, matched := regex.Exec(source, p, notbol)
		if !matched {
			break
		}

		numMatches++

		// Copy the portion of the source string before the match to the result
		b.WriteString(source[p:pmatch[0]])

		if command {
			// construct the command as a list
			cmdListObj := cmdPrefix.dup()
			for j := 0; j < maxSubMatches; j++ {
				if 2*j+1 >= len(pmatch) || pmatch[2*j] == -1 {
					break
				}
				i.ListAppendElement(cmdListObj, i.NewStringObj(source[pmatch[2*j]:pmatch[2*j+1]]))
			}

			result = i.EvalObj(cmdListObj)
			if result != OK {
				return result
			}
			b.WriteString(regexpCString(i.Result().String()))
		} else {
			// Append the subSpec argument, making appropriate substitutions
			regsubReplace(&b, replace, source, pmatch)
		}

		remain -= pmatch[1] - p
		p = pmatch[1]

		// If -all is not specified, or there is no source left, we are done
		if !all || remain == 0 {
			break
		}

		notbol = false
		if pmatch[1] == pmatch[0] {
			// Matched a zero length string. Need to avoid matching the same
			// position again
			if u8.ByteAt(pattern, 0) == '^' {
				// An anchored search sets REG_NOTBOL
				notbol = true
			} else {
				// A non-anchored search advances by one char
				charlen := u8.CharLen(u8.ByteAt(source, p))
				if p+charlen > len(source) {
					charlen = len(source) - p
				}
				b.WriteString(source[p : p+charlen])
				p += charlen
				remain -= charlen
			}
		}
	}

	// Copy the portion of the string after the last match to the result
	b.WriteString(regexpCString(source[p:]))

	// And now set or return the result variable
	if len(argv)-n == 4 {
		result = i.SetVariable(argv[n+3], i.NewStringObj(b.String()))
		if result == OK {
			i.SetResultInt(int64(numMatches))
		}
	} else {
		i.SetResultString(b.String())
	}

	return result
}

// RegexpMatchObj reports whether pattern matches the string, as the
// -regexp options of [switch] and [lsearch] do (C: JimRegexpMatch).
func (i *Interp) RegexpMatchObj(pattern, s *Obj) (bool, int) {
	argv := []*Obj{i.NewStringObj("regexp"), pattern, s}

	rc := i.EvalObjVector(argv)
	if rc != OK {
		return false, rc
	}
	// [regexp] without -inline always returns the number of matches, so the
	// conversion cannot fail; the C treats a failure as "no match" too.
	eq, grc := i.GetWide(i.Result())
	return grc == OK && eq != 0, OK
}

// registerRegexpCommands registers the commands of the regexp extension
// (C: Jim_regexpInit).
func registerRegexpCommands(i *Interp) {
	i.PackageProvide("regexp", "1.0", ErrMsg)

	i.RegisterCmd("regexp", "?-option ...? exp string ?matchVar? ?subMatchVar ...?", 2, -1, regexpCmd, 0)
	i.RegisterCmd("regsub", "?-option ...? exp string subSpec ?varName?", 3, -1, regsubCmd, 0)
}
