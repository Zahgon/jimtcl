/* Jim - A small embeddable Tcl interpreter: variables and the $a(b) sugar.
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

import "strings"

// Flags of SetDictKeysVector(), from jim.h.
const (
	// MustExist makes a missing variable or key an error (C: JIM_MUSTEXIST).
	MustExist = 8
	// NoResult leaves the updated dictionary out of the result (C: JIM_NORESULT).
	NoResult = 16
)

// dictSugarCode is the third result of setVariableFromAny: the name is not a
// plain variable name but syntax glue for [dict], i.e. its last character is
// ')' (C: JIM_DICT_SUGAR).
const dictSugarCode = 100

// varRef is a resolved variable: its value record and the frame it lives in.
type varRef struct {
	vv     *VarVal
	frame  *CallFrame
	global bool
}

// dictSubst is the internal representation of a $var(index) reference: the
// variable name and the (not yet substituted) index.
type dictSubst struct {
	varName *Obj
	index   *Obj
}

// stripGlobal removes the leading "::" of a fully qualified name, as the
// variable functions do before looking the name up in the global frame.
func stripGlobal(name string) string {
	return strings.TrimLeft(name, ":")
}

// isGlobalName reports whether a variable name is fully qualified.
func isGlobalName(name string) bool {
	return len(name) >= 2 && name[0] == ':' && name[1] == ':'
}

// setVariable adds a new variable to a call frame, keeping the creation order
// so that [info locals] and [info vars] can report it.
func (cf *CallFrame) setVariable(name string, vv *VarVal) {
	cf.vars.set(name, vv)
}

// unsetVariable removes a variable from a call frame.
func (cf *CallFrame) unsetVariable(name string) int {
	if _, ok := cf.vars.get(name); !ok {
		return ERR
	}
	cf.vars.delete(name)
	return OK
}

// setVariableFromAny resolves a variable name. It returns OK and the resolved
// variable when it exists, ERR when it does not and dictSugarCode when the
// name is really an array element reference (C: SetVariableFromAny).
func (i *Interp) setVariableFromAny(nameObj *Obj) (*varRef, int) {
	if nameObj.kind == kindDictSubst {
		return nil, dictSugarCode
	}

	varName := nameObj.String()

	/* Make sure it's not syntax glue to get/set dict. */
	if len(varName) != 0 && varName[len(varName)-1] == ')' && strings.IndexByte(varName, '(') >= 0 {
		return nil, dictSugarCode
	}

	var framePtr *CallFrame
	var vv *VarVal
	global := false

	if isGlobalName(varName) {
		global = true
		framePtr = i.topFramePtr
		vv, _ = framePtr.vars.get(stripGlobal(varName))
	} else {
		framePtr = i.framePtr
		vv, _ = framePtr.vars.get(varName)
		if vv == nil && framePtr.staticVars != nil {
			/* Try with static vars. */
			vv, _ = framePtr.staticVars.get(varName)
		}
	}

	if vv == nil {
		return nil, ERR
	}
	return &varRef{vv: vv, frame: framePtr, global: global}, OK
}

// createVariable creates a new variable in the current or the global frame.
func (i *Interp) createVariable(nameObj *Obj, valObj *Obj) *VarVal {
	vv := &VarVal{Obj: valObj}

	name := nameObj.String()
	if isGlobalName(name) {
		i.topFramePtr.setVariable(stripGlobal(name), vv)
	} else {
		i.framePtr.setVariable(name, vv)
	}
	return vv
}

// SetVariable sets the variable named by nameObj to valObj.
func (i *Interp) SetVariable(nameObj *Obj, valObj *Obj) int {
	ref, rc := i.setVariableFromAny(nameObj)

	switch rc {
	case dictSugarCode:
		return i.dictSugarSet(nameObj, valObj)

	case ERR:
		i.createVariable(nameObj, valObj)
		return OK

	default:
		vv := ref.vv
		if vv.LinkFrame == nil {
			vv.Obj = valObj
			return OK
		}
		/* Else handle the link */
		savedCallFrame := i.framePtr
		i.framePtr = vv.LinkFrame
		ret := i.SetVariable(vv.Obj, valObj)
		i.framePtr = savedCallFrame
		return ret
	}
}

// SetVariableStr sets the variable with the given name.
func (i *Interp) SetVariableStr(name string, val *Obj) int {
	return i.SetVariable(i.NewStringObj(name), val)
}

// SetGlobalVariableStr sets a variable in the global frame.
func (i *Interp) SetGlobalVariableStr(name string, val *Obj) int {
	savedFramePtr := i.framePtr
	i.framePtr = i.topFramePtr
	result := i.SetVariableStr(name, val)
	i.framePtr = savedFramePtr
	return result
}

// SetVariableStrWithStr sets a variable to a string value.
func (i *Interp) SetVariableStrWithStr(name, val string) int {
	return i.SetVariableStr(name, i.NewStringObj(val))
}

// SetVariableLink makes nameObj an alias for targetNameObj in
// targetCallFrame, as [upvar] and [global] do.
func (i *Interp) SetVariableLink(nameObj *Obj, targetNameObj *Obj, targetCallFrame *CallFrame) int {
	/* Check for an existing variable or link */
	ref, rc := i.setVariableFromAny(nameObj)
	switch rc {
	case dictSugarCode:
		/* This message seems unnecessarily verbose, but it matches Tcl */
		return i.Errorf("bad variable name \"%#s\": upvar won't create a scalar variable that looks like an array element", nameObj)

	case OK:
		if ref.vv.LinkFrame == nil {
			return i.Errorf("variable \"%#s\" already exists", nameObj)
		}
		/* It exists, but is a link, so first delete the link */
		ref.vv.LinkFrame = nil
	}

	/* Resolve the call frames for both variables */
	varName := nameObj.String()
	var framePtr *CallFrame

	if isGlobalName(varName) {
		varName = stripGlobal(varName)
		/* Linking a global var does nothing */
		framePtr = i.topFramePtr
	} else {
		framePtr = i.framePtr
	}

	targetName := targetNameObj.String()
	if isGlobalName(targetName) {
		targetNameObj = i.NewStringObj(stripGlobal(targetName))
		targetCallFrame = i.topFramePtr
	}

	if framePtr.level < targetCallFrame.level {
		return i.Errorf("bad variable name \"%#s\": upvar won't create namespace variable that refers to procedure variable", nameObj)
	}

	/* Check for cycles. */
	if framePtr == targetCallFrame {
		objPtr := targetNameObj

		/* Cycles are only possible with 'uplevel 0' */
		for {
			if objPtr.String() == varName {
				i.SetResultString("can't upvar from variable to itself")
				return ERR
			}
			cycleRef, rc := i.setVariableFromAny(objPtr)
			if rc != OK {
				break
			}
			if cycleRef.vv.LinkFrame != targetCallFrame {
				break
			}
			objPtr = cycleRef.vv.Obj
		}
	}

	/* Perform the binding */
	i.SetVariable(nameObj, targetNameObj)
	/* We are now sure the variable exists as a plain variable */
	linkRef, _ := i.setVariableFromAny(nameObj)
	linkRef.vv.LinkFrame = targetCallFrame
	return OK
}

// GetVariable returns the value of a variable, or nil when it does not exist
// in the current context. With the ErrMsg flag an error message is set.
//
// If Unshared is set and the variable is an array element in a dictionary
// which is shared, the array variable value is duplicated first, which lets
// the element be updated without affecting other references to the dictionary.
func (i *Interp) GetVariable(nameObj *Obj, flags int) *Obj {
	if i.safeexpr {
		return nameObj
	}
	ref, rc := i.setVariableFromAny(nameObj)
	switch rc {
	case OK:
		vv := ref.vv
		if vv.LinkFrame == nil {
			return vv.Obj
		}
		/* The variable is a link? Resolve it. */
		savedCallFrame := i.framePtr
		i.framePtr = vv.LinkFrame
		objPtr := i.GetVariable(vv.Obj, flags)
		i.framePtr = savedCallFrame
		if objPtr != nil {
			return objPtr
		}
		/* Error, so fall through to the error message */

	case dictSugarCode:
		/* [dict] syntax sugar. */
		return i.dictSugarGet(nameObj, flags)
	}
	if flags&ErrMsg != 0 {
		i.Errorf("can't read \"%#s\": no such variable", nameObj)
	}
	return nil
}

// GetVariableStr returns the value of the named variable.
func (i *Interp) GetVariableStr(name string, flags int) *Obj {
	return i.GetVariable(i.NewStringObj(name), flags)
}

// GetGlobalVariable returns the value of a variable in the global frame.
func (i *Interp) GetGlobalVariable(nameObj *Obj, flags int) *Obj {
	savedFramePtr := i.framePtr
	i.framePtr = i.topFramePtr
	objPtr := i.GetVariable(nameObj, flags)
	i.framePtr = savedFramePtr
	return objPtr
}

// GetGlobalVariableStr returns the value of a named variable in the global
// frame.
func (i *Interp) GetGlobalVariableStr(name string, flags int) *Obj {
	savedFramePtr := i.framePtr
	i.framePtr = i.topFramePtr
	objPtr := i.GetVariableStr(name, flags)
	i.framePtr = savedFramePtr
	return objPtr
}

// UnsetVariable unsets a variable. On success all cached variable objects are
// invalidated by changing the call frame id.
func (i *Interp) UnsetVariable(nameObj *Obj, flags int) int {
	ref, retval := i.setVariableFromAny(nameObj)
	if retval == dictSugarCode {
		/* [dict] syntax sugar. */
		return i.dictSugarSet(nameObj, nil)
	} else if retval == OK {
		vv := ref.vv

		/* If it's a link, unset the target variable instead */
		if vv.LinkFrame != nil {
			framePtr := i.framePtr
			i.framePtr = vv.LinkFrame
			retval = i.UnsetVariable(vv.Obj, None)
			i.framePtr = framePtr
		} else {
			var framePtr *CallFrame
			if ref.global {
				framePtr = i.topFramePtr
				retval = framePtr.unsetVariable(stripGlobal(nameObj.String()))
			} else {
				framePtr = i.framePtr
				retval = framePtr.unsetVariable(nameObj.String())
			}

			if retval == OK {
				/* Change the callframe id, invalidating var lookup caching */
				framePtr.id = i.callFrameEpoch
				i.callFrameEpoch++
			}
		}
	}
	if retval != OK && flags&ErrMsg != 0 {
		i.Errorf("can't unset \"%#s\": no such variable", nameObj)
	}
	return retval
}

/* ---------------------------------------------------------------------------
 * Dict syntax sugar (similar to array Tcl syntax)
 * ------------------------------------------------------------------------ */

// dictSugarParseVarKey splits a variable name used for the [dict] syntax
// sugar into the variable name and the key. For example "foo(bar)" gives
// "foo" and "bar". The function can't fail.
func (i *Interp) dictSugarParseVarKey(objPtr *Obj) dictSubst {
	str := objPtr.String()
	p := strings.IndexByte(str, '(')

	keyEnd := len(str)
	if str[len(str)-1] == ')' {
		keyEnd--
	}

	return dictSubst{
		varName: i.NewStringObj(str[:p]),
		index:   i.NewStringObj(str[p+1 : keyEnd]),
	}
}

// dictSugarSet is the helper of SetVariable for dict-syntax variable names.
// It is also used by UnsetVariable with a nil value.
func (i *Interp) dictSugarSet(objPtr *Obj, valObjPtr *Obj) int {
	i.setDictSubstFromAny(objPtr)
	ds := objPtr.aux.(dictSubst)

	err := i.SetDictKeysVector(ds.varName, []*Obj{ds.index}, valObjPtr, MustExist)

	if err == OK {
		i.SetResult(i.emptyObj)
		return err
	}
	if valObjPtr == nil {
		/* Better error message for unset a(2) where a exists but a(2) doesn't */
		if i.GetVariable(ds.varName, None) != nil {
			return i.Errorf("can't unset \"%#s\": no such element in array", objPtr)
		}
	}
	/* Make the error more informative and Tcl-compatible */
	action := "unset"
	if valObjPtr != nil {
		action = "set"
	}
	i.Errorf("can't %s \"%#s\": variable isn't array", action, objPtr)
	return err
}

// dictExpandArrayVariable expands the array variable (dict sugar) and returns
// the result, or nil on error.
func (i *Interp) dictExpandArrayVariable(varObjPtr, keyObjPtr *Obj, flags int) *Obj {
	dictObjPtr := i.GetVariable(varObjPtr, ErrMsg)
	if dictObjPtr == nil {
		return nil
	}

	resObjPtr, ret := i.DictKey(dictObjPtr, keyObjPtr, None)
	if ret != OK {
		kind := "no such element in"
		if ret < 0 {
			kind = "variable isn't"
		}
		i.Errorf("can't read \"%#s(%#s)\": %s array", varObjPtr, keyObjPtr, kind)
	}
	return resObjPtr
}

// dictSugarGet is the helper of GetVariable for dict-syntax variable names.
func (i *Interp) dictSugarGet(objPtr *Obj, flags int) *Obj {
	i.setDictSubstFromAny(objPtr)
	ds := objPtr.aux.(dictSubst)
	return i.dictExpandArrayVariable(ds.varName, ds.index, flags)
}

// setDictSubstFromAny caches the parsed variable name and key of an object
// which must be in dict-sugar format.
func (i *Interp) setDictSubstFromAny(objPtr *Obj) {
	if objPtr.kind == kindDictSubst {
		return
	}

	var ds dictSubst
	if objPtr.kind == kindInterpolated {
		/* An interpolated object in dict-sugar form */
		ds = objPtr.aux.(dictSubst)
	} else {
		ds = i.dictSugarParseVarKey(objPtr)
	}

	objPtr.freeIntRep()
	objPtr.kind = kindDictSubst
	objPtr.aux = ds
}

// expandDictSugar expands [dict get] sugar in the form of $var(INDEX). objPtr
// is guaranteed to be in the form VARNAME(INDEX). The index part is
// substituted and used to look up a key in the dictionary held by VARNAME.
func (i *Interp) expandDictSugar(objPtr *Obj) *Obj {
	if i.safeexpr {
		return objPtr
	}

	i.setDictSubstFromAny(objPtr)
	ds := objPtr.aux.(dictSubst)

	substKeyObjPtr, rc := i.SubstObj(ds.index, None)
	if rc != OK {
		return nil
	}
	return i.dictExpandArrayVariable(ds.varName, substKeyObjPtr, 0)
}
