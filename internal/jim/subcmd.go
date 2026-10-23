/*
 * Makes it easy to support "ensembles". i.e. commands with subcommands
 * like [string] and [array]
 *
 * (c) 2008 Steve Bennett <steveb@workware.net.au>
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

// Subcommand table flags.
const (
	ModFlagHidden   = 0x0001 // don't show the subcommand in usage or -commands
	ModFlagFullArgv = 0x0002 // the subcommand proc is called with the full argv
)

// subCmdBadArgs is returned by a subcommand function whose arguments are wrong;
// the dispatcher then reports the subcommand's usage.
const subCmdBadArgs = -1

// SubCmd describes one subcommand of an ensemble command such as [string].
type SubCmd struct {
	Cmd     string  // name of the subcommand
	Args    string  // textual description of the allowed arguments
	Fn      CmdProc // implementation; nil for a table used only for lookup
	MinArgs int     // minimum number of arguments after the subcommand
	MaxArgs int     // maximum, -1 for no limit, or -n for a multiple of n
	Flags   int     // ModFlag...
}

// dummySubCmd is returned for -help and -commands, whose result is already set.
var dummySubCmd = &SubCmd{Cmd: "dummy", Fn: func(i *Interp, argv []*Obj) int { return OK }, Flags: ModFlagHidden}

// subCmdList returns the non-hidden subcommand names, sorted with [lsort] and
// joined with sep.
func (i *Interp) subCmdList(table []SubCmd, sep string) *Obj {
	listObj := i.NewListObj(nil)
	for n := range table {
		if table[n].Flags&ModFlagHidden == 0 {
			i.ListAppendElement(listObj, i.NewStringObj(table[n].Cmd))
		}
	}
	// There is no direct API to sort a list, so just invoke lsort here.
	if i.EvalObjVector([]*Obj{i.NewStringObj("lsort"), listObj}) == OK {
		return i.ListJoin(i.Result(), sep)
	}
	return i.Result()
}

func (i *Interp) badSubCmd(table []SubCmd, kind string, cmd, subcmd *Obj) {
	i.Errorf("%#s, %s command \"%#s\": should be %#s", cmd, kind, subcmd, i.subCmdList(table, ", "))
}

func (i *Interp) showCmdUsage(table []SubCmd, argv []*Obj) {
	i.Errorf("Usage: \"%#s command ... \", where command is one of: %#s", argv[0], i.subCmdList(table, ", "))
}

func (i *Interp) addCmdUsage(ct *SubCmd, cmd *Obj) {
	var b strings.Builder
	b.WriteString(i.Result().String())
	if cmd != nil {
		b.WriteString(cmd.String())
		b.WriteString(" ")
	}
	b.WriteString(ct.Cmd)
	if ct.Args != "" {
		b.WriteString(" ")
		b.WriteString(ct.Args)
	}
	i.SetResultString(b.String())
}

// SubCmdArgError sets an error result describing the usage of a subcommand.
func (i *Interp) SubCmdArgError(ct *SubCmd, subcmd *Obj) {
	i.SetResultString("wrong # args: should be \"")
	i.addCmdUsage(ct, subcmd)
	i.SetResultString(i.Result().String() + "\"")
}

// ParseSubCmd looks up the subcommand named by argv[1] in table, handling the
// built-in -help and -commands options and unambiguous abbreviations. It
// returns nil with an error result when the lookup or the argument count fails.
func (i *Interp) ParseSubCmd(table []SubCmd, argv []*Obj) *SubCmd {
	if len(argv) < 2 {
		i.Errorf("wrong # args: should be \"%#s command ...\"\nUse \"%#s -help ?command?\" for help", argv[0], argv[0])
		return nil
	}

	cmd := argv[1]
	help := 0

	if cmd.String() == "-help" {
		if len(argv) == 2 {
			// Usage for the command, not the subcommand
			i.showCmdUsage(table, argv)
			return dummySubCmd
		}
		help = 1
		cmd = argv[2]
	}

	if cmd.String() == "-commands" {
		i.SetResult(i.subCmdList(table, " "))
		return dummySubCmd
	}

	cmdstr := cmd.String()
	var ct *SubCmd
	var partial *SubCmd
	for n := range table {
		if cmdstr == table[n].Cmd {
			ct = &table[n]
			break
		}
		if strings.HasPrefix(table[n].Cmd, cmdstr) {
			if partial != nil {
				// Ambiguous
				if help != 0 {
					i.showCmdUsage(table, argv)
					return dummySubCmd
				}
				i.badSubCmd(table, "ambiguous", argv[0], argv[1+help])
				return nil
			}
			partial = &table[n]
		}
	}

	// If we had an unambiguous partial match
	if ct == nil && partial != nil {
		ct = partial
	}

	if ct == nil {
		if help != 0 {
			i.showCmdUsage(table, argv)
			return dummySubCmd
		}
		i.badSubCmd(table, "unknown", argv[0], argv[1+help])
		return nil
	}

	if help != 0 {
		i.SetResultString("Usage: ")
		i.addCmdUsage(ct, argv[0])
		return dummySubCmd
	}

	argsok := true
	switch {
	case len(argv)-2 < ct.MinArgs:
		argsok = false
	case ct.MaxArgs >= 0 && len(argv)-2 > ct.MaxArgs:
		argsok = false
	case ct.MaxArgs < -1 && (len(argv)-2)%-ct.MaxArgs != 0:
		argsok = false
	}
	if !argsok {
		i.SetResultString("wrong # args: should be \"")
		i.addCmdUsage(ct, argv[0])
		i.SetResultString(i.Result().String() + "\"")
		return nil
	}

	return ct
}

// CallSubCmd invokes the subcommand returned by ParseSubCmd.
func (i *Interp) CallSubCmd(ct *SubCmd, argv []*Obj) int {
	if ct == nil {
		return ERR
	}
	var ret int
	if ct.Flags&ModFlagFullArgv != 0 {
		ret = ct.Fn(i, argv)
	} else {
		ret = ct.Fn(i, argv[2:])
	}
	if ret < 0 {
		i.SubCmdArgError(ct, argv[0])
		ret = ERR
	}
	return ret
}

// SubCmdProc is the command procedure of an ensemble command registered with
// RegisterSubCmd.
func subCmdProc(table []SubCmd) CmdProc {
	return func(i *Interp, argv []*Obj) int {
		return i.CallSubCmd(i.ParseSubCmd(table, argv), argv)
	}
}

// RegisterSubCmd registers an ensemble command dispatching to table.
func (i *Interp) RegisterSubCmd(name string, table []SubCmd) *Cmd {
	cmd := i.RegisterCmd(name, "subcommand ?arg ...?", 1, -1, subCmdProc(table), 0)
	cmd.subCmds = table
	return cmd
}
