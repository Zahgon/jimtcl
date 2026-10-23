/* Jim - A small embeddable Tcl interpreter: the array command.
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

// Jim arrays are dictionaries held in an ordinary variable, so every
// subcommand below works on the dictionary value of the named variable.

func arrayCmdExists(i *Interp, argv []*Obj) int {
	// Just a regular [info exists]
	dictObj := i.GetVariable(argv[0], Unshared)
	exists := false
	if dictObj != nil {
		_, rc := i.DictSize(dictObj)
		exists = rc == OK
	}
	i.SetResultBool(exists)
	return OK
}

func arrayCmdGet(i *Interp, argv []*Obj) int {
	o := i.GetVariable(argv[0], None)
	if o == nil {
		return OK
	}

	var patternObj *Obj
	if len(argv) != 1 {
		patternObj = argv[1]
	}

	// Optimise the "all" case
	if patternObj == nil || patternObj.String() == "*" {
		if o.IsList() && i.ListLength(o)%2 == 0 {
			// A list with an even number of elements
			i.SetResult(o)
			return OK
		}
	}

	return i.dictMatchTypes(o, patternObj, DictMatchKeys, DictMatchKeys|DictMatchValues)
}

func arrayCmdNames(i *Interp, argv []*Obj) int {
	o := i.GetVariable(argv[0], None)
	if o == nil {
		return OK
	}
	var patternObj *Obj
	if len(argv) != 1 {
		patternObj = argv[1]
	}
	return i.dictMatchTypes(o, patternObj, DictMatchKeys, DictMatchKeys)
}

func arrayCmdUnset(i *Interp, argv []*Obj) int {
	if len(argv) == 1 || argv[1].String() == "*" {
		// Unset the whole array
		i.UnsetVariable(argv[0], None)
		return OK
	}

	o := i.GetVariable(argv[0], None)
	if o == nil {
		// Doesn't exist, so nothing to do
		return OK
	}

	dictValues, length := i.DictPairs(o)
	if dictValues == nil {
		// Not an array: tclsh ignores this and returns nothing, be compatible
		i.SetResultString("")
		return OK
	}

	// Create a new object with the values which don't match
	resultObj := i.NewDictObj(nil)
	for n := 0; n < length; n += 2 {
		if !i.StringMatchObj(argv[1], dictValues[n], false) {
			i.DictAddElement(resultObj, dictValues[n], dictValues[n+1])
		}
	}

	i.SetVariable(argv[0], resultObj)
	return OK
}

func arrayCmdSize(i *Interp, argv []*Obj) int {
	length := 0

	// Not found means zero length
	if o := i.GetVariable(argv[0], None); o != nil {
		size, rc := i.DictSize(o)
		if rc != OK {
			// Not an array: tclsh ignores this and returns 0, be compatible
			i.SetResultInt(0)
			return OK
		}
		length = size
	}

	i.SetResultInt(int64(length))
	return OK
}

func arrayCmdStat(i *Interp, argv []*Obj) int {
	if o := i.GetVariable(argv[0], None); o != nil {
		return i.dictInfo(o)
	}
	return i.Errorf("\"%#s\" isn't an array", argv[0])
}

func arrayCmdSet(i *Interp, argv []*Obj) int {
	listObj := argv[1]

	length := i.ListLength(listObj)
	if length%2 != 0 {
		i.SetResultString("list must have an even number of elements")
		return ERR
	}

	dictObj := i.GetVariable(argv[0], Unshared)
	if dictObj == nil {
		// Doesn't exist, so just set the list directly
		return i.SetVariable(argv[0], listObj)
	}
	if _, rc := i.DictSize(dictObj); rc != OK {
		return ERR
	}

	dictObj = dictObj.dup()

	for n := 0; n < length; n += 2 {
		nameObj, _ := i.ListIndex(listObj, n, None)
		valueObj, _ := i.ListIndex(listObj, n+1, None)

		i.DictAddElement(dictObj, nameObj, valueObj)
	}
	return i.SetVariable(argv[0], dictObj)
}

var arraySubCmds = []SubCmd{
	{Cmd: "exists", Args: "arrayName", Fn: arrayCmdExists, MinArgs: 1, MaxArgs: 1},
	{Cmd: "get", Args: "arrayName ?pattern?", Fn: arrayCmdGet, MinArgs: 1, MaxArgs: 2},
	{Cmd: "names", Args: "arrayName ?pattern?", Fn: arrayCmdNames, MinArgs: 1, MaxArgs: 2},
	{Cmd: "set", Args: "arrayName list", Fn: arrayCmdSet, MinArgs: 2, MaxArgs: 2},
	{Cmd: "size", Args: "arrayName", Fn: arrayCmdSize, MinArgs: 1, MaxArgs: 1},
	{Cmd: "stat", Args: "arrayName", Fn: arrayCmdStat, MinArgs: 1, MaxArgs: 1},
	{Cmd: "unset", Args: "arrayName ?pattern?", Fn: arrayCmdUnset, MinArgs: 1, MaxArgs: 2},
}

// registerArrayCommands registers the [array] command, as the statically
// linked array extension does.
func registerArrayCommands(i *Interp) {
	if i.PackageProvide("array", "1.0", ErrMsg) != OK {
		return
	}
	i.RegisterSubCmd("array", arraySubCmds)
}
