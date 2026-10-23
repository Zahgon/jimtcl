/* Jim Tcl 0.84 ported from C to Go: the [file] ensemble, [pwd] and [cd]
 * (C: jim-file.c).
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
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// maxPathLen is the C implementation's MAXPATHLEN, the size of the buffers
// [file join] and [file normalize] build their result in.
const maxPathLen = 4096

// fileGetFileType names the type of a file from its mode word
// (C: JimGetFileType).
func fileGetFileType(mode uint32) string {
	switch mode & syscall.S_IFMT {
	case syscall.S_IFREG:
		return "file"
	case syscall.S_IFDIR:
		return "directory"
	case syscall.S_IFCHR:
		return "characterSpecial"
	case syscall.S_IFBLK:
		return "blockSpecial"
	case syscall.S_IFIFO:
		return "fifo"
	case syscall.S_IFLNK:
		return "link"
	case syscall.S_IFSOCK:
		return "socket"
	}
	return "unknown"
}

// fileStoreStatData breaks a stat buffer out into a key/value list, optionally
// merging it into the array variable varName (C: Jim_FileStoreStatData).
func (i *Interp) fileStoreStatData(varName *Obj, sb *syscall.Stat_t) int {
	// Just use a list to store the data
	listObj := i.NewListObj(nil)
	appendElement := func(key string, value int64) {
		i.ListAppendElement(listObj, i.NewStringObj(key))
		i.ListAppendElement(listObj, i.NewIntObj(value))
	}

	appendElement("dev", int64(sb.Dev))
	appendElement("ino", int64(sb.Ino))
	appendElement("mode", int64(sb.Mode))
	appendElement("nlink", int64(sb.Nlink))
	appendElement("uid", int64(sb.Uid))
	appendElement("gid", int64(sb.Gid))
	appendElement("size", sb.Size)
	appendElement("atime", sb.Atim.Sec)
	appendElement("mtime", sb.Mtim.Sec)
	appendElement("ctime", sb.Ctim.Sec)
	appendElement("mtimeus", fileMtimeUsec(sb))
	i.ListAppendElement(listObj, i.NewStringObj("type"))
	i.ListAppendElement(listObj, i.NewStringObj(fileGetFileType(uint32(sb.Mode))))

	// Was a variable specified?
	if varName != nil {
		if objPtr := i.GetVariable(varName, None); objPtr != nil {
			merged := i.fileDictMerge(objPtr, listObj)
			if merged == nil {
				// This message matches the one from Tcl
				return i.Errorf("can't set \"%#s(dev)\": variable isn't array", varName)
			}
			merged.invalidateStringRep()
			listObj = merged
		}
		i.SetVariable(varName, listObj)
	}

	// And also return the value
	i.SetResult(listObj)
	return OK
}

// fileMtimeUsec is the modification time in microseconds (C: STAT_MTIME_US).
func fileMtimeUsec(sb *syscall.Stat_t) int64 {
	return sb.Mtim.Sec*1000000 + sb.Mtim.Nsec/1000
}

// fileDictMerge builds a dictionary from the given key/value lists, returning
// nil if either is not a valid dictionary (C: Jim_DictMerge).
func (i *Interp) fileDictMerge(objv ...*Obj) *Obj {
	objPtr := i.NewDictObj(nil)
	for _, o := range objv {
		// DictPairs returns the number of elements, and a nil table with a
		// non-zero length when the value is not a valid dictionary.
		table, tablelen := i.DictPairs(o)
		if tablelen != 0 && table == nil {
			return nil
		}
		for n := 0; n+1 < len(table); n += 2 {
			i.DictAddElement(objPtr, table[n], table[n+1])
		}
	}
	return objPtr
}

// filePathLenNoTrailingSlashes returns the length of path with any trailing
// slashes removed (C: JimPathLenNoTrailingSlashes).
func filePathLenNoTrailingSlashes(path string) int {
	i := len(path)
	for i > 1 && path[i-1] == '/' {
		i--
	}
	return i
}

// fileStripTrailingSlashes returns path without any trailing slash
// (C: JimStripTrailingSlashes).
func fileStripTrailingSlashes(path string) string {
	return path[:filePathLenNoTrailingSlashes(path)]
}

func fileCmdDirname(i *Interp, argv []*Obj) int {
	path := fileStripTrailingSlashes(argv[0].String())
	p := strings.LastIndexByte(path, '/')

	switch {
	case p < 0:
		i.SetResultString(".")
	case p == len(path)-1:
		// Trailing slash so do nothing
		i.SetResultString(path)
	case p == 0:
		i.SetResultString("/")
	default:
		// Strip any trailing slashes from the result
		i.SetResultString(path[:filePathLenNoTrailingSlashes(path[:p])])
	}
	return OK
}

func fileCmdSplit(i *Interp, argv []*Obj) int {
	listObj := i.NewListObj(nil)
	path := argv[0].String()

	if strings.HasPrefix(path, "/") {
		i.ListAppendElement(listObj, i.NewStringObj("/"))
	}
	for {
		path = strings.TrimLeft(path, "/")
		if path == "" {
			break
		}
		pt := strings.IndexByte(path, '/')
		if pt < 0 {
			i.ListAppendElement(listObj, i.NewStringObj(path))
			break
		}
		i.ListAppendElement(listObj, i.NewStringObj(path[:pt]))
		path = path[pt:]
	}
	i.SetResult(listObj)
	return OK
}

func fileCmdRootname(i *Interp, argv []*Obj) int {
	path := argv[0].String()
	lastSlash := strings.LastIndexByte(path, '/')
	p := strings.LastIndexByte(path, '.')

	if p < 0 || lastSlash > p {
		i.SetResult(argv[0])
	} else {
		i.SetResultString(path[:p])
	}
	return OK
}

func fileCmdExtension(i *Interp, argv []*Obj) int {
	path := fileStripTrailingSlashes(argv[0].String())
	lastSlash := strings.LastIndexByte(path, '/')
	p := strings.LastIndexByte(path, '.')

	if p < 0 || lastSlash >= p {
		i.SetResultString("")
	} else {
		i.SetResultString(path[p:])
	}
	return OK
}

func fileCmdTail(i *Interp, argv []*Obj) int {
	path := fileStripTrailingSlashes(argv[0].String())
	if lastSlash := strings.LastIndexByte(path, '/'); lastSlash >= 0 {
		i.SetResultString(path[lastSlash+1:])
	} else {
		i.SetResultString(path)
	}
	return OK
}

// fileCmdNormalize resolves a path with realpath(3) (C: file_cmd_normalize).
func fileCmdNormalize(i *Interp, argv []*Obj) int {
	newname, err := fileRealPath(argv[0].String())
	if err != nil {
		return i.Errorf("can't normalize \"%#s\": %s", argv[0], ioStrerror(err))
	}
	i.SetResultString(newname)
	return OK
}

// fileRealPath is realpath(3): it makes the path absolute and resolves every
// ".", ".." and symbolic link. Like realpath(), it fails unless every
// component of the path exists.
func fileRealPath(path string) (string, error) {
	if path == "" {
		return "", syscall.ENOENT
	}
	if !strings.HasPrefix(path, "/") {
		cwd, err := syscall.Getwd()
		if err != nil {
			return "", err
		}
		path = cwd + "/" + path
	}
	return filepath.EvalSymlinks(path)
}

// fileCmdJoin joins path components, restarting at any absolute one
// (C: file_cmd_join).
func fileCmdJoin(i *Interp, argv []*Obj) int {
	newname := make([]byte, 0, maxPathLen+1)

	for _, a := range argv {
		part := a.Bytes()

		if len(part) > 0 && part[0] == '/' {
			// Absolute component, so go back to the start
			newname = newname[:0]
		} else if len(part) > 0 && part[0] == '.' {
			if len(part) > 1 && part[1] == '/' {
				part = part[2:]
			} else if len(part) == 1 && len(newname) != 0 {
				// Adding '.' to an existing path does nothing
				continue
			}
		}

		// Add a slash if needed
		if len(newname) != 0 && newname[len(newname)-1] != '/' {
			newname = append(newname, '/')
		}
		if len(part) != 0 {
			if len(newname)+len(part) >= maxPathLen {
				i.SetResultString("Path too long")
				return ERR
			}
			newname = append(newname, part...)
		}

		// Remove a slash if needed
		if len(newname) > 1 && newname[len(newname)-1] == '/' {
			newname = newname[:len(newname)-1]
		}
	}
	i.SetResult(i.NewStringObjBytes(newname))
	return OK
}

// fileAccess is access(2) (C: file_access).
func fileAccess(i *Interp, filename *Obj, mode uint32) int {
	i.SetResultBool(syscall.Access(filename.String(), mode) == nil)
	return OK
}

func fileCmdReadable(i *Interp, argv []*Obj) int   { return fileAccess(i, argv[0], 4) }
func fileCmdWritable(i *Interp, argv []*Obj) int   { return fileAccess(i, argv[0], 2) }
func fileCmdExecutable(i *Interp, argv []*Obj) int { return fileAccess(i, argv[0], 1) }
func fileCmdExists(i *Interp, argv []*Obj) int     { return fileAccess(i, argv[0], 0) }

func fileCmdDelete(i *Interp, argv []*Obj) int {
	force := argv[0].String() == "-force"
	if force || argv[0].String() == "--" {
		argv = argv[1:]
	}

	for _, a := range argv {
		path := a.String()
		err := syscall.Unlink(path)
		if err == nil || err == syscall.ENOENT {
			continue
		}
		err = syscall.Rmdir(path)
		if err == nil {
			continue
		}
		// Maybe try using the script helper from tclcompat.tcl
		if !force || i.evalPrefix("file delete force", a) != OK {
			return i.Errorf("couldn't delete file \"%s\": %s", path, ioStrerror(err))
		}
	}
	return OK
}

// evalPrefix invokes a command whose name is itself several words, as the
// script helpers in tclcompat.tcl are (C: Jim_EvalPrefix).
func (i *Interp) evalPrefix(prefix string, args ...*Obj) int {
	argv := make([]*Obj, 0, 1+len(args))
	argv = append(argv, i.NewStringObj(prefix))
	argv = append(argv, args...)
	return i.EvalObjVector(argv)
}

// fileMkdirAll creates a directory and all the intermediate paths it needs
// (C: mkdir_all).
func fileMkdirAll(path string) error {
	for {
		err := syscall.Mkdir(path, 0755)
		if err == nil {
			return nil
		}
		if err == syscall.ENOENT {
			// Create the parent and try again
			slash := strings.LastIndexByte(path, '/')
			if slash > 0 {
				if err := fileMkdirAll(path[:slash]); err != nil {
					return err
				}
				continue
			}
			return err
		}
		// Maybe it already exists as a directory
		if err == syscall.EEXIST || err == syscall.ENOTDIR {
			var sb syscall.Stat_t
			if syscall.Stat(path, &sb) == nil && sb.Mode&syscall.S_IFMT == syscall.S_IFDIR {
				return nil
			}
			return syscall.EEXIST
		}
		return err
	}
}

func fileCmdMkdir(i *Interp, argv []*Obj) int {
	for _, a := range argv {
		if err := fileMkdirAll(a.String()); err != nil {
			return i.Errorf("can't create directory \"%#s\": %s", a, ioStrerror(err))
		}
	}
	return OK
}

// fileMakeTempFile creates a temporary file from a mkstemp(3) template and
// sets the result to its name (C: Jim_MakeTempFile).
func fileMakeTempFile(i *Interp, template string) (int, int) {
	name := template
	if template == "" {
		tmpdir := os.Getenv("TMPDIR")
		if tmpdir == "" || syscall.Access(tmpdir, 2) != nil {
			tmpdir = "/tmp/"
		}
		if !strings.HasSuffix(tmpdir, "/") {
			tmpdir += "/"
		}
		name = tmpdir + "tcl.tmp.XXXXXX"
	}

	mask := syscall.Umask(syscall.S_IXUSR | syscall.S_IRWXG | syscall.S_IRWXO)
	fd, path, err := fileMkstemp(name)
	syscall.Umask(mask)
	if err != nil {
		i.Errorf("%s: %s", path, ioStrerror(err))
		return -1, ERR
	}
	i.SetResultString(path)
	return fd, OK
}

// fileMkstemp is mkstemp(3): it replaces the trailing "XXXXXX" of the
// template with random characters and creates that file exclusively.
func fileMkstemp(template string) (int, string, error) {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	if !strings.HasSuffix(template, "XXXXXX") {
		return -1, template, syscall.EINVAL
	}
	stem := template[:len(template)-6]
	var random [6]byte
	for try := 0; try < 1000; try++ {
		if _, err := rand.Read(random[:]); err != nil {
			return -1, template, err
		}
		for n, b := range random {
			random[n] = letters[int(b)%len(letters)]
		}
		path := stem + string(random[:])
		fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_CREAT|syscall.O_EXCL, 0600)
		if err == nil {
			return fd, path, nil
		}
		if err != syscall.EEXIST {
			return -1, path, err
		}
	}
	return -1, template, syscall.EEXIST
}

func fileCmdTempfile(i *Interp, argv []*Obj) int {
	template := ""
	if len(argv) >= 1 {
		template = argv[0].String()
	}
	fd, rc := fileMakeTempFile(i, template)
	if rc != OK {
		return ERR
	}
	syscall.Close(fd)
	return OK
}

func fileCmdRename(i *Interp, argv []*Obj) int {
	force := false
	if len(argv) == 3 {
		if argv[0].String() != "-force" {
			return subCmdBadArgs
		}
		force = true
		argv = argv[1:]
	}

	source := argv[0].String()
	dest := argv[1].String()

	if !force && syscall.Access(dest, 0) == nil {
		return i.Errorf("error renaming \"%#s\" to \"%#s\": target exists", argv[0], argv[1])
	}
	if err := syscall.Rename(source, dest); err != nil {
		return i.Errorf("error renaming \"%#s\" to \"%#s\": %s", argv[0], argv[1], ioStrerror(err))
	}
	return OK
}

func fileCmdLink(i *Interp, argv []*Obj) int {
	options := []string{"-hard", "-symbolic"}
	const (
		optHard = iota
		optSymbolic
	)
	option := optHard

	if len(argv) == 3 {
		var rc int
		if option, rc = i.GetEnum(argv[0], options, "", EnumAbbrev|ErrMsg); rc != OK {
			return ERR
		}
		argv = argv[1:]
	}

	dest := argv[0].String()
	source := argv[1].String()

	var err error
	if option == optHard {
		err = syscall.Link(source, dest)
	} else {
		err = syscall.Symlink(source, dest)
	}
	if err != nil {
		return i.Errorf("error linking \"%#s\" to \"%#s\": %s", argv[0], argv[1], ioStrerror(err))
	}
	return OK
}

// fileStat is stat(2) with the shared error message (C: file_stat).
func (i *Interp) fileStat(filename *Obj, sb *syscall.Stat_t) int {
	if err := syscall.Stat(filename.String(), sb); err != nil {
		return i.Errorf("could not read \"%#s\": %s", filename, ioStrerror(err))
	}
	return OK
}

// fileLstat is lstat(2) with the shared error message (C: file_lstat).
func (i *Interp) fileLstat(filename *Obj, sb *syscall.Stat_t) int {
	if err := syscall.Lstat(filename.String(), sb); err != nil {
		return i.Errorf("could not read \"%#s\": %s", filename, ioStrerror(err))
	}
	return OK
}

func fileCmdAtime(i *Interp, argv []*Obj) int {
	var sb syscall.Stat_t
	if i.fileStat(argv[0], &sb) != OK {
		return ERR
	}
	i.SetResultInt(sb.Atim.Sec)
	return OK
}

// fileSetFileTimes sets a file's atime and mtime to the given time in
// microseconds since the epoch (C: JimSetFileTimes).
func fileSetFileTimes(i *Interp, filename string, us int64) int {
	tv := syscall.Timeval{Sec: us / 1000000, Usec: us % 1000000}
	if err := syscall.Utimes(filename, []syscall.Timeval{tv, tv}); err != nil {
		return i.Errorf("can't set time on \"%s\": %s", filename, ioStrerror(err))
	}
	return OK
}

func fileCmdMtime(i *Interp, argv []*Obj) int {
	if len(argv) == 2 {
		secs, rc := i.GetWide(argv[1])
		if rc != OK {
			return ERR
		}
		return fileSetFileTimes(i, argv[0].String(), secs*1000000)
	}
	var sb syscall.Stat_t
	if i.fileStat(argv[0], &sb) != OK {
		return ERR
	}
	i.SetResultInt(sb.Mtim.Sec)
	return OK
}

func fileCmdMtimeus(i *Interp, argv []*Obj) int {
	if len(argv) == 2 {
		us, rc := i.GetWide(argv[1])
		if rc != OK {
			return ERR
		}
		return fileSetFileTimes(i, argv[0].String(), us)
	}
	var sb syscall.Stat_t
	if i.fileStat(argv[0], &sb) != OK {
		return ERR
	}
	i.SetResultInt(fileMtimeUsec(&sb))
	return OK
}

// fileCmdCopy hands over to the Tcl implementation in tclcompat.tcl
// (C: file_cmd_copy).
func fileCmdCopy(i *Interp, argv []*Obj) int {
	return i.evalPrefix("file copy", argv...)
}

func fileCmdSize(i *Interp, argv []*Obj) int {
	var sb syscall.Stat_t
	if i.fileStat(argv[0], &sb) != OK {
		return ERR
	}
	i.SetResultInt(sb.Size)
	return OK
}

func fileCmdIsdirectory(i *Interp, argv []*Obj) int {
	var sb syscall.Stat_t
	ret := false
	if i.fileStat(argv[0], &sb) == OK {
		ret = sb.Mode&syscall.S_IFMT == syscall.S_IFDIR
	}
	i.SetResultBool(ret)
	return OK
}

func fileCmdIsfile(i *Interp, argv []*Obj) int {
	var sb syscall.Stat_t
	ret := false
	if i.fileStat(argv[0], &sb) == OK {
		ret = sb.Mode&syscall.S_IFMT == syscall.S_IFREG
	}
	i.SetResultBool(ret)
	return OK
}

func fileCmdOwned(i *Interp, argv []*Obj) int {
	var sb syscall.Stat_t
	ret := false
	if i.fileStat(argv[0], &sb) == OK {
		ret = syscall.Geteuid() == int(sb.Uid)
	}
	i.SetResultBool(ret)
	return OK
}

func fileCmdReadlink(i *Interp, argv []*Obj) int {
	buf := make([]byte, maxPathLen+1)
	n, err := syscall.Readlink(argv[0].String(), buf)
	if err != nil {
		return i.Errorf("could not read link \"%#s\": %s", argv[0], ioStrerror(err))
	}
	i.SetResult(i.NewStringObjBytes(buf[:n]))
	return OK
}

func fileCmdType(i *Interp, argv []*Obj) int {
	var sb syscall.Stat_t
	if i.fileLstat(argv[0], &sb) != OK {
		return ERR
	}
	i.SetResultString(fileGetFileType(uint32(sb.Mode)))
	return OK
}

func fileCmdLstat(i *Interp, argv []*Obj) int {
	var sb syscall.Stat_t
	if i.fileLstat(argv[0], &sb) != OK {
		return ERR
	}
	return i.fileStoreStatData(fileStatVar(argv), &sb)
}

func fileCmdStat(i *Interp, argv []*Obj) int {
	var sb syscall.Stat_t
	if i.fileStat(argv[0], &sb) != OK {
		return ERR
	}
	return i.fileStoreStatData(fileStatVar(argv), &sb)
}

// fileStatVar is the optional array variable of [file stat] and [file lstat].
func fileStatVar(argv []*Obj) *Obj {
	if len(argv) == 2 {
		return argv[1]
	}
	return nil
}

// fileCommandTable is the [file] ensemble (C: file_command_table).
var fileCommandTable = []SubCmd{
	{Cmd: "atime", Args: "name", Fn: fileCmdAtime, MinArgs: 1, MaxArgs: 1},
	{Cmd: "mtime", Args: "name ?time?", Fn: fileCmdMtime, MinArgs: 1, MaxArgs: 2},
	{Cmd: "mtimeus", Args: "name ?time?", Fn: fileCmdMtimeus, MinArgs: 1, MaxArgs: 2},
	{Cmd: "copy", Args: "?-force? source dest", Fn: fileCmdCopy, MinArgs: 2, MaxArgs: 3},
	{Cmd: "dirname", Args: "name", Fn: fileCmdDirname, MinArgs: 1, MaxArgs: 1},
	{Cmd: "rootname", Args: "name", Fn: fileCmdRootname, MinArgs: 1, MaxArgs: 1},
	{Cmd: "extension", Args: "name", Fn: fileCmdExtension, MinArgs: 1, MaxArgs: 1},
	{Cmd: "tail", Args: "name", Fn: fileCmdTail, MinArgs: 1, MaxArgs: 1},
	{Cmd: "split", Args: "name", Fn: fileCmdSplit, MinArgs: 1, MaxArgs: 1},
	{Cmd: "normalize", Args: "name", Fn: fileCmdNormalize, MinArgs: 1, MaxArgs: 1},
	{Cmd: "join", Args: "name ?name ...?", Fn: fileCmdJoin, MinArgs: 1, MaxArgs: -1},
	{Cmd: "readable", Args: "name", Fn: fileCmdReadable, MinArgs: 1, MaxArgs: 1},
	{Cmd: "writable", Args: "name", Fn: fileCmdWritable, MinArgs: 1, MaxArgs: 1},
	{Cmd: "executable", Args: "name", Fn: fileCmdExecutable, MinArgs: 1, MaxArgs: 1},
	{Cmd: "exists", Args: "name", Fn: fileCmdExists, MinArgs: 1, MaxArgs: 1},
	{Cmd: "delete", Args: "?-force|--? name ...", Fn: fileCmdDelete, MinArgs: 1, MaxArgs: -1},
	{Cmd: "mkdir", Args: "dir ...", Fn: fileCmdMkdir, MinArgs: 1, MaxArgs: -1},
	{Cmd: "tempfile", Args: "?template?", Fn: fileCmdTempfile, MinArgs: 0, MaxArgs: 1},
	{Cmd: "rename", Args: "?-force? source dest", Fn: fileCmdRename, MinArgs: 2, MaxArgs: 3},
	{Cmd: "link", Args: "?-symbolic|-hard? newname target", Fn: fileCmdLink, MinArgs: 2, MaxArgs: 3},
	{Cmd: "readlink", Args: "name", Fn: fileCmdReadlink, MinArgs: 1, MaxArgs: 1},
	{Cmd: "size", Args: "name", Fn: fileCmdSize, MinArgs: 1, MaxArgs: 1},
	{Cmd: "stat", Args: "name ?var?", Fn: fileCmdStat, MinArgs: 1, MaxArgs: 2},
	{Cmd: "lstat", Args: "name ?var?", Fn: fileCmdLstat, MinArgs: 1, MaxArgs: 2},
	{Cmd: "type", Args: "name", Fn: fileCmdType, MinArgs: 1, MaxArgs: 1},
	{Cmd: "owned", Args: "name", Fn: fileCmdOwned, MinArgs: 1, MaxArgs: 1},
	{Cmd: "isdirectory", Args: "name", Fn: fileCmdIsdirectory, MinArgs: 1, MaxArgs: 1},
	{Cmd: "isfile", Args: "name", Fn: fileCmdIsfile, MinArgs: 1, MaxArgs: 1},
}

func fileCdCmd(i *Interp, argv []*Obj) int {
	path := argv[1].String()
	if err := syscall.Chdir(path); err != nil {
		return i.Errorf("couldn't change working directory to \"%s\": %s", path, ioStrerror(err))
	}
	return OK
}

func filePwdCmd(i *Interp, argv []*Obj) int {
	cwd, err := syscall.Getwd()
	if err != nil {
		i.SetResultString("Failed to get pwd")
		return ERR
	}
	i.SetResultString(cwd)
	return OK
}

// registerFileCommands registers [file], [pwd] and [cd] (C: Jim_fileInit).
func registerFileCommands(i *Interp) {
	i.PackageProvide("file", "1.0", ErrMsg)
	i.RegisterSubCmd("file", fileCommandTable)
	i.RegisterCmd("pwd", "", 0, 0, filePwdCmd, 0)
	i.RegisterCmd("cd", "dirname", 1, 1, fileCdCmd, 0)
}
