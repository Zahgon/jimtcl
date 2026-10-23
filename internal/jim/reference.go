/* Jim Tcl 0.84 ported from C to Go: references, their garbage collection and
 * the [ref], [getref], [setref], [finalize] and [collect] commands.
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

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

/* The string representation of references has two features in order
 * to make the GC faster. The first is that every reference starts
 * with a non common character '<', in order to make the string matching
 * faster. The second is that the reference string rep is 42 characters
 * in length, this means that it is not necessary to check any object with a
 * string repr < 42, and usually there aren't many of these objects. */

// referenceTagLen is the fixed length of a reference tag (C:
// JIM_REFERENCE_TAGLEN) and referenceSpace the length of the string

// formatReference renders the canonical string representation of a reference
// (C: JimFormatReference).
func formatReference(tag string, id uint64) string {
	return fmt.Sprintf("<reference.<%s>.%020d>", tag, id)
}

// NewReference returns a new reference to objPtr with cmdNamePtr as its
// finalizer command, or no finalizer when cmdNamePtr is nil
// (C: Jim_NewReference).
func (i *Interp) NewReference(objPtr, tagPtr, cmdNamePtr *Obj) *Obj {
	refPtr := &Reference{Obj: objPtr, FinalizerCmd: cmdNamePtr}

	id := i.referenceNextID
	i.referenceNextID++
	i.references[strconv.FormatUint(id, 10)] = refPtr
	// The C increments the id counter a second time here, so reference ids
	// advance in steps of two.
	i.referenceNextID++

	/* Set the tag. Trimmed at referenceTagLen. Everything
	 * that does not pass the isRefChar test is replaced with '_' */
	tag := tagPtr.Bytes()
	var b strings.Builder
	for n := 0; n < referenceTagLen; n++ {
		if n < len(tag) && isRefChar(tag[n]) {
			b.WriteByte(tag[n])
		} else {
			b.WriteByte('_')
		}
	}
	refPtr.Tag = b.String()

	refObjPtr := NewObj()
	refObjPtr.kind = kindReference
	refObjPtr.wide = int64(id)
	refObjPtr.aux = refPtr
	// The string representation is installed eagerly so that it does not have
	// to be regenerated from the reference later.
	refObjPtr.setStringRep(formatReference(refPtr.Tag, id))
	return refObjPtr
}

// referenceID returns the id of a reference object.
func referenceID(o *Obj) uint64 { return uint64(o.wide) }

// GetReference returns the reference an object denotes, or nil with an error
// result (C: Jim_GetReference).
func (i *Interp) GetReference(objPtr *Obj) *Reference {
	if objPtr.kind != kindReference && i.setReferenceFromAny(objPtr) == ERR {
		return nil
	}
	refPtr, _ := objPtr.aux.(*Reference)
	return refPtr
}

// SetFinalizer installs the finalizer command of a reference
// (C: Jim_SetFinalizer).
func (i *Interp) SetFinalizer(objPtr, cmdNamePtr *Obj) int {
	refPtr := i.GetReference(objPtr)
	if refPtr == nil {
		return ERR
	}
	refPtr.FinalizerCmd = cmdNamePtr
	return OK
}

// GetFinalizer returns the finalizer command of a reference, which is nil when
// no finalizer is registered (C: Jim_GetFinalizer).
func (i *Interp) GetFinalizer(objPtr *Obj) (*Obj, int) {
	refPtr := i.GetReference(objPtr)
	if refPtr == nil {
		return nil, ERR
	}
	return refPtr.FinalizerCmd, OK
}

/* -----------------------------------------------------------------------------
 * References Garbage Collection
 * ---------------------------------------------------------------------------*/

// refMarkString marks every reference id found in the string representation of
// an object, as the mark phase of Jim_Collect() does. The checks match the C
// exactly: a candidate must be long enough and have '>' and '.' where the tag
// ends, followed by twenty digits.
func refMarkString(str string, marks map[uint64]bool) {
	if len(str) < referenceSpace {
		return
	}
	str = strings.TrimPrefix(str, "::")
	for p := 0; ; {
		idx := strings.Index(str[p:], "<reference.<")
		if idx < 0 {
			return
		}
		p += idx
		if len(str)-p < referenceSpace {
			return
		}
		if str[p+41] != '>' || str[p+19] != '>' || str[p+20] != '.' {
			return
		}
		id, err := strconv.ParseUint(str[p+21:p+41], 10, 64)
		if err != nil {
			return
		}
		marks[id] = true
		p += referenceSpace
	}
}

// refMarkObj marks the references reachable from objPtr. The C scans the string
// representation of every live object; this port instead walks the objects
// reachable from the interpreter roots, descending into lists and dictionaries
// rather than rendering them.
func refMarkObj(objPtr *Obj, marks map[uint64]bool, seen map[*Obj]bool) {
	if objPtr == nil || seen[objPtr] {
		return
	}
	seen[objPtr] = true
	switch objPtr.kind {
	case kindReference:
		marks[referenceID(objPtr)] = true
	case kindList:
		for _, e := range objPtr.list {
			refMarkObj(e, marks, seen)
		}
	case kindDict:
		if objPtr.dict != nil {
			for _, e := range objPtr.dict.table {
				refMarkObj(e, marks, seen)
			}
		}
	default:
		refMarkString(objPtr.String(), marks)
	}
}

// refMarkFrames marks the references held by a chain of call frames.
func refMarkFrames(framePtr *CallFrame, marks map[uint64]bool, seen map[*Obj]bool) {
	for ; framePtr != nil; framePtr = framePtr.parent {
		for _, e := range framePtr.vars.entries() {
			vv := e.val
			refMarkObj(vv.Obj, marks, seen)
		}
		for _, e := range framePtr.staticVars.entries() {
			vv := e.val
			refMarkObj(vv.Obj, marks, seen)
		}
		for _, o := range framePtr.argv {
			refMarkObj(o, marks, seen)
		}
		for _, o := range framePtr.localCommands {
			refMarkObj(o, marks, seen)
		}
		refMarkObj(framePtr.procArgsObj, marks, seen)
		refMarkObj(framePtr.procBodyObj, marks, seen)
		refMarkObj(framePtr.nsObj, marks, seen)
		refMarkObj(framePtr.tailcallObj, marks, seen)
	}
}

// Collect performs the garbage collection of references and returns the number
// of references collected (C: Jim_Collect).
func (i *Interp) Collect() int {
	collected := 0
	marks := make(map[uint64]bool)
	seen := make(map[*Obj]bool)

	/* Mark all the references found into the 'marks' table.
	 * The references are searched in every reachable object. */
	for _, o := range []*Obj{i.result, i.stackTrace, i.errorProc, i.unknown, i.deferObj,
		i.traceCmdObj, i.currentFileNameObj} {
		refMarkObj(o, marks, seen)
	}
	refMarkFrames(i.framePtr, marks, seen)
	refMarkFrames(i.topFramePtr, marks, seen)
	for evalFrame := i.evalFrame; evalFrame != nil; evalFrame = evalFrame.parent {
		for _, o := range evalFrame.argv {
			refMarkObj(o, marks, seen)
		}
		refMarkObj(evalFrame.scriptObj, marks, seen)
	}
	/* A command name object with a reference count of one is only held by the
	 * command table itself, so the C does not mark it; command names are
	 * therefore not roots here either. */
	for _, ce := range i.commands.entries() {
		cmd := ce.val
		for ; cmd != nil; cmd = cmd.prevCmd {
			refMarkObj(cmd.argList, marks, seen)
			refMarkObj(cmd.body, marks, seen)
			refMarkObj(cmd.nsObj, marks, seen)
			if privData, ok := cmd.privData.(*Obj); ok {
				refMarkObj(privData, marks, seen)
			}
			for _, e := range cmd.staticVars.entries() {
				vv := e.val
				refMarkObj(vv.Obj, marks, seen)
			}
		}
	}
	for _, refPtr := range i.references {
		refMarkObj(refPtr.Obj, marks, seen)
		refMarkObj(refPtr.FinalizerCmd, marks, seen)
	}

	/* Run the references table to destroy every reference that
	 * is not referenced outside (not present in the marks table). */
	for _, key := range refSortedIDs(i.references) {
		refPtr, ok := i.references[key]
		id, _ := strconv.ParseUint(key, 10, 64)
		if !ok || marks[id] {
			continue
		}
		collected++
		/* Drop the reference, but call the finalizer if registered. The C
		 * removes the entry only after the finalizer has run and relies on
		 * interp->lastCollectId to refuse a reentrant collection; here the
		 * entry goes first so that a finalizer calling [collect] terminates. */
		delete(i.references, key)
		if refPtr.FinalizerCmd != nil {
			objv := []*Obj{refPtr.FinalizerCmd,
				i.NewStringObj(formatReference(refPtr.Tag, id)), refPtr.Obj}
			// Call the finalizer, ignoring any error, and keep the result.
			oldResult := i.result
			i.EvalObjVector(objv)
			i.SetResult(oldResult)
		}
	}
	return collected
}

// refSortedIDs returns the keys of the reference table ordered by reference id.
// The C iterates its hash table, whose order is not reproducible here.
func refSortedIDs(m map[string]*Reference) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(a, b int) bool {
		x, _ := strconv.ParseUint(keys[a], 10, 64)
		y, _ := strconv.ParseUint(keys[b], 10, 64)
		return x < y
	})
	return keys
}

// refCoreCommand implements [ref] (C: Jim_RefCoreCommand).
func refCoreCommand(i *Interp, argv []*Obj) int {
	if len(argv) == 3 {
		i.SetResult(i.NewReference(argv[1], argv[2], nil))
	} else {
		i.SetResult(i.NewReference(argv[1], argv[2], argv[3]))
	}
	return OK
}

// getrefCoreCommand implements [getref] (C: Jim_GetrefCoreCommand).
func getrefCoreCommand(i *Interp, argv []*Obj) int {
	refPtr := i.GetReference(argv[1])
	if refPtr == nil {
		return ERR
	}
	i.SetResult(refPtr.Obj)
	return OK
}

// setrefCoreCommand implements [setref] (C: Jim_SetrefCoreCommand).
func setrefCoreCommand(i *Interp, argv []*Obj) int {
	refPtr := i.GetReference(argv[1])
	if refPtr == nil {
		return ERR
	}
	refPtr.Obj = argv[2]
	i.SetResult(argv[2])
	return OK
}

// collectCoreCommand implements [collect] (C: Jim_CollectCoreCommand).
func collectCoreCommand(i *Interp, argv []*Obj) int {
	i.SetResultInt(int64(i.Collect()))
	return OK
}

// finalizeCoreCommand implements [finalize reference ?finalizerProc?]
// (C: Jim_FinalizeCoreCommand).
func finalizeCoreCommand(i *Interp, argv []*Obj) int {
	if len(argv) == 2 {
		cmdNamePtr, rc := i.GetFinalizer(argv[1])
		if rc != OK {
			return ERR
		}
		if cmdNamePtr != nil {
			// otherwise the null string is returned.
			i.SetResult(cmdNamePtr)
		}
		return OK
	}
	if i.SetFinalizer(argv[1], argv[2]) != OK {
		return ERR
	}
	i.SetResult(argv[2])
	return OK
}

// infoReferences implements [info references] (C: JimInfoReferences).
func (i *Interp) infoReferences() int {
	listObjPtr := i.NewListObj(nil)
	for _, key := range refSortedIDs(i.references) {
		id, _ := strconv.ParseUint(key, 10, 64)
		i.ListAppendElement(listObjPtr, i.NewStringObj(formatReference(i.references[key].Tag, id)))
	}
	i.SetResult(listObjPtr)
	return OK
}
