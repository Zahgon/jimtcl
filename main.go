/*
 * jimsh - An interactive shell for Jim
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

// Command jimsh is the Jim Tcl shell.
package main

import (
	_ "embed"

	"github.com/msteveb/jimtcl/internal/jim"
)

// The Tcl-implemented static extensions and the jimsh init script. The C build
// turns these files into C source with make-c-ext.tcl; here they are embedded
// as they are and prepared by jim.StaticExtSource.
var (
	//go:embed stdlib.tcl
	stdlibTcl string
	//go:embed nshelper.tcl
	nshelperTcl string
	//go:embed tclcompat.tcl
	tclcompatTcl string
	//go:embed initjimsh.tcl
	initjimshTcl string
)

func main() {
	// The order is the one _load-static-exts.c registers them in.
	jim.Extensions = []jim.TclSource{
		jim.StaticExtSource("stdlib.tcl", stdlibTcl),
		jim.StaticExtSource("nshelper.tcl", nshelperTcl),
		jim.StaticExtSource("tclcompat.tcl", tclcompatTcl),
	}
	jim.InitJimsh = jim.StaticExtSource("initjimsh.tcl", initjimshTcl)
	jim.Main()
}
