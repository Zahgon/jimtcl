/* Jim - A small embeddable Tcl interpreter: the list commands.
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

// Usage is returned by a command whose arguments are wrong (C:
// JIM_USAGE); the interpreter then reports the command's usage message.
// listCmdIter walks the elements of a list object (C: Jim_ListIter).
type listCmdIter struct {
	obj *Obj
	idx int
}

// next returns the next element, or nil at the end of the list.
func (it *listCmdIter) next(i *Interp) *Obj {
	if it.idx >= i.ListLength(it.obj) {
		return nil
	}
	e := it.obj.list[it.idx]
	it.idx++
	return e
}

// done reports whether the end of the list has been reached.
func (it *listCmdIter) done(i *Interp) bool {
	return it.idx >= i.ListLength(it.obj)
}

/* [list] */
func listCoreCommand(i *Interp, argv []*Obj) int {
	i.SetResult(i.NewListObj(argv[1:]))
	return OK
}

/* [lindex] */
func lindexCoreCommand(i *Interp, argv []*Obj) int {
	o, ret := i.listIndices(argv[1], argv[2:], None)
	if ret < 0 {
		// Returns an empty object if the index is out of range
		i.SetResult(i.EmptyObj())
		return OK
	}
	if ret == OK {
		i.SetResult(o)
	}
	return ret
}

/* [llength] */
func llengthCoreCommand(i *Interp, argv []*Obj) int {
	i.SetResultInt(int64(i.ListLength(argv[1])))
	return OK
}

/* [lsearch] */
func lsearchCoreCommand(i *Interp, argv []*Obj) int {
	options := []string{
		"-bool", "-not", "-nocase", "-exact", "-glob", "-regexp", "-all", "-inline", "-command",
		"-stride", "-index",
	}
	const (
		optBool = iota
		optNot
		optNoCase
		optExact
		optGlob
		optRegexp
		optAll
		optInline
		optCommand
		optStride
		optIndex
	)
	optBoolean := false
	optNegate := false
	optAllMatches := false
	optInlineResult := false
	optMatch := optExact
	var listObj, commandObj, indexObj *Obj
	matchFlags := 0
	stride := int64(1)

	n := 1
	for ; n < len(argv)-2; n++ {
		option, ret := i.GetEnum(argv[n], options, "option", ErrMsg)
		if ret != OK {
			return ERR
		}
		switch option {
		case optBool:
			optBoolean = true
			optInlineResult = false
		case optNot:
			optNegate = true
		case optNoCase:
			matchFlags |= NoCase
		case optInline:
			optInlineResult = true
			optBoolean = false
		case optAll:
			optAllMatches = true
		case optRegexp:
			optMatch = option
			matchFlags |= OptEnd
		case optCommand:
			if n >= len(argv)-2 {
				return Usage
			}
			n++
			commandObj = argv[n]
			optMatch = optCommand
		case optExact, optGlob:
			optMatch = option
		case optIndex:
			if n >= len(argv)-2 {
				return Usage
			}
			n++
			indexObj = argv[n]
		case optStride:
			if n >= len(argv)-2 {
				return Usage
			}
			n++
			s, ret := i.GetWide(argv[n])
			if ret != OK {
				return ERR
			}
			stride = s
			if stride < 1 {
				i.SetResultString("stride length must be at least 1")
				return ERR
			}
		}
	}

	rest := argv[n:]
	if len(rest) < 2 {
		return Usage
	}

	listlen := i.ListLength(rest[0])
	if int64(listlen)%stride != 0 {
		i.SetResultString("list size must be a multiple of the stride length")
		return ERR
	}

	if optAllMatches {
		listObj = i.NewListObj(nil)
	}
	if optMatch == optRegexp {
		commandObj = i.NewStringObj("regexp")
	}

	for n := 0; n < listlen; n += int(stride) {
		eq := 0
		var searchListObj, o *Obj
		offset := 0

		if indexObj != nil {
			indexes := i.ListElements(indexObj)
			if stride == 1 {
				searchListObj = i.ListGetIndex(rest[0], n)
			} else {
				searchListObj = i.NewListObj(i.ListElements(rest[0])[n : n+int(stride)])
			}
			var rc int
			o, rc = i.listIndices(searchListObj, indexes, ErrMsg)
			if rc != OK {
				return ERR
			}
		} else {
			// No -index, so we have an implicit {0} as indexObj
			searchListObj = rest[0]
			offset = n
			o = i.ListGetIndex(searchListObj, n)
		}

		switch optMatch {
		case optExact:
			eq = boolToInt(i.StringCompareObj(rest[1], o, matchFlags&NoCase != 0) == 0)
		case optGlob:
			eq = boolToInt(i.StringMatchObj(rest[1], o, matchFlags&NoCase != 0))
		case optRegexp, optCommand:
			eq = i.commandMatchObj(commandObj, rest[1], o, matchFlags)
			if eq < 0 {
				return ERR
			}
		}

		// Got a match (or a non-match for -not), or (-bool and -all)
		if (!optBoolean && (eq != 0) == !optNegate) || (optBoolean && (eq != 0 || optAllMatches)) {
			var resultObj *Obj

			switch {
			case optBoolean:
				resultObj = i.NewIntObj(int64(eq ^ boolToInt(optNegate)))
			case !optInlineResult:
				resultObj = i.NewIntObj(int64(n))
			case stride == 1:
				resultObj = o
			case optAllMatches:
				// Add the entire sublist directly for -all -stride > 1
				listInsertElements(listObj, -1, searchListObj.list[offset:offset+int(stride)])
			default:
				resultObj = i.NewListObj(searchListObj.list[offset : offset+int(stride)])
			}

			if optAllMatches {
				// The stride > 1 case has already been handled above
				if stride == 1 {
					i.ListAppendElement(listObj, resultObj)
				}
			} else {
				i.SetResult(resultObj)
				return OK
			}
		}
	}

	if optAllMatches {
		i.SetResult(listObj)
	} else if optBoolean {
		// No match
		i.SetResultBool(optNegate)
	} else if !optInlineResult {
		i.SetResultInt(-1)
	}
	return OK
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// commandMatchObj compares a pattern and a string by calling a command, as
// [lsearch -command] and [lsearch -regexp] do. A negative result means the

/* [lappend] */
func lappendCoreCommand(i *Interp, argv []*Obj) int {
	listObj := i.GetVariable(argv[1], Unshared)
	if listObj == nil {
		// Create the list if it does not exist
		listObj = i.NewListObj(nil)
	} else {
		listObj = listObj.dup()
	}
	for _, a := range argv[2:] {
		i.ListAppendElement(listObj, a)
	}
	if i.SetVariable(argv[1], listObj) != OK {
		return ERR
	}
	i.SetResult(listObj)
	return OK
}

/* [linsert] */
func linsertCoreCommand(i *Interp, argv []*Obj) int {
	listObj := argv[1].dup()
	idx, ret := i.GetIndex(argv[2])
	if ret != OK {
		return ERR
	}
	length := i.ListLength(listObj)
	if idx >= length {
		idx = length
	} else if idx < 0 {
		idx = length + idx + 1
	}
	i.ListInsertElements(listObj, idx, argv[3:])
	i.SetResult(listObj)
	return OK
}

/* [lreplace] */
func lreplaceCoreCommand(i *Interp, argv []*Obj) int {
	first, ret := i.GetIndex(argv[2])
	if ret != OK {
		return ERR
	}
	last, ret := i.GetIndex(argv[3])
	if ret != OK {
		return ERR
	}

	listObj := argv[1]
	length := i.ListLength(listObj)

	first = listRelToAbsIndex(length, first)
	last = listRelToAbsIndex(length, last)
	rangeLen := listRelToAbsRange(length, &first, &last)

	// Trying to replace past the end of the list means end of list (TIP #505)
	if first > length {
		first = length
	}

	// <elements before first> <supplied elements> <elements after last>
	newListObj := i.NewListObj(listObj.list[:first])
	listInsertElements(newListObj, -1, argv[4:])
	listInsertElements(newListObj, -1, listObj.list[first+rangeLen:length])

	i.SetResult(newListObj)
	return OK
}

/* [lset] */
func lsetCoreCommand(i *Interp, argv []*Obj) int {
	if len(argv) == 3 {
		// With no indexes, simply implements [set]
		if i.SetVariable(argv[1], argv[2]) != OK {
			return ERR
		}
		i.SetResult(argv[2])
		return OK
	}
	return i.ListSetIndex(argv[1], argv[2:len(argv)-1], argv[len(argv)-1])
}

/* [lsort] */
func lsortCoreCommand(i *Interp, argv []*Obj) int {
	options := []string{
		"-ascii", "-nocase", "-increasing", "-decreasing", "-command", "-integer", "-real",
		"-index", "-unique", "-stride", "-dictionary",
	}
	const (
		optAscii = iota
		optNoCase
		optIncreasing
		optDecreasing
		optCommand
		optInteger
		optReal
		optIndex
		optUnique
		optStride
		optDict
	)
	stride := int64(1)
	info := lsortInfo{typ: lsortAscii, order: 1, interp: i}

	for n := 1; n < len(argv)-1; n++ {
		option, ret := i.GetEnum(argv[n], options, "option", EnumAbbrev|ErrMsg)
		if ret != OK {
			return ERR
		}
		switch option {
		case optAscii:
			info.typ = lsortAscii
		case optDict:
			info.typ = lsortDict
		case optNoCase:
			info.typ = lsortNoCase
		case optInteger:
			info.typ = lsortInteger
		case optReal:
			info.typ = lsortReal
		case optIncreasing:
			info.order = 1
		case optDecreasing:
			info.order = -1
		case optUnique:
			info.unique = true
		case optCommand:
			if n >= len(argv)-2 {
				i.SetResultString("\"-command\" option must be followed by comparison command")
				return ERR
			}
			info.typ = lsortCommand
			info.command = argv[n+1]
			n++
		case optStride:
			if n >= len(argv)-2 {
				return Usage
			}
			n++
			s, ret := i.GetWide(argv[n])
			if ret != OK {
				return ERR
			}
			stride = s
			if stride < 2 {
				i.SetResultString("stride length must be at least 2")
				return ERR
			}
		case optIndex:
			if n >= len(argv)-2 {
				i.SetResultString("\"-index\" option must be followed by list index")
				return ERR
			}
			info.indexv = i.ListElements(argv[n+1])
			if len(info.indexv) == 0 {
				i.SetResultString("\"-index\" option must be followed by list index")
				return ERR
			}
			n++
		}
	}

	resObj := argv[len(argv)-1]
	elements := i.ListElements(resObj)
	listlen := len(elements)
	if listlen <= 1 {
		// Nothing to do
		i.SetResult(resObj)
		return OK
	}

	if stride > 1 {
		if int64(listlen)%stride != 0 {
			i.SetResultString("list size must be a multiple of the stride length")
			return ERR
		}
		// Need to create a new list of lists for sorting
		tmpListObj := i.NewListObj(nil)
		for n := 0; n < listlen; n += int(stride) {
			i.ListAppendElement(tmpListObj, i.NewListObj(elements[n:n+int(stride)]))
		}
		retCode := i.listSortElements(tmpListObj, &info)
		if retCode == OK {
			resObj = i.NewListObj(nil)
			// Now we need to unpack the result back into a flat list
			for n := 0; n < listlen; n += int(stride) {
				i.ListAppendList(resObj, i.ListGetIndex(tmpListObj, n/int(stride)))
			}
			i.SetResult(resObj)
		}
		return retCode
	}

	resObj = resObj.dup()
	retCode := i.listSortElements(resObj, &info)
	if retCode == OK {
		i.SetResult(resObj)
	}
	return retCode
}

/* [lassign] */
func lassignCoreCommand(i *Interp, argv []*Obj) int {
	iter := listCmdIter{obj: argv[1]}

	for _, nameObj := range argv[2:] {
		valObj := iter.next(i)
		if valObj == nil {
			valObj = i.EmptyObj()
		}
		if result := i.SetVariable(nameObj, valObj); result != OK {
			return result
		}
	}

	resultObj := i.NewListObj(nil)
	for !iter.done(i) {
		i.ListAppendElement(resultObj, iter.next(i))
	}

	i.SetResult(resultObj)
	return OK
}

/* [concat] */
func concatCoreCommand(i *Interp, argv []*Obj) int {
	i.SetResult(i.ConcatObj(argv[1:]))
	return OK
}

/* [join] */
func joinCoreCommand(i *Interp, argv []*Obj) int {
	joinStr := " "
	if len(argv) != 2 {
		joinStr = argv[2].String()
	}
	i.SetResult(i.ListJoin(argv[1], joinStr))
	return OK
}

/* [split] */
func splitCoreCommand(i *Interp, argv []*Obj) int {
	str := argv[1].Bytes()
	if len(str) == 0 {
		return OK
	}
	strLen := i.Utf8Length(argv[1])

	splitChars := []byte(" \n\t\r")
	splitLen := 4
	if len(argv) != 2 {
		splitChars = argv[2].Bytes()
		splitLen = i.Utf8Length(argv[2])
	}

	pos := 0
	noMatchStart := 0
	resObj := i.NewListObj(nil)

	if splitLen != 0 {
		for ; strLen > 0; strLen-- {
			c, sl := u8.ToUnicode(str[pos:])
			sc := splitChars
			for scLen := splitLen; scLen > 0; scLen-- {
				pc, n := u8.ToUnicode(sc)
				sc = sc[n:]
				if c == pc {
					i.ListAppendElement(resObj, i.NewStringObjBytes(str[noMatchStart:pos]))
					noMatchStart = pos + sl
					break
				}
			}
			pos += sl
		}
		i.ListAppendElement(resObj, i.NewStringObjBytes(str[noMatchStart:pos]))
	} else {
		// This handles the special case of splitchars eq {}. The C shares one
		// object per common ASCII character, which is not observable.
		for ; strLen > 0; strLen-- {
			_, n := u8.ToUnicode(str[pos:])
			i.ListAppendElement(resObj, i.NewStringObjBytes(str[pos:pos+n]))
			pos += n
		}
	}

	i.SetResult(resObj)
	return OK
}

/* [lrange] */
func lrangeCoreCommand(i *Interp, argv []*Obj) int {
	o, ret := i.ListRange(argv[1], argv[2], argv[3])
	if ret != OK {
		return ERR
	}
	i.SetResult(o)
	return OK
}

/* [lrepeat] */
func lrepeatCoreCommand(i *Interp, argv []*Obj) int {
	count, ret := i.GetWideExpr(argv[1])
	if ret != OK || count < 0 {
		return Usage
	}
	if count == 0 || len(argv) == 2 {
		i.SetResult(i.EmptyObj())
		return OK
	}

	elems := argv[2:]
	o := i.NewListObj(nil)
	if total := int64(len(elems)) * count; total <= maxInt32 {
		listEnsureLength(o, int(total))
	}
	for ; count > 0; count-- {
		listInsertElements(o, -1, elems)
	}

	i.SetResult(o)
	return OK
}

/* [lreverse] */
func lreverseCoreCommand(i *Interp, argv []*Obj) int {
	ele := i.ListElements(argv[1])
	revObj := i.NewListObj(nil)
	listEnsureLength(revObj, len(ele))
	for n := len(ele) - 1; n >= 0; n-- {
		listAppendElement(revObj, ele[n])
	}
	i.SetResult(revObj)
	return OK
}

// listRangeLen computes the number of elements produced by [range].
func listRangeLen(start, end, step int64) int {
	if step == 0 {
		return -1
	}
	if start == end {
		return 0
	} else if step > 0 && start > end {
		return -1
	} else if step < 0 && end > start {
		return -1
	}
	length := end - start
	if length < 0 {
		length = -length
	}
	if step < 0 {
		step = -step
	}
	length = 1 + (length-1)/step
	// We can truncate safely to INT_MAX: [range] will always fail for such a
	// long range because Tcl lists can't be that long.
	if length > maxInt32 {
		length = maxInt32
	}
	if length < 0 {
		return -1
	}
	return int(length)
}

/* [range] */
func rangeCoreCommand(i *Interp, argv []*Obj) int {
	var start, end, step int64 = 0, 0, 1

	if len(argv) == 2 {
		e, ret := i.GetWideExpr(argv[1])
		if ret != OK {
			return ERR
		}
		end = e
	} else {
		s, ret := i.GetWideExpr(argv[1])
		if ret != OK {
			return ERR
		}
		start = s
		e, ret := i.GetWideExpr(argv[2])
		if ret != OK {
			return ERR
		}
		end = e
		if len(argv) == 4 {
			st, ret := i.GetWideExpr(argv[3])
			if ret != OK {
				return ERR
			}
			step = st
		}
	}
	length := listRangeLen(start, end, step)
	if length == -1 {
		i.SetResultString("Invalid (infinite?) range specified")
		return ERR
	}
	o := i.NewListObj(nil)
	listEnsureLength(o, length)
	for n := 0; n < length; n++ {
		listAppendElement(o, i.NewIntObj(start+int64(n)*step))
	}
	i.SetResult(o)
	return OK
}

// registerListCommands registers the list commands, with the same argument
// limits and usage strings as the C core command table.
func registerListCommands(i *Interp) {
	i.RegisterCmd("concat", "?arg ...?", 0, -1, concatCoreCommand, 0)
	i.RegisterCmd("join", "list ?joinString?", 1, 2, joinCoreCommand, 0)
	i.RegisterCmd("lappend", "varName ?value value ...?", 1, -1, lappendCoreCommand, 0)
	i.RegisterCmd("lassign", "varList list ?varName ...?", 2, -1, lassignCoreCommand, 0)
	i.RegisterCmd("lindex", "list ?index ...?", 1, -1, lindexCoreCommand, 0)
	i.RegisterCmd("linsert", "list index ?element ...?", 2, -1, linsertCoreCommand, 0)
	i.RegisterCmd("list", "?arg ...?", 0, -1, listCoreCommand, 0)
	i.RegisterCmd("llength", "list", 1, 1, llengthCoreCommand, 0)
	i.RegisterCmd("lrange", "list first last", 3, 3, lrangeCoreCommand, 0)
	i.RegisterCmd("lrepeat", "count ?value ...?", 1, -1, lrepeatCoreCommand, 0)
	i.RegisterCmd("lreplace", "list first last ?element ...?", 3, -1, lreplaceCoreCommand, 0)
	i.RegisterCmd("lreverse", "list", 1, 1, lreverseCoreCommand, 0)
	i.RegisterCmd("lsearch", "?-exact|-glob|-regexp|-command 'command'? ?-bool|-inline? ?-not? ?-nocase? ?-all? ?-stride len? ?-index val? list value", 2, -1, lsearchCoreCommand, 0)
	i.RegisterCmd("lset", "listVar ?index ...? value", 2, -1, lsetCoreCommand, 0)
	i.RegisterCmd("lsort", "?options? list", 1, -1, lsortCoreCommand, 0)
	i.RegisterCmd("range", "?start? end ?step?", 1, 3, rangeCoreCommand, 0)
	i.RegisterCmd("split", "string ?splitChars?", 1, 2, splitCoreCommand, 0)
}
