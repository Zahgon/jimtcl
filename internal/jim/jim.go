/*
 * Jim - A small embeddable Tcl interpreter
 *
 * Copyright 2005 Salvatore Sanfilippo <antirez@invece.org>
 * Copyright 2005 Clemens Hintze <c.hintze@gmx.net>
 * Copyright 2005 patthoyts - Pat Thoyts <patthoyts@users.sf.net>
 * Copyright 2008 oharboe - Oyvind Harboe - oyvind.harboe@zylin.com
 * Copyright 2008 Andrew Lunn <andrew@lunn.ch>
 * Copyright 2008 Duane Ellis <openocd@duaneellis.com>
 * Copyright 2008 Uwe Klein <uklein@klein-messgeraete.de>
 * Copyright 2008 Steve Bennett <steveb@workware.net.au>
 * Copyright 2009 Nico Coesel <ncoesel@dealogic.nl>
 * Copyright 2009 Zachary T Welch zw@superlucidity.net
 * Copyright 2009 David Brownell
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

// Package jim implements the Jim Tcl interpreter: the object model, the script
// parser, the evaluation engine and the built-in commands, ported from the C
// implementation of Jim Tcl 0.84.
package jim

import (
	"fmt"
	"strings"
)

// Completion codes returned by commands and by script evaluation.
const (
	OK       = 0
	ERR      = 1
	RETURN   = 2
	BREAK    = 3
	CONTINUE = 4
	SIGNAL   = 5
	EXIT     = 6
	EVAL     = 7 // tailcall
)

// Flags accepted by the variable, command and enum lookup helpers.
const (
	None            = 0
	ErrMsg          = 1
	EnumAbbrev      = 2
	Unshared        = 4
	SubstNoVar      = 1
	SubstNoCmd      = 2
	SubstNoEsc      = 4
	SubstFlag       = 128
	CaseSens        = 0
	NoCase          = 1
	OptEnd          = 2
	DictMatchKeys   = 0x0001
	DictMatchValues = 0x0002
)

// Nesting limits used for infinite-recursion detection.
const (
	MaxCallFrameDepth = 1000
	MaxEvalDepth      = 2000
)

// Command flags.
const (
	CmdIsProc    = 1
	CmdIsChannel = 2
	CmdIsAlias   = 4
)

// Version is the interpreter version, as reported by [info version].
const Version = "0.84"

// objKind identifies which internal representation of an Obj is valid.
type objKind int

const (
	kindNone objKind = iota // string only
	kindInt
	kindDouble
	kindCoercedDouble // an integer value used as a double; keeps the string rep
	kindBool
	kindList
	kindDict
	kindIndex
	kindReturnCode
	kindScript
	kindSubst
	kindExpr
	kindRegexp
	kindReference
	kindInterpolated
	kindVariable
	kindCommand
	kindSource
	kindScriptLine
	kindFormat
	kindDictSubst
	kindAio
	kindGlobMatch
)

// Obj is a Jim object: a cached string representation plus at most one typed
// internal representation. Objects are treated as immutable values; commands
// that modify a value build a new object, which is what the C implementation
// does whenever an object is shared.
type Obj struct {
	bytes []byte // string representation; valid only when strValid is true
	// charLen caches the UTF-8 length, -1 when not yet computed.
	charLen  int
	strValid bool
	kind     objKind

	wide int64
	dbl  float64
	list []*Obj
	dict *Dict
	aux  any // internal representation of the remaining types

	// Source information attached to objects parsed from a script.
	fileName *Obj
	lineNr   int
}

// Dict is Jim's ordered dictionary: key and value objects in insertion order in
// table, indexed by an open-addressing hash table.
type Dict struct {
	table    []*Obj // key, value, key, value, ...
	ht       []dictHashEntry
	sizeMask uint32
	uniq     uint32
	dummy    int
}

type dictHashEntry struct {
	offset int
	hash   uint32
}

// CmdProc is the implementation of a built-in command. It returns a completion
// code and leaves its result in the interpreter.
type CmdProc func(i *Interp, argv []*Obj) int

// Cmd is a command: either a native (Go) command or a Tcl procedure.
type Cmd struct {
	flags      int
	prevCmd    *Cmd // previous definition, for [local]
	cmdNameObj *Obj
	deleted    bool

	// native command
	proc     CmdProc
	usage    string
	help     string
	minArgs  int
	maxArgs  int // -1 for unlimited
	subCmds  []SubCmd
	privData any

	// Tcl procedure
	argList    *Obj
	body       *Obj
	staticVars *htable[*VarVal]
	argListLen int
	reqArity   int
	optArity   int
	argsPos    int
	upcall     bool
	args       []ProcArg
	nsObj      *Obj
}

// ProcArg is one formal parameter of a Tcl procedure.
type ProcArg struct {
	Name    *Obj
	Default *Obj
}

// VarVal holds a variable's value, or a link to a variable in another frame.
type VarVal struct {
	Obj       *Obj
	LinkFrame *CallFrame
	RefCount  int
}

// CallFrame is a procedure call frame holding local variables.
type CallFrame struct {
	id            uint64
	level         int
	vars          htable[*VarVal]
	staticVars    *htable[*VarVal]
	parent        *CallFrame
	argv          []*Obj
	procArgsObj   *Obj
	procBodyObj   *Obj
	nsObj         *Obj
	localCommands []*Obj
	tailcallObj   *Obj
	tailcallCmd   *Cmd
}

// EvalFrame records one command invocation, for [info frame] and stack traces.
type EvalFrame struct {
	framePtr  *CallFrame
	level     int
	procLevel int
	cmd       *Cmd
	parent    *EvalFrame
	argv      []*Obj
	scriptObj *Obj
}

// Interp is a Jim Tcl interpreter.
type Interp struct {
	result             *Obj
	currentFileNameObj *Obj
	breakLevel         int
	maxCallFrameDepth  int
	maxEvalDepth       int
	evalDepth          int
	returnCode         int
	returnLevel        int
	exitCode           int
	id                 int64
	framePtr           *CallFrame
	topFramePtr        *CallFrame
	commands           htable[*Cmd]
	procEpoch          uint64
	callFrameEpoch     uint64
	local              int
	safeexpr           bool
	topEvalFrame       EvalFrame
	evalFrame          *EvalFrame
	procLevel          int
	nullScriptObj      *Obj
	emptyObj           *Obj
	trueObj            *Obj
	falseObj           *Obj
	referenceNextID    uint64
	references         map[string]*Reference
	stackTrace         *Obj
	errorProc          *Obj
	unknown            *Obj
	deferObj           *Obj
	traceCmdObj        *Obj
	unknownCalled      bool
	hasErrorStackTrace bool
	prng               *prngState
	packages           htable[*Obj]
	signalLevel        int
	channels           any
	randSeeded         bool

	// Added by the engine area.
	unknownDepth int // C: unknown_called, limits recursive [unknown] calls
	cmdPrivData  any // C: cmdPrivData, private data of the running native command
}

// Reference is the target of a Jim reference (see [ref] and [getref]).
type Reference struct {
	Obj          *Obj
	Tag          string
	FinalizerCmd *Obj
}

type prngState struct {
	sbox [256]byte
	i, j byte
}

/* ---------------------------------------------------------------------------
 * Object creation and the string representation
 * ------------------------------------------------------------------------ */

// NewObj returns an empty object with no valid representation.
func NewObj() *Obj {
	return &Obj{charLen: -1}
}

// NewStringObj returns a new string object with the given contents.
func (i *Interp) NewStringObj(s string) *Obj {
	return &Obj{bytes: []byte(s), strValid: true, charLen: -1, kind: kindNone}
}

// NewStringObjBytes returns a new string object holding the given bytes.
func (i *Interp) NewStringObjBytes(b []byte) *Obj {
	cp := make([]byte, len(b))
	copy(cp, b)
	return &Obj{bytes: cp, strValid: true, charLen: -1, kind: kindNone}
}

// NewIntObj returns a new integer object.
func (i *Interp) NewIntObj(w int64) *Obj {
	return &Obj{kind: kindInt, wide: w, charLen: -1}
}

// NewDoubleObj returns a new floating-point object.
func (i *Interp) NewDoubleObj(d float64) *Obj {
	return &Obj{kind: kindDouble, dbl: d, charLen: -1}
}

// EmptyObj returns the shared empty string object.
func (i *Interp) EmptyObj() *Obj { return i.emptyObj }

// String returns the object's string representation, generating it if needed.
func (o *Obj) String() string {
	return string(o.Bytes())
}

// Bytes returns the object's string representation as bytes. The caller must
// not modify the returned slice.
func (o *Obj) Bytes() []byte {
	if !o.strValid {
		o.updateStringRep()
	}
	return o.bytes
}

// Len returns the length of the string representation in bytes.
func (o *Obj) Len() int { return len(o.Bytes()) }

// setStringRep installs a freshly generated string representation.
func (o *Obj) setStringRep(s string) {
	o.bytes = []byte(s)
	o.strValid = true
	o.charLen = -1
}

// invalidateStringRep discards a cached string representation.
func (o *Obj) invalidateStringRep() {
	o.bytes = nil
	o.strValid = false
	o.charLen = -1
}

/* ---------------------------------------------------------------------------
 * Results and errors
 * ------------------------------------------------------------------------ */

// SetResult sets the interpreter result.
func (i *Interp) SetResult(o *Obj) { i.result = o }

// SetResultString sets the interpreter result to a string.
func (i *Interp) SetResultString(s string) { i.result = i.NewStringObj(s) }

// SetResultInt sets the interpreter result to an integer.
func (i *Interp) SetResultInt(w int64) { i.result = i.NewIntObj(w) }

// SetResultBool sets the interpreter result to 0 or 1.
func (i *Interp) SetResultBool(b bool) {
	if b {
		i.result = i.NewIntObj(1)
	} else {
		i.result = i.NewIntObj(0)
	}
}

// Result returns the current interpreter result.
func (i *Interp) Result() *Obj { return i.result }

// Errorf sets the interpreter result to a formatted error message and returns
// ERR. It is the equivalent of Jim_SetResultFormatted() followed by a JIM_ERR
// return; "%#s" formats a *Obj using its string representation.
func (i *Interp) Errorf(format string, args ...any) int {
	i.result = i.NewStringObj(formatMessage(format, args...))
	return ERR
}

// formatMessage expands the C implementation's message formats: %s for a
// string, %#s for an object and %c for a character.
func formatMessage(format string, args ...any) string {
	var b strings.Builder
	ai := 0
	for n := 0; n < len(format); n++ {
		if format[n] != '%' || n+1 >= len(format) {
			b.WriteByte(format[n])
			continue
		}
		spec := format[n+1]
		width := 1
		if spec == '#' && n+2 < len(format) {
			spec = format[n+2]
			width = 2
		}
		if ai >= len(args) {
			b.WriteByte(format[n])
			continue
		}
		switch spec {
		case 's':
			switch v := args[ai].(type) {
			case *Obj:
				b.WriteString(v.String())
			case string:
				b.WriteString(v)
			default:
				b.WriteString(fmt.Sprint(v))
			}
			ai++
			n += width
		case 'c':
			b.WriteString(fmt.Sprintf("%c", args[ai]))
			ai++
			n += width
		case 'd', 'l':
			b.WriteString(fmt.Sprint(args[ai]))
			ai++
			n += width
		case '%':
			b.WriteByte('%')
			n++
		default:
			b.WriteByte(format[n])
		}
	}
	return b.String()
}
