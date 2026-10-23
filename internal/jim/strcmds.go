/* Jim Tcl 0.84 ported from C to Go: the [string] command.
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

// The option tables of the [string] subcommands that take options.
var (
	strNocaseOptions       = []string{"-nocase"}
	strNocaseLengthOptions = []string{"-nocase", "-length"}
)

// registerStringCommands registers [string] (C: Jim_StringCoreCommand and its
// subcommand table).
//
// Every subcommand is declared with ModFlagFullArgv so that it sees the same
// argv as the single C function does, which keeps the argument indices and the
// error messages identical.
func registerStringCommands(i *Interp) {
	full := ModFlagFullArgv
	i.RegisterSubCmd("string", []SubCmd{
		{Cmd: "bytelength", Args: "string", Fn: strCmdByteLength, MinArgs: 1, MaxArgs: 1, Flags: full},
		{Cmd: "byterange", Args: "string first last", Fn: strCmdByteRange, MinArgs: 3, MaxArgs: 3, Flags: full},
		{Cmd: "cat", Args: "?string ...?", Fn: strCmdCat, MinArgs: 0, MaxArgs: -1, Flags: full},
		{Cmd: "compare", Args: "?-nocase? ?-length int? string1 string2", Fn: strCmdCompare, MinArgs: 2, MaxArgs: 5, Flags: full},
		{Cmd: "equal", Args: "?-nocase? ?-length int? string1 string2", Fn: strCmdEqual, MinArgs: 2, MaxArgs: 5, Flags: full},
		{Cmd: "first", Args: "subString string ?index?", Fn: strCmdFirst, MinArgs: 2, MaxArgs: 3, Flags: full},
		{Cmd: "index", Args: "string index", Fn: strCmdIndex, MinArgs: 2, MaxArgs: 2, Flags: full},
		{Cmd: "is", Args: "class ?-strict? str", Fn: strCmdIs, MinArgs: 2, MaxArgs: 3, Flags: full},
		{Cmd: "last", Args: "subString string ?index?", Fn: strCmdLast, MinArgs: 2, MaxArgs: 3, Flags: full},
		{Cmd: "length", Args: "string", Fn: strCmdLength, MinArgs: 1, MaxArgs: 1, Flags: full},
		{Cmd: "map", Args: "?-nocase? mapList string", Fn: strCmdMap, MinArgs: 2, MaxArgs: 3, Flags: full},
		{Cmd: "match", Args: "?-nocase? pattern string", Fn: strCmdMatch, MinArgs: 2, MaxArgs: 3, Flags: full},
		{Cmd: "range", Args: "string first last", Fn: strCmdRange, MinArgs: 3, MaxArgs: 3, Flags: full},
		{Cmd: "repeat", Args: "string count", Fn: strCmdRepeat, MinArgs: 2, MaxArgs: 2, Flags: full},
		{Cmd: "replace", Args: "string first last ?string?", Fn: strCmdReplace, MinArgs: 3, MaxArgs: 4, Flags: full},
		{Cmd: "reverse", Args: "string", Fn: strCmdReverse, MinArgs: 1, MaxArgs: 1, Flags: full},
		{Cmd: "tolower", Args: "string", Fn: strCmdToLower, MinArgs: 1, MaxArgs: 1, Flags: full},
		{Cmd: "totitle", Args: "string", Fn: strCmdToTitle, MinArgs: 1, MaxArgs: 1, Flags: full},
		{Cmd: "toupper", Args: "string", Fn: strCmdToUpper, MinArgs: 1, MaxArgs: 1, Flags: full},
		{Cmd: "trim", Args: "string ?trimchars?", Fn: strCmdTrim, MinArgs: 1, MaxArgs: 2, Flags: full},
		{Cmd: "trimleft", Args: "string ?trimchars?", Fn: strCmdTrimLeft, MinArgs: 1, MaxArgs: 2, Flags: full},
		{Cmd: "trimright", Args: "string ?trimchars?", Fn: strCmdTrimRight, MinArgs: 1, MaxArgs: 2, Flags: full},
	})
}

// [string length]
func strCmdLength(i *Interp, argv []*Obj) int {
	i.SetResultInt(int64(i.Utf8Length(argv[2])))
	return OK
}

// [string bytelength]
func strCmdByteLength(i *Interp, argv []*Obj) int {
	i.SetResultInt(int64(argv[2].Len()))
	return OK
}

// [string cat]
func strCmdCat(i *Interp, argv []*Obj) int {
	if len(argv) == 3 {
		// optimise the one-arg case
		i.SetResult(argv[2])
		return OK
	}
	o := i.NewStringObj("")
	for _, a := range argv[2:] {
		i.AppendObj(o, a)
	}
	i.SetResult(o)
	return OK
}

// strCmdCompare implements both [string compare] and [string equal], which the
// C handles in one case with the OPT_COMPARE/OPT_EQUAL distinction.
func strCmdCompare(i *Interp, argv []*Obj) int { return i.strCompareOrEqual(argv, true) }
func strCmdEqual(i *Interp, argv []*Obj) int   { return i.strCompareOrEqual(argv, false) }

func (i *Interp) strCompareOrEqual(argv []*Obj, compare bool) int {
	optCase := true
	optLength := int64(-1)

	// n is the number of remaining option args
	n := len(argv) - 4
	k := 2
	for n > 0 {
		subopt, rc := i.GetEnum(argv[k], strNocaseLengthOptions, "", EnumAbbrev)
		k++
		if rc != OK {
			return subCmdBadArgs
		}
		if subopt == 0 {
			// -nocase
			optCase = false
			n--
		} else {
			// -length
			if n < 2 {
				return subCmdBadArgs
			}
			w, rc := i.GetWide(argv[k])
			k++
			if rc != OK {
				return ERR
			}
			optLength = w
			n -= 2
		}
	}
	if n != 0 {
		return subCmdBadArgs
	}

	a, b := argv[len(argv)-2], argv[len(argv)-1]
	if optLength < 0 && !compare && optCase {
		// Fast version - [string equal], case sensitive, no length
		i.SetResultBool(i.StringEqObj(a, b))
		return OK
	}

	l1, l2 := i.Utf8Length(a), i.Utf8Length(b)
	if optLength >= 0 {
		if int64(l1) > optLength {
			l1 = int(optLength)
		}
		if int64(l2) > optLength {
			l2 = int(optLength)
		}
	}
	res := strCompareUtf8(a.Bytes(), l1, b.Bytes(), l2, !optCase)
	if compare {
		i.SetResultInt(int64(res))
	} else {
		i.SetResultBool(res == 0)
	}
	return OK
}

// strNocaseArg handles the optional leading -nocase of [string match] and
// [string map], returning the argv to use and whether case is significant.
// Like the C it reports a wrong # args error rather than a bad option error.
func (i *Interp) strNocaseArg(argv []*Obj, usage string) ([]*Obj, bool, int) {
	if len(argv) == 4 {
		return argv, true, OK
	}
	if len(argv) == 5 {
		if _, rc := i.GetEnum(argv[2], strNocaseOptions, "", EnumAbbrev); rc == OK {
			return argv[1:], false, OK
		}
	}
	i.WrongNumArgs(2, argv, usage)
	return nil, false, ERR
}

// [string match]
func strCmdMatch(i *Interp, argv []*Obj) int {
	argv, optCase, rc := i.strNocaseArg(argv, "?-nocase? pattern string")
	if rc != OK {
		return ERR
	}
	i.SetResultBool(i.StringMatchObj(argv[2], argv[3], !optCase))
	return OK
}

// [string map]
func strCmdMap(i *Interp, argv []*Obj) int {
	argv, optCase, rc := i.strNocaseArg(argv, "?-nocase? mapList string")
	if rc != OK {
		return ERR
	}
	o, rc := i.stringMap(argv[2], argv[3], !optCase)
	if rc != OK {
		return ERR
	}
	i.SetResult(o)
	return OK
}

// [string range]
func strCmdRange(i *Interp, argv []*Obj) int {
	o, rc := i.StringRangeObj(argv[2], argv[3], argv[4])
	if rc != OK {
		return ERR
	}
	i.SetResult(o)
	return OK
}

// [string byterange]
func strCmdByteRange(i *Interp, argv []*Obj) int {
	o, rc := i.StringByteRangeObj(argv[2], argv[3], argv[4])
	if rc != OK {
		return ERR
	}
	i.SetResult(o)
	return OK
}

// [string replace]
func strCmdReplace(i *Interp, argv []*Obj) int {
	var newStr *Obj
	if len(argv) == 6 {
		newStr = argv[5]
	}
	o, rc := i.stringReplaceObj(argv[2], argv[3], argv[4], newStr)
	if rc != OK {
		return ERR
	}
	i.SetResult(o)
	return OK
}

// [string repeat]
func strCmdRepeat(i *Interp, argv []*Obj) int {
	count, rc := i.GetWideExpr(argv[3])
	if rc != OK {
		return ERR
	}
	o := i.NewStringObj("")
	for ; count > 0; count-- {
		i.AppendObj(o, argv[2])
	}
	i.SetResult(o)
	return OK
}

// [string reverse]
func strCmdReverse(i *Interp, argv []*Obj) int {
	str := argv[2].Bytes()
	// Reverses whole characters, so each UTF-8 sequence is copied as a unit.
	buf := make([]byte, len(str))
	p := len(str)
	for n := 0; n < len(str); {
		_, l := u8.ToUnicode(str[n:])
		p -= l
		copy(buf[p:], str[n:n+l])
		n += l
	}
	i.SetResult(i.NewStringObjBytes(buf))
	return OK
}

// [string index]
func strCmdIndex(i *Interp, argv []*Obj) int {
	idx, rc := i.GetIndex(argv[3])
	if rc != OK {
		return ERR
	}
	str := argv[2].Bytes()
	length := i.Utf8Length(argv[2])
	idx = strRelToAbsIndex(length, idx)
	switch {
	case idx < 0 || idx >= length:
		i.SetResultString("")
	case length == len(str):
		// ASCII optimisation
		i.SetResult(i.NewStringObjBytes(str[idx : idx+1]))
	default:
		n := u8.ByteIndex(str, idx)
		_, w := u8.ToUnicode(u8.Slice(str, n))
		i.SetResult(i.NewStringObjBytes(strRangeBytes(str, n, w)))
	}
	return OK
}

// strCmdFirst and strCmdLast implement [string first] and [string last], which
// the C handles in one case.
func strCmdFirst(i *Interp, argv []*Obj) int { return i.strFirstOrLast(argv, true) }
func strCmdLast(i *Interp, argv []*Obj) int  { return i.strFirstOrLast(argv, false) }

func (i *Interp) strFirstOrLast(argv []*Obj, first bool) int {
	s1, s2 := argv[2].Bytes(), argv[3].Bytes()
	l1, l2 := i.Utf8Length(argv[2]), i.Utf8Length(argv[3])

	idx := 0
	switch {
	case len(argv) == 5:
		n, rc := i.GetIndex(argv[4])
		if rc != OK {
			return ERR
		}
		idx = strRelToAbsIndex(l2, n)
		if idx < 0 {
			idx = 0
		}
	case !first:
		idx = l2
	}

	if first {
		i.SetResultInt(int64(stringFirst(s1, l1, s2, l2, idx)))
	} else {
		// Note that [string last] searches the first idx characters only, so
		// the index is passed where the haystack length would be.
		i.SetResultInt(int64(stringLastUtf8(s1, l1, s2, idx)))
	}
	return OK
}

// strTrimChars returns the optional trimchars argument, or nil for the default.
func strTrimChars(argv []*Obj) *Obj {
	if len(argv) == 4 {
		return argv[3]
	}
	return nil
}

// [string trim]
func strCmdTrim(i *Interp, argv []*Obj) int {
	i.SetResult(i.stringTrim(argv[2], strTrimChars(argv)))
	return OK
}

// [string trimleft]
func strCmdTrimLeft(i *Interp, argv []*Obj) int {
	i.SetResult(i.stringTrimLeft(argv[2], strTrimChars(argv)))
	return OK
}

// [string trimright]
func strCmdTrimRight(i *Interp, argv []*Obj) int {
	i.SetResult(i.stringTrimRight(argv[2], strTrimChars(argv)))
	return OK
}

// [string tolower]
func strCmdToLower(i *Interp, argv []*Obj) int {
	i.SetResult(i.stringToLower(argv[2]))
	return OK
}

// [string toupper]
func strCmdToUpper(i *Interp, argv []*Obj) int {
	i.SetResult(i.stringToUpper(argv[2]))
	return OK
}

// [string totitle]
func strCmdToTitle(i *Interp, argv []*Obj) int {
	i.SetResult(i.stringToTitle(argv[2]))
	return OK
}

// [string is]
func strCmdIs(i *Interp, argv []*Obj) int {
	if len(argv) == 5 && !i.CompareStringImmediate(argv[3], "-strict") {
		return subCmdBadArgs
	}
	return i.stringIs(argv[len(argv)-1], argv[2], len(argv) == 5)
}

// stringMap performs the [string map] substitution, scanning the string once
// and taking the first entry of the map list that matches at each position
// (C: JimStringMap).
func (i *Interp) stringMap(mapList, s *Obj, nocase bool) (*Obj, int) {
	numMaps := i.ListLength(mapList)
	if numMaps%2 != 0 {
		i.SetResultString("list must contain an even number of elements")
		return nil, ERR
	}

	str := s.Bytes()
	strLen := i.Utf8Length(s)

	// Map it
	result := i.NewStringObj("")
	pos := 0
	noMatchStart := -1
	for strLen > 0 {
		n := 0
		for ; n < numMaps; n += 2 {
			key := i.ListGetIndex(mapList, n)
			k := key.Bytes()
			kl := i.Utf8Length(key)

			if strLen >= kl && kl != 0 && strCompareUtf8(u8.Slice(str, pos), kl, k, kl, nocase) == 0 {
				if noMatchStart >= 0 {
					result.appendBytes(strRangeBytes(str, noMatchStart, pos-noMatchStart))
					noMatchStart = -1
				}
				i.AppendObj(result, i.ListGetIndex(mapList, n+1))
				pos += u8.ByteIndex(u8.Slice(str, pos), kl)
				strLen -= kl
				break
			}
		}
		if n == numMaps { // no match
			if noMatchStart < 0 {
				noMatchStart = pos
			}
			_, w := u8.ToUnicode(u8.Slice(str, pos))
			pos += w
			strLen--
		}
	}
	if noMatchStart >= 0 {
		result.appendBytes(strRangeBytes(str, noMatchStart, pos-noMatchStart))
	}
	return result, OK
}
