/* Jim Tcl 0.84 ported from C to Go: the [package] command.
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

import (
	"os"
	"strings"
)

// packageVersion1 is the fixed, dummy version that every package has.
const packageVersion1 = "1.0"

// libPathVariable is the global variable holding the package search path
// (C: JIM_LIBPATH).
const libPathVariable = "auto_path"

// packageSubCmds is the subcommand table of [package].
var packageSubCmds = []SubCmd{
	{Cmd: "forget", Args: "package ...", Fn: packageCmdForget, MinArgs: 1, MaxArgs: -1},
	{Cmd: "provide", Args: "name ?version?", Fn: packageCmdProvide, MinArgs: 1, MaxArgs: 2},
	{Cmd: "require", Args: "name ?version?", Fn: packageCmdRequire, MinArgs: 1, MaxArgs: 2},
	{Cmd: "list", Fn: packageCmdNames, MinArgs: 0, MaxArgs: 0, Flags: ModFlagHidden},
	{Cmd: "names", Fn: packageCmdNames, MinArgs: 0, MaxArgs: 0},
}

// registerPackageCommands registers [package] (C: Jim_packageInit).
func registerPackageCommands(i *Interp) {
	i.RegisterSubCmd("package", packageSubCmds)
}

// PackageProvide records that the named package is provided by the interpreter
// (C: Jim_PackageProvide). An existing entry with an empty version is the
// automatic entry added while loading and may be replaced.
func (i *Interp) PackageProvide(name, version string, flags int) int {
	if ver, ok := i.packages.get(name); ok && ver.Len() != 0 {
		if flags&ErrMsg != 0 {
			return i.Errorf("package \"%s\" was already provided", name)
		}
		return ERR
	}
	i.packages.set(name, i.NewStringObj(version))
	return OK
}

// packageFind searches the directories of prefixListObj for the given package
// and returns the path of the file implementing it (C: JimFindPackage).
func (i *Interp) packageFind(prefixListObj *Obj, pkgName string) string {
	for _, prefixObjPtr := range i.ListElements(prefixListObj) {
		prefix := prefixObjPtr.String()

		// Loadable modules would be tried first, but dynamic loading is not
		// supported by this build.
		var buf string
		if prefix == "." {
			buf = pkgName + ".tcl"
		} else {
			buf = prefix + "/" + pkgName + ".tcl"
		}
		if _, err := os.Stat(buf); err == nil {
			return buf
		}
	}
	return ""
}

// packageLoad searches for the named package below every directory of
// auto_path and sources it if found (C: JimLoadPackage).
func (i *Interp) packageLoad(name string, flags int) int {
	retCode := ERR
	libPathObjPtr := i.GetGlobalVariableStr(libPathVariable, None)
	if libPathObjPtr == nil {
		return retCode
	}
	path := i.packageFind(libPathObjPtr, name)
	if path == "" {
		return retCode
	}

	/* Note: Even if the file fails to load, we consider the package loaded.
	 *       This prevents issues with recursion.
	 *       Use a dummy version of "" to signify this case.
	 */
	i.PackageProvide(name, "", 0)

	if strings.HasSuffix(path, ".tcl") {
		retCode = i.packageEvalFileGlobal(path)
	}
	if retCode != OK {
		// Upon failure, remove the dummy entry
		i.packages.delete(name)
	}
	return retCode
}

// PackageRequire loads the named package if it is not provided yet and returns
// its version in the interpreter result (C: Jim_PackageRequire).
func (i *Interp) PackageRequire(name string, flags int) int {
	// Start with an empty error string
	i.SetResult(i.emptyObj)

	if _, ok := i.packages.get(name); !ok {
		// Try to load the package.
		if retcode := i.packageLoad(name, flags); retcode != OK {
			if flags&ErrMsg != 0 {
				sep := ""
				if i.Result().Len() != 0 {
					sep = "\n"
				}
				// The completion code of the sourced file is returned as it
				// is, so a package that calls [exit] exits.
				i.Errorf("%#s%sCan't load package %s", i.Result(), sep, name)
				return retcode
			}
			return retcode
		}
		// In case the package did not 'package provide'
		i.PackageProvide(name, packageVersion1, 0)
	}
	ver, _ := i.packages.get(name)
	i.SetResult(ver)
	return OK
}

func packageCmdForget(i *Interp, argv []*Obj) int {
	for _, nameObj := range argv {
		i.packages.delete(nameObj.String())
	}
	return OK
}

// packageCmdProvide implements [package provide name ?version?]: it declares
// that the package is now present. The given version is ignored and the result
// is left empty.
func packageCmdProvide(i *Interp, argv []*Obj) int {
	return i.PackageProvide(argv[0].String(), packageVersion1, ErrMsg)
}

// packageCmdRequire implements [package require name ?version?]: it loads the
// package, ignoring the version, and sets the version as the result.
func packageCmdRequire(i *Interp, argv []*Obj) int {
	return i.PackageRequire(argv[0].String(), ErrMsg)
}

// packageCmdNames implements [package names] and the deprecated
// [package list]: it returns the list of known packages.
func packageCmdNames(i *Interp, argv []*Obj) int {
	listObjPtr := i.NewListObj(nil)
	for _, name := range i.packages.keys() {
		i.ListAppendElement(listObjPtr, i.NewStringObj(name))
	}
	i.SetResult(listObjPtr)
	return OK
}

// packageEvalFileGlobal evaluates a file in the global scope
// (C: Jim_EvalFileGlobal).
func (i *Interp) packageEvalFileGlobal(filename string) int {
	framePtr := i.framePtr
	i.framePtr = i.topFramePtr
	retval := i.EvalFile(filename)
	i.framePtr = framePtr
	return retval
}
