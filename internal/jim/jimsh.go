/* Jim Tcl 0.84 ported from C to Go: the jimsh entry point (C: jimsh.c) and the
 * C library stdout stream it writes through.
 *
 * Copyright 2005 Salvatore Sanfilippo <antirez@invece.org>
 * Copyright 2009 Steve Bennett <steveb@workware.net.au>
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
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"
)

// Extensions holds the Tcl-implemented static extensions, in the order the C
// build registers them. The jimsh command fills it in from the scripts it
// embeds at the repository root.
var Extensions []TclSource

// InitJimsh is initjimsh.tcl, the pseudo-package jimsh evaluates once the
// interpreter is up (C: Jim_initjimshInit).
var InitJimsh TclSource

// StaticExtSource prepares one embedded Tcl script for evaluation, applying
// the transformation the build's make-c-ext.tcl applies: a comment line
// becomes an empty line, so that the line numbers in error messages still
// match the original file.
func StaticExtSource(fileName, source string) TclSource {
	lines := strings.Split(strings.ReplaceAll(source, "\r", ""), "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		// The generated C has one string per line read, so a trailing newline
		// does not produce an extra line.
		lines = lines[:n-1]
	}
	for n, line := range lines {
		if trimmed := strings.TrimLeft(line, " \t"); strings.HasPrefix(trimmed, "#") {
			lines[n] = ""
		}
	}
	var b strings.Builder
	for _, line := range lines {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return TclSource{
		Package:  strings.TrimSuffix(fileName, ".tcl"),
		FileName: fileName,
		Source:   b.String(),
	}
}

/* ---------------------------------------------------------------------------
 * The C library's stdout stream
 * ------------------------------------------------------------------------ */

// ioCStdout emulates the C library's stdout stream. jimsh.c and jim.c's
// [puts] write through printf()/fwrite() rather than through the [stdout]
// channel, and the C runtime flushes that stream at process exit, after
// Jim_FreeInterp() has flushed the channels. Reproducing both buffers keeps
// interleaved output in the same order when stdout is redirected.
type ioCStdout struct {
	buf []byte
	// unbuffered is set when stdout is a terminal. The C library line buffers
	// in that case, and since every write here is followed by a newline that
	// amounts to writing straight through.
	unbuffered bool
	checked    bool
}

var cStdout ioCStdout

func (s *ioCStdout) write(b []byte) {
	if !s.checked {
		s.unbuffered = aioIsatty(1)
		s.checked = true
	}
	if s.unbuffered {
		syscall.Write(1, b)
		return
	}
	s.buf = append(s.buf, b...)
}

func (s *ioCStdout) printf(format string, args ...any) {
	s.write([]byte(fmt.Sprintf(format, args...)))
}

func (s *ioCStdout) flush() {
	for len(s.buf) > 0 {
		n, err := syscall.Write(1, s.buf)
		if err != nil || n <= 0 {
			s.buf = nil
			return
		}
		s.buf = s.buf[n:]
	}
}

/* ---------------------------------------------------------------------------
 * The entry point
 * ------------------------------------------------------------------------ */

// jimSetArgv populates the argv and argc global variables (C: JimSetArgv).
func jimSetArgv(i *Interp, argv []string) {
	listObj := i.NewListObj(nil)
	for _, a := range argv {
		i.ListAppendElement(listObj, i.NewStringObj(a))
	}
	i.SetVariableStr("argv", listObj)
	i.SetVariableStr("argc", i.NewIntObj(int64(len(argv))))
}

// jimPrintErrorMessage writes the error and its stack trace to stderr, which
// is unbuffered in the C library (C: JimPrintErrorMessage).
func jimPrintErrorMessage(i *Interp) {
	i.MakeErrorMessage()
	os.Stderr.WriteString(i.Result().String() + "\n")
}

// jimUsage prints the --help text (C: usage).
func jimUsage(executableName string) {
	cStdout.printf("jimsh version %s\n", Version)
	cStdout.printf("Usage: %s\n", executableName)
	cStdout.printf("or   : %s [options] [filename]\n", executableName)
	cStdout.printf("\n")
	cStdout.printf("Without options: Interactive mode\n")
	cStdout.printf("\n")
	cStdout.printf("Options:\n")
	cStdout.printf("      --version  : prints the version string\n")
	cStdout.printf("      --help     : prints this text\n")
	cStdout.printf("      -e CMD     : executes command CMD\n")
	cStdout.printf("                   NOTE: all subsequent options will be passed as arguments to the command\n")
	cStdout.printf("    [filename|-] : executes the script contained in the named file, or from stdin if \"-\"\n")
	cStdout.printf("                   NOTE: all subsequent options will be passed to the script\n\n")
}

// jimExit flushes both output buffers in the order the C implementation does -
// the channels when the interpreter is freed, the C library stream at process
// exit - and exits.
func jimExit(i *Interp, retcode int) {
	if i != nil {
		i.FlushChannels()
	}
	cStdout.flush()
	os.Exit(retcode)
}

// Main is the entry point of the jimsh command (C: jimsh.c main()).
func Main() {
	argv := os.Args
	origArgv0 := argv[0]

	// Parse initial arguments before the interpreter is started
	if len(argv) > 1 && argv[1] == "--version" {
		cStdout.printf("%s\n", Version)
		jimExit(nil, 0)
	} else if len(argv) > 1 && argv[1] == "--help" {
		jimUsage(argv[0])
		jimExit(nil, 0)
	}

	i := NewInterp(Extensions...)

	i.SetVariableStrWithStr("jim::argv0", origArgv0)
	interactive := "0"
	if len(argv) == 1 {
		interactive = "1"
	}
	i.SetVariableStrWithStr("tcl_interactive", interactive)
	// linenoise is not part of this build
	i.SetVariableStrWithStr("jim::lineedit", "0")

	retcode := i.initJimsh()

	evalStdin := false
	if len(argv) == 1 {
		// Executable name is the only argument - start interactive prompt
		if retcode == ERR {
			jimPrintErrorMessage(i)
		}
		if retcode != EXIT {
			jimSetArgv(i, nil)
			if !aioIsatty(0) {
				// Just read from stdin and evaluate
				evalStdin = true
			} else {
				retcode = i.interactivePrompt()
			}
		}
	}
	if len(argv) > 1 || evalStdin {
		// Additional arguments - interpret them
		switch {
		case evalStdin:
			retcode = i.Eval("eval [info source [stdin read] stdin 1]")
		case len(argv) > 2 && argv[1] == "-e":
			// Evaluate code in subsequent argument
			jimSetArgv(i, argv[3:])
			retcode = i.Eval(argv[2])
			if retcode != ERR {
				cStdout.write(i.Result().Bytes())
				cStdout.write([]byte{'\n'})
			}
		default:
			i.SetVariableStr("argv0", i.NewStringObj(argv[1]))
			jimSetArgv(i, argv[2:])
			if argv[1] == "-" {
				retcode = i.Eval("eval [info source [stdin read] stdin 1]")
			} else {
				retcode = i.EvalFile(argv[1])
			}
		}
		if retcode == ERR {
			jimPrintErrorMessage(i)
		}
	}

	switch retcode {
	case EXIT:
		retcode = i.ExitCode()
	case ERR:
		retcode = 1
	default:
		retcode = 0
	}
	jimExit(i, retcode)
}

// initJimsh provides the initjimsh pseudo-package and evaluates
// initjimsh.tcl (C: Jim_initjimshInit, generated by make-c-ext.tcl).
func (i *Interp) initJimsh() int {
	if i.PackageProvide(InitJimsh.Package, "1.0", ErrMsg) != OK {
		return ERR
	}
	return i.EvalSource(InitJimsh.FileName, 1, InitJimsh.Source)
}

// ioMaxLineLen is the length of one input line of the interactive prompt
// (C: MAX_LINE_LEN).
const ioMaxLineLen = 1024

// interactivePrompt is the read-eval-print loop of an interactive jimsh
// without line editing (C: Jim_InteractivePrompt).
func (i *Interp) interactivePrompt() int {
	cStdout.printf("Welcome to Jim version %s\n", Version)
	i.SetVariableStrWithStr("tcl_interactive", "1")

	in := bufio.NewReaderSize(os.Stdin, ioMaxLineLen)
	retcode := OK
	for {
		prompt := ". "
		if retcode != OK {
			if name := ReturnCodeName(retcode); strings.HasPrefix(name, "?") {
				prompt = fmt.Sprintf("[%d] . ", retcode)
			} else {
				prompt = fmt.Sprintf("[%s] . ", name)
			}
		}

		var script strings.Builder
		eof := false
		for {
			cStdout.printf("%s", prompt)
			cStdout.flush()
			line, err := in.ReadString('\n')
			if err != nil && line == "" {
				eof = true
				break
			}
			if script.Len() != 0 {
				// Line continuation
				script.WriteByte('\n')
			}
			script.WriteString(strings.TrimRight(line, "\r\n"))
			complete, state := i.ScriptIsComplete(i.NewStringObj(script.String()))
			if complete {
				break
			}
			prompt = fmt.Sprintf("%c> ", state)
		}
		if eof {
			return OK
		}

		retcode = i.EvalObj(i.NewStringObj(script.String()))
		if retcode == EXIT {
			return retcode
		}
		if retcode == ERR {
			i.MakeErrorMessage()
		}
		if result := i.Result().Bytes(); len(result) != 0 {
			cStdout.write(result)
			cStdout.write([]byte{'\n'})
		}
	}
}
