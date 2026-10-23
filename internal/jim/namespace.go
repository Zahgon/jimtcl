/* Jim Tcl 0.84 ported from C to Go: the [namespace] and [variable] commands.
 *
 * Copyright 2011 Steve Bennett <steveb@workware.net.au>
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

import "strings"

// nsSubCmds is the subcommand table of [namespace]. Only a few subcommands are
// implemented here; the rest are Tcl procs named "namespace <subcommand>" in
// nshelper.tcl, which the default branch dispatches to.
var nsSubCmds = []SubCmd{
	{Cmd: "canonical", Args: "?current? ?name?", MinArgs: 0, MaxArgs: 2},
	{Cmd: "code", Args: "arg", MinArgs: 1, MaxArgs: 1},
	{Cmd: "current", MinArgs: 0, MaxArgs: 0},
	{Cmd: "delete", Args: "?name ...?", MinArgs: 0, MaxArgs: -1},
	{Cmd: "ensemble", Args: "subcommand ?arg ...?", MinArgs: 1, MaxArgs: -1},
	{Cmd: "eval", Args: "name arg ?arg ...?", MinArgs: 2, MaxArgs: -1},
	{Cmd: "export", Args: "?...?", MinArgs: 0, MaxArgs: -1},
	{Cmd: "import", Args: "?...?", MinArgs: 0, MaxArgs: -1},
	{Cmd: "inscope", Args: "name arg ?arg ...?", MinArgs: 2, MaxArgs: -1},
	{Cmd: "origin", Args: "name", MinArgs: 1, MaxArgs: 1},
	{Cmd: "parent", Args: "?name?", MinArgs: 0, MaxArgs: 1},
	{Cmd: "qualifiers", Args: "string", MinArgs: 1, MaxArgs: 1},
	{Cmd: "tail", Args: "string", MinArgs: 1, MaxArgs: 1},
	{Cmd: "upvar", Args: "ns ?arg ...?", MinArgs: 1, MaxArgs: -1},
	{Cmd: "which", Args: "?-command|-variable? name", MinArgs: 1, MaxArgs: 2},
}

// registerNamespaceCommands registers the commands of jim-namespace.c
// (C: Jim_namespaceInit).
func registerNamespaceCommands(i *Interp) {
	i.PackageProvide("namespace", "1.0", ErrMsg)
	i.RegisterCmd("namespace", "subcommand ?arg ...?", 1, -1, nsNamespaceCmd, 0)
	i.RegisterCmd("variable", "name ?value?", 1, 2, nsVariableCmd, 0)
}

// nsObjLen is Jim_Length() for an object that may not have been set yet, such
// as the namespace of a call frame built by another area.
func nsObjLen(o *Obj) int {
	if o == nil {
		return 0
	}
	return o.Len()
}

// canonicalNamespace appends name to the canonical namespace name nsObj and
// returns the resulting canonical name (C: JimCanonicalNamespace).
//
//	""    "abc"      => abc
//	""    "::abc"    => abc
//	""    "abc::def" => abc::def
//	"abc" "def"      => abc::def
//	"abc" "::def"    => def
func (i *Interp) canonicalNamespace(nsObj, nameObj *Obj) *Obj {
	name := nameObj.String()
	if strings.HasPrefix(name, "::") {
		// Absolute namespace
		return i.NewStringObj(strings.TrimLeft(name, ":"))
	}
	if nsObjLen(nsObj) == 0 {
		// Relative to the global namespace
		return nameObj
	}
	// Relative to non-global namespace. The C duplicates nsObj and appends to
	// it, which yields the same bytes as building the name in one go.
	return i.NewStringObj(nsObj.String() + "::" + name)
}

// NamespaceQualifiers returns the namespace part of a qualified name
// (C: Jim_NamespaceQualifiers).
//
//	::bob::tom => ::bob
//	bob::tom   => bob
//	::bob      => ::
//	bob        => ""
//	::         => ""
//	""         => ""
func (i *Interp) NamespaceQualifiers(ns *Obj) *Obj {
	name := ns.String()
	if pt := strings.LastIndexByte(name, ':'); pt > 0 && name[pt-1] == ':' {
		return i.NewStringObj(name[:pt-1])
	}
	return i.emptyObj
}

// NamespaceTail returns the last component of a qualified name
// (C: Jim_NamespaceTail).
func (i *Interp) NamespaceTail(ns *Obj) *Obj {
	name := ns.String()
	if pt := strings.LastIndexByte(name, ':'); pt > 0 && name[pt-1] == ':' {
		return i.NewStringObj(name[pt+1:])
	}
	return ns
}

// namespaceCurrent returns the fully qualified name of the current namespace
// (C: JimNamespaceCurrent).
func (i *Interp) namespaceCurrent() *Obj {
	return i.NewStringObj("::" + i.framePtr.nsObj.String())
}

// nsEvalEnsemble invokes the Tcl helper proc named "basecmd subcmd" with objv
// as its arguments (C: Jim_EvalEnsemble). Note that the subcommand name used is
// the one the caller typed, so an abbreviation names a non-existent command.
func (i *Interp) nsEvalEnsemble(basecmd, subcmd string, objv []*Obj) int {
	return i.evalPrefixStr(basecmd+" "+subcmd, objv)
}

// nsNamespaceCmd implements [namespace] (C: JimNamespaceCmd).
func nsNamespaceCmd(i *Interp, argv []*Obj) int {
	ct := i.ParseSubCmd(nsSubCmds, argv)
	if ct == nil {
		return ERR
	}
	if ct.Fn != nil {
		// This is -help or -commands, whose result is already set.
		return ct.Fn(i, argv)
	}
	argc := len(argv)

	switch ct.Cmd {
	case "eval":
		var objPtr *Obj
		if argc == 4 {
			objPtr = argv[3]
		} else {
			objPtr = i.ConcatObj(argv[3:])
		}
		nsObj := i.canonicalNamespace(i.framePtr.nsObj, argv[2])
		return i.nsEvalNamespace(objPtr, nsObj)

	case "current":
		i.SetResult(i.namespaceCurrent())
		return OK

	case "canonical":
		switch argc {
		case 2:
			i.SetResult(i.framePtr.nsObj)
		case 3:
			i.SetResult(i.canonicalNamespace(i.framePtr.nsObj, argv[2]))
		default:
			i.SetResult(i.canonicalNamespace(argv[2], argv[3]))
		}
		return OK

	case "qualifiers":
		i.SetResult(i.NamespaceQualifiers(argv[2]))
		return OK

	case "export":
		// Every command of a namespace is exported, so nothing to do.
		return OK

	case "tail":
		i.SetResult(i.NamespaceTail(argv[2]))
		return OK

	case "parent":
		objPtr := i.framePtr.nsObj
		if argc == 3 {
			objPtr = argv[2]
		}
		if nsObjLen(objPtr) == 0 || objPtr.String() == "::" {
			// The global namespace has no parent; the result stays empty.
			return OK
		}
		objPtr = i.NamespaceQualifiers(objPtr)
		if !strings.HasPrefix(objPtr.String(), "::") {
			// Make it fully scoped
			i.SetResultString("::" + objPtr.String())
		} else {
			i.SetResult(objPtr)
		}
		return OK
	}

	/* Implemented as a Tcl helper proc.
	 * Note that calling a proc will change the current namespace,
	 * so helper procs must call [uplevel namespace canon] to get the callers
	 * namespace.
	 */
	return i.nsEvalEnsemble("namespace", argv[1].String(), argv[2:])
}

// CreateNamespaceVariable links a local name to a variable of the global
// namespace (C: Jim_CreateNamespaceVariable).
func (i *Interp) CreateNamespaceVariable(varNameObj, targetNameObj *Obj) int {
	rc := i.nsSetVariableLink(varNameObj, targetNameObj, i.topFramePtr)
	if rc == ERR {
		// This is the only reason the link can fail
		i.Errorf("can't define \"%#s\": name refers to an element in an array", varNameObj)
	}
	return rc
}

// nsVariableCmd implements [variable] (C: JimVariableCmd).
func nsVariableCmd(i *Interp, argv []*Obj) int {
	retcode := OK
	argc := len(argv)

	if argc > 1 {
		targetNameObj := i.canonicalNamespace(i.framePtr.nsObj, argv[1])
		localNameObj := i.NamespaceTail(argv[1])
		if i.framePtr.level != 0 || nsObjLen(i.framePtr.nsObj) != 0 {
			retcode = i.CreateNamespaceVariable(localNameObj, targetNameObj)
		}
		// Set the variable via the local name
		if retcode == OK && argc > 2 {
			retcode = i.SetVariable(localNameObj, argv[2])
		}
	}
	return retcode
}

// nsEvalNamespace evaluates a script in a fresh call frame belonging to nsObj
// (C: Jim_EvalNamespace).
func (i *Interp) nsEvalNamespace(scriptObj, nsObj *Obj) int {
	callFramePtr := &CallFrame{
		id:          i.callFrameEpoch,
		level:       i.framePtr.level + 1,
		parent:      i.framePtr,
		nsObj:       nsObj,
		procBodyObj: scriptObj,
	}
	i.callFrameEpoch++
	if i.evalFrame != nil {
		callFramePtr.argv = i.evalFrame.argv
	}
	i.framePtr = callFramePtr

	retcode := OK
	if callFramePtr.level == i.maxCallFrameDepth {
		i.SetResultString("Too many nested calls. Infinite recursion?")
		retcode = ERR
	} else {
		retcode = i.EvalObj(scriptObj)
	}

	i.framePtr = callFramePtr.parent
	return retcode
}

// nsIsDictSugar reports whether a variable name is really an array element
// reference such as "a(1)" (C: the JIM_DICT_SUGAR case of SetVariableFromAny).
func nsIsDictSugar(name string) bool {
	return strings.HasSuffix(name, ")") && strings.ContainsRune(name, '(')
}

// nsSetVariableLink binds nameObjPtr in the current frame to the variable
// targetNameObjPtr of targetCallFrame (C: Jim_SetVariableLink).
func (i *Interp) nsSetVariableLink(nameObjPtr, targetNameObjPtr *Obj, targetCallFrame *CallFrame) int {
	varName := nameObjPtr.String()
	if nsIsDictSugar(varName) {
		/* XXX: This message seem unnecessarily verbose, but it matches Tcl */
		return i.Errorf("bad variable name \"%#s\": upvar won't create a scalar variable that looks like an array element", nameObjPtr)
	}

	framePtr := i.framePtr
	if strings.HasPrefix(varName, "::") {
		varName = strings.TrimLeft(varName, ":")
		// Linking a global var does nothing
		framePtr = i.topFramePtr
	}
	if vv, ok := framePtr.vars.get(varName); ok {
		if vv.LinkFrame == nil {
			return i.Errorf("variable \"%#s\" already exists", nameObjPtr)
		}
		// It exists, but is a link, so first delete the link
		vv.LinkFrame = nil
	}

	targetName := targetNameObjPtr.String()
	if strings.HasPrefix(targetName, "::") {
		targetNameObjPtr = i.NewStringObj(strings.TrimLeft(targetName, ":"))
		targetCallFrame = i.topFramePtr
	}

	if framePtr.level < targetCallFrame.level {
		return i.Errorf("bad variable name \"%#s\": upvar won't create namespace variable that refers to procedure variable", nameObjPtr)
	}
	// Cycles are only possible with 'uplevel 0'
	if framePtr == targetCallFrame && targetNameObjPtr.String() == varName {
		i.SetResultString("can't upvar from variable to itself")
		return ERR
	}

	// Perform the binding
	if rc := i.SetVariable(nameObjPtr, targetNameObjPtr); rc != OK {
		return rc
	}
	if vv, ok := framePtr.vars.get(varName); ok {
		vv.LinkFrame = targetCallFrame
	}
	return OK
}
