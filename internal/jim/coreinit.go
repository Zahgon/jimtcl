/* Jim Tcl 0.84 ported from C to Go: registration of the built-in commands.
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

// coreCommandOrder is the order of jim.c's Jim_CoreCommandsTable in this
// build (without the debug and taint commands, which are compiled out). The
// commands table is a hash table whose iteration order, visible through
// [info commands] and [info procs], depends on the order its entries were
// inserted, so the port registers the core commands in this order even though
// their implementations are spread over several files.
var coreCommandOrder = []string{
	"*", "+", "-", "/", "alias", "append", "apply", "break", "catch", "concat",
	"continue", "dict", "env", "error", "eval", "exists", "exit", "expr", "for",
	"foreach", "format", "global", "if", "incr", "info", "join", "lappend",
	"lassign", "lindex", "linsert", "list", "llength", "lmap", "local", "loop",
	"lrange", "lrepeat", "lreplace", "lreverse", "lsearch", "lset", "lsort",
	"lsubst", "proc", "puts", "rand", "range", "rename", "return", "scan", "set",
	"source", "split", "stacktrace", "string", "subst", "switch", "tailcall",
	"time", "timerate", "try", "unset", "upcall", "uplevel", "upvar", "while",
	"xtrace", "collect", "finalize", "getref", "ref", "setref",
}

// registerCoreTable registers the commands of Jim_CoreCommandsTable. They are
// implemented in several files, so they are registered file by file and the
// table is then rebuilt in the original's insertion order.
func (i *Interp) registerCoreTable() {
	i.registerCoreCommands()
	registerExprCommands(i)
	registerStringCommands(i)
	registerListCommands(i)
	registerDictCommands(i)
	registerFormatCommands(i)
	registerInfoCommands(i)
	registerMiscCommands(i)
	registerPutsCommand(i)
	i.reorderCoreTable()
}

// reorderCoreTable re-inserts the core commands in the order of
// Jim_CoreCommandsTable, which fixes the iteration order of the commands
// table. Commands registered later (the static extensions and the procedures
// of the Tcl extensions) are appended by their own registration, as in the C.
func (i *Interp) reorderCoreTable() {
	registered := i.commands
	i.commands = htable[*Cmd]{}
	for _, name := range coreCommandOrder {
		if cmd, ok := registered.get(name); ok {
			i.commands.set(name, cmd)
		}
	}
	for _, e := range registered.entries() {
		if _, ok := i.commands.get(e.key); !ok {
			i.commands.set(e.key, e.val)
		}
	}
}

// registerBuiltins registers every built-in command in the order the C build
// does: the core command table first, then the static extensions in the order
// of Jim_InitStaticExtensions, with the Tcl-implemented extensions evaluated
// in their place in that sequence.
func (i *Interp) registerBuiltins(exts []TclSource) {
	i.registerCoreTable()

	evaluated := make(map[string]bool, len(exts))
	eval := func(name string) {
		for _, ext := range exts {
			if ext.Package == name && !evaluated[name] {
				evaluated[name] = true
				if i.PackageProvide(ext.Package, "1.0", ErrMsg) == OK {
					// As the generated C wrappers do, an error is reported in
					// the result but does not stop initialisation.
					i.EvalSource(ext.FileName, 1, ext.Source)
				}
			}
		}
	}

	eval("stdlib")
	registerAioCommands(i)
	registerArrayCommands(i)
	registerClockCommands(i)
	registerFileCommands(i)
	registerNamespaceCommands(i)
	eval("nshelper")
	registerPackageCommands(i)
	registerRegexpCommands(i)
	eval("tclcompat")

	for _, ext := range exts {
		if !evaluated[ext.Package] {
			eval(ext.Package)
		}
	}
}
