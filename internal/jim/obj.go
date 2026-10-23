/* Jim - A small embeddable Tcl interpreter: object string representations.
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

// updateStringRep regenerates the string representation of an object from its
// internal representation, as the C implementation's per-type updateStringProc
// does.
func (o *Obj) updateStringRep() {
	switch o.kind {
	case kindInt:
		o.setStringRep(formatWide(o.wide))
	case kindCoercedDouble:
		o.setStringRep(formatWide(o.wide))
	case kindDouble:
		o.setStringRep(formatDouble(o.dbl))
	case kindBool:
		o.setStringRep(formatWide(o.wide))
	case kindList:
		o.setStringRep(makeListStringRep(o.list))
	case kindDict:
		o.setStringRep(makeListStringRep(o.dict.table))
	case kindIndex:
		o.setStringRep(indexStringRep(o))
	case kindReturnCode:
		o.setStringRep(ReturnCodeName(int(o.wide)))
	case kindReference:
		o.setStringRep(referenceStringRep(o))
	default:
		// Types with no string representation of their own keep the string
		// they were created from; an object with neither is the empty string.
		o.setStringRep("")
	}
}

// freeIntRep drops the internal representation, keeping the string.
func (o *Obj) freeIntRep() {
	o.kind = kindNone
	o.list = nil
	o.dict = nil
	o.aux = nil
}

// dup returns a copy of the object, as Jim_DuplicateObj() does.
func (o *Obj) dup() *Obj {
	n := &Obj{bytes: o.bytes, strValid: o.strValid, charLen: o.charLen, kind: o.kind,
		wide: o.wide, dbl: o.dbl, aux: o.aux, fileName: o.fileName, lineNr: o.lineNr}
	if o.list != nil {
		n.list = make([]*Obj, len(o.list))
		copy(n.list, o.list)
	}
	if o.dict != nil {
		n.dict = o.dict.dup()
	}
	return n
}

// Kind reporting helpers used across the implementation.

// IsList reports whether the object's current representation is a list.
func (o *Obj) IsList() bool { return o.kind == kindList }

// IsDict reports whether the object's current representation is a dictionary.
func (o *Obj) IsDict() bool { return o.kind == kindDict }

// IsInt reports whether the object's current representation is an integer.
func (o *Obj) IsInt() bool { return o.kind == kindInt }

// IsDouble reports whether the object holds a floating-point value.
func (o *Obj) IsDouble() bool { return o.kind == kindDouble || o.kind == kindCoercedDouble }

// SourceInfo returns the file name and line number recorded for an object
// parsed from a script, if any.
func (o *Obj) SourceInfo() (*Obj, int) { return o.fileName, o.lineNr }

// SetSourceInfo attaches source information to an object.
func (o *Obj) SetSourceInfo(fileName *Obj, line int) {
	o.fileName = fileName
	o.lineNr = line
}
