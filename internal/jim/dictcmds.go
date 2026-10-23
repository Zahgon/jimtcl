/* Jim - A small embeddable Tcl interpreter: the dict command.
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

// dictSubCmds is the subcommand table of [dict]. The entries without an
// implementation here are implemented in Tcl in stdlib.tcl and are reached
// through the ensemble dispatch below.
var dictSubCmds = []SubCmd{
	{Cmd: "create", Args: "?key value ...?", MinArgs: 0, MaxArgs: -2},
	{Cmd: "get", Args: "dictionary ?key ...?", MinArgs: 1, MaxArgs: -1},
	{Cmd: "getdef", Args: "dictionary ?key ...? key default", MinArgs: 3, MaxArgs: -1, Flags: ModFlagHidden},
	{Cmd: "getwithdefault", Args: "dictionary ?key ...? key default", MinArgs: 3, MaxArgs: -1},
	{Cmd: "set", Args: "varName key ?key ...? value", MinArgs: 3, MaxArgs: -1},
	{Cmd: "unset", Args: "varName key ?key ...?", MinArgs: 2, MaxArgs: -1},
	{Cmd: "exists", Args: "dictionary key ?key ...?", MinArgs: 2, MaxArgs: -1},
	{Cmd: "keys", Args: "dictionary ?pattern?", MinArgs: 1, MaxArgs: 2},
	{Cmd: "size", Args: "dictionary", MinArgs: 1, MaxArgs: 1},
	{Cmd: "info", Args: "dictionary", MinArgs: 1, MaxArgs: 1},
	{Cmd: "merge", Args: "?...?", MinArgs: 0, MaxArgs: -1},
	{Cmd: "with", Args: "dictVar ?key ...? script", MinArgs: 2, MaxArgs: -1},
	{Cmd: "append", Args: "varName key ?value ...?", MinArgs: 2, MaxArgs: -1},
	{Cmd: "lappend", Args: "varName key ?value ...?", MinArgs: 2, MaxArgs: -1},
	{Cmd: "incr", Args: "varName key ?increment?", MinArgs: 2, MaxArgs: 3},
	{Cmd: "remove", Args: "dictionary ?key ...?", MinArgs: 1, MaxArgs: -1},
	{Cmd: "values", Args: "dictionary ?pattern?", MinArgs: 1, MaxArgs: 2},
	{Cmd: "for", Args: "vars dictionary script", MinArgs: 3, MaxArgs: 3},
	{Cmd: "replace", Args: "dictionary ?key value ...?", MinArgs: 1, MaxArgs: -1},
	{Cmd: "update", Args: "varName ?arg ...? script", MinArgs: 2, MaxArgs: -1},
}

// dictWith implements [dict with]: it exposes the entries of a (nested)
// dictionary as local variables, runs a script and copies the variables back.
func (i *Interp) dictWith(dictVarName *Obj, keyv []*Obj, scriptObj *Obj) int {
	// Open up the appropriate level of the dictionary
	dictObj := i.GetVariable(dictVarName, ErrMsg)
	if dictObj == nil {
		return ERR
	}
	o, rc := i.DictKeysVector(dictObj, keyv, ErrMsg)
	if rc != OK {
		return ERR
	}
	// Set the local variables
	dictValues, length := i.DictPairs(o)
	if length != 0 && dictValues == nil {
		return ERR
	}
	for n := 0; n < length; n += 2 {
		if i.SetVariable(dictValues[n], dictValues[n+1]) == ERR {
			return ERR
		}
	}

	// As an optimisation, if the script is empty there is no need to evaluate
	// it or to update the dictionary
	if scriptObj.Len() == 0 {
		return OK
	}

	ret := i.EvalObj(scriptObj)

	// Now if the dictionary still exists, update it from the local variables
	if ret == OK && i.GetVariable(dictVarName, None) != nil {
		// We need a copy of keyv with one extra element at the end
		newkeyv := make([]*Obj, len(keyv)+1)
		copy(newkeyv, keyv)

		for n := 0; n < length; n += 2 {
			// If an element mirrors the dictionary name, skip it to avoid
			// creating a recursive data structure
			if i.StringCompareObj(dictVarName, dictValues[n], false) != 0 {
				// This is nil if the variable no longer exists, which deletes
				// the entry from the dictionary
				o := i.GetVariable(dictValues[n], None)
				newkeyv[len(keyv)] = dictValues[n]
				i.SetDictKeysVector(dictVarName, newkeyv, o, NoResult)
			}
		}
	}

	return ret
}

/* [dict] */
func dictCoreCommand(i *Interp, argv []*Obj) int {
	ct := i.ParseSubCmd(dictSubCmds, argv)
	if ct == nil {
		return ERR
	}
	if ct.Fn != nil {
		// This is -help or -commands
		return ct.Fn(i, argv)
	}

	argc := len(argv)
	types := DictMatchKeys

	switch ct.Cmd {
	case "get":
		o, rc := i.DictKeysVector(argv[2], argv[3:], ErrMsg)
		if rc != OK {
			return ERR
		}
		i.SetResult(o)
		return OK

	case "getdef", "getwithdefault":
		o, rc := i.DictKeysVector(argv[2], argv[3:argc-1], ErrMsg)
		if rc == -1 {
			// Not a valid dictionary
			return ERR
		}
		if rc == ERR {
			i.SetResult(argv[argc-1])
		} else {
			i.SetResult(o)
		}
		return OK

	case "set":
		return i.SetDictKeysVector(argv[2], argv[3:argc-1], argv[argc-1], ErrMsg|Unshared)

	case "exists":
		_, rc := i.DictKeysVector(argv[2], argv[3:], None)
		if rc < 0 {
			return ERR
		}
		i.SetResultBool(rc == OK)
		return OK

	case "unset":
		if i.SetDictKeysVector(argv[2], argv[3:], nil, Unshared) != OK {
			return ERR
		}
		return OK

	case "values":
		types = DictMatchValues
		fallthrough
	case "keys":
		var pattern *Obj
		if argc == 4 {
			pattern = argv[3]
		}
		return i.dictMatchTypes(argv[2], pattern, types, types)

	case "size":
		size, rc := i.DictSize(argv[2])
		if rc != OK {
			return ERR
		}
		i.SetResultInt(int64(size))
		return OK

	case "merge":
		if argc == 2 {
			return OK
		}
		o := i.dictMerge(argv[2:])
		if o == nil {
			return ERR
		}
		i.SetResult(o)
		return OK

	case "create":
		o := i.NewDictObj(argv[2:])
		if o == nil {
			return ERR
		}
		i.SetResult(o)
		return OK

	case "info":
		return i.dictInfo(argv[2])

	case "with":
		return i.dictWith(argv[2], argv[3:argc-1], argv[argc-1])

	case "update":
		if argc < 6 || argc%2 != 0 {
			// Better error message: let the Tcl implementation complain
			argc = 2
		}
	}

	// The remaining subcommands are implemented in Tcl in stdlib.tcl, as
	// procedures named "dict <subcommand>".
	return i.evalEnsemble("dict", argv[1].String(), argv[2:argc])
}

// evalEnsemble invokes the command named "<basecmd> <subcmd>" with the given
// arguments (C: Jim_EvalEnsemble).
func (i *Interp) evalEnsemble(basecmd, subcmd string, argv []*Obj) int {
	nargv := make([]*Obj, 0, len(argv)+1)
	nargv = append(nargv, i.NewStringObj(basecmd+" "+subcmd))
	nargv = append(nargv, argv...)
	return i.EvalObjVector(nargv)
}

// registerDictCommands registers the [dict] command.
func registerDictCommands(i *Interp) {
	i.RegisterCmd("dict", "subcommand ?arg ...?", 1, -1, dictCoreCommand, 0)
}
