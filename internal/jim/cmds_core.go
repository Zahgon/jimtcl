/* Jim - A small embeddable Tcl interpreter: the core control-flow commands.
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

// coreCmd describes one entry of the core commands table.
type coreCmd struct {
	name    string
	proc    CmdProc
	minArgs int
	maxArgs int
	usage   string
}

// coreCommandsTable lists the core commands implemented here, in the same
// order as the C implementation's Jim_CoreCommandsTable. Every core command
// has a usage string.
var coreCommandsTable = []coreCmd{
	{"alias", aliasCoreCommand, 2, -1, "newname command ?args ...?"},
	{"append", appendCoreCommand, 1, -1, "varName ?value ...?"},
	{"apply", applyCoreCommand, 1, -1, "lambdaExpr ?arg ...?"},
	{"break", breakCoreCommand, 0, 1, "?level?"},
	{"catch", catchCoreCommand, 1, -1, "?-?no?code ... --? script ?resultVarName? ?optionVarName?"},
	{"continue", continueCoreCommand, 0, 1, "?level?"},
	{"error", errorCoreCommand, 1, 2, "message ?stacktrace?"},
	{"eval", evalCoreCommand, 1, -1, "arg ?arg ...?"},
	{"exists", existsCoreCommand, 1, 2, "?-command|-proc|-alias|-channel|-var? name"},
	{"exit", exitCoreCommand, 0, 1, "?exitCode?"},
	{"for", forCoreCommand, 4, 4, "start test next body"},
	{"foreach", foreachCoreCommand, 3, -1, "varList list ?varList list ...? script"},
	{"global", globalCoreCommand, 1, -1, "varName ?varName ...?"},
	{"if", ifCoreCommand, 2, -1, "condition ?then? trueBody ?elseif ...? ?else? ?falseBody?"},
	{"incr", incrCoreCommand, 1, 2, "varName ?increment?"},
	{"lmap", lmapCoreCommand, 3, -1, "varList list ?varList list ...? script"},
	{"local", localCoreCommand, 1, -1, "cmd ?args ...?"},
	{"loop", loopCoreCommand, 3, 5, "var ?first? limit ?incr? body"},
	{"lsubst", lsubstCoreCommand, 1, 2, "?-line? string"},
	{"proc", procCoreCommand, 3, 4, "name arglist ?statics? body"},
	{"rename", renameCoreCommand, 2, 2, "oldName newName"},
	{"return", returnCoreCommand, 0, -1, "?-code code? ?-errorinfo stacktrace? ?-level level? ?result?"},
	{"set", setCoreCommand, 1, 2, "varName ?newValue?"},
	{"source", sourceCoreCommand, 1, 1, "fileName"},
	{"subst", substCoreCommand, 1, 4, "?options? string"},
	{"switch", switchCoreCommand, 2, -1, "?options? string pattern body ... ?default body? or pattern body ?pattern body ...?"},
	{"tailcall", tailcallCoreCommand, 0, -1, "?cmd arg ...?"},
	{"try", tryCoreCommand, 1, -1, "?-?no?code ... --? script ?on|trap codes vars script? ... ?finally script?"},
	{"unset", unsetCoreCommand, 0, -1, "?-nocomplain? ?--? ?varName ...?"},
	{"upcall", upcallCoreCommand, 1, -1, "cmd ?args ...?"},
	{"uplevel", uplevelCoreCommand, 1, -1, "?level? command ?arg ...?"},
	{"upvar", upvarCoreCommand, 2, -1, "?level? otherVar myVar ?otherVar myVar ...?"},
	{"while", whileCoreCommand, 2, 2, "condition body"},
}

// registerCoreCommands registers the commands implemented in the core.
func (i *Interp) registerCoreCommands() {
	for n := range coreCommandsTable {
		c := &coreCommandsTable[n]
		i.RegisterCmd(c.name, c.usage, c.minArgs, c.maxArgs, c.proc, 0)
	}
}

/* ---------------------------------------------------------------------------
 * [set] and [unset]
 * ------------------------------------------------------------------------ */

// setCoreCommand implements [set].
func setCoreCommand(i *Interp, argv []*Obj) int {
	if len(argv) == 2 {
		objPtr := i.GetVariable(argv[1], ErrMsg)
		if objPtr == nil {
			return ERR
		}
		i.SetResult(objPtr)
		return OK
	}
	/* argc == 3 case. */
	if i.SetVariable(argv[1], argv[2]) != OK {
		return ERR
	}
	i.SetResult(argv[2])
	return OK
}

// unsetCoreCommand implements [unset ?-nocomplain? ?--? ?varName ...?].
func unsetCoreCommand(i *Interp, argv []*Obj) int {
	n := 1
	complain := true

	for n < len(argv) {
		if argv[n].String() == "--" {
			n++
			break
		}
		if argv[n].String() == "-nocomplain" {
			complain = false
			n++
			continue
		}
		break
	}

	flags := None
	if complain {
		flags = ErrMsg
	}
	for ; n < len(argv); n++ {
		if i.UnsetVariable(argv[n], flags) != OK && complain {
			return ERR
		}
	}

	i.SetResult(i.emptyObj)
	return OK
}

// appendCoreCommand implements [append].
func appendCoreCommand(i *Interp, argv []*Obj) int {
	var stringObjPtr *Obj

	if len(argv) == 2 {
		stringObjPtr = i.GetVariable(argv[1], ErrMsg)
		if stringObjPtr == nil {
			return ERR
		}
	} else {
		stringObjPtr = i.GetVariable(argv[1], Unshared)
		if stringObjPtr == nil {
			/* Create the string if it doesn't exist */
			stringObjPtr = i.NewStringObj("")
		} else {
			/* Objects are values, so build a new one to modify */
			stringObjPtr = stringObjPtr.dup()
		}
		for n := 2; n < len(argv); n++ {
			i.AppendObj(stringObjPtr, argv[n])
		}
		if i.SetVariable(argv[1], stringObjPtr) != OK {
			return ERR
		}
	}
	i.SetResult(stringObjPtr)
	return OK
}

// incrCoreCommand implements [incr].
func incrCoreCommand(i *Interp, argv []*Obj) int {
	increment := int64(1)

	if len(argv) == 3 {
		var rc int
		if increment, rc = i.GetWideExpr(argv[2]); rc != OK {
			return ERR
		}
	}
	wideValue := int64(0)
	intObjPtr := i.GetVariable(argv[1], Unshared)
	if intObjPtr != nil {
		var rc int
		if wideValue, rc = i.GetWide(intObjPtr); rc != OK {
			return ERR
		}
	}
	intObjPtr = i.NewIntObj(wideValue + increment)
	if i.SetVariable(argv[1], intObjPtr) != OK {
		return ERR
	}
	i.SetResult(intObjPtr)
	return OK
}

/* ---------------------------------------------------------------------------
 * Loops
 * ------------------------------------------------------------------------ */

// checkLoopRetcode is used by every command that supports break and continue
// from a loop (while, loop, foreach, for) to honour [break level].
//
// If breakLevel is > 0 it is decremented and true is returned, meaning that
// the completion code must be passed further up.
func (i *Interp) checkLoopRetcode(retval int) bool {
	if retval == BREAK || retval == CONTINUE {
		i.breakLevel--
		if i.breakLevel > 0 {
			return true
		}
	}
	return false
}

// whileCoreCommand implements [while].
func whileCoreCommand(i *Interp, argv []*Obj) int {
	for {
		boolean, retval := i.ExprBool(argv[1])
		if retval != OK {
			return retval
		}
		if !boolean {
			break
		}

		if retval = i.EvalObj(argv[2]); retval != OK {
			if i.checkLoopRetcode(retval) {
				return retval
			}
			switch retval {
			case BREAK:
				i.SetResult(i.emptyObj)
				return OK
			case CONTINUE:
				continue
			default:
				return retval
			}
		}
	}
	i.SetResult(i.emptyObj)
	return OK
}

// forCoreCommand implements [for].
func forCoreCommand(i *Interp, argv []*Obj) int {
	immediate := false

	/* Do the initialisation */
	if retval := i.EvalObj(argv[1]); retval != OK {
		return retval
	}

	/* And do the first test now; it is better for optimisation if we can do
	 * next/test at the bottom of the loop */
	boolean, retval := i.ExprBool(argv[2])

	for boolean && (retval == OK || retval == CONTINUE) {
		/* Body */
		retval = i.EvalObj(argv[4])
		if i.checkLoopRetcode(retval) {
			immediate = true
			break
		}
		if retval == OK || retval == CONTINUE {
			/* increment */
			retval = i.EvalObj(argv[3])
			if retval == OK || retval == CONTINUE {
				/* test */
				boolean, retval = i.ExprBool(argv[2])
			}
		}
	}

	if !immediate {
		if retval == CONTINUE || retval == BREAK || retval == OK {
			i.SetResult(i.emptyObj)
			return OK
		}
	}

	return retval
}

// loopCoreCommand implements [loop].
func loopCoreCommand(i *Interp, argv []*Obj) int {
	argc := len(argv)
	limit := int64(0)
	incr := int64(1)

	n, retval := i.GetWideExpr(argv[2])
	if argc > 4 && retval == OK {
		limit, retval = i.GetWideExpr(argv[3])
	}
	if argc > 5 && retval == OK {
		incr, _ = i.GetWideExpr(argv[4])
	}
	if retval != OK {
		return retval
	}
	if argc == 4 {
		limit = n
		n = 0
	}
	bodyObjPtr := argv[argc-1]

	retval = i.SetVariable(argv[1], i.NewIntObj(n))

	for ((n < limit && incr > 0) || (n > limit && incr < 0)) && retval == OK {
		retval = i.EvalObj(bodyObjPtr)
		if i.checkLoopRetcode(retval) {
			return retval
		}
		if retval == OK || retval == CONTINUE {
			/* The C looks the variable up but ignores a failure: a body that
			 * unsets the loop variable simply gets it recreated. */
			i.GetVariable(argv[1], ErrMsg)
			retval = OK

			/* Increment */
			n += incr
			retval = i.SetVariable(argv[1], i.NewIntObj(n))
		}
	}

	if retval == OK || retval == CONTINUE || retval == BREAK {
		i.SetResult(i.emptyObj)
		return OK
	}
	return retval
}

// listIter makes it easy to iterate over a list.
type listIter struct {
	objPtr *Obj
	idx    int
}

// next returns the next object from the list, or nil at the end.
func (i *Interp) iterNext(iter *listIter) *Obj {
	if iter.idx >= i.ListLength(iter.objPtr) {
		return nil
	}
	o := i.ListGetIndex(iter.objPtr, iter.idx)
	iter.idx++
	return o
}

// iterDone reports whether the end of the list has been reached.
func (i *Interp) iterDone(iter *listIter) bool {
	return iter.idx >= i.ListLength(iter.objPtr)
}

// foreachMapHelper implements both [foreach] and [lmap].
func foreachMapHelper(i *Interp, argv []*Obj, doMap bool) int {
	argc := len(argv)
	result := OK

	if argc%2 != 0 {
		return Usage
	}
	script := argv[argc-1]  /* Last argument is a script */
	numargs := argc - 1 - 1 /* argc - 'foreach' - script */

	iters := make([]listIter, numargs)
	for n := 0; n < numargs; n++ {
		iters[n] = listIter{objPtr: argv[n+1]}
		if n%2 == 0 && i.iterDone(&iters[n]) {
			result = ERR
		}
	}
	if result != OK {
		i.SetResultString("foreach varlist is empty")
		return result
	}

	var resultObj *Obj
	if doMap {
		resultObj = i.NewListObj(nil)
	} else {
		resultObj = i.emptyObj
	}

	for {
		/* Have we expired all lists? */
		n := 0
		for ; n < numargs; n += 2 {
			if !i.iterDone(&iters[n+1]) {
				break
			}
		}
		if n == numargs {
			/* All done */
			break
		}

		/* For each list */
		for n := 0; n < numargs; n += 2 {
			/* foreach var */
			iters[n].idx = 0
			for {
				varName := i.iterNext(&iters[n])
				if varName == nil {
					break
				}
				valObj := i.iterNext(&iters[n+1])
				if valObj == nil {
					/* Ran out, so store the empty string */
					valObj = i.emptyObj
				}
				if result = i.SetVariable(varName, valObj); result != OK {
					return result
				}
			}
		}
		result = i.EvalObj(script)
		if i.checkLoopRetcode(result) {
			return result
		}
		switch result {
		case OK:
			if doMap {
				i.ListAppendElement(resultObj, i.result)
			}
		case CONTINUE:
		case BREAK:
			i.SetResult(resultObj)
			return OK
		default:
			return result
		}
	}
	i.SetResult(resultObj)
	return OK
}

// foreachCoreCommand implements [foreach].
func foreachCoreCommand(i *Interp, argv []*Obj) int {
	return foreachMapHelper(i, argv, false)
}

// lmapCoreCommand implements [lmap].
func lmapCoreCommand(i *Interp, argv []*Obj) int {
	return foreachMapHelper(i, argv, true)
}

/* ---------------------------------------------------------------------------
 * [if] and [switch]
 * ------------------------------------------------------------------------ */

// ifCoreCommand implements [if].
func ifCoreCommand(i *Interp, argv []*Obj) int {
	argc := len(argv)
	current := 1
	falsebody := 0

	for {
		/* not enough arguments given! */
		if current >= argc {
			return Usage
		}
		boolean, retval := i.ExprBool(argv[current])
		current++
		if retval != OK {
			return retval
		}
		/* There lacks something, isn't it? */
		if current >= argc {
			return Usage
		}
		if argv[current].String() == "then" {
			current++
		}
		/* Tsk tsk, no then-clause? */
		if current >= argc {
			return Usage
		}
		if boolean {
			return i.EvalObj(argv[current])
		}
		/* Ok: no else-clause follows */
		current++
		if current >= argc {
			i.SetResult(i.NewStringObj(""))
			return OK
		}
		falsebody = current
		current++
		if argv[falsebody].String() == "else" {
			/* IIICKS - else-clause isn't last cmd? */
			if current != argc-1 {
				return Usage
			}
			return i.EvalObj(argv[current])
		} else if argv[falsebody].String() == "elseif" {
			/* Ok: elseif follows, meaning all the stuff again */
			continue
		} else if falsebody != argc-1 {
			/* OOPS - else-clause is not last cmd? */
			return Usage
		}
		return i.EvalObj(argv[falsebody])
	}
}

// commandMatchObj invokes a match command with the supplied pattern and
// candidate string, as
//
//	command ?-nocase? ?--? pattern string
//
// It returns 1 for a match, 0 for no match, or the negated Jim return code if
// the command itself fails or returns a non-integer result.
func (i *Interp) commandMatchObj(commandObj, patternObj, stringObj *Obj, flags int) int {
	parms := []*Obj{commandObj}
	if flags&NoCase != 0 {
		parms = append(parms, i.NewStringObj("-nocase"))
	}
	if flags&OptEnd != 0 {
		parms = append(parms, i.NewStringObj("--"))
	}
	parms = append(parms, patternObj, stringObj)

	rc := i.EvalObjVector(parms)

	// The C evaluates Jim_GetLong() only when the command succeeded, so a
	// failing command keeps its own error message in the result.
	if rc == OK {
		if eq, grc := i.GetWide(i.Result()); grc == OK {
			return int(eq)
		}
	}
	return -rc
}

// switchCoreCommand implements [switch].
func switchCoreCommand(i *Interp, argv []*Obj) int {
	const (
		switchExact = iota
		switchGlob
		switchRE
		switchCmd
	)
	argc := len(argv)
	matchOpt := switchExact
	matchFlags := 0
	var command, scriptObj *Obj

	opt := 1
	for ; opt < argc; opt++ {
		option := argv[opt].String()

		/* Note that the C compares only the first two characters of the
		 * option, so "-explode" is accepted as "-exact". */
		if !strings.HasPrefix(option, "-") {
			break
		} else if strings.HasPrefix(option, "--") {
			opt++
			break
		} else if strings.HasPrefix(option, "-e") {
			matchOpt = switchExact
		} else if strings.HasPrefix(option, "-g") {
			matchOpt = switchGlob
		} else if strings.HasPrefix(option, "-r") {
			matchOpt = switchRE
			matchFlags |= OptEnd
		} else if strings.HasPrefix(option, "-c") {
			matchOpt = switchCmd
			if argc-opt < 2 {
				return Usage
			}
			opt++
			command = argv[opt]
		} else {
			return i.Errorf("bad option \"%#s\": must be -exact, -glob, -regexp, -command procname or --", argv[opt])
		}
		if argc-opt < 2 {
			return Usage
		}
	}
	strObj := argv[opt]
	opt++
	singleList := argc-opt == 1
	caseList := argv[opt:]
	if singleList {
		caseList = i.ListElements(argv[opt])
	}
	if len(caseList) == 0 || len(caseList)%2 != 0 {
		return Usage
	}
	n := 0
	for ; scriptObj == nil && n < len(caseList); n += 2 {
		patObj := caseList[n]

		if patObj.String() != "default" || n < len(caseList)-2 {
			switch matchOpt {
			case switchExact:
				if i.StringEqObj(strObj, patObj) {
					scriptObj = caseList[n+1]
				}
			case switchGlob:
				if i.StringMatchObj(patObj, strObj, false) {
					scriptObj = caseList[n+1]
				}
			case switchRE, switchCmd:
				if matchOpt == switchRE {
					command = i.NewStringObj("regexp")
				}
				rc := i.commandMatchObj(command, patObj, strObj, matchFlags)

				/* After the execution of a command we need to make sure to
				 * reconvert the object into a list again. Only for the
				 * single-list style [switch]. */
				if singleList {
					caseList = i.ListElements(argv[opt])
				}
				if rc < 0 {
					return -rc
				}
				if rc != 0 {
					scriptObj = caseList[n+1]
				}
			}
		} else {
			scriptObj = caseList[n+1]
		}
	}
	for ; n < len(caseList) && scriptObj != nil && scriptObj.String() == "-"; n += 2 {
		scriptObj = caseList[n+1]
	}
	if scriptObj != nil && scriptObj.String() == "-" {
		return i.Errorf("no body specified for pattern \"%#s\"", caseList[n-2])
	}
	i.SetResult(i.emptyObj)
	if scriptObj != nil {
		return i.EvalObj(scriptObj)
	}
	return OK
}

/* ---------------------------------------------------------------------------
 * [eval], [uplevel] and friends
 * ------------------------------------------------------------------------ */

// evalCoreCommand implements [eval].
func evalCoreCommand(i *Interp, argv []*Obj) int {
	if len(argv) == 2 {
		return i.EvalObj(argv[1])
	}
	return i.EvalObj(i.ConcatObj(argv[1:]))
}

// uplevelCoreCommand implements [uplevel].
func uplevelCoreCommand(i *Interp, argv []*Obj) int {
	/* Save the old callframe pointer */
	savedCallFrame := i.framePtr

	/* Lookup the target frame pointer */
	var targetCallFrame *CallFrame
	str := argv[1].String()
	if (str != "" && str[0] >= '0' && str[0] <= '9') || strings.HasPrefix(str, "#") {
		targetCallFrame = i.CallFrameByLevel(argv[1])
		argv = argv[1:]
	} else {
		targetCallFrame = i.CallFrameByLevel(nil)
	}
	if targetCallFrame == nil {
		return ERR
	}
	if len(argv) < 2 {
		return Usage
	}
	/* Eval the code in the target callframe. */
	i.framePtr = targetCallFrame
	var retcode int
	if len(argv) == 2 {
		retcode = i.EvalObj(argv[1])
	} else {
		retcode = i.EvalObj(i.ConcatObj(argv[1:]))
	}
	i.framePtr = savedCallFrame
	return retcode
}

// breakContinueHelper is the shared implementation of [break] and [continue].
func breakContinueHelper(i *Interp, argv []*Obj, retcode int) int {
	if len(argv) == 2 {
		level, ret := i.GetWide(argv[1])
		if ret != OK {
			return ret
		}
		i.breakLevel = int(level)
	}
	return retcode
}

// breakCoreCommand implements [break].
func breakCoreCommand(i *Interp, argv []*Obj) int {
	return breakContinueHelper(i, argv, BREAK)
}

// continueCoreCommand implements [continue].
func continueCoreCommand(i *Interp, argv []*Obj) int {
	return breakContinueHelper(i, argv, CONTINUE)
}

// returnCoreCommand implements [return].
func returnCoreCommand(i *Interp, argv []*Obj) int {
	argc := len(argv)
	var stackTraceObj, errorCodeObj *Obj
	returnCode := OK
	level := int64(1)

	n := 1
options:
	for ; n < argc-1; n += 2 {
		switch argv[n].String() {
		case "-code":
			var rc int
			if returnCode, rc = i.GetReturnCode(argv[n+1]); rc == ERR {
				return ERR
			}
		case "-errorinfo":
			stackTraceObj = argv[n+1]
		case "-errorcode":
			errorCodeObj = argv[n+1]
		case "-level":
			var rc int
			if level, rc = i.GetWide(argv[n+1]); rc != OK || level < 0 {
				return i.Errorf("bad level \"%#s\"", argv[n+1])
			}
		default:
			break options
		}
	}

	if n != argc-1 && n != argc {
		return Usage
	}

	/* If a stack trace is supplied and the code is error, set the stack trace */
	if stackTraceObj != nil && returnCode == ERR {
		i.setStackTrace(stackTraceObj)
	}
	/* If an error code list is supplied, set the global $errorCode */
	if errorCodeObj != nil && returnCode == ERR {
		i.SetGlobalVariableStr("errorCode", errorCodeObj)
	}
	i.returnCode = returnCode
	i.returnLevel = int(level)

	if n == argc-1 {
		i.SetResult(argv[n])
	}
	if level == 0 {
		return returnCode
	}
	return RETURN
}

// tailcallCoreCommand implements [tailcall].
func tailcallCoreCommand(i *Interp, argv []*Obj) int {
	if i.framePtr.level == 0 {
		i.SetResultString("tailcall can only be called from a proc or lambda")
		return ERR
	} else if len(argv) >= 2 {
		/* Need to resolve the tailcall command in the current context */
		cf := i.framePtr.parent

		cmdPtr := i.GetCommand(argv[1], ErrMsg)
		if cmdPtr == nil {
			return ERR
		}

		/* Stash this pre-resolved command and the command list. When the
		 * stack unwinds to the previous proc they will be evaluated. */
		cf.tailcallCmd = cmdPtr
		cf.tailcallObj = i.NewListObj(argv[1:])

		return EVAL
	}
	return OK
}

// aliasCmd invokes an alias by prepending its stored command prefix.
func aliasCmd(i *Interp, argv []*Obj) int {
	prefixListObj := i.CmdPrivData().(*Obj)

	/* prefixListObj is a list to which the args need to be appended */
	cmdList := prefixListObj.dup()
	i.ListInsertElements(cmdList, i.ListLength(cmdList), argv[1:])

	return i.evalObjList(cmdList)
}

// aliasCoreCommand implements [alias], which creates command-prefix aliases.
func aliasCoreCommand(i *Interp, argv []*Obj) int {
	prefixListObj := i.NewListObj(argv[2:])
	i.SetResult(argv[1])

	i.RegisterCommand(argv[1], aliasCmd, "", "", 0, -1, CmdIsAlias, prefixListObj)
	return OK
}

// procCoreCommand implements [proc].
func procCoreCommand(i *Interp, argv []*Obj) int {
	var cmd *Cmd

	if len(argv) == 4 {
		cmd = i.createProcedureCmd(argv[2], nil, argv[3], nil)
	} else {
		cmd = i.createProcedureCmd(argv[2], argv[3], argv[4], nil)
	}

	if cmd == nil {
		return ERR
	}

	/* Add the new command */
	nameObjPtr := i.qualifyName(argv[1])
	i.createCommand(nameObjPtr, cmd)

	/* Calculate and set the namespace for this proc */
	i.updateProcNamespace(cmd, nameObjPtr)

	/* Unlike Tcl, set the name of the proc as the result */
	i.SetResult(argv[1])
	return OK
}

// localCoreCommand implements [local].
func localCoreCommand(i *Interp, argv []*Obj) int {
	/* Evaluate the arguments with 'local' in force */
	i.local++
	retcode := i.EvalObjVector(argv[1:])
	i.local--

	/* If OK, and the result is a proc, add it to the list of local procs */
	if retcode == OK {
		cmdNameObj := i.Result()

		if i.GetCommand(cmdNameObj, ErrMsg) == nil {
			return ERR
		}
		i.framePtr.localCommands = append(i.framePtr.localCommands, cmdNameObj)
	}

	return retcode
}

// upcallCoreCommand implements [upcall].
func upcallCoreCommand(i *Interp, argv []*Obj) int {
	cmdPtr := i.GetCommand(argv[1], ErrMsg)
	if cmdPtr == nil || cmdPtr.flags&CmdIsProc == 0 || cmdPtr.prevCmd == nil {
		return i.Errorf("no previous command: \"%#s\"", argv[1])
	}
	/* Mark this command as being in an upcall */
	cmdPtr.upcall = true

	/* Invoke the command as normal */
	retcode := i.EvalObjVector(argv[1:])

	/* No longer in an upcall */
	cmdPtr.upcall = false

	return retcode
}

// applyCoreCommand implements [apply].
func applyCoreCommand(i *Interp, argv []*Obj) int {
	var nsObj *Obj

	length := i.ListLength(argv[1])
	if length != 2 && length != 3 {
		return i.Errorf("can't interpret \"%#s\" as a lambda expression", argv[1])
	}

	if length == 3 {
		/* Note that the namespace is always treated as global */
		nsObj = i.ListGetIndex(argv[1], 2)
	}
	argListObjPtr := i.ListGetIndex(argv[1], 0)
	bodyObjPtr := i.ListGetIndex(argv[1], 1)

	cmd := i.createProcedureCmd(argListObjPtr, nil, bodyObjPtr, nsObj)
	if cmd == nil {
		return ERR
	}

	/* Create a new argv array with a dummy argv[0], for error messages */
	nargv := make([]*Obj, 0, len(argv)-1)
	nargv = append(nargv, i.NewStringObj("apply lambdaExpr"))
	nargv = append(nargv, argv[2:]...)
	return i.callProcedure(cmd, nargv)
}

// upvarCoreCommand implements [upvar].
func upvarCoreCommand(i *Interp, argv []*Obj) int {
	/* Lookup the target frame pointer */
	var targetCallFrame *CallFrame
	if len(argv) > 3 && len(argv)%2 == 0 {
		targetCallFrame = i.CallFrameByLevel(argv[1])
		argv = argv[1:]
	} else {
		targetCallFrame = i.CallFrameByLevel(nil)
	}
	if targetCallFrame == nil {
		return ERR
	}

	/* Check for arity */
	if len(argv) < 3 {
		return Usage
	}

	/* Now... for every other/local couple: */
	for n := 1; n < len(argv); n += 2 {
		if i.SetVariableLink(argv[n+1], argv[n], targetCallFrame) != OK {
			return ERR
		}
	}
	return OK
}

// globalCoreCommand implements [global].
func globalCoreCommand(i *Interp, argv []*Obj) int {
	/* Link every var to the toplevel having the same name */
	if i.framePtr.level == 0 {
		return OK /* global at toplevel... */
	}
	for n := 1; n < len(argv); n++ {
		/* global ::blah does nothing */
		if !isGlobalName(argv[n].String()) {
			if i.SetVariableLink(argv[n], argv[n], i.topFramePtr) != OK {
				return ERR
			}
		}
	}
	return OK
}

/* ---------------------------------------------------------------------------
 * [exit], [catch], [try] and [error]
 * ------------------------------------------------------------------------ */

// exitCoreCommand implements [exit].
func exitCoreCommand(i *Interp, argv []*Obj) int {
	exitCode := int64(0)

	if len(argv) == 2 {
		var rc int
		if exitCode, rc = i.GetWide(argv[1]); rc != OK {
			return ERR
		}
		i.SetResult(argv[1])
	}
	i.exitCode = int(exitCode)
	return EXIT
}

// matchReturnCodes checks whether a return code matches one of the requested
// codes, returning OK on a match, -1 when there is none and ERR when the list
// contains something that is not a return code.
func (i *Interp) matchReturnCodes(retcodeListObj *Obj, rc int) int {
	length := i.ListLength(retcodeListObj)
	for n := 0; n < length; n++ {
		returncode, ret := i.GetReturnCode(i.ListGetIndex(retcodeListObj, n))
		if ret != OK {
			return ERR
		}
		if rc == returncode {
			return OK
		}
	}
	return -1
}

// findReturnCodeByName returns the code whose name is arg, or -1.
func findReturnCodeByName(arg string) int {
	for code := 0; code < 8; code++ {
		if ReturnCodeName(code) == arg {
			return code
		}
	}
	return -1
}

// catchTryHelper implements both [try] and [catch].
func catchTryHelper(i *Interp, istry bool, argv []*Obj) int {
	argc := len(argv)
	var finallyScriptObj, msgVarObj, optsVarObj, handlerScriptObj *Obj

	/* Which return codes are ignored (passed through)? By default, only
	 * exit, eval and signal */
	ignoreMask := int64((1 << EXIT) | (1 << EVAL) | (1 << SIGNAL))
	const maxIgnoreCode = 64

	/* Reset the error code before catch/try. Note that this is not strictly
	 * correct. */
	i.SetGlobalVariableStr("errorCode", i.NewStringObj("NONE"))

	n := 1
	for ; n < argc-1; n++ {
		arg := argv[n].String()

		/* It's a pity we can't use GetEnum here :-( */
		if arg == "--" {
			n++
			break
		}
		if !strings.HasPrefix(arg, "-") {
			break
		}

		ignore := false
		if strings.HasPrefix(arg, "-no") {
			arg = arg[3:]
			ignore = true
		} else {
			arg = arg[1:]
		}

		option, ok, _ := stringToWide(arg, 10)
		if !ok {
			option = -1
		}
		if option < 0 {
			option = int64(findReturnCodeByName(arg))
		}
		if option < 0 {
			return Usage
		}

		if ignore {
			ignoreMask |= 1 << option
		} else {
			ignoreMask &= ^(1 << option)
		}
	}

	idx := n

	if argc-idx < 1 {
		return Usage
	}

	sig := 0
	if ignoreMask&(1<<SIGNAL) == 0 {
		sig++
	}

	i.signalLevel += sig
	exitCode := i.EvalObj(argv[idx])
	/* Once caught, a new error will set a stack trace again */
	i.hasErrorStackTrace = false
	i.signalLevel -= sig

	errorCodeObj := i.GetGlobalVariableStr("errorCode", None)

	/* For try we need to find both a matching return or trap code, and
	 * finally, if they exist. */
	idx++
	if istry {
		for idx < argc {
			tryOptions := []string{"on", "trap", "finally"}
			const (
				tryOn = iota
				tryTrap
				tryFinally
			)

			option, rc := i.GetEnum(argv[idx], tryOptions, "handler", ErrMsg)
			if rc != OK {
				return ERR
			}
			switch option {
			case tryOn, tryTrap:
				if idx+4 > argc {
					return Usage
				}
				ret := -1
				if option == tryOn {
					ret = i.matchReturnCodes(argv[idx+1], exitCode)
					if ret > OK {
						return Usage
					}
				} else if errorCodeObj != nil {
					length := i.ListLength(argv[idx+1])

					if length > i.ListLength(errorCodeObj) {
						/* More elements in the sublist than in the errorCode
						 * so we can't match */
						ret = -1
					} else {
						/* Try to match the sublist against errorCode */
						ret = OK
						for n := 0; n < length; n++ {
							matchObj := i.ListGetIndex(argv[idx+1], n)
							objPtr := i.ListGetIndex(errorCodeObj, n)
							if i.StringCompareObj(matchObj, objPtr, false) != 0 {
								ret = -1
								break
							}
						}
					}
				}
				/* Save the details of the first match */
				if ret == OK && handlerScriptObj == nil {
					msgVarObj = i.ListGetIndex(argv[idx+2], 0)
					optsVarObj = i.ListGetIndex(argv[idx+2], 1)
					handlerScriptObj = argv[idx+3]
				}
				idx += 4
			case tryFinally:
				if idx+2 != argc {
					return Usage
				}
				finallyScriptObj = argv[idx+1]
				idx += 2
			}
		}
	} else if argc-idx >= 1 {
		msgVarObj = argv[idx]
		idx++
		if argc-idx >= 1 {
			optsVarObj = argv[idx]
			idx++
		}
	}

	/* Catch or pass through? Only the first 64 codes can be passed through */
	if exitCode >= 0 && exitCode < maxIgnoreCode && (1<<exitCode)&ignoreMask != 0 {
		/* Not caught, pass it up */
		if finallyScriptObj != nil {
			i.EvalObj(finallyScriptObj)
		}
		return exitCode
	}

	ok := true
	if msgVarObj != nil && msgVarObj.Len() != 0 {
		if i.SetVariable(msgVarObj, i.Result()) != OK {
			ok = false
		}
	}
	if ok && optsVarObj != nil && optsVarObj.Len() != 0 {
		optListObj := i.NewListObj(nil)

		i.ListAppendElement(optListObj, i.NewStringObj("-code"))
		code := exitCode
		if exitCode == RETURN {
			code = i.returnCode
		}
		i.ListAppendElement(optListObj, i.NewIntObj(int64(code)))
		i.ListAppendElement(optListObj, i.NewStringObj("-level"))
		i.ListAppendElement(optListObj, i.NewIntObj(int64(i.returnLevel)))
		if exitCode == ERR {
			i.ListAppendElement(optListObj, i.NewStringObj("-errorinfo"))
			i.ListAppendElement(optListObj, i.stackTrace)

			if errorCodeObj != nil {
				i.ListAppendElement(optListObj, i.NewStringObj("-errorcode"))
				i.ListAppendElement(optListObj, errorCodeObj)
			}
		}
		if i.SetVariable(optsVarObj, optListObj) != OK {
			ok = false
		}
	}
	if ok && handlerScriptObj != nil {
		/* Execute the on script. Any return code replaces the original. */
		exitCode = i.EvalObj(handlerScriptObj)
	}

	if finallyScriptObj != nil {
		/* Execute the finally script. If OK, restore the previous result and
		 * exit code */
		prevResultObj := i.Result()
		ret := i.EvalObj(finallyScriptObj)
		if ret == OK {
			i.SetResult(prevResultObj)
		} else {
			exitCode = ret
		}
	}
	if !istry {
		i.SetResultInt(int64(exitCode))
		exitCode = OK
	}
	return exitCode
}

// catchCoreCommand implements [catch].
func catchCoreCommand(i *Interp, argv []*Obj) int {
	return catchTryHelper(i, false, argv)
}

// tryCoreCommand implements [try].
func tryCoreCommand(i *Interp, argv []*Obj) int {
	return catchTryHelper(i, true, argv)
}

// renameCoreCommand implements [rename].
func renameCoreCommand(i *Interp, argv []*Obj) int {
	return i.RenameCommand(argv[1], argv[2])
}

// substCoreCommand implements [subst].
func substCoreCommand(i *Interp, argv []*Obj) int {
	options := []string{"-nobackslashes", "-nocommands", "-novariables"}
	const (
		optNoBackslashes = iota
		optNoCommands
		optNoVariables
	)
	flags := SubstFlag

	for n := 1; n < len(argv)-1; n++ {
		option, rc := i.GetEnum(argv[n], options, "", ErrMsg|EnumAbbrev)
		if rc != OK {
			return ERR
		}
		switch option {
		case optNoBackslashes:
			flags |= SubstNoEsc
		case optNoCommands:
			flags |= SubstNoCmd
		case optNoVariables:
			flags |= SubstNoVar
		}
	}
	objPtr, rc := i.SubstObj(argv[len(argv)-1], flags)
	if rc != OK {
		return ERR
	}
	i.SetResult(objPtr)
	return OK
}

// lsubstCoreCommand implements [lsubst].
func lsubstCoreCommand(i *Interp, argv []*Obj) int {
	if len(argv) == 3 {
		if argv[1].String() == "-line" {
			return i.listSubstObj(argv[2], lsubstLine)
		}
		return Usage
	}
	return i.listSubstObj(argv[1], 0)
}

// existsCoreCommand implements [exists].
func existsCoreCommand(i *Interp, argv []*Obj) int {
	options := []string{"-command", "-proc", "-alias", "-channel", "-var"}
	const (
		optCommand = iota
		optProc
		optAlias
		optChannel
		optVar
	)
	var objPtr *Obj
	var option int
	result := false

	if len(argv) == 2 {
		option = optVar
		objPtr = argv[1]
	} else {
		var rc int
		if option, rc = i.GetEnum(argv[1], options, "", ErrMsg|EnumAbbrev); rc != OK {
			return ERR
		}
		objPtr = argv[2]
	}

	if option == optVar {
		result = i.GetVariable(objPtr, 0) != nil
	} else if cmd := i.GetCommand(objPtr, None); cmd != nil {
		/* Now different kinds of commands */
		switch option {
		case optCommand:
			result = true
		case optAlias:
			result = cmd.flags&CmdIsAlias != 0
		case optProc:
			result = cmd.flags&CmdIsProc != 0
		case optChannel:
			result = cmd.flags&CmdIsChannel != 0
		}
	}
	i.SetResultBool(result)
	return OK
}

// errorCoreCommand implements [error].
func errorCoreCommand(i *Interp, argv []*Obj) int {
	i.SetResult(argv[1])
	if len(argv) == 3 {
		i.setStackTrace(argv[2])
	}
	return ERR
}

// sourceCoreCommand implements [source].
func sourceCoreCommand(i *Interp, argv []*Obj) int {
	retval := i.EvalFile(argv[1].String())
	if retval == RETURN {
		return OK
	}
	return retval
}
