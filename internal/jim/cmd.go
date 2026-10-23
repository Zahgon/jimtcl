/* Jim - A small embeddable Tcl interpreter: command creation and lookup.
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

// cmdKey returns the key a command name is stored under: the commands table
// ignores any leading namespace qualifier, so "::foo" and "foo" are the same
// entry (C: Jim_GetStringNoQualifier).
func cmdKey(name string) string {
	if len(name) >= 2 && name[0] == ':' && name[1] == ':' {
		return strings.TrimLeft(name, ":")
	}
	return name
}

// interpIncrProcEpoch invalidates every cached command resolution.
func (i *Interp) interpIncrProcEpoch() {
	i.procEpoch++
}

// MakeGlobalNamespaceName returns name qualified into the global namespace,
// i.e. prefixed with "::" unless it already is.
func (i *Interp) MakeGlobalNamespaceName(nameObj *Obj) *Obj {
	name := nameObj.String()
	if len(name) >= 2 && name[0] == ':' && name[1] == ':' {
		return nameObj
	}
	return i.NewStringObj("::" + name)
}

// qualifyName qualifies a command or variable name with the current
// namespace, unless the name is already fully qualified or the global
// namespace is in effect.
func (i *Interp) qualifyName(objPtr *Obj) *Obj {
	if i.framePtr.nsObj.Len() != 0 {
		name := objPtr.String()
		if len(name) < 2 || name[0] != ':' || name[1] != ':' {
			/* Need to qualify this name */
			return i.NewStringObj(i.framePtr.nsObj.String() + "::" + name)
		}
	}
	return objPtr
}

// CurrentNamespace returns the namespace object of the current call frame.
func (i *Interp) CurrentNamespace() *Obj { return i.framePtr.nsObj }

// createCommand adds a command to the commands table, replacing any existing
// command of the same name. While [local] is in force the existing command is
// stashed in prevCmd instead of being replaced.
func (i *Interp) createCommand(nameObjPtr *Obj, cmd *Cmd) {
	key := cmdKey(nameObjPtr.String())
	cmd.cmdNameObj = nameObjPtr

	if i.local != 0 {
		if prev, ok := i.commands.get(key); ok {
			/* Push this command over the top of the previous one. The proc
			 * epoch must be incremented so that the new command is used. */
			cmd.prevCmd = prev
			i.commands.set(key, cmd)
			i.interpIncrProcEpoch()
			return
		}
	}

	/* Otherwise simply replace any existing command. Note that it is not
	 * necessary to increment the proc epoch, because a replaced command is
	 * never cached beyond the epoch. */
	i.commands.set(key, cmd)
}

// deleteCommandKey removes a command from the commands table.
func (i *Interp) deleteCommandKey(key string) int {
	if _, ok := i.commands.get(key); !ok {
		return ERR
	}
	i.commands.delete(key)
	return OK
}

// RegisterCommand creates a native command with the given argument limits,
// usage and private data.
func (i *Interp) RegisterCommand(cmdNameObj *Obj, proc CmdProc, usage, help string,
	minArgs, maxArgs, flags int, privData any) *Cmd {
	cmd := &Cmd{
		flags:    flags,
		proc:     proc,
		usage:    usage,
		help:     help,
		minArgs:  minArgs,
		maxArgs:  maxArgs,
		privData: privData,
	}
	i.createCommand(cmdNameObj, cmd)
	return cmd
}

// RegisterCmd creates a native command with the given name, usage string and
// argument limits. maxArgs may be -1 for no limit, or -n to require a
// multiple of n arguments.
func (i *Interp) RegisterCmd(name, usage string, minArgs, maxArgs int, proc CmdProc, flags int) *Cmd {
	return i.RegisterCommand(i.NewStringObj(name), proc, usage, "", minArgs, maxArgs, flags, nil)
}

// CmdPrivData returns the private data of the native command being executed.
func (i *Interp) CmdPrivData() any { return i.cmdPrivData }

// createProcedureStatics creates the static variables declared for a proc.
func (i *Interp) createProcedureStatics(cmd *Cmd, staticsListObjPtr *Obj) int {
	length := i.ListLength(staticsListObjPtr)
	if length == 0 {
		return OK
	}

	cmd.staticVars = &htable[*VarVal]{}
	for n := 0; n < length; n++ {
		var initObjPtr *Obj
		var vv *VarVal
		objPtr := i.ListGetIndex(staticsListObjPtr, n)
		subLen := i.ListLength(objPtr)
		byref := false

		/* Check if it's composed of one or two elements. */
		if subLen != 1 && subLen != 2 {
			return i.Errorf("too many fields in static specifier \"%#s\"", objPtr)
		}

		nameObjPtr := i.ListGetIndex(objPtr, 0)

		/* How to initialise or link? */
		if subLen == 1 {
			pt := nameObjPtr.String()
			if strings.HasPrefix(pt, "&") {
				/* Create as a reference */
				nameObjPtr = i.NewStringObj(pt[1:])
				byref = true
			}

			ref, rc := i.setVariableFromAny(nameObjPtr)
			switch rc {
			case dictSugarCode:
				/* This message seems unnecessarily verbose, but it matches Tcl */
				if byref {
					return i.Errorf("Can't link to array element \"%#s\"", nameObjPtr)
				}
				return i.Errorf("Can't initialise array element \"%#s\"", nameObjPtr)

			case OK:
				if byref {
					vv = ref.vv
				} else {
					initObjPtr = i.GetVariable(nameObjPtr, None)
					if initObjPtr == nil {
						return i.Errorf("Could not resolve upvar \"%#s\"'s value", nameObjPtr)
					}
				}

			default:
				/* Doesn't exist */
				return i.Errorf("variable for initialization of static \"%#s\" not found in the local context", nameObjPtr)
			}
		} else {
			initObjPtr = i.ListGetIndex(objPtr, 1)
		}

		if vv == nil {
			vv = &VarVal{Obj: initObjPtr}
		}

		if _, exists := cmd.staticVars.get(nameObjPtr.String()); exists {
			return i.Errorf("static variable name \"%#s\" duplicated in statics list", nameObjPtr)
		}
		cmd.staticVars.set(nameObjPtr.String(), vv)
	}
	return OK
}

// updateProcNamespace sets or updates the cached namespace of a proc from its
// command name.
func (i *Interp) updateProcNamespace(cmd *Cmd, nameObjPtr *Obj) {
	if cmd.flags&CmdIsProc == 0 {
		return
	}
	cmdname := cmdKey(nameObjPtr.String())
	pt := strings.LastIndexByte(cmdname, ':')
	if pt > 0 && cmdname[pt-1] == ':' {
		/* cmdname[:pt-1] is the namespace, cmdname[pt+1:] the base name,
		 * e.g. for "abc::def::ghi" the namespace is "abc::def" */
		cmd.nsObj = i.NewStringObj(cmdname[:pt-1])

		if _, ok := i.commands.get(cmdname[pt+1:]); ok {
			/* This command shadows a global command, so a proc epoch update
			 * is required */
			i.interpIncrProcEpoch()
		}
	}
}

// createProcedureCmd creates a Tcl procedure, parsing and validating its
// argument list and static variables. It returns nil and sets an error
// message when the specification is invalid.
func (i *Interp) createProcedureCmd(argListObjPtr, staticsListObjPtr, bodyObjPtr, nsObj *Obj) *Cmd {
	argListLen := i.ListLength(argListObjPtr)

	cmd := &Cmd{
		flags:      CmdIsProc,
		argList:    argListObjPtr,
		argListLen: argListLen,
		body:       bodyObjPtr,
		argsPos:    -1,
		args:       make([]ProcArg, argListLen),
		nsObj:      nsObj,
	}
	if cmd.nsObj == nil {
		cmd.nsObj = i.emptyObj
	}

	/* Create the statics table. */
	if staticsListObjPtr != nil && i.createProcedureStatics(cmd, staticsListObjPtr) != OK {
		return nil
	}

	/* Examine the argument list for default parameters and 'args' */
	for n := 0; n < argListLen; n++ {
		var nameObjPtr, defaultObjPtr *Obj

		argPtr := i.ListGetIndex(argListObjPtr, n)
		length := i.ListLength(argPtr)
		if length == 0 {
			i.SetResultString("argument with no name")
			return nil
		}
		if length > 2 {
			i.Errorf("too many fields in argument specifier \"%#s\"", argPtr)
			return nil
		}

		if length == 2 {
			/* Optional parameter */
			nameObjPtr = i.ListGetIndex(argPtr, 0)
			defaultObjPtr = i.ListGetIndex(argPtr, 1)
		} else {
			/* Required parameter */
			nameObjPtr = argPtr
		}

		if nameObjPtr.String() == "args" {
			if cmd.argsPos >= 0 {
				i.SetResultString("'args' specified more than once")
				return nil
			}
			cmd.argsPos = n
		} else if length == 2 {
			cmd.optArity++
		} else {
			cmd.reqArity++
		}

		cmd.args[n] = ProcArg{Name: nameObjPtr, Default: defaultObjPtr}
	}

	return cmd
}

// DeleteCommand deletes a command by name.
func (i *Interp) DeleteCommand(nameObj *Obj) int {
	nameObj = i.qualifyName(nameObj)

	if i.deleteCommandKey(cmdKey(nameObj.String())) == ERR {
		return i.Errorf("can't delete \"%#s\": command doesn't exist", nameObj)
	}
	return OK
}

// RenameCommand renames a command. An empty new name deletes the command.
func (i *Interp) RenameCommand(oldNameObj, newNameObj *Obj) int {
	if newNameObj.Len() == 0 {
		return i.DeleteCommand(oldNameObj)
	}

	/* Each name may need to have the current namespace added to it */
	oldNameObj = i.qualifyName(oldNameObj)
	newNameObj = i.qualifyName(newNameObj)

	oldKey := cmdKey(oldNameObj.String())
	newKey := cmdKey(newNameObj.String())

	cmd, ok := i.commands.get(oldKey)
	if !ok {
		return i.Errorf("can't rename \"%#s\": command doesn't exist", oldNameObj)
	}
	if _, ok := i.commands.get(newKey); ok {
		return i.Errorf("can't rename to \"%#s\": command already exists", newNameObj)
	}
	if cmd.prevCmd != nil {
		/* If the command replaced another command with 'local', renaming it
		 * would break the usage of upcall, so don't allow it. */
		return i.Errorf("can't rename local command \"%#s\"", oldNameObj)
	}

	/* Add the new name first, then remove the old one */
	i.updateProcNamespace(cmd, newNameObj)
	cmd.cmdNameObj = newNameObj
	i.commands.set(newKey, cmd)
	i.deleteCommandKey(oldKey)

	i.interpIncrProcEpoch()
	return OK
}

// GetCommand returns the command named by objPtr, respecting the 'upcall'
// setting. With the ErrMsg flag an error message is set when the command does
// not exist.
func (i *Interp) GetCommand(objPtr *Obj, flags int) *Cmd {
	qualifiedNameObj := i.qualifyName(objPtr)
	cmd, ok := i.commands.get(cmdKey(qualifiedNameObj.String()))
	if !ok && i.framePtr.nsObj.Len() != 0 {
		/* Fall back to the global namespace */
		cmd, ok = i.commands.get(cmdKey(objPtr.String()))
	}
	if !ok {
		if flags&ErrMsg != 0 {
			i.Errorf("invalid command name \"%#s\"", objPtr)
		}
		return nil
	}
	for cmd.flags&CmdIsProc != 0 && cmd.upcall {
		cmd = cmd.prevCmd
	}
	return cmd
}

// cmdUsage returns the usage string of a command. When no usage is available
// it just returns "cmd ..." (C: JimCmdUsage).
func (i *Interp) cmdUsage(cmdNameObj *Obj, cmd *Cmd) *Obj {
	var b strings.Builder
	b.WriteString(cmdNameObj.String())

	switch {
	case cmd.flags&CmdIsProc != 0:
		for n := 0; n < cmd.argListLen; n++ {
			b.WriteString(" ")

			if n == cmd.argsPos {
				if cmd.args[n].Default != nil {
					/* Renamed args */
					b.WriteString("?")
					b.WriteString(cmd.args[n].Default.String())
					b.WriteString(" ...?")
				} else {
					/* We have plain args */
					b.WriteString("?arg ...?")
				}
			} else if cmd.args[n].Default != nil {
				b.WriteString("?")
				b.WriteString(cmd.args[n].Name.String())
				b.WriteString("?")
			} else {
				arg := cmd.args[n].Name.String()
				b.WriteString(strings.TrimPrefix(arg, "&"))
			}
		}
	case cmd.usage != "":
		b.WriteString(" ")
		b.WriteString(cmd.usage)
	}

	return i.NewStringObj(b.String())
}

// setProcWrongArgs sets the interp result to an error message describing the
// required proc arguments.
func (i *Interp) setProcWrongArgs(procNameObj *Obj, cmd *Cmd) {
	i.Errorf("wrong # args: should be \"%#s\"", i.cmdUsage(procNameObj, cmd))
}
