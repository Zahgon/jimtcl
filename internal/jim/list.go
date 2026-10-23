/* Jim - A small embeddable Tcl interpreter: the list object.
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

/* ---------------------------------------------------------------------------
 * The string representation of a list
 * ------------------------------------------------------------------------ */

// Quoting required to emit one element of a list as a string.
const (
	eleStrSimple = iota // can be written as-is
	eleStrBrace         // must be wrapped in braces
	eleStrQuote         // must be backslash-escaped
)

// listElementQuotingType classifies how the given element has to be quoted so
// that the list string representation parses back to the same elements.
func listElementQuotingType(s []byte) int {
	trySimple := true
	var level, blevel int

	if len(s) == 0 {
		return eleStrBrace
	}
	if s[0] == '"' || s[0] == '{' {
		trySimple = false
		goto testbrace
	}
	for _, c := range s {
		switch c {
		case ' ', '$', '"', '[', ']', ';', '\\', '\r', '\n', '\t', '\f', '\v':
			trySimple = false
			goto testbrace
		case '{', '}':
			goto testbrace
		}
	}
	return eleStrSimple

testbrace:
	// Test if it's possible to do with braces
	if s[len(s)-1] == '\\' {
		return eleStrQuote
	}
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '{':
			level++
		case '}':
			level--
			if level < 0 {
				return eleStrQuote
			}
		case '[':
			blevel++
		case ']':
			blevel--
		case '\\':
			// The C reads the terminating nul here, which stops the skip.
			if i+1 < len(s) {
				if s[i+1] == '\n' {
					return eleStrQuote
				}
				i++
			}
		}
	}
	if blevel < 0 {
		return eleStrQuote
	}

	if level == 0 {
		if !trySimple {
			return eleStrBrace
		}
		for _, c := range s {
			switch c {
			case ' ', '$', '"', '[', ']', ';', '\\', '\r', '\n', '\t', '\f', '\v':
				return eleStrBrace
			}
		}
		return eleStrSimple
	}
	return eleStrQuote
}

// backslashQuoteString backslash-escapes s for use as a list element.
func backslashQuoteString(s []byte) []byte {
	q := make([]byte, 0, len(s)*2)
	for _, c := range s {
		switch c {
		case ' ', '$', '"', '[', ']', '{', '}', ';', '\\':
			q = append(q, '\\', c)
		case '\n':
			q = append(q, '\\', 'n')
		case '\r':
			q = append(q, '\\', 'r')
		case '\t':
			q = append(q, '\\', 't')
		case '\f':
			q = append(q, '\\', 'f')
		case '\v':
			q = append(q, '\\', 'v')
		default:
			q = append(q, c)
		}
	}
	return q
}

// makeListStringRep builds the string representation shared by the list and
// dictionary types (C: JimMakeListStringRep).
func makeListStringRep(elems []*Obj) string {
	quotingType := make([]int, len(elems))
	bufLen := 0

	for i, e := range elems {
		s := e.Bytes()
		quotingType[i] = listElementQuotingType(s)
		if quotingType[i] == eleStrSimple {
			if i != 0 || s[0] != '#' {
				bufLen += len(s)
				bufLen++
				continue
			}
			// A leading '#' has to be braced, otherwise the string
			// representation would parse back as a comment.
			quotingType[i] = eleStrBrace
		}
		switch quotingType[i] {
		case eleStrBrace:
			bufLen += len(s) + 2
		case eleStrQuote:
			bufLen += len(s) * 2
		}
		bufLen++ // elements separator
	}

	var b strings.Builder
	b.Grow(bufLen + 1)
	for i, e := range elems {
		s := e.Bytes()
		switch quotingType[i] {
		case eleStrSimple:
			b.Write(s)
		case eleStrBrace:
			b.WriteByte('{')
			b.Write(s)
			b.WriteByte('}')
		case eleStrQuote:
			if i == 0 && s[0] == '#' {
				b.WriteByte('\\')
			}
			b.Write(backslashQuoteString(s))
		}
		if i+1 != len(elems) {
			b.WriteByte(' ')
		}
	}
	return b.String()
}

/* ---------------------------------------------------------------------------
 * The list internal representation
 * ------------------------------------------------------------------------ */

// setListFromAny converts an object to the list representation. Like the C's
// SetListFromAny, which only ever returns JIM_OK, the conversion cannot fail
// and must not: every string is a list, and an unbalanced brace or quote ends
// the final element instead of raising an error, so that `llength "a {b c"` is
// 2 with the last element `{b c}`, exactly as in the original. The errors a
// script writer sees for an unterminated brace come from the script parser
// (parse.go), not from this conversion.
func (i *Interp) setListFromAny(o *Obj) {
	if o.kind == kindList {
		return
	}

	// Optimise dict -> list for an object with no string rep: the dictionary
	// donates its key/value table and the hash table is discarded.
	if o.kind == kindDict && !o.strValid {
		table := o.dict.table
		o.freeIntRep()
		o.kind = kindList
		o.list = table
		return
	}

	// Try to preserve information about filename / line number
	fileNameObj, linenr := i.getSourceInfo(o)
	str := o.Bytes()

	o.freeIntRep()
	o.kind = kindList
	if len(str) != 0 {
		elems, lines := i.parseListObjs(str, linenr)
		o.list = elems
		if fileNameObj != nil {
			// The C records the line of each element, so that a script taken
			// out of a list (a [switch] branch, a [proc] body) reports the
			// right line numbers.
			for n, e := range o.list {
				e.SetSourceInfo(fileNameObj, lines[n])
			}
		}
	}
}

// NewListObj returns a new list object holding copies of the given elements.
func (i *Interp) NewListObj(elems []*Obj) *Obj {
	o := &Obj{charLen: -1, kind: kindList}
	if len(elems) != 0 {
		listInsertElements(o, 0, elems)
	}
	return o
}

// ListElements returns the elements of a list object. The caller must not
// modify the returned slice (C: JimListGetElements).
func (i *Interp) ListElements(o *Obj) []*Obj {
	i.setListFromAny(o)
	return o.list
}

// listEnsureLength makes room for at least n elements without changing the
// length, which is what the C ListEnsureLength() does by reallocating.
func listEnsureLength(l *Obj, n int) {
	if n > cap(l.list) {
		ele := make([]*Obj, len(l.list), n)
		copy(ele, l.list)
		l.list = ele
	}
}

// listInsertElements is the low-level insertion used inside the list object; it
// does not invalidate the string representation. An index of -1 means append.
func listInsertElements(l *Obj, idx int, elems []*Obj) {
	if len(elems) == 0 {
		return
	}
	if idx < 0 || idx >= len(l.list) {
		l.list = append(l.list, elems...)
		return
	}
	ele := make([]*Obj, 0, len(l.list)+len(elems))
	ele = append(ele, l.list[:idx]...)
	ele = append(ele, elems...)
	ele = append(ele, l.list[idx:]...)
	l.list = ele
}

// listAppendElement appends one element without invalidating the string rep.
func listAppendElement(l, e *Obj) {
	l.list = append(l.list, e)
}

// listAppendList appends every element of src, which must be a list.
func listAppendList(l, src *Obj) {
	listInsertElements(l, -1, src.list)
}

// ListAppendElement appends an element to a list object the caller owns.
func (i *Interp) ListAppendElement(l, e *Obj) {
	i.setListFromAny(l)
	l.invalidateStringRep()
	listAppendElement(l, e)
}

// ListAppendList appends every element of src to a list object the caller owns.
func (i *Interp) ListAppendList(l, src *Obj) {
	i.setListFromAny(l)
	i.setListFromAny(src)
	l.invalidateStringRep()
	listAppendList(l, src)
}

// ListInsertElements inserts elements at idx into a list object the caller
// owns; idx is clamped to the list bounds.
func (i *Interp) ListInsertElements(l *Obj, idx int, elems []*Obj) {
	i.setListFromAny(l)
	if idx >= 0 && idx > len(l.list) {
		idx = len(l.list)
	} else if idx < 0 {
		idx = 0
	}
	l.invalidateStringRep()
	listInsertElements(l, idx, elems)
}

// ListLength returns the number of elements of a list object.
func (i *Interp) ListLength(o *Obj) int {
	i.setListFromAny(o)
	return len(o.list)
}

// ListGetIndex returns the element at idx, or nil if idx is out of range.
// Negative indices count from the end of the list.
func (i *Interp) ListGetIndex(o *Obj, idx int) *Obj {
	i.setListFromAny(o)
	if (idx >= 0 && idx >= len(o.list)) || (idx < 0 && -idx-1 >= len(o.list)) {
		return nil
	}
	if idx < 0 {
		idx = len(o.list) + idx
	}
	return o.list[idx]
}

// ListIndex returns the element at idx, setting an error result when the index
// is out of range and flags includes ErrMsg.
func (i *Interp) ListIndex(o *Obj, idx, flags int) (*Obj, int) {
	e := i.ListGetIndex(o, idx)
	if e == nil {
		if flags&ErrMsg != 0 {
			i.SetResultString("list index out of range")
		}
		return nil, ERR
	}
	return e, OK
}

// listIndices walks a list with a vector of indices, as [lindex] does.
// It returns -1 (not ERR) when an index is out of range, in which case an
// error message is set only if flags includes ErrMsg.
func (i *Interp) listIndices(l *Obj, indexv []*Obj, flags int) (*Obj, int) {
	idxes := make([]int, len(indexv))

	// In the rare, contrived case where an index is also the list (or an
	// element) we need to extract the indices first.
	for n, idxObj := range indexv {
		idx, ret := i.GetIndex(idxObj)
		if ret != OK {
			return nil, ret
		}
		idxes[n] = idx
	}

	for n := range indexv {
		o := i.ListGetIndex(l, idxes[n])
		if o == nil {
			if flags&ErrMsg != 0 {
				if idxes[n] < 0 || idxes[n] > i.ListLength(l) {
					i.Errorf("index \"%#s\" out of range", indexv[n])
				} else {
					i.Errorf("element %#s missing from sublist \"%#s\"", indexv[n], l)
				}
			}
			return nil, -1
		}
		l = o
	}
	return l, OK
}

// listSetIndex replaces one element of a list object the caller owns.
func (i *Interp) listSetIndex(l *Obj, idx int, newVal *Obj, flags int) int {
	i.setListFromAny(l)
	if (idx >= 0 && idx >= len(l.list)) || (idx < 0 && -idx-1 >= len(l.list)) {
		if flags&ErrMsg != 0 {
			i.SetResultString("list index out of range")
		}
		return ERR
	}
	if idx < 0 {
		idx = len(l.list) + idx
	}
	l.list[idx] = newVal
	return OK
}

// ListSetIndex modifies the list held in the variable varName, replacing the
// element addressed by indices with newVal. It implements [lset].
func (i *Interp) ListSetIndex(varName *Obj, indices []*Obj, newVal *Obj) int {
	o := i.GetVariable(varName, ErrMsg|Unshared)
	if o == nil {
		return ERR
	}
	// Objects are values here, so always work on a private copy of the
	// variable's value, which is what the C does for a shared object.
	varObj := o.dup()
	o = varObj
	for n := 0; n < len(indices)-1; n++ {
		listObj := o
		idx, ret := i.GetIndex(indices[n])
		if ret != OK {
			return ERR
		}
		o = i.ListGetIndex(listObj, idx)
		if o == nil {
			return i.Errorf("index \"%#s\" out of range", indices[n])
		}
		o = o.dup()
		i.listSetIndex(listObj, idx, o, None)
		listObj.invalidateStringRep()
	}
	idx, ret := i.GetIndex(indices[len(indices)-1])
	if ret != OK {
		return ERR
	}
	if i.listSetIndex(o, idx, newVal, ErrMsg) == ERR {
		return ERR
	}
	o.invalidateStringRep()
	varObj.invalidateStringRep()
	if i.SetVariable(varName, varObj) != OK {
		return ERR
	}
	i.SetResult(varObj)
	return OK
}

// ListJoin returns the elements of a list joined with sep.
func (i *Interp) ListJoin(l *Obj, sep string) *Obj {
	listLen := i.ListLength(l)
	var b strings.Builder
	for n := 0; n < listLen; {
		b.Write(i.ListGetIndex(l, n).Bytes())
		n++
		if n != listLen {
			b.WriteString(sep)
		}
	}
	return i.NewStringObj(b.String())
}

// listIsSpace reports whether c is a space according to the C isspace().
func listIsSpace(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\v', '\f', '\r':
		return true
	}
	return false
}

// ConcatObj concatenates its arguments: a list of the elements of every
// argument if all of them are already lists, otherwise the strings glued
// together with a space, each with leading and trailing whitespace removed.
func (i *Interp) ConcatObj(argv []*Obj) *Obj {
	n := 0
	for ; n < len(argv); n++ {
		if !argv[n].IsList() {
			break
		}
	}
	if n == len(argv) {
		o := i.NewListObj(nil)
		for _, a := range argv {
			listAppendList(o, a)
		}
		return o
	}

	var b strings.Builder
	for n, a := range argv {
		s := a.Bytes()
		// Remove leading space
		for len(s) != 0 && listIsSpace(s[0]) {
			s = s[1:]
		}
		// And trailing space, but keep a space quoted with a backslash
		for len(s) != 0 && listIsSpace(s[len(s)-1]) {
			if len(s) > 1 && s[len(s)-2] == '\\' {
				break
			}
			s = s[:len(s)-1]
		}
		b.Write(s)
		// An element which is empty after trimming gets no separator at all
		if n+1 != len(argv) && len(s) != 0 {
			b.WriteByte(' ')
		}
	}
	return i.NewStringObj(b.String())
}

// listRelToAbsIndex converts an index as returned by GetIndex into an absolute
// index, which may still be out of range.
func listRelToAbsIndex(length, idx int) int {
	if idx < 0 && idx > -math.MaxInt32 {
		return length + idx
	}
	return idx
}

// listRelToAbsRange clamps the pair of absolute indices to a valid range and
// returns the number of elements it addresses.
func listRelToAbsRange(length int, first, last *int) int {
	rangeLen := 0
	if *first <= *last {
		rangeLen = *last - *first + 1
		if rangeLen != 0 {
			if *first < 0 {
				rangeLen += *first
				*first = 0
			}
			if *last >= length {
				rangeLen -= *last - (length - 1)
				*last = length - 1
			}
		}
	}
	if rangeLen < 0 {
		rangeLen = 0
	}
	return rangeLen
}

// ListRange returns a list of the elements of l in the range first..last.
func (i *Interp) ListRange(l, first, last *Obj) (*Obj, int) {
	f, ret := i.GetIndex(first)
	if ret != OK {
		return nil, ret
	}
	lst, ret := i.GetIndex(last)
	if ret != OK {
		return nil, ret
	}
	length := i.ListLength(l) /* will convert into list */
	f = listRelToAbsIndex(length, f)
	lst = listRelToAbsIndex(length, lst)
	rangeLen := listRelToAbsRange(length, &f, &lst)
	if f == 0 && lst == length {
		return l, OK
	}
	if rangeLen == 0 {
		// The C passes "ele + first" with a length of zero, which may be past
		// the end of the array but is never dereferenced.
		return i.NewListObj(nil), OK
	}
	return i.NewListObj(l.list[f : f+rangeLen]), OK
}

/* ---------------------------------------------------------------------------
 * Sorting
 * ------------------------------------------------------------------------ */

// lsort comparison types.
const (
	lsortAscii = iota
	lsortNoCase
	lsortInteger
	lsortReal
	lsortCommand
	lsortDict
)

// lsortInfo collects the options of one [lsort] invocation (C: lsort_info).
type lsortInfo struct {
	command *Obj
	interp  *Interp
	typ     int
	order   int
	indexv  []*Obj
	unique  bool
	subfn   func(l, r *Obj) int
	// rc records a failing comparison; the C longjmps out of qsort() instead,
	// so once it is set no further comparison is evaluated.
	rc int
}

// listSign is the sign of w, as the C JimSign() computes it.
func listSign(w int64) int {
	switch {
	case w == 0:
		return 0
	case w < 0:
		return -1
	}
	return 1
}

// fail records a comparison error and reports "equal" for the rest of the sort.
func (info *lsortInfo) fail(rc int) int {
	if info.rc == OK {
		info.rc = rc
	}
	return 0
}

func (info *lsortInfo) sortString(l, r *Obj) int {
	return info.interp.StringCompareObj(l, r, false) * info.order
}

func (info *lsortInfo) sortStringNoCase(l, r *Obj) int {
	return info.interp.StringCompareObj(l, r, true) * info.order
}

// sortDict compares in dictionary order: embedded numbers compare numerically
// and letters compare without case, with a case sensitive tie break.
func (info *lsortInfo) sortDict(l, r *Obj) int {
	left := l.Bytes()
	right := r.Bytes()

	for {
		if len(left) != 0 && len(right) != 0 && u8.IsDigit(left[0]) && u8.IsDigit(right[0]) {
			lint, lend := numStrtoull(string(left))
			rint, rend := numStrtoull(string(right))
			if lint != rint {
				return listSign(lint-rint) * info.order
			}
			// If the integers are equal but of unequal length, then one must
			// have more leading zeros. The shorter one compares less.
			if lend != rend {
				return listSign(int64(lend-rend)) * info.order
			}
			left = left[lend:]
			right = right[rend:]
		} else {
			cl, ln := listUpperAt(left)
			cr, rn := listUpperAt(right)
			left = left[ln:]
			right = right[rn:]
			if cl != cr {
				return listSign(int64(cl-cr)) * info.order
			}
			if cl == 0 {
				// Equal: use a case sensitive comparison as a tie breaker
				return info.interp.StringCompareObj(l, r, false) * info.order
			}
		}
	}
}

// listUpperAt decodes the character at the start of s folded to upper case,
// returning 0 at the end of the string as the C does for the nul terminator.
func listUpperAt(s []byte) (int, int) {
	if len(s) == 0 {
		return 0, 0
	}
	uc, n := u8.ToUnicode(s)
	return u8.Upper(uc), n
}

func (info *lsortInfo) sortInteger(l, r *Obj) int {
	if info.rc != OK {
		return 0
	}
	lhs, ret := info.interp.GetWide(l)
	if ret != OK {
		return info.fail(ERR)
	}
	rhs, ret := info.interp.GetWide(r)
	if ret != OK {
		return info.fail(ERR)
	}
	return listSign(lhs-rhs) * info.order
}

func (info *lsortInfo) sortReal(l, r *Obj) int {
	if info.rc != OK {
		return 0
	}
	lhs, ret := info.interp.GetDouble(l)
	if ret != OK {
		return info.fail(ERR)
	}
	rhs, ret := info.interp.GetDouble(r)
	if ret != OK {
		return info.fail(ERR)
	}
	switch {
	case lhs == rhs:
		return 0
	case lhs > rhs:
		return info.order
	}
	return -info.order
}

// sortCommand compares by calling a user supplied comparison command.
func (info *lsortInfo) sortCommand(l, r *Obj) int {
	if info.rc != OK {
		return 0
	}
	compareScript := info.command.dup()
	info.interp.ListAppendElement(compareScript, l)
	info.interp.ListAppendElement(compareScript, r)

	if rc := info.interp.EvalObj(compareScript); rc != OK {
		return info.fail(rc)
	}
	ret, rc := info.interp.GetWide(info.interp.Result())
	if rc != OK {
		// The C longjmps with the (successful) eval code here, and a longjmp
		// value of 0 makes setjmp() return 1, so this is an error.
		return info.fail(ERR)
	}
	return listSign(ret) * info.order
}

// sortIndexHelper compares the -index values extracted from two elements.
func (info *lsortInfo) sortIndexHelper(l, r *Obj) int {
	if info.rc != OK {
		return 0
	}
	lObj, ret := info.interp.listIndices(l, info.indexv, ErrMsg)
	if ret != OK {
		return info.fail(ERR)
	}
	rObj, ret := info.interp.listIndices(r, info.indexv, ErrMsg)
	if ret != OK {
		return info.fail(ERR)
	}
	return info.subfn(lObj, rObj)
}

// listRemoveDuplicates removes adjacent equal elements of a sorted list,
// keeping the last of each run, as the C does.
func listRemoveDuplicates(l *Obj, comp func(a, b *Obj) int) {
	ele := l.list
	dst := 0
	for src := 1; src < len(ele); src++ {
		if comp(ele[dst], ele[src]) != 0 {
			dst++
		}
		ele[dst] = ele[src]
	}
	// The C also copies one element past the end here, which is never visible
	// because the length is reduced to dst.
	l.list = ele[:dst+1]
}

// listSortElements sorts a list object the caller owns, in place.
func (i *Interp) listSortElements(l *Obj, info *lsortInfo) int {
	i.setListFromAny(l)

	var fn func(a, b *Obj) int
	switch info.typ {
	case lsortAscii:
		fn = info.sortString
	case lsortNoCase:
		fn = info.sortStringNoCase
	case lsortInteger:
		fn = info.sortInteger
	case lsortReal:
		fn = info.sortReal
	case lsortCommand:
		fn = info.sortCommand
	case lsortDict:
		fn = info.sortDict
	}

	if len(info.indexv) != 0 {
		// Need to interpose a "list index" function
		info.subfn = fn
		fn = info.sortIndexHelper
	}

	listMergeSort(l.list, make([]*Obj, len(l.list)), fn)
	if info.rc != OK {
		return info.rc
	}

	if info.unique && len(l.list) > 1 {
		listRemoveDuplicates(l, fn)
	}
	l.invalidateStringRep()
	return OK
}

// listMergeSort is the merge sort that the C library's qsort() uses for arrays
// of pointers. It is reproduced here because the order in which the elements
// are compared is observable: it decides which element a failing comparison
// reports, and in which order an [lsort -command] script is called.
func listMergeSort(b, tmp []*Obj, cmp func(a, b *Obj) int) {
	n := len(b)
	if n <= 1 {
		return
	}
	n1 := n / 2
	n2 := n - n1
	listMergeSort(b[:n1], tmp, cmp)
	listMergeSort(b[n1:], tmp, cmp)

	i1, i2, t := 0, n1, 0
	for n1 > 0 && n2 > 0 {
		if cmp(b[i1], b[i2]) <= 0 {
			tmp[t] = b[i1]
			i1++
			n1--
		} else {
			tmp[t] = b[i2]
			i2++
			n2--
		}
		t++
	}
	for ; n1 > 0; n1-- {
		tmp[t] = b[i1]
		i1++
		t++
	}
	// The elements left in the second half are already in place
	copy(b[:n-n2], tmp[:n-n2])
}
