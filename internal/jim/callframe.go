/* Jim - A small embeddable Tcl interpreter: call frames, local procs, defer.
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

// createCallFrame returns a new call frame whose parent is parent and whose
// namespace is nsObj.
func (i *Interp) createCallFrame(parent *CallFrame, nsObj *Obj) *CallFrame {
	cf := &CallFrame{parent: parent, nsObj: nsObj}
	cf.id = i.callFrameEpoch
	i.callFrameEpoch++
	if parent != nil {
		cf.level = parent.level + 1
	}
	return cf
}

// deleteLocalProcs deletes the commands marked local for a call frame,
// restoring any command they replaced.
func (i *Interp) deleteLocalProcs(localCommands []*Obj) {
	/* Delete the local procs in reverse order of creation */
	for n := len(localCommands) - 1; n >= 0; n-- {
		key := cmdKey(localCommands[n].String())
		cmd, _ := i.commands.get(key)
		if cmd != nil {
			if cmd.prevCmd != nil {
				/* Restore the original */
				i.commands.set(key, cmd.prevCmd)
				cmd.prevCmd = nil
			} else {
				i.deleteCommandKey(key)
			}
		}
	}
}

// invokeDefer runs any $jim::defer scripts for the current call frame.
// retcode is the return code from the current proc; the new return code is
// returned.
func (i *Interp) invokeDefer(retcode int) int {
	/* Fast check for the likely case that the variable doesn't exist */
	if vv, _ := i.framePtr.vars.get(i.deferObj.String()); vv == nil {
		return retcode
	}
	objPtr := i.GetVariable(i.deferObj, None)
	if objPtr == nil {
		return retcode
	}

	ret := OK
	listLen := i.ListLength(objPtr)

	/* Need to save away the current interp result and restore it if
	 * appropriate */
	resultObjPtr := i.Result()
	i.SetResult(i.emptyObj)

	/* Invoke in reverse order */
	for n := listLen; n > 0; n-- {
		/* If a defer script returns an error, don't evaluate the rest */
		ret = i.EvalObj(i.ListGetIndex(objPtr, n-1))
		if ret != OK {
			break
		}
	}

	if ret == OK || retcode == ERR {
		/* The defer script had no error, or the proc had an error so restore
		 * the proc result */
		i.SetResult(resultObjPtr)
	} else {
		retcode = ret
	}
	return retcode
}

// freeCallFrame releases a call frame, deleting its local commands.
func (i *Interp) freeCallFrame(cf *CallFrame) {
	i.deleteLocalProcs(cf.localCommands)
	cf.localCommands = nil
}

// CallFrameByLevel returns the call frame for the level named by levelObj, in
// the form accepted by [uplevel] and [upvar]. A nil levelObj means level "1".
// It returns nil and sets an error message on failure.
func (i *Interp) CallFrameByLevel(levelObj *Obj) *CallFrame {
	var level int64
	str := "1" /* Needed to format the error message */

	if levelObj != nil {
		str = levelObj.String()
		if len(str) != 0 && str[0] == '#' {
			l, ok, _ := stringToWide(str[1:], 10)
			if len(str) == 1 || !ok {
				level = -1
			} else {
				level = l
			}
		} else {
			l, ok := i.GetWideNoErr(levelObj)
			if !ok || l < 0 {
				level = -1
			} else {
				/* Convert from a relative to an absolute level */
				level = int64(i.framePtr.level) - l
			}
		}
	} else {
		level = int64(i.framePtr.level) - 1
	}

	if level == 0 {
		return i.topFramePtr
	}
	if level > 0 {
		for framePtr := i.framePtr; framePtr != nil; framePtr = framePtr.parent {
			if int64(framePtr.level) == level {
				return framePtr
			}
		}
	}

	i.Errorf("bad level \"%s\"", str)
	return nil
}

// callFrameByInteger is like CallFrameByLevel but takes a relative integer
// level, as [info level ?level?] does.
func (i *Interp) callFrameByInteger(level int) *CallFrame {
	if level == 0 {
		return i.framePtr
	}
	if level < 0 {
		/* Convert from a relative to an absolute level */
		level = i.framePtr.level + level
	}
	if level > 0 {
		for framePtr := i.framePtr; framePtr != nil; framePtr = framePtr.parent {
			if framePtr.level == level {
				return framePtr
			}
		}
	}
	return nil
}
