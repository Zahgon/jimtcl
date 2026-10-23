/* Jim - A small embeddable Tcl interpreter: the evaluation engine.
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
	"errors"
	"io/fs"
	"os"
)

// Usage is returned by a command procedure whose arguments are wrong; the
// caller then reports the command's usage (C: JIM_USAGE).
const Usage = -1

// lsubstLine is the flag of listSubstObj that returns each "statement" as a
// list of {command arg...}.
const lsubstLine = 0x0001

// traceCallback invokes the [xtrace] callback for a command or proc call.
func (i *Interp) traceCallback(kind string, argv []*Obj) int {
	traceCmdObj := i.traceCmdObj
	resultObj := i.Result()
	var s *script

	/* Where were we called from? This may be nil for EvalObjVector() */
	if i.evalFrame.scriptObj != nil {
		s = i.getScript(i.evalFrame.scriptObj)
	}

	fileNameObj := i.emptyObj
	linenr := 1
	if s != nil {
		fileNameObj = s.fileName
		linenr = s.lineNr
	}
	nargv := []*Obj{
		traceCmdObj,
		i.NewStringObj(kind),
		fileNameObj,
		i.NewIntObj(int64(linenr)),
		resultObj,
		argv[0],
		i.NewListObj(argv[1:]),
	}

	/* Remove the trace while executing the trace callback */
	i.traceCmdObj = nil
	ret := i.EvalObjVector(nargv)

	if ret == OK || ret == RETURN {
		/* Reinstall the trace callback */
		i.traceCmdObj = traceCmdObj
		i.SetResult(i.emptyObj)
		ret = OK
	}
	return ret
}

// unknownCommand handles calls to the [unknown] command.
func (i *Interp) unknownCommand(argv []*Obj) int {
	/* If unknownCommand() is recursively called too many times, give up */
	if i.unknownDepth > 50 {
		return ERR
	}

	/* The object i.unknown just contains the "unknown" string; it is used to
	 * avoid looking up the unknown command every time. */
	if i.GetCommand(i.unknown, None) == nil {
		return ERR
	}

	i.unknownDepth++
	retcode := i.EvalObjPrefix(i.unknown, argv)
	i.unknownDepth--

	return retcode
}

// invokeCommand looks up and calls the command named by objv[0]. It is called
// from EvalObj, evalObjList and EvalObjVector.
func (i *Interp) invokeCommand(objv []*Obj) int {
	cmdPtr := i.GetCommand(objv[0], ErrMsg)
	if cmdPtr == nil {
		return i.unknownCommand(objv)
	}

	if i.evalDepth == i.maxEvalDepth {
		i.SetResultString("Infinite eval recursion")
		i.setErrorStack(nil)
		return ERR
	}
	i.evalDepth++
	prevPrivData := i.cmdPrivData

	retcode := OK
	for {
		i.evalFrame.argv = objv
		i.evalFrame.cmd = cmdPtr

		if i.traceCmdObj == nil || i.traceCallback("cmd", objv) == OK {
			/* Call it -- make sure the result is an empty object. */
			i.SetResult(i.emptyObj)
			if cmdPtr.flags&CmdIsProc != 0 {
				retcode = i.callProcedure(cmdPtr, objv)
			} else {
				retcode = i.callNative(cmdPtr, objv)
			}
			if retcode == ERR {
				i.setErrorStack(nil)
			}
		} else {
			retcode = ERR
		}

		/* These are now invalid */
		i.evalFrame.argv = nil

		/* If a tailcall is returned for this frame, loop to invoke the new
		 * command */
		if retcode != EVAL || i.framePtr.tailcallObj == nil {
			break
		}
		cmdPtr = i.framePtr.tailcallCmd
		i.framePtr.tailcallCmd = nil
		/* The tailcall object can only be constructed by the tailcall
		 * command, so its list representation is always available */
		objv = i.framePtr.tailcallObj.list
		i.framePtr.tailcallObj = nil
	}

	i.cmdPrivData = prevPrivData
	i.evalDepth--

	if retcode == ERR {
		i.setErrorStack(nil)
	}

	if i.framePtr.tailcallObj != nil {
		/* We might have skipped invoking a tailcall, perhaps because of an
		 * error in defer handling, so clean up now */
		i.framePtr.tailcallCmd = nil
		i.framePtr.tailcallObj = nil
	}

	return retcode
}

// callNative calls a command implemented in Go, checking the argument count.
func (i *Interp) callNative(cmd *Cmd, argv []*Obj) int {
	argsok := true
	argc := len(argv)

	switch {
	case argc-1 < cmd.minArgs:
		argsok = false
	case cmd.maxArgs >= 0 && argc-1 > cmd.maxArgs:
		argsok = false
	case cmd.maxArgs < -1 && (argc-1)%-cmd.maxArgs != 0:
		/* -2 means there must be a multiple of 2 args */
		argsok = false
	}
	if argsok {
		i.cmdPrivData = cmd.privData
		ret := cmd.proc(i, argv)
		if ret != Usage {
			return ret
		}
		/* This means an argument error */
	}

	return i.Errorf("wrong # args: should be \"%#s\"", i.cmdUsage(argv[0], cmd))
}

// EvalObjVector evaluates the object vector objv, using every element as a
// single argument.
func (i *Interp) EvalObjVector(objv []*Obj) int {
	var frame EvalFrame

	/* Note that we have no source for this command, so push a nil scriptObj */
	i.pushEvalFrame(&frame, nil)
	retcode := i.invokeCommand(objv)
	i.popEvalFrame()

	return retcode
}

// EvalObjPrefix invokes prefix as a command with objv as its arguments.
func (i *Interp) EvalObjPrefix(prefix *Obj, objv []*Obj) int {
	nargv := make([]*Obj, 0, len(objv)+1)
	nargv = append(nargv, prefix)
	nargv = append(nargv, objv...)
	return i.EvalObjVector(nargv)
}

// substOneToken performs one substitution step for a parsed script token. It
// returns the substituted object, or nil and the completion code on failure.
func (i *Interp) substOneToken(token *scriptToken) (*Obj, int) {
	switch token.typ {
	case ttStr, ttEsc:
		return token.objPtr, OK
	case ttVar:
		if objPtr := i.GetVariable(token.objPtr, ErrMsg); objPtr != nil {
			return objPtr, OK
		}
	case ttDictSugar:
		if objPtr := i.expandDictSugar(token.objPtr); objPtr != nil {
			return objPtr, OK
		}
	case ttExprSugar:
		objPtr, ret := i.EvalExpression(token.objPtr)
		if ret == OK {
			return objPtr, OK
		}
		return nil, ret
	case ttCmd:
		ret := i.EvalObj(token.objPtr)
		if ret == OK || ret == RETURN {
			return i.result, OK
		}
		/* includes BREAK and CONTINUE */
		return nil, ret
	}
	return nil, ERR
}

// interpolateTokens interpolates the given tokens into a single object. It is
// only called by EvalObj() and SubstObj(), and returns nil on error.
func (i *Interp) interpolateTokens(token []scriptToken, flags int) *Obj {
	tokens := len(token)
	intv := make([]*Obj, tokens)
	totlen := 0

	/* Compute every token forming the argument into the intv vector */
	for n := 0; n < tokens; n++ {
		objPtr, ret := i.substOneToken(&token[n])
		if objPtr == nil {
			errorAction := ""
			switch ret {
			case BREAK:
				if flags&SubstFlag != 0 {
					/* Stop here */
					tokens = n
					continue
				}
				errorAction = "break"
			case CONTINUE:
				if flags&SubstFlag != 0 {
					continue
				}
				errorAction = "continue"
			}
			if errorAction != "" {
				i.Errorf("invoked \"%s\" outside of a loop", errorAction)
			}
			return nil
		}
		intv[n] = objPtr
		totlen += objPtr.Len()
	}

	/* Fast path return for a single token */
	if tokens == 1 && intv[0] != nil {
		return intv[0]
	}

	/* Concatenate every token into a single object */
	b := make([]byte, 0, totlen)
	for n := 0; n < tokens; n++ {
		if intv[n] != nil {
			b = append(b, intv[n].Bytes()...)
		}
	}
	objPtr := i.NewStringObjBytes(b)

	if tokens == 4 && token[0].typ == ttEsc && token[1].typ == ttEsc && token[2].typ == ttVar {
		/* May be able to do a fast interpolated object -> dictSubst */
		objPtr.kind = kindInterpolated
		objPtr.aux = dictSubst{varName: token[0].objPtr, index: intv[2]}
	} else if tokens != 0 && intv[0] != nil && intv[0].fileName != nil {
		/* The first interpolated token has source info, so preserve it */
		fileNameObj, line := i.getSourceInfo(intv[0])
		objPtr.SetSourceInfo(fileNameObj, line)
	}

	return objPtr
}

// listSubstObj parses a string as an [lsubst] argument and sets the interp
// result. It is modelled on EvalObj(); with the lsubstLine flag each
// "statement" is returned as a list of {command arg...}.
func (i *Interp) listSubstObj(objPtr *Obj, flags int) int {
	s := i.getScript(objPtr)
	if i.parseCheckMissing(s.missing) == ERR {
		i.setErrorStack(s)
		return ERR
	}

	token := s.token
	retcode := OK

	/* Build the result list here */
	resultListObj := i.NewListObj(nil)

	for n := 0; n < len(token) && retcode == OK; {
		lineListObj := resultListObj

		/* The first token of the line is always a LINE token */
		line := token[n].objPtr.aux.(scriptLine)
		s.lineNr = line.line
		n++

		if flags&lsubstLine != 0 {
			lineListObj = i.NewListObj(nil)
		}

		/* Extract the words from this line */
		for j := 0; j < line.argc; j++ {
			wordtokens := 1
			expand := false

			if token[n].typ == ttWord {
				wordtokens = int(token[n].objPtr.wide)
				n++
				if wordtokens < 0 {
					expand = true
					wordtokens = -wordtokens
				}
			}

			/* Note we don't worry about a fast path here */
			wordObjPtr := i.interpolateTokens(token[n:n+wordtokens], None)
			if wordObjPtr == nil {
				retcode = ERR
				break
			}
			n += wordtokens

			if !expand {
				i.ListAppendElement(lineListObj, wordObjPtr)
			} else {
				/* Need to add each word of wordObjPtr to the result list */
				for _, e := range i.ListElements(wordObjPtr) {
					i.ListAppendElement(lineListObj, e)
				}
			}
		}

		if flags&lsubstLine != 0 {
			i.ListAppendElement(resultListObj, lineListObj)
		}
	}

	if retcode == OK {
		i.SetResult(resultListObj)
	}
	return retcode
}

// evalObjList evaluates a list object, using the first element as the command
// and the remaining elements as the arguments.
func (i *Interp) evalObjList(listPtr *Obj) int {
	var frame EvalFrame
	retcode := OK

	i.pushEvalFrame(&frame, nil)

	if len(listPtr.list) != 0 {
		retcode = i.invokeCommand(listPtr.list)
	}

	i.popEvalFrame()

	return retcode
}

// EvalObjList evaluates the contents of a list as a command.
func (i *Interp) EvalObjList(listPtr *Obj) int {
	i.setListFromAny(listPtr)
	return i.evalObjList(listPtr)
}

// EvalObj evaluates a script object.
func (i *Interp) EvalObj(scriptObjPtr *Obj) int {
	/* If the object is of type "list" with no string rep we can call a
	 * specialized version of EvalObj() */
	if scriptObjPtr.IsList() && !scriptObjPtr.strValid {
		return i.evalObjList(scriptObjPtr)
	}

	s := i.getScript(scriptObjPtr)
	if i.parseCheckMissing(s.missing) == ERR {
		i.setErrorStack(s)
		return ERR
	}

	/* Reset the interpreter result. This is useful to return the empty result
	 * in the case of an empty program. */
	i.SetResult(i.emptyObj)

	token := s.token
	if len(token) == 0 {
		return OK
	}

	var frame EvalFrame
	i.pushEvalFrame(&frame, scriptObjPtr)

	/* Collect a new error stack trace if an error occurs */
	i.hasErrorStackTrace = false
	retcode := OK

	/* Execute every command sequentially until the end of the script or an
	 * error occurs. */
	for n := 0; n < len(token) && retcode == OK; {
		/* The first token of the line is always a LINE token */
		line := token[n].objPtr.aux.(scriptLine)
		s.lineNr = line.line
		n++

		argv := make([]*Obj, 0, line.argc)

		/* Populate the arguments objects */
		for j := 0; j < line.argc; j++ {
			wordtokens := 1
			expand := false
			var wordObjPtr *Obj

			if token[n].typ == ttWord {
				wordtokens = int(token[n].objPtr.wide)
				n++
				if wordtokens < 0 {
					expand = true
					wordtokens = -wordtokens
				}
			}

			if wordtokens == 1 {
				/* Fast path if the token does not need interpolation */
				switch token[n].typ {
				case ttEsc, ttStr:
					wordObjPtr = token[n].objPtr
				case ttVar:
					wordObjPtr = i.GetVariable(token[n].objPtr, ErrMsg)
				case ttExprSugar:
					var objPtr *Obj
					objPtr, retcode = i.EvalExpression(token[n].objPtr)
					if retcode == OK {
						wordObjPtr = objPtr
					}
				case ttDictSugar:
					wordObjPtr = i.expandDictSugar(token[n].objPtr)
				case ttCmd:
					retcode = i.EvalObj(token[n].objPtr)
					if retcode == OK {
						wordObjPtr = i.Result()
					}
				}
			} else {
				/* For interpolation we call a helper function */
				wordObjPtr = i.interpolateTokens(token[n:n+wordtokens], None)
			}

			if wordObjPtr == nil {
				if retcode == OK {
					retcode = ERR
				}
				break
			}
			n += wordtokens

			if !expand {
				argv = append(argv, wordObjPtr)
			} else {
				/* Expand wordObjPtr into multiple arguments */
				argv = append(argv, i.ListElements(wordObjPtr)...)
			}
		}

		if retcode == OK && len(argv) != 0 {
			/* Invoke the command */
			retcode = i.invokeCommand(argv)
		}
	}

	/* Possibly add to the error stack trace */
	if retcode == ERR {
		i.setErrorStack(nil)
	}

	i.popEvalFrame()

	return retcode
}

// setProcArg binds one proc argument, performing an automatic upvar when the
// formal parameter name begins with '&'.
func (i *Interp) setProcArg(argNameObj, argValObj *Obj) int {
	varname := argNameObj.String()
	if varname == "" || varname[0] != '&' {
		return i.SetVariable(argNameObj, argValObj)
	}

	/* First check that the target variable exists */
	savedCallFrame := i.framePtr
	i.framePtr = i.framePtr.parent
	objPtr := i.GetVariable(argValObj, ErrMsg)
	i.framePtr = savedCallFrame
	if objPtr == nil {
		return ERR
	}

	/* It exists, so perform the binding. */
	return i.SetVariableLink(i.NewStringObj(varname[1:]), argValObj, i.framePtr.parent)
}

// bindProcArgs assigns the actual arguments to the formal parameters of a
// procedure in the newly created call frame.
func (i *Interp) bindProcArgs(cmd *Cmd, argv []*Obj) int {
	argc := len(argv)
	/* How many optional args are available */
	optargs := argc - 1 - cmd.reqArity

	/* Step 'n' along the actual args, and 'd' along the formal args */
	n := 1
	for d := 0; d < cmd.argListLen; d++ {
		nameObjPtr := cmd.args[d].Name

		if d == cmd.argsPos {
			/* assign $args */
			argsLen := 0
			if cmd.reqArity+cmd.optArity < argc-1 {
				argsLen = argc - 1 - (cmd.reqArity + cmd.optArity)
			}
			listObjPtr := i.NewListObj(argv[n : n+argsLen])

			/* It is possible to rename args. */
			if cmd.args[d].Default != nil {
				nameObjPtr = cmd.args[d].Default
			}
			if retcode := i.SetVariable(nameObjPtr, listObjPtr); retcode != OK {
				return retcode
			}
			n += argsLen
			continue
		}

		/* Optional or required? */
		useArg := cmd.args[d].Default == nil
		if !useArg {
			useArg = optargs > 0
			optargs--
		}

		var retcode int
		if useArg {
			retcode = i.setProcArg(nameObjPtr, argv[n])
			n++
		} else {
			/* Ran out, so use the default */
			retcode = i.SetVariable(nameObjPtr, cmd.args[d].Default)
		}
		if retcode != OK {
			return retcode
		}
	}
	return OK
}

// callProcedure calls a procedure implemented in Tcl.
func (i *Interp) callProcedure(cmd *Cmd, argv []*Obj) int {
	argc := len(argv)

	/* Check arity */
	if argc-1 < cmd.reqArity ||
		(cmd.argsPos < 0 && argc-1 > cmd.reqArity+cmd.optArity) {
		i.setProcWrongArgs(argv[0], cmd)
		return ERR
	}

	if cmd.body.Len() == 0 {
		/* Optimise for a procedure with no body - useful for optional debugging */
		return OK
	}

	/* Check if there are too many nested calls */
	if i.framePtr.level == i.maxCallFrameDepth {
		i.SetResultString("Too many nested calls. Infinite recursion?")
		return ERR
	}

	/* Create a new callframe */
	callFramePtr := i.createCallFrame(i.framePtr, cmd.nsObj)
	callFramePtr.argv = argv
	callFramePtr.procArgsObj = cmd.argList
	callFramePtr.procBodyObj = cmd.body
	callFramePtr.staticVars = cmd.staticVars

	i.procLevel++
	i.framePtr = callFramePtr

	retcode := i.bindProcArgs(cmd, argv)

	if retcode == OK {
		if i.traceCmdObj == nil {
			retcode = i.EvalObj(cmd.body)
		} else if retcode = i.traceCallback("proc", argv); retcode == OK {
			retcode = i.EvalObj(cmd.body)
		}
	}

	/* Invoke $jim::defer then destroy the callframe */
	retcode = i.invokeDefer(retcode)
	i.framePtr = i.framePtr.parent
	i.freeCallFrame(callFramePtr)

	/* Handle the RETURN return code */
	if retcode == RETURN {
		i.returnLevel--
		if i.returnLevel <= 0 {
			retcode = i.returnCode
			i.returnCode = OK
			i.returnLevel = 0
		}
	}
	i.procLevel--

	return retcode
}

// EvalSource evaluates a script which comes from the given file name and line.
func (i *Interp) EvalSource(fileName string, line int, s string) int {
	scriptObjPtr := i.NewStringObj(s)
	if fileName != "" {
		scriptObjPtr.SetSourceInfo(i.NewStringObj(fileName), line)
	}
	return i.EvalObj(scriptObjPtr)
}

// Eval evaluates a script.
func (i *Interp) Eval(script string) int {
	return i.EvalObj(i.NewStringObj(script))
}

// EvalGlobal evaluates a script in the scope of the global level.
func (i *Interp) EvalGlobal(script string) int {
	savedFramePtr := i.framePtr
	i.framePtr = i.topFramePtr
	retval := i.Eval(script)
	i.framePtr = savedFramePtr
	return retval
}

// evalStrerror renders an I/O error the way the C library's strerror() does,
// i.e. with an initial capital letter.
func evalStrerror(err error) string {
	var perr *fs.PathError
	if errors.As(err, &perr) {
		err = perr.Err
	}
	msg := err.Error()
	if msg != "" && msg[0] >= 'a' && msg[0] <= 'z' {
		return string(msg[0]-'a'+'A') + msg[1:]
	}
	return msg
}

// EvalFile reads a file and evaluates its contents as a script.
func (i *Interp) EvalFile(filename string) int {
	data, err := os.ReadFile(filename)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) {
			return i.Errorf("couldn't read file \"%s\": %s", filename, evalStrerror(err))
		}
		return i.Errorf("failed to load file \"%s\": %s", filename, evalStrerror(err))
	}

	/* Convert the string object to one with a filename and line */
	scriptObjPtr := i.NewStringObjBytes(data)
	filenameObj := i.NewStringObj(filename)
	scriptObjPtr.SetSourceInfo(filenameObj, 1)

	oldFilenameObj := i.currentFileNameObj
	i.currentFileNameObj = filenameObj

	retcode := i.EvalObj(scriptObjPtr)

	i.currentFileNameObj = oldFilenameObj

	/* Handle the RETURN return code */
	if retcode == RETURN {
		i.returnLevel--
		if i.returnLevel <= 0 {
			retcode = i.returnCode
			i.returnCode = OK
			i.returnLevel = 0
		}
	}

	return retcode
}
