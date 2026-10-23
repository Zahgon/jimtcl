/* Jim Tcl 0.84 ported from C to Go: the [info] command.
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
	"strings"
)

// Selectors for infoCommandsList(), in the order the [info] subcommands shift
// them: the C code starts with mode 1 and shifts it left once per fall-through
// so that [info commands] is 1, [info procs] 2, [info aliases] 4 and
// [info channels] 8.
const (
	infoCmdListCommands = 1
	infoCmdListProcs    = 2
	infoCmdListAliases  = 4
	infoCmdListChannels = 8
	infoCmdListAll      = 0x1000
)

// Selectors for infoVariablesList(), shifted the same way: [info globals] is 1,
// [info locals] 2 and [info vars] 4.
const (
	infoVarListGlobals = 1
	infoVarListLocals  = 2
	infoVarListVars    = 4
	infoVarListValues  = 0x1000
)

// infoSubCmds is the subcommand table of [info]. The entries have no function
// of their own: Jim_InfoCoreCommand() looks the subcommand up and then switches
// on it, so the dispatch below switches on the resolved subcommand name.
var infoSubCmds = []SubCmd{
	{Cmd: "alias", Args: "command", MinArgs: 1, MaxArgs: 1},
	{Cmd: "aliases", Args: "?-all? ?pattern?", MinArgs: 0, MaxArgs: 2},
	{Cmd: "args", Args: "procname", MinArgs: 1, MaxArgs: 1},
	{Cmd: "body", Args: "procname", MinArgs: 1, MaxArgs: 1},
	{Cmd: "channels", Args: "?-all? ?pattern?", MinArgs: 0, MaxArgs: 2},
	{Cmd: "commands", Args: "?-all? ?pattern?", MinArgs: 0, MaxArgs: 2},
	{Cmd: "complete", Args: "script ?missing?", MinArgs: 1, MaxArgs: 2},
	{Cmd: "exists", Args: "varName", MinArgs: 1, MaxArgs: 1},
	{Cmd: "frame", Args: "?levelNum?", MinArgs: 0, MaxArgs: 1},
	{Cmd: "globals", Args: "?pattern?", MinArgs: 0, MaxArgs: 1},
	{Cmd: "help", Args: "command", MinArgs: 1, MaxArgs: 1},
	{Cmd: "hostname", MinArgs: 0, MaxArgs: 0},
	{Cmd: "level", Args: "?levelNum?", MinArgs: 0, MaxArgs: 1},
	{Cmd: "locals", Args: "?pattern?", MinArgs: 0, MaxArgs: 1},
	{Cmd: "nameofexecutable", MinArgs: 0, MaxArgs: 0},
	{Cmd: "patchlevel", MinArgs: 0, MaxArgs: 0},
	{Cmd: "procs", Args: "?-all? ?pattern?", MinArgs: 0, MaxArgs: 2},
	{Cmd: "references", MinArgs: 0, MaxArgs: 0},
	{Cmd: "returncodes", Args: "?code?", MinArgs: 0, MaxArgs: 1},
	{Cmd: "script", Args: "?filename?", MinArgs: 0, MaxArgs: 1},
	{Cmd: "source", Args: "source ?filename line?", MinArgs: 1, MaxArgs: 3},
	{Cmd: "stacktrace", MinArgs: 0, MaxArgs: 0},
	{Cmd: "statics", Args: "procname", MinArgs: 1, MaxArgs: 1},
	{Cmd: "tainted", Args: "value", MinArgs: 1, MaxArgs: 1},
	{Cmd: "usage", Args: "command", MinArgs: 1, MaxArgs: 1},
	{Cmd: "vars", Args: "?pattern?", MinArgs: 0, MaxArgs: 1},
	{Cmd: "version", MinArgs: 0, MaxArgs: 0},
}

// registerInfoCommands registers [info].
func registerInfoCommands(i *Interp) {
	i.RegisterCmd("info", "subcommand ?arg ...?", 1, -1, infoCoreCommand, 0)
}

// infoCoreCommand implements [info] (C: Jim_InfoCoreCommand).
func infoCoreCommand(i *Interp, argv []*Obj) int {
	// [info -nons ...] is for internal use only: it suppresses the redirection
	// to the namespace-aware Tcl helper.
	// Note that the C shifts argv rather than removing the option, so the
	// command name reported by any error message becomes "-nons".
	nons := false
	if len(argv) > 2 && argv[1].String() == "-nons" {
		argv = argv[1:]
		nons = true
	}

	ct := i.ParseSubCmd(infoSubCmds, argv)
	if ct == nil {
		return ERR
	}
	if ct.Fn != nil {
		// This is -help or -commands, whose result is already set.
		return ct.Fn(i, argv)
	}

	argc := len(argv)
	mode := 1

	switch ct.Cmd {
	case "exists":
		i.SetResultBool(i.GetVariable(argv[2], 0) != nil)
		return OK

	case "alias":
		cmd := i.GetCommand(argv[2], ErrMsg)
		if cmd == nil {
			return ERR
		}
		if cmd.flags&CmdIsAlias == 0 {
			return i.Errorf("command \"%#s\" is not an alias", argv[2])
		}
		target, _ := cmd.privData.(*Obj)
		i.SetResult(target)
		return OK

	case "tainted":
		// Taint tracking is not compiled in, so nothing is ever tainted.
		i.SetResultBool(false)
		return OK

	case "channels":
		mode <<= 1
		fallthrough
	case "aliases":
		mode <<= 1
		fallthrough
	case "procs":
		mode <<= 1
		fallthrough
	case "commands":
		n := 0
		if argc > 2 && argv[2].String() == "-all" {
			mode |= infoCmdListAll
			n++
		}
		if argc < 2+n || argc > 3+n {
			return i.Errorf("wrong # args: should be \"info %#s %s\"", argv[1], ct.Args)
		}
		if !nons {
			// Respect the current namespace unless called as [info -nons ...].
			if nsObjLen(i.framePtr.nsObj) != 0 || (argc == 3 && infoIsGlobalNamespace(argv[2])) {
				return i.evalPrefixStr("namespace info", argv[1:])
			}
		}
		var pattern *Obj
		if argc == 3+n {
			pattern = argv[2+n]
		}
		i.SetResult(i.infoCommandsList(pattern, mode))
		return OK

	case "vars":
		mode <<= 1
		fallthrough
	case "locals":
		mode <<= 1
		fallthrough
	case "globals":
		if !nons {
			if nsObjLen(i.framePtr.nsObj) != 0 || (argc == 3 && infoIsGlobalNamespace(argv[2])) {
				return i.evalPrefixStr("namespace info", argv[1:])
			}
		}
		var pattern *Obj
		if argc == 3 {
			pattern = argv[2]
		}
		i.SetResult(i.infoVariablesList(pattern, mode))
		return OK

	case "script":
		if argc == 3 {
			i.currentFileNameObj = argv[2]
		}
		i.SetResult(i.currentFileNameObj)
		return OK

	case "source":
		if argc == 4 {
			i.SubCmdArgError(ct, argv[0])
			return ERR
		}
		var resObjPtr *Obj
		if argc == 5 {
			line, rc := i.GetWide(argv[4])
			if rc != OK {
				return ERR
			}
			resObjPtr = i.NewStringObjBytes(argv[2].Bytes())
			resObjPtr.SetSourceInfo(argv[3], int(line))
		} else {
			fileNameObj, line := argv[2].SourceInfo()
			if fileNameObj == nil {
				// An object with no source information reads as line 1 of "".
				fileNameObj, line = i.emptyObj, 1
			}
			resObjPtr = i.NewListObj(nil)
			i.ListAppendElement(resObjPtr, fileNameObj)
			i.ListAppendElement(resObjPtr, i.NewIntObj(int64(line)))
		}
		i.SetResult(resObjPtr)
		return OK

	case "stacktrace":
		i.SetResult(i.stackTrace)
		return OK

	case "level":
		if argc == 2 {
			i.SetResultInt(int64(i.framePtr.level))
			return OK
		}
		objPtr, rc := i.infoLevel(argv[2])
		if rc != OK {
			return rc
		}
		i.SetResult(objPtr)
		return OK

	case "frame":
		if argc == 2 {
			i.SetResultInt(int64(i.procLevel + 1))
			return OK
		}
		objPtr, rc := i.infoFrame(argv[2])
		if rc != OK {
			return rc
		}
		i.SetResult(objPtr)
		return OK

	case "usage", "help":
		cmd := i.GetCommand(argv[2], ErrMsg)
		if cmd == nil {
			return ERR
		}
		if ct.Cmd == "usage" {
			i.SetResult(i.cmdUsage(argv[2], cmd))
			return OK
		}
		if cmd.flags&CmdIsProc == 0 && cmd.help != "" {
			i.SetResultString(cmd.help)
			return OK
		}
		// Not having any help is not an error.
		i.Errorf("No help available for command \"%#s\"", argv[2])
		return OK

	case "body", "statics", "args":
		cmd := i.GetCommand(argv[2], ErrMsg)
		if cmd == nil {
			return ERR
		}
		if cmd.flags&CmdIsProc == 0 {
			return i.Errorf("command \"%#s\" is not a procedure", argv[2])
		}
		switch ct.Cmd {
		case "body":
			i.SetResult(cmd.body)
		case "args":
			i.SetResult(cmd.argList)
		case "statics":
			if cmd.staticVars != nil {
				i.SetResult(i.infoStaticsList(cmd.staticVars))
			}
		}
		return OK

	case "patchlevel", "version":
		// JIM_GITVERSION is the release version in a release build, so
		// [info patchlevel] and [info version] agree.
		i.SetResultString(Version)
		return OK

	case "complete":
		complete, missing := i.ScriptIsComplete(argv[2])
		i.SetResultBool(complete)
		if missing != ' ' && argc == 4 {
			i.SetVariable(argv[3], i.NewStringObjBytes([]byte{missing}))
		}
		return OK

	case "hostname":
		// Redirect to os.gethostname if it exists
		return i.Eval("os.gethostname")

	case "nameofexecutable":
		// Redirect to Tcl proc
		return i.Eval("{info nameofexecutable}")

	case "returncodes":
		if argc == 2 {
			listObjPtr := i.NewListObj(nil)
			for code := OK; code <= EVAL; code++ {
				i.ListAppendElement(listObjPtr, i.NewIntObj(int64(code)))
				i.ListAppendElement(listObjPtr, i.NewStringObj(ReturnCodeName(code)))
			}
			i.SetResult(listObjPtr)
		} else if argc == 3 {
			code, rc := i.GetWide(argv[2])
			if rc != OK {
				return ERR
			}
			name := ReturnCodeName(int(code))
			if strings.HasPrefix(name, "?") {
				i.SetResultInt(code)
			} else {
				i.SetResultString(name)
			}
		}
		return OK

	case "references":
		return i.infoReferences()
	}
	return OK
}

// infoIsGlobalNamespace reports whether a namespace object names the global
// namespace (C: JimIsGlobalNamespace).
func infoIsGlobalNamespace(objPtr *Obj) bool {
	return strings.HasPrefix(objPtr.String(), "::")
}

// evalPrefixStr invokes prefix, which may itself contain spaces and so names a
// single command, with objv as its arguments (C: Jim_EvalPrefix).
func (i *Interp) evalPrefixStr(prefix string, objv []*Obj) int {
	nargv := make([]*Obj, 0, len(objv)+1)
	nargv = append(nargv, i.NewStringObj(prefix))
	return i.EvalObjVector(append(nargv, objv...))
}

// infoTrivialMatch reports whether a pattern has no glob metacharacters, in
// which case the C implementation looks the name up directly instead of
// iterating (C: JimTrivialMatch).
func infoTrivialMatch(pattern string) bool {
	return !strings.ContainsAny(pattern, "*[?\\")
}

// infoNoQualifier strips a leading namespace qualifier, as the command hash
// table does when hashing and comparing names (C: Jim_GetStringNoQualifier).
func infoNoQualifier(s string) string {
	if strings.HasPrefix(s, "::") {
		return strings.TrimLeft(s, ":")
	}
	return s
}

// infoCommandsList returns the list of command names selected by mode that
// match pattern, or all of them when pattern is nil (C: JimCommandsList).
func (i *Interp) infoCommandsList(pattern *Obj, mode int) *Obj {
	listObjPtr := i.NewListObj(nil)
	if pattern != nil && infoTrivialMatch(pattern.String()) {
		name := infoNoQualifier(pattern.String())
		if cmd, ok := i.commands.get(name); ok {
			i.infoCommandMatch(listObjPtr, name, cmd, pattern, mode)
		}
		return listObjPtr
	}
	seen := make(map[string]bool, i.commands.len())
	for _, name := range i.commands.keys() {
		cmd, ok := i.commands.get(name)
		if !ok || seen[name] {
			// The name has since been deleted, or redefined and so listed twice.
			continue
		}
		seen[name] = true
		i.infoCommandMatch(listObjPtr, name, cmd, pattern, mode)
	}
	return listObjPtr
}

// infoCommandMatch appends name to listObjPtr if the command passes the type
// filters in mode and matches pattern (C: JimCommandMatch).
func (i *Interp) infoCommandMatch(listObjPtr *Obj, name string, cmd *Cmd, pattern *Obj, mode int) {
	if mode&infoCmdListProcs != 0 && cmd.flags&CmdIsProc == 0 {
		return
	}
	if mode&infoCmdListChannels != 0 && cmd.flags&CmdIsChannel == 0 {
		return
	}
	if mode&infoCmdListAliases != 0 && cmd.flags&CmdIsAlias == 0 {
		return
	}
	if mode&infoCmdListAll == 0 && strings.ContainsRune(name, ' ') {
		// Commands whose name contains a space, such as the [namespace eval]
		// helpers, are hidden unless -all is given.
		return
	}
	if pattern != nil && !globMatch([]byte(infoNoQualifier(pattern.String())), []byte(infoNoQualifier(name)), false) {
		return
	}
	i.ListAppendElement(listObjPtr, i.NewStringObj(name))
}

// infoVariablesList returns the list of variable names selected by mode that
// match pattern, or all of them when pattern is nil (C: JimVariablesList).
func (i *Interp) infoVariablesList(pattern *Obj, mode int) *Obj {
	if mode&infoVarListLocals != 0 && i.framePtr == i.topFramePtr {
		/* For [info locals], if we are at top level an empty list
		 * is returned. I don't agree, but we aim at compatibility (SS) */
		return i.emptyObj
	}
	framePtr := i.framePtr
	if mode&infoVarListGlobals != 0 {
		framePtr = i.topFramePtr
	}
	listObjPtr := i.NewListObj(nil)
	if pattern != nil && infoTrivialMatch(pattern.String()) {
		name := pattern.String()
		if vv, ok := framePtr.vars.get(name); ok {
			i.infoVariableMatch(listObjPtr, name, vv, pattern, mode)
		}
		return listObjPtr
	}
	seen := make(map[string]bool, framePtr.vars.len())
	for _, name := range framePtr.vars.keys() {
		vv, ok := framePtr.vars.get(name)
		if !ok || seen[name] {
			continue
		}
		seen[name] = true
		i.infoVariableMatch(listObjPtr, name, vv, pattern, mode)
	}
	return listObjPtr
}

// infoStaticsList returns the static variables of a procedure as a
// name/value list, as [info statics] reports them.
func (i *Interp) infoStaticsList(staticVars *htable[*VarVal]) *Obj {
	listObjPtr := i.NewListObj(nil)
	for _, e := range staticVars.entries() {
		i.infoVariableMatch(listObjPtr, e.key, e.val, nil, infoVarListLocals|infoVarListValues)
	}
	return listObjPtr
}

// infoVariableMatch appends name, and with infoVarListValues also its value, to
// listObjPtr if the variable matches pattern (C: JimVariablesMatch).
func (i *Interp) infoVariableMatch(listObjPtr *Obj, name string, vv *VarVal, pattern *Obj, mode int) {
	if mode&infoVarListLocals != 0 && vv.LinkFrame != nil {
		// [info locals] ignores variables linked from another frame.
		return
	}
	nameObj := i.NewStringObj(name)
	if pattern != nil && !i.StringMatchObj(pattern, nameObj, false) {
		return
	}
	i.ListAppendElement(listObjPtr, nameObj)
	if mode&infoVarListValues != 0 {
		i.ListAppendElement(listObjPtr, vv.Obj)
	}
}

// infoLevel returns the command words of one call frame (C: JimInfoLevel).
func (i *Interp) infoLevel(levelObjPtr *Obj) (*Obj, int) {
	if level, rc := i.GetWide(levelObjPtr); rc == OK {
		if targetCallFrame := i.callFrameByInteger(int(level)); targetCallFrame != nil && targetCallFrame != i.topFramePtr {
			return i.NewListObj(targetCallFrame.argv), OK
		}
	}
	return nil, i.Errorf("bad level \"%#s\"", levelObjPtr)
}

// infoFrame returns a dictionary describing one eval frame (C: JimInfoFrame).
func (i *Interp) infoFrame(levelObjPtr *Obj) (*Obj, int) {
	if level, rc := i.GetWide(levelObjPtr); rc == OK {
		if frame := i.getEvalFrameByProcLevel(int(level)); frame != nil {
			listObj := i.NewListObj(nil)

			i.ListAppendElement(listObj, i.NewStringObj("type"))
			i.ListAppendElement(listObj, i.NewStringObj("source"))
			if frame.scriptObj != nil {
				script := i.getScript(frame.scriptObj)
				i.ListAppendElement(listObj, i.NewStringObj("line"))
				i.ListAppendElement(listObj, i.NewIntObj(int64(script.lineNr)))
				i.ListAppendElement(listObj, i.NewStringObj("file"))
				i.ListAppendElement(listObj, script.fileName)
			}
			i.ListAppendElement(listObj, i.NewStringObj("cmd"))
			i.ListAppendElement(listObj, i.NewListObj(frame.argv))
			if procNameObj := i.procForEvalFrame(frame); procNameObj != nil {
				i.ListAppendElement(listObj, i.NewStringObj("proc"))
				i.ListAppendElement(listObj, procNameObj)
			}
			i.ListAppendElement(listObj, i.NewStringObj("level"))
			i.ListAppendElement(listObj, i.NewIntObj(int64(i.framePtr.level-frame.framePtr.level)))

			return listObj, OK
		}
	}
	return nil, i.Errorf("bad level \"%#s\"", levelObjPtr)
}
