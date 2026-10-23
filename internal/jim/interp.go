/* Jim - A small embeddable Tcl interpreter: the interpreter and error stacks.
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
	"fmt"
	"github.com/msteveb/jimtcl/internal/u8"
	"sort"
	"strings"
)

// Values of the tcl_platform array and auto_path, as the configure script
// determined them for this build.
const (
	tclLibrary               = "/usr/local/lib/jim"
	tclPlatformOS            = "linux"
	tclPlatformPlatform      = "unix"
	tclPlatformPathSeparator = ":"
)

// libPath and interactiveVar are the names of the two variables every
// interpreter defines (C: JIM_LIBPATH, JIM_INTERACTIVE).
const (
	libPath        = "auto_path"
	interactiveVar = "tcl_interactive"
)

// referenceTagLen is the fixed length of a reference tag, and referenceSpace
// the length of the whole string representation of a reference.
const (
	referenceTagLen = 7
	referenceSpace  = 35 + referenceTagLen
)

// TclSource is a Tcl-implemented static extension: a package name, the script
// file name used in error messages and the script itself.
type TclSource struct {
	Package  string
	FileName string
	Source   string
}

// NewInterp creates an interpreter, registers the built-in commands and
// evaluates the Tcl-implemented static extensions in order.
func NewInterp(exts ...TclSource) *Interp {
	i := &Interp{
		maxCallFrameDepth: MaxCallFrameDepth,
		maxEvalDepth:      MaxEvalDepth,
		references:        map[string]*Reference{},
	}

	i.emptyObj = i.NewStringObj("")
	i.trueObj = i.NewIntObj(1)
	i.falseObj = i.NewIntObj(0)
	i.topFramePtr = i.createCallFrame(nil, i.emptyObj)
	i.framePtr = i.topFramePtr
	i.result = i.emptyObj
	i.stackTrace = i.NewListObj(nil)
	i.unknown = i.NewStringObj("unknown")
	i.deferObj = i.NewStringObj("jim::defer")
	i.errorProc = i.emptyObj
	i.nullScriptObj = i.NewStringObj("")
	i.evalFrame = &i.topEvalFrame
	i.currentFileNameObj = i.NewStringObj("")

	/* Initialize key variables every interpreter should contain */
	i.SetVariableStrWithStr(libPath, tclLibrary)
	i.SetVariableStrWithStr(interactiveVar, "0")

	i.SetVariableStrWithStr("tcl_platform(engine)", "Jim")
	i.SetVariableStrWithStr("tcl_platform(os)", tclPlatformOS)
	i.SetVariableStrWithStr("tcl_platform(platform)", tclPlatformPlatform)
	i.SetVariableStrWithStr("tcl_platform(pathSeparator)", tclPlatformPathSeparator)
	i.SetVariableStrWithStr("tcl_platform(byteOrder)", "littleEndian")
	i.SetVariableStrWithStr("tcl_platform(threaded)", "0")
	i.SetVariableStrWithStr("tcl_platform(bootstrap)", "0")
	i.SetVariableStr("tcl_platform(pointerSize)", i.NewIntObj(8))
	i.SetVariableStr("tcl_platform(wordSize)", i.NewIntObj(8))
	i.SetVariableStr("tcl_platform(stackFormat)", i.NewIntObj(4))

	i.registerBuiltins(exts)

	return i
}

// ExitCode returns the code passed to [exit].
func (i *Interp) ExitCode() int { return i.exitCode }

/* ---------------------------------------------------------------------------
 * Evaluation frames and stack traces
 * ------------------------------------------------------------------------ */

// pushEvalFrame pushes a new evaluation frame while executing a script.
func (i *Interp) pushEvalFrame(frame *EvalFrame, scriptObj *Obj) {
	*frame = EvalFrame{
		parent:    i.evalFrame,
		level:     i.evalFrame.level + 1,
		procLevel: i.procLevel,
		framePtr:  i.framePtr,
		scriptObj: scriptObj,
	}
	if scriptObj == nil {
		frame.scriptObj = frame.parent.scriptObj
	}
	i.evalFrame = frame
}

// popEvalFrame pops the current evaluation frame.
func (i *Interp) popEvalFrame() {
	i.evalFrame = i.evalFrame.parent
}

// getEvalFrameByProcLevel finds the evaluation frame for the requested proc
// nesting level, which may be relative (negative).
func (i *Interp) getEvalFrameByProcLevel(proclevel int) *EvalFrame {
	if proclevel == 0 {
		return i.evalFrame
	}
	if proclevel < 0 {
		/* Convert from a relative to an absolute level */
		proclevel = i.procLevel + proclevel
	}
	if proclevel >= 0 {
		for frame := i.evalFrame; frame != nil; frame = frame.parent {
			if frame.procLevel == proclevel {
				return frame
			}
		}
	}
	return nil
}

// procForEvalFrame finds the proc that called the code executing in frame.
//
// When a proc is called the command name is stored in the eval frame, so to
// find the proc that called the currently executing code we look back through
// the eval frames until one with a command name is found.
func (i *Interp) procForEvalFrame(frame *EvalFrame) *Obj {
	/* If at the lowest level, or if this level called a proc directly, go
	 * look for the caller */
	if frame == i.evalFrame || (frame.cmd != nil && frame.cmd.cmdNameObj != nil) {
		var e *EvalFrame
		for e = frame.parent; e != nil; e = e.parent {
			if e.cmd != nil && e.cmd.flags&CmdIsProc != 0 && e.cmd.cmdNameObj != nil {
				break
			}
		}
		if e != nil && e.cmd != nil && e.cmd.cmdNameObj != nil {
			return e.cmd.cmdNameObj
		}
	}
	return nil
}

// addStackFrame appends the stack trace info (proc, file, line, cmd) of an
// evaluation frame to listObj.
func (i *Interp) addStackFrame(frame *EvalFrame, listObj *Obj) {
	procNameObj := i.procForEvalFrame(frame)
	fileNameObj := i.emptyObj
	linenr := 1

	if frame.scriptObj != nil {
		s := i.getScript(frame.scriptObj)
		fileNameObj = s.fileName
		linenr = s.lineNr
	}

	if procNameObj == nil {
		procNameObj = i.emptyObj
	}
	i.ListAppendElement(listObj, procNameObj)
	i.ListAppendElement(listObj, fileNameObj)
	i.ListAppendElement(listObj, i.NewIntObj(int64(linenr)))
	i.ListAppendElement(listObj, i.NewListObj(frame.argv))
}

// setStackTrace replaces the interpreter's cached stack trace.
func (i *Interp) setStackTrace(stackTraceObj *Obj) {
	i.stackTrace = stackTraceObj
	i.hasErrorStackTrace = true
}

// setErrorStack captures a stack trace for the current error, unless one has
// already been recorded for it.
func (i *Interp) setErrorStack(s *script) {
	if i.hasErrorStackTrace {
		return
	}
	stackTrace := i.NewListObj(nil)

	if i.procLevel == 0 && s != nil {
		/* At the top level with a script, so use the script info rather than
		 * the proc info */
		i.ListAppendElement(stackTrace, i.emptyObj)
		i.ListAppendElement(stackTrace, s.fileName)
		i.ListAppendElement(stackTrace, i.NewIntObj(int64(s.lineNr)))
		i.ListAppendElement(stackTrace, i.emptyObj)
	} else {
		for n := 0; n <= i.procLevel; n++ {
			if frame := i.getEvalFrameByProcLevel(-n); frame != nil {
				i.addStackFrame(frame, stackTrace)
			}
		}
	}
	i.setStackTrace(stackTrace)
}

// MakeErrorMessage sets the interpreter result to the full error message for
// the current error, by evaluating the Tcl proc [errorInfo].
func (i *Interp) MakeErrorMessage() {
	i.EvalObjVector([]*Obj{i.NewStringObj("errorInfo"), i.result})
}

/* ---------------------------------------------------------------------------
 * Argument and enumeration helpers
 * ------------------------------------------------------------------------ */

// WrongNumArgs sets the standard "wrong # args" error for argv[0:argc] plus
// msg.
//
// This should not be necessary for simple commands: set minArgs/maxArgs and a
// usage string when registering the command instead.
func (i *Interp) WrongNumArgs(argc int, argv []*Obj, msg string) {
	parts := make([]string, argc)
	for n := 0; n < argc; n++ {
		parts[n] = argv[n].String()
	}
	cmd := strings.Join(parts, " ")
	if msg != "" {
		i.Errorf("wrong # args: should be \"%s %s\"", cmd, msg)
	} else {
		i.Errorf("wrong # args: should be \"%s\"", cmd)
	}
}

// setFailedEnumResult sets the error message listing the valid values of an
// enumeration, which are reported in sorted order.
func (i *Interp) setFailedEnumResult(arg, badtype, prefix string, table []string, name string) {
	if name == "" {
		name = "option"
	}

	sorted := make([]string, len(table))
	copy(sorted, table)
	// The C sorts the table with qsortCompareStringPointers, which puts "--"
	// after every other option to match Tcl 9.0.
	sort.SliceStable(sorted, func(a, b int) bool {
		if sorted[a] == "--" {
			return false
		}
		if sorted[b] == "--" {
			return true
		}
		return sorted[a] < sorted[b]
	})

	var b strings.Builder
	fmt.Fprintf(&b, "%s%s \"%s\": must be ", badtype, name, arg)
	for n := range sorted {
		if n == len(sorted)-1 && n > 0 {
			b.WriteString("or ")
		}
		b.WriteString(prefix)
		b.WriteString(sorted[n])
		if n != len(sorted)-1 {
			b.WriteString(", ")
		}
	}
	i.SetResultString(b.String())
}

// GetEnum resolves o against table, allowing unambiguous abbreviations when
// flags has EnumAbbrev, and sets an error mentioning what on failure.
func (i *Interp) GetEnum(o *Obj, table []string, what string, flags int) (int, int) {
	bad := "bad "
	match := -1
	arg := o.String()

	for n := range table {
		if arg == table[n] {
			/* Found an exact match */
			return n, OK
		}
		if flags&EnumAbbrev != 0 {
			/* Accept an unambiguous abbreviation. Note that '-' doesn't
			 * constitute a valid abbreviation */
			if strings.HasPrefix(table[n], arg) {
				if arg == "-" {
					break
				}
				if match >= 0 {
					bad = "ambiguous "
					match = -1
					break
				}
				match = n
			}
		}
	}

	/* If we had an unambiguous partial match */
	if match >= 0 {
		return match, OK
	}

	if flags&ErrMsg != 0 {
		i.setFailedEnumResult(arg, bad, "", table, what)
	}
	return -1, ERR
}

/* ---------------------------------------------------------------------------
 * References
 * ------------------------------------------------------------------------ */

// referenceStringRep renders a reference object (C: UpdateStringOfReference).
func referenceStringRep(o *Obj) string {
	ref := o.aux.(*Reference)
	return fmt.Sprintf("<reference.<%s>.%020d>", ref.Tag, uint64(o.wide))
}

// isRefChar reports whether c is a valid reference tag character, i.e. in the
// range [_a-zA-Z0-9].
func isRefChar(c byte) bool {
	return c == '_' || u8.IsAlnum(c)
}

// setReferenceFromAny converts an object to the internal reference
// representation.
func (i *Interp) setReferenceFromAny(objPtr *Obj) int {
	if objPtr.kind == kindReference {
		return OK
	}

	str := objPtr.String()
	if strings.HasPrefix(str, "::") {
		str = str[2:]
	}

	/* The string rep of a reference is always exactly
	 * "<reference.<TAGTAGT>.00000000000000000000>" */
	badFormat := len(str) != referenceSpace ||
		!strings.HasPrefix(str, "<reference.<") ||
		str[12+referenceTagLen] != '>' || str[len(str)-1] != '>'
	if !badFormat {
		/* The tag can't contain chars other than a-zA-Z0-9 and '_' */
		for n := 0; n < referenceTagLen; n++ {
			if !isRefChar(str[12+n]) {
				badFormat = true
				break
			}
		}
	}
	var id int64
	if !badFormat {
		refID := str[14+referenceTagLen : 14+referenceTagLen+20]
		var ok bool
		id, ok, _ = stringToWide(refID, 10)
		badFormat = !ok
	}
	if badFormat {
		return i.Errorf("expected reference but got \"%#s\"", objPtr)
	}

	/* Check if the reference really exists */
	ref, ok := i.references[refKey(id)]
	if !ok {
		return i.Errorf("invalid reference id \"%#s\"", objPtr)
	}

	objPtr.freeIntRep()
	objPtr.kind = kindReference
	objPtr.wide = id
	objPtr.aux = ref
	return OK
}

// refKey returns the key a reference is stored under in the references table.
func refKey(id int64) string {
	return fmt.Sprint(id)
}
