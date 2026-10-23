/* Jim Tcl 0.84 ported from C to Go: the aio channel object type, the [open]
 * command and the stdin/stdout/stderr channels (C: jim-aio.c).
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
	"errors"
	"fmt"
	"math"
	"net"
	"sort"
	"strings"
	"syscall"
	"unsafe"
)

const (
	aioDefaultRbufLen   = 256       // read size for gets, read
	aioDefaultWbufLimit = 64 * 1024 // max size of writebuf before flushing
)

// Channel flags (C: AIO_*).
const (
	aioKeepOpen = 1  // don't close on command delete
	aioNoDelete = 2  // don't delete AF_UNIX path on close
	aioEOF      = 4  // EOF was reached
	aioWbufNone = 8  // default to buffering=none
	aioNonBlock = 16 // channel is non-blocking
	aioNoTaint  = 32 // don't set taint on the channel

	aioOneRead = 32 // passed to aioReadLen() to return after a single read
)

// Write buffering modes (C: enum wbuftype).
const (
	wbufOptNone = iota // write immediately
	wbufOptLine        // write if NL is seen
	wbufOptFull        // write when the write buffer is full or on flush
)

// aioFile is the state of one channel (C: struct AioFile).
type aioFile struct {
	filename  *Obj // filename or equivalent, for error reporting
	cmdName   *Obj // name of the channel command
	wbuft     int  // one of wbufOpt...
	flags     int  // aioKeepOpen | aioNoDelete | aioEOF | ...
	fd        int
	addrFam   int
	readbuf   []byte // buffered read data; nil when there is none at all
	writebuf  []byte // buffered write data
	rbuf      []byte // temporary read buffer, allocated on first use
	rbufLen   int    // size of rbuf
	wbufLimit int    // max size of writebuf before flushing
	// errno holds the error of the last read or write, which is what the C
	// implementation reads back out of errno when it reports the error.
	errno error
}

// aioChannels holds an interpreter's channels in creation order. The C
// implementation relies on each channel command's delete procedure to flush
// and close the channel; the Go port keeps the list instead so that
// FlushChannels can do the same work when the interpreter goes away.
type aioChannels struct {
	list []*aioFile
}

func (i *Interp) aioChannels() *aioChannels {
	ch, _ := i.channels.(*aioChannels)
	if ch == nil {
		ch = &aioChannels{}
		i.channels = ch
	}
	return ch
}

/* ---------------------------------------------------------------------------
 * Errors
 * ------------------------------------------------------------------------ */

// ioStrerror renders an errno the way the C library's strerror() does. Go
// spells the same messages with a lower case first letter, and reports an
// unknown number as "errno N" rather than "Unknown error N".
func ioStrerror(err error) string {
	var errno syscall.Errno
	if err == nil || (errors.As(err, &errno) && errno == 0) {
		return "Success"
	}
	if !errors.As(err, &errno) {
		return err.Error()
	}
	s := errno.Error()
	if strings.HasPrefix(s, "errno ") {
		return fmt.Sprintf("Unknown error %d", int(errno))
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func ioErrno(err error) syscall.Errno {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno
	}
	return 0
}

// aioErrorString is the channel's strerror (C: JimAioErrorString).
func (af *aioFile) errorString() string { return ioStrerror(af.errno) }

// aioSetError sets the result from the channel's last error, prefixed by name
// when one is given (C: JimAioSetError).
func aioSetError(i *Interp, af *aioFile, name *Obj) {
	if name != nil {
		i.Errorf("%#s: %s", name, af.errorString())
	} else {
		i.SetResultString(af.errorString())
	}
}

func (af *aioFile) eof() bool { return af.flags&aioEOF != 0 }

// stdioError reports whether the channel's last error is a real error; the C
// implementation treats the "would block" conditions as normal.
func (af *aioFile) stdioError() bool {
	if af.eof() {
		return false
	}
	switch ioErrno(af.errno) {
	case 0, syscall.EAGAIN, syscall.EINTR, syscall.ETIMEDOUT, syscall.ECONNRESET, syscall.ECONNABORTED:
		return false
	}
	return true
}

// aioCheckStreamError reports the channel's error, if any (C: JimCheckStreamError).
func aioCheckStreamError(i *Interp, af *aioFile) bool {
	if !af.eof() && af.stdioError() {
		aioSetError(i, af, af.filename)
		return true
	}
	return false
}

/* ---------------------------------------------------------------------------
 * Reading and writing
 * ------------------------------------------------------------------------ */

// aioIsatty reports whether fd refers to a terminal. The C implementation
// calls isatty(), which is a TCGETS ioctl that fails with ENOTTY otherwise.
func aioIsatty(fd int) bool { _, err := aioTcgets(fd); return err == nil }

func aioTcgets(fd int) (syscall.Termios, error) {
	var t syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.TCGETS,
		uintptr(unsafe.Pointer(&t)))
	if errno != 0 {
		return t, errno
	}
	return t, nil
}

// write is the channel's writer (C: stdio_writer).
func (af *aioFile) write(buf []byte) (int, error) {
	n, err := syscall.Write(af.fd, buf)
	af.errno = err
	if err == syscall.EPIPE {
		// Also discard the write buffer since otherwise when we try to flush
		// on shutdown we may get SIGPIPE.
		af.writebuf = af.writebuf[:0]
	}
	return n, err
}

// read is the channel's reader (C: stdio_reader).
func (af *aioFile) read(buf []byte) (int, error) {
	n, err := syscall.Read(af.fd, buf)
	af.errno = err
	if n <= 0 && err != syscall.EAGAIN && err != syscall.EINTR {
		af.flags |= aioEOF
	}
	return n, err
}

// aioSetNonblocking sets or clears O_NDELAY if it differs from the current
// setting (C: aio_set_nonblocking).
func aioSetNonblocking(af *aioFile, nb bool) {
	if af.flags&aioNonBlock != 0 == nb {
		return
	}
	fmode, _, _ := syscall.Syscall(syscall.SYS_FCNTL, uintptr(af.fd), syscall.F_GETFL, 0)
	if nb {
		fmode |= syscall.O_NDELAY
		af.flags |= aioNonBlock
	} else {
		fmode &= ^uintptr(syscall.O_NDELAY)
		af.flags &= ^aioNonBlock
	}
	syscall.Syscall(syscall.SYS_FCNTL, uintptr(af.fd), syscall.F_SETFL, fmode)
}

// aioFlush writes af.writebuf to the channel and removes what was written
// (C: aio_flush).
func aioFlush(i *Interp, af *aioFile) int {
	if len(af.writebuf) == 0 {
		return OK
	}
	n, err := af.write(af.writebuf)
	if err == nil && n > 0 {
		af.writebuf = af.writebuf[n:]
	}
	if err != nil {
		if aioCheckStreamError(i, af) {
			return ERR
		}
		return OK
	}
	if len(af.writebuf) != 0 {
		// Without an event loop there is nobody to finish the write for us.
		i.SetResultString("send buffer is full")
		return ERR
	}
	return OK
}

// aioReadLen reads until neededLen bytes are available in af.readbuf, or until
// eof when neededLen is -1 (C: aio_read_len).
func aioReadLen(i *Interp, af *aioFile, flags, neededLen int) int {
	if af.readbuf == nil {
		af.readbuf = []byte{}
	}
	if neededLen >= 0 {
		neededLen -= len(af.readbuf)
		if neededLen <= 0 {
			return OK
		}
	}

	for neededLen != 0 && !af.eof() {
		readlen := af.rbufLen
		if neededLen != -1 && neededLen < readlen {
			readlen = neededLen
		}
		if len(af.rbuf) < af.rbufLen {
			af.rbuf = make([]byte, af.rbufLen)
		}
		n, _ := af.read(af.rbuf[:readlen])
		if n > 0 {
			af.readbuf = append(af.readbuf, af.rbuf[:n]...)
			if neededLen != -1 {
				neededLen -= n
			}
			if flags&aioOneRead != 0 {
				return OK
			}
			continue
		}
		if flags&aioOneRead != 0 || aioCheckStreamError(i, af) {
			return ERR
		}
		break
	}
	return OK
}

// aioReadConsume takes neededLen bytes from af.readbuf, or everything when
// neededLen is negative. It returns nil when there is no buffer at all
// (C: aio_read_consume).
func aioReadConsume(i *Interp, af *aioFile, neededLen int) *Obj {
	if af.readbuf == nil {
		return nil
	}
	if neededLen < 0 || len(af.readbuf) <= neededLen {
		objPtr := i.NewStringObjBytes(af.readbuf)
		af.readbuf = nil
		return objPtr
	}
	objPtr := i.NewStringObjBytes(af.readbuf[:neededLen])
	af.readbuf = af.readbuf[neededLen:]
	return objPtr
}

/* ---------------------------------------------------------------------------
 * The channel subcommands
 * ------------------------------------------------------------------------ */

func aioCmdRead(i *Interp, af *aioFile, argv []*Obj) int {
	options := []string{"-pending", "-nonewline"}
	const (
		optPending = iota
		optNonewline
	)
	nonewline := false
	neededLen := -1 // -1 is "read as much as possible"

	if len(argv) > 0 {
		if strings.HasPrefix(argv[0].String(), "-") {
			option, rc := i.GetEnum(argv[0], options, "", ErrMsg)
			if rc != OK {
				return ERR
			}
			switch option {
			case optPending:
				// accepted for compatibility, but ignored
			case optNonewline:
				nonewline = true
			}
		} else {
			w, rc := i.GetWide(argv[0])
			if rc != OK {
				return ERR
			}
			if w < 0 {
				i.SetResultString("invalid parameter: negative len")
				return ERR
			}
			neededLen = int(w)
		}
		argv = argv[1:]
	}
	if len(argv) > 0 {
		return subCmdBadArgs
	}

	if aioReadLen(i, af, 0, neededLen) != OK {
		return ERR
	}
	objPtr := aioReadConsume(i, af, neededLen)
	if objPtr == nil {
		i.SetResult(i.emptyObj)
		return OK
	}
	if nonewline {
		if b := objPtr.Bytes(); len(b) > 0 && b[len(b)-1] == '\n' {
			objPtr = i.NewStringObjBytes(b[:len(b)-1])
		}
	}
	i.SetResult(objPtr)
	return OK
}

// aioCmdGetfd returns the underlying file descriptor (C: aio_cmd_getfd).
func aioCmdGetfd(i *Interp, af *aioFile, argv []*Obj) int {
	aioFlush(i, af)
	i.SetResultInt(int64(af.fd))
	return OK
}

// aioCmdGettaint returns the channel's taint, which is always 0 in a build
// without taint support (C: aio_cmd_gettaint).
func aioCmdGettaint(i *Interp, af *aioFile, argv []*Obj) int {
	if _, rc := i.GetEnum(argv[0], []string{"-source", "-sink"}, "", ErrMsg); rc != OK {
		return ERR
	}
	i.SetResultInt(0)
	return OK
}

// aioCmdCopy implements [copyto] by driving the target channel's own [flush]
// and [puts -nonewline], exactly as the C implementation does so that the
// target may be any command that looks like a channel (C: aio_cmd_copy).
func aioCmdCopy(i *Interp, af *aioFile, argv []*Obj) int {
	var count int64
	maxlen := int64(math.MaxInt64)
	if len(argv) == 2 {
		var rc int
		if maxlen, rc = i.GetWide(argv[1]); rc != OK {
			return ERR
		}
	}

	objv := make([]*Obj, 4)
	objv[0] = argv[0]
	objv[1] = i.NewStringObj("gettaint")
	objv[2] = i.NewStringObj("-sink")
	if i.EvalObjVector(objv[:3]) != OK {
		return i.Errorf("Not a filehandle: \"%#s\"", argv[0])
	}
	if _, rc := i.GetWide(i.Result()); rc != OK {
		return i.Errorf("Not a filehandle: \"%#s\"", argv[0])
	}

	// Need to flush any write data first. This could fail because of a full
	// send buffer, but more likely because the target isn't a filehandle.
	objv[1] = i.NewStringObj("flush")
	if i.EvalObjVector(objv[:2]) != OK {
		return i.Errorf("Not a filehandle: \"%#s\"", argv[0])
	}

	objv[1] = i.NewStringObj("puts")
	objv[2] = i.NewStringObj("-nonewline")

	ok := true
	for count < maxlen {
		length := maxlen - count
		if length > int64(af.rbufLen) {
			length = int64(af.rbufLen)
		}
		if aioReadLen(i, af, 0, int(length)) != OK {
			ok = false
			break
		}
		objv[3] = aioReadConsume(i, af, int(length))
		if objv[3] == nil {
			objv[3] = i.emptyObj
		}
		count += int64(objv[3].Len())
		if i.EvalObjVector(objv) != OK {
			ok = false
			break
		}
		if af.eof() {
			break
		}
		if count >= 16384 && af.rbufLen < 65536 {
			// Heuristic check - for large copy speed-up
			af.rbufLen = 65536
			af.rbuf = nil
		}
	}
	if !ok {
		return ERR
	}
	i.SetResultInt(count)
	return OK
}

func aioCmdGets(i *Interp, af *aioFile, argv []*Obj) int {
	options := []string{"-eol", "-keep"}
	const (
		optEol = iota
		optKeep
	)
	nlstr := []byte("\n")
	keepnl := int64(0)

	for len(argv) >= 2 {
		if !strings.HasPrefix(argv[0].String(), "-") {
			return subCmdBadArgs
		}
		opt, rc := i.GetEnum(argv[0], options, "", ErrMsg|EnumAbbrev)
		if rc != OK {
			return ERR
		}
		switch opt {
		case optEol:
			nlstr = argv[1].Bytes()
		case optKeep:
			if keepnl, rc = i.GetWide(argv[1]); rc != OK {
				return ERR
			}
		}
		argv = argv[2:]
	}

	af.errno = nil

	var objPtr *Obj
	found := false
	offset := 0
	for !af.eof() {
		if af.readbuf != nil {
			idx := aioIndexBytes(af.readbuf[offset:], nlstr)
			if idx >= 0 {
				nl := offset + idx
				end := nl
				if keepnl != 0 {
					end = nl + len(nlstr)
				}
				objPtr = i.NewStringObjBytes(af.readbuf[:end])
				af.readbuf = af.readbuf[nl+len(nlstr):]
				found = true
				break
			}
			offset = len(af.readbuf)
		}
		// Not got a line yet, so read more
		if aioReadLen(i, af, aioOneRead, -1) != OK {
			break
		}
	}

	if !found && af.eof() && af.readbuf != nil {
		// Just take what we have as the line
		objPtr = i.NewStringObjBytes(af.readbuf)
		af.readbuf = nil
	} else if objPtr == nil {
		objPtr = i.NewStringObj("")
	}

	if len(argv) > 0 {
		if i.SetVariable(argv[0], objPtr) != OK {
			return ERR
		}
		length := objPtr.Len()
		if !found && length == 0 {
			// On eof or a partial line with an empty result, returns -1 if
			// varName was specified.
			length = -1
		}
		i.SetResultInt(int64(length))
	} else {
		i.SetResult(objPtr)
	}
	return OK
}

// aioIndexBytes is strstr() over a byte slice (C: jim_strstr).
func aioIndexBytes(haystack, needle []byte) int {
	if len(needle) == 0 {
		return 0
	}
	for n := 0; n+len(needle) <= len(haystack); n++ {
		if string(haystack[n:n+len(needle)]) == string(needle) {
			return n
		}
	}
	return -1
}

func aioCmdPuts(i *Interp, af *aioFile, argv []*Obj) int {
	strObj := argv[0]
	nl := true
	if len(argv) == 2 {
		if argv[0].String() != "-nonewline" {
			return subCmdBadArgs
		}
		strObj = argv[1]
		nl = false
	}

	// Keep it simple and always go via the writebuf instead of trying to
	// optimise the case that we can write immediately.
	af.writebuf = append(af.writebuf, strObj.Bytes()...)
	if nl {
		af.writebuf = append(af.writebuf, '\n')
	}

	wnow := false
	switch af.wbuft {
	case wbufOptNone:
		wnow = true
	case wbufOptLine:
		// Write everything if it contains a newline, or -nonewline wasn't given
		wnow = nl || aioIndexBytes(af.writebuf, []byte{'\n'}) >= 0
	case wbufOptFull:
		wnow = len(af.writebuf) >= af.wbufLimit
	}
	if wnow {
		return aioFlush(i, af)
	}
	return OK
}

func aioCmdIsatty(i *Interp, af *aioFile, argv []*Obj) int {
	i.SetResultBool(aioIsatty(af.fd))
	return OK
}

func aioCmdFlush(i *Interp, af *aioFile, argv []*Obj) int { return aioFlush(i, af) }

func aioCmdEof(i *Interp, af *aioFile, argv []*Obj) int {
	i.SetResultBool(af.eof())
	return OK
}

// aioCmdClose closes the channel and deletes its command. The C
// implementation does the flushing and closing in the command's delete
// procedure; there is no such hook in the Go port, so it happens here
// (C: aio_cmd_close plus JimAioDelProc).
func aioCmdClose(i *Interp, af *aioFile, argv []*Obj) int {
	if len(argv) == 3 {
		options := []string{"r", "w", "-nodelete"}
		const (
			optR = iota
			optW
			optNodelete
		)
		option, rc := i.GetEnum(argv[2], options, "", ErrMsg)
		if rc != OK {
			return ERR
		}
		switch option {
		case optR, optW:
			how := syscall.SHUT_WR
			if option == optR {
				how = syscall.SHUT_RD
			}
			err := syscall.Shutdown(af.fd, how)
			if err == nil {
				return OK
			}
			af.errno = err
			aioSetError(i, af, nil)
			return ERR
		case optNodelete:
			if af.addrFam == syscall.AF_UNIX {
				af.flags |= aioNoDelete
				break
			}
			fallthrough
		default:
			i.SetResultString("not supported")
			return ERR
		}
	}

	// Explicit close ignores aioKeepOpen
	af.flags &= ^aioKeepOpen
	aioDeleteChannel(i, af)
	return i.DeleteCommand(argv[0])
}

// aioDeleteChannel flushes and closes a channel and forgets about it
// (C: JimAioDelProc).
func aioDeleteChannel(i *Interp, af *aioFile) {
	// Try to flush and write data before close
	aioFlush(i, af)
	if af.addrFam == syscall.AF_UNIX && af.flags&aioNoDelete == 0 {
		// If this is bound, delete the socket file now
		if sa, err := syscall.Getsockname(af.fd); err == nil {
			if name := aioFormatSocketAddress(sa); name != "" {
				syscall.Unlink(name)
			}
		}
	}
	if af.flags&aioKeepOpen == 0 {
		syscall.Close(af.fd)
	}
	af.readbuf = nil
	af.rbuf = nil
	list := i.aioChannels().list
	for n, c := range list {
		if c == af {
			i.aioChannels().list = append(list[:n:n], list[n+1:]...)
			break
		}
	}
}

func aioCmdSeek(i *Interp, af *aioFile, argv []*Obj) int {
	orig := 0 // SEEK_SET
	if len(argv) == 2 {
		switch argv[1].String() {
		case "start":
			orig = 0
		case "current":
			orig = 1
		case "end":
			orig = 2
		default:
			return subCmdBadArgs
		}
	}
	offset, rc := i.GetWide(argv[0])
	if rc != OK {
		return ERR
	}
	if orig != 1 || offset != 0 {
		// Try to write flush if seeking
		aioFlush(i, af)
	}
	if _, err := syscall.Seek(af.fd, offset, orig); err != nil {
		af.errno = err
		aioSetError(i, af, af.filename)
		return ERR
	}
	af.readbuf = nil
	af.flags &= ^aioEOF
	return OK
}

func aioCmdTell(i *Interp, af *aioFile, argv []*Obj) int {
	pos, _ := syscall.Seek(af.fd, 0, 1)
	i.SetResultInt(pos)
	return OK
}

func aioCmdFilename(i *Interp, af *aioFile, argv []*Obj) int {
	i.SetResult(af.filename)
	return OK
}

func aioCmdNdelay(i *Interp, af *aioFile, argv []*Obj) int {
	if len(argv) > 0 {
		nb, rc := i.GetWide(argv[0])
		if rc != OK {
			return ERR
		}
		aioSetNonblocking(af, nb != 0)
	}
	i.SetResultBool(af.flags&aioNonBlock != 0)
	return OK
}

func aioCmdSync(i *Interp, af *aioFile, argv []*Obj) int {
	if aioFlush(i, af) != OK {
		return ERR
	}
	syscall.Fsync(af.fd)
	return OK
}

func aioCmdBuffering(i *Interp, af *aioFile, argv []*Obj) int {
	options := []string{"none", "line", "full"}

	if len(argv) > 0 {
		wbuft, rc := i.GetEnum(argv[0], options, "", ErrMsg)
		if rc != OK {
			return ERR
		}
		af.wbuft = wbuft
		if af.wbuft == wbufOptFull && len(argv) == 2 {
			l, rc := i.GetWide(argv[1])
			if rc != OK || l <= 0 {
				return ERR
			}
			af.wbufLimit = int(l)
		}
		if af.wbuft == wbufOptNone {
			if aioFlush(i, af) != OK {
				return ERR
			}
		}
		// don't bother flushing when switching from full to line
	}

	resultObj := i.NewListObj(nil)
	i.ListAppendElement(resultObj, i.NewStringObj(options[af.wbuft]))
	if af.wbuft == wbufOptFull {
		i.ListAppendElement(resultObj, i.NewIntObj(int64(af.wbufLimit)))
	}
	i.SetResult(resultObj)
	return OK
}

// aioCmdTranslation only validates its argument; the translation modes are a
// Windows-only feature (C: aio_cmd_translation).
func aioCmdTranslation(i *Interp, af *aioFile, argv []*Obj) int {
	if _, rc := i.GetEnum(argv[0], []string{"binary", "text"}, "", ErrMsg); rc != OK {
		return ERR
	}
	return OK
}

func aioCmdReadsize(i *Interp, af *aioFile, argv []*Obj) int {
	if len(argv) > 0 {
		l, rc := i.GetWide(argv[0])
		if rc != OK || l <= 0 {
			return ERR
		}
		af.rbufLen = int(l)
		af.rbuf = nil
	}
	i.SetResultInt(int64(af.rbufLen))
	return OK
}

func aioCmdStat(i *Interp, af *aioFile, argv []*Obj) int {
	var sb syscall.Stat_t
	if err := syscall.Fstat(af.fd, &sb); err != nil {
		af.errno = err
		aioSetError(i, af, nil)
		return ERR
	}
	var varName *Obj
	if len(argv) == 1 {
		varName = argv[0]
	}
	return i.fileStoreStatData(varName, &sb)
}

// aioCmdLock takes an exclusive write lock on the whole file (C: aio_cmd_lock).
func aioCmdLock(i *Interp, af *aioFile, argv []*Obj) int {
	lockmode := syscall.F_SETLK
	if len(argv) == 1 {
		if argv[0].String() != "-wait" {
			return subCmdBadArgs
		}
		lockmode = syscall.F_SETLKW
	}
	fl := syscall.Flock_t{Type: syscall.F_WRLCK, Whence: 0, Start: 0, Len: 0}
	err := syscall.FcntlFlock(uintptr(af.fd), lockmode, &fl)
	switch {
	case err == nil:
		i.SetResultInt(1)
	case err == syscall.EACCES || err == syscall.EAGAIN:
		i.SetResultInt(0)
	default:
		return i.Errorf("lock failed: %s", ioStrerror(err))
	}
	return OK
}

func aioCmdUnlock(i *Interp, af *aioFile, argv []*Obj) int {
	fl := syscall.Flock_t{Type: syscall.F_UNLCK, Whence: 0, Start: 0, Len: 0}
	i.SetResultBool(syscall.FcntlFlock(uintptr(af.fd), syscall.F_SETLK, &fl) == nil)
	return OK
}

func aioCmdTty(i *Interp, af *aioFile, argv []*Obj) int {
	if len(argv) == 0 {
		// get the current settings as a dictionary
		dictObj := aioGetTtySettings(i, af.fd)
		if dictObj == nil {
			af.errno = syscall.ENOTTY
			aioSetError(i, af, nil)
			return ERR
		}
		i.SetResult(dictObj)
		return OK
	}
	dictObj := argv[0]
	if len(argv) > 1 {
		// Convert name value arguments to a dictionary
		dictObj = i.NewListObj(argv)
	}
	if i.ListLength(dictObj)%2 != 0 {
		// Must be a valid dictionary
		return subCmdBadArgs
	}
	ret := aioSetTtySettings(i, af.fd, dictObj)
	if ret < 0 {
		af.errno = syscall.ENOTTY
		aioSetError(i, af, nil)
		ret = ERR
	}
	return ret
}

func aioCmdTtycontrol(i *Interp, af *aioFile, argv []*Obj) int {
	if len(argv) == 0 {
		dictObj := aioGetTtyControlSettings(i, af.fd)
		if dictObj == nil {
			af.errno = syscall.ENOTTY
			aioSetError(i, af, nil)
			return ERR
		}
		i.SetResult(dictObj)
		return OK
	}
	dictObj := argv[0]
	if len(argv) > 1 {
		dictObj = i.NewListObj(argv)
	}
	if i.ListLength(dictObj)%2 != 0 {
		return subCmdBadArgs
	}
	ret := aioSetTtyControlSettings(i, af.fd, dictObj)
	if ret < 0 {
		af.errno = syscall.ENOTTY
		aioSetError(i, af, nil)
		ret = ERR
	}
	return ret
}

/* ---------------------------------------------------------------------------
 * The termios settings of a tty channel (C: jim-tty.c)
 * ------------------------------------------------------------------------ */

// The termios constants Go's syscall package does not export. These are the
// asm-generic values, which Linux uses on every architecture this port runs on.
const (
	ttyTCSETSF = 0x5404     // tcsetattr() with TCSAFLUSH
	ttyTCSBRKP = 0x5425     // tcsendbreak()
	ttyCBAUD   = 0x100f     // the baud rate bits of the control flags
	ttyCBAUDEX = 0x1000     // the extended baud rate bit
	ttyCRTSCTS = 0x80000000 // RTS/CTS hardware handshaking
)

// ttyBaudTable maps a baud rate to its termios speed code (C: baudtable).
var ttyBaudTable = []struct {
	baud  int64
	speed uint32
}{
	{0, syscall.B0},
	{50, syscall.B50},
	{75, syscall.B75},
	{110, syscall.B110},
	{134, syscall.B134},
	{150, syscall.B150},
	{200, syscall.B200},
	{300, syscall.B300},
	{600, syscall.B600},
	{1200, syscall.B1200},
	{1800, syscall.B1800},
	{2400, syscall.B2400},
	{4800, syscall.B4800},
	{9600, syscall.B9600},
	{19200, syscall.B19200},
	{38400, syscall.B38400},
	{57600, syscall.B57600},
	{115200, syscall.B115200},
	{230400, syscall.B230400},
	{460800, syscall.B460800},
}

// ttyFlagName is one entry of a termios flag name table
// (C: struct flag_name_map).
type ttyFlagName struct {
	name  string
	value uint32
}

var (
	ttyParityMap   = []ttyFlagName{{"none", 0}, {"even", syscall.PARENB}, {"odd", syscall.PARENB | syscall.PARODD}}
	ttyDataSizeMap = []ttyFlagName{{"5", syscall.CS5}, {"6", syscall.CS6}, {"7", syscall.CS7}, {"8", syscall.CS8}}
	ttyStopSizeMap = []ttyFlagName{{"1", 0}, {"2", syscall.CSTOPB}}
	ttyInputMap    = []ttyFlagName{{"raw", 0}, {"cooked", syscall.ICANON}}
	ttyOutputMap   = []ttyFlagName{{"raw", 0}, {"cooked", syscall.OPOST}}
)

var ttySettingsNames = []string{
	"baud", "data", "handshake", "input", "output", "parity", "stop",
	"vmin", "vtime", "echo", "vstart", "vstop",
}

const (
	ttyOptBaud = iota
	ttyOptData
	ttyOptHandshake
	ttyOptInput
	ttyOptOutput
	ttyOptParity
	ttyOptStop
	ttyOptVmin
	ttyOptVtime
	ttyOptEcho
	ttyOptVstart
	ttyOptVstop
)

// ttyFlagNameToValue looks a name up in a flag table. As the C does, only the
// first character is compared, since all the names in a table differ in it.
func ttyFlagNameToValue(table []ttyFlagName, name string) (uint32, bool) {
	if name == "" {
		return 0, false
	}
	for _, e := range table {
		if name[0] == e.name[0] {
			return e.value, true
		}
	}
	return 0, false
}

func ttyFlagValueToName(table []ttyFlagName, value uint32) (string, bool) {
	for _, e := range table {
		if value == e.value {
			return e.name, true
		}
	}
	return "", false
}

// ttyListAddPair appends a name and its value to the list, and does nothing
// when the value has no name (C: JimListAddPair).
func (i *Interp) ttyListAddPair(listObj *Obj, name, value string, ok bool) {
	if ok {
		i.ListAppendElement(listObj, i.NewStringObj(name))
		i.ListAppendElement(listObj, i.NewStringObj(value))
	}
}

func aioTcsets(fd int, t *syscall.Termios) error {
	// TCSETSF is tcsetattr() with TCSAFLUSH
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(ttyTCSETSF),
		uintptr(unsafe.Pointer(t)))
	if errno != 0 {
		return errno
	}
	return nil
}

// aioGetTtySettings returns the tty settings as a dictionary, or nil if the
// channel is not a tty (C: Jim_GetTtySettings).
func aioGetTtySettings(i *Interp, fd int) *Obj {
	tio, err := aioTcgets(fd)
	if err != nil {
		return nil
	}

	listObj := i.NewListObj(nil)

	p, ok := ttyFlagValueToName(ttyParityMap, tio.Cflag&(syscall.PARENB|syscall.PARODD))
	i.ttyListAddPair(listObj, "parity", p, ok)
	p, ok = ttyFlagValueToName(ttyDataSizeMap, tio.Cflag&syscall.CSIZE)
	i.ttyListAddPair(listObj, "data", p, ok)
	p, ok = ttyFlagValueToName(ttyStopSizeMap, tio.Cflag&syscall.CSTOPB)
	i.ttyListAddPair(listObj, "stop", p, ok)
	switch {
	case tio.Iflag&(syscall.IXON|syscall.IXOFF) != 0:
		p = "xonxoff"
	case tio.Cflag&uint32(ttyCRTSCTS) != 0:
		p = "rtscts"
	default:
		p = "none"
	}
	i.ttyListAddPair(listObj, "handshake", p, true)
	p, ok = ttyFlagValueToName(ttyInputMap, tio.Lflag&syscall.ICANON)
	i.ttyListAddPair(listObj, "input", p, ok)
	p, ok = ttyFlagValueToName(ttyOutputMap, tio.Oflag&syscall.OPOST)
	i.ttyListAddPair(listObj, "output", p, ok)

	for _, cc := range []struct {
		name string
		idx  int
	}{{"vmin", syscall.VMIN}, {"vtime", syscall.VTIME}, {"vstart", syscall.VSTART}, {"vstop", syscall.VSTOP}} {
		i.ListAppendElement(listObj, i.NewStringObj(cc.name))
		i.ListAppendElement(listObj, i.NewIntObj(int64(tio.Cc[cc.idx])))
	}

	// On Linux the speed is held in the baud bits of the control flags
	speed := tio.Cflag & uint32(ttyCBAUD)
	baud := int64(0)
	for _, e := range ttyBaudTable {
		if e.speed == speed {
			baud = e.baud
			break
		}
	}
	i.ListAppendElement(listObj, i.NewStringObj("baud"))
	i.ListAppendElement(listObj, i.NewIntObj(baud))

	return listObj
}

// aioSetTtySettings applies a dictionary of tty settings. It returns OK, ERR
// for a bad setting or value, or -1 if the channel is not a tty
// (C: Jim_SetTtySettings).
func aioSetTtySettings(i *Interp, fd int, dictObj *Obj) int {
	tio, err := aioTcgets(fd)
	if err != nil {
		return -1
	}

	elems := i.ListElements(dictObj)
	for n := 0; n+1 < len(elems); n += 2 {
		nameObj, valueObj := elems[n], elems[n+1]
		opt, rc := i.GetEnum(nameObj, ttySettingsNames, "setting", ErrMsg|EnumAbbrev)
		if rc != OK {
			return ERR
		}
		badValue := func() int {
			return i.Errorf("bad value for %#s: %#s", nameObj, valueObj)
		}

		switch opt {
		case ttyOptBaud:
			l, rc := i.GetWide(valueObj)
			if rc != OK {
				return badValue()
			}
			speed, found := uint32(0), false
			for _, e := range ttyBaudTable {
				if e.baud == l {
					speed, found = e.speed, true
					break
				}
			}
			if !found {
				return badValue()
			}
			tio.Cflag &= ^uint32(ttyCBAUD | ttyCBAUDEX)
			tio.Cflag |= speed
			tio.Ispeed, tio.Ospeed = speed, speed

		case ttyOptParity:
			v, ok := ttyFlagNameToValue(ttyParityMap, valueObj.String())
			if !ok {
				return badValue()
			}
			tio.Cflag &= ^uint32(syscall.PARENB | syscall.PARODD)
			tio.Cflag |= v

		case ttyOptStop:
			v, ok := ttyFlagNameToValue(ttyStopSizeMap, valueObj.String())
			if !ok {
				return badValue()
			}
			tio.Cflag &= ^uint32(syscall.CSTOPB)
			tio.Cflag |= v

		case ttyOptData:
			v, ok := ttyFlagNameToValue(ttyDataSizeMap, valueObj.String())
			if !ok {
				return badValue()
			}
			tio.Cflag &= ^uint32(syscall.CSIZE)
			tio.Cflag |= v

		case ttyOptHandshake:
			tio.Iflag &= ^uint32(syscall.IXON | syscall.IXOFF)
			tio.Cflag &= ^uint32(uint32(ttyCRTSCTS))
			switch valueObj.String() {
			case "xonxoff":
				tio.Iflag |= syscall.IXON | syscall.IXOFF
			case "rtscts":
				tio.Cflag |= uint32(ttyCRTSCTS)
			case "none":
			default:
				return badValue()
			}

		case ttyOptVmin, ttyOptVtime, ttyOptVstart, ttyOptVstop:
			l, rc := i.GetWide(valueObj)
			if rc != OK {
				return badValue()
			}
			idx := map[int]int{ttyOptVmin: syscall.VMIN, ttyOptVtime: syscall.VTIME,
				ttyOptVstart: syscall.VSTART, ttyOptVstop: syscall.VSTOP}[opt]
			tio.Cc[idx] = uint8(l)

		case ttyOptOutput:
			v, ok := ttyFlagNameToValue(ttyOutputMap, valueObj.String())
			if !ok {
				return badValue()
			}
			tio.Oflag &= ^uint32(syscall.OPOST)
			tio.Oflag |= v

		case ttyOptInput:
			v, ok := ttyFlagNameToValue(ttyInputMap, valueObj.String())
			if !ok {
				return badValue()
			}
			const cooked = syscall.ECHO | syscall.ECHOE | syscall.ECHOK | syscall.ECHONL |
				syscall.ICANON | syscall.IEXTEN | syscall.ISIG | syscall.NOFLSH | syscall.TOSTOP
			if v != 0 {
				tio.Lflag |= cooked
				tio.Iflag |= syscall.ICRNL
			} else {
				tio.Lflag &= ^uint32(cooked)
				tio.Iflag &= ^uint32(syscall.ICRNL)
			}

		case ttyOptEcho:
			l, rc := i.GetWide(valueObj)
			if rc != OK {
				return badValue()
			}
			if l != 0 {
				tio.Lflag |= syscall.ECHO
			} else {
				tio.Lflag &= ^uint32(syscall.ECHO)
			}
		}
	}

	if aioTcsets(fd, &tio) != nil {
		return -1
	}
	return OK
}

// ttyModemSignals are the modem control lines [ttycontrol] reports
// (C: modem_signals).
var ttyModemSignals = []struct {
	name  string
	value int
}{
	{"rts", syscall.TIOCM_RTS},
	{"dtr", syscall.TIOCM_DTR},
	{"dcd", syscall.TIOCM_CAR},
	{"dsr", syscall.TIOCM_DSR},
	{"ring", syscall.TIOCM_RNG},
	{"cts", syscall.TIOCM_CTS},
}

func aioModemStatus(fd int) (int32, error) {
	var status int32
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.TIOCMGET,
		uintptr(unsafe.Pointer(&status)))
	if errno != 0 {
		return 0, errno
	}
	return status, nil
}

// aioGetTtyControlSettings returns the modem control lines as a dictionary
// (C: Jim_GetTtyControlSettings).
func aioGetTtyControlSettings(i *Interp, fd int) *Obj {
	status, err := aioModemStatus(fd)
	if err != nil {
		return nil
	}
	listObj := i.NewListObj(nil)
	for _, sig := range ttyModemSignals {
		i.ListAppendElement(listObj, i.NewStringObj(sig.name))
		i.ListAppendElement(listObj, i.NewIntObj(int64(status)&int64(sig.value)))
	}
	return listObj
}

// aioSetTtyControlSettings sets the modem control lines, or sends a break
// (C: Jim_SetTtyControlSettings).
func aioSetTtyControlSettings(i *Interp, fd int, dictObj *Obj) int {
	signalNames := []string{"rts", "dtr", "break"}
	const (
		optRts = iota
		optDtr
		optBreak
	)

	// Get the current status of DTR and RTS in case we aren't setting everything
	status, err := aioModemStatus(fd)
	if err != nil {
		return -1
	}

	elems := i.ListElements(dictObj)
	for n := 0; n+1 < len(elems); n += 2 {
		nameObj, valueObj := elems[n], elems[n+1]
		opt, rc := i.GetEnum(nameObj, signalNames, "signal", ErrMsg|EnumAbbrev)
		if rc != OK {
			return ERR
		}
		l, rc := i.GetWide(valueObj)
		if rc != OK {
			return i.Errorf("bad value for %#s: %#s", nameObj, valueObj)
		}

		switch opt {
		case optRts, optDtr:
			bit := int32(syscall.TIOCM_DTR)
			if opt == optRts {
				bit = syscall.TIOCM_RTS
			}
			if l != 0 {
				status |= bit
			} else {
				status &= ^bit
			}
		case optBreak:
			// tcsendbreak() asks the driver for a break of l tenths of a second
			syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(ttyTCSBRKP),
				uintptr((l+99)/100))
		}
	}

	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.TIOCMSET,
		uintptr(unsafe.Pointer(&status)))
	if errno != 0 {
		return -1
	}
	return OK
}

/* ---------------------------------------------------------------------------
 * The socket subcommands and the address handling they share with the
 * [socket] command.
 * ------------------------------------------------------------------------ */

// aioFormatSocketAddress renders a socket address the way the original
// reports it: "addr:port" for IPv4, "[addr]:port" for IPv6 and the bare path
// for a Unix domain socket (C: JimFormatSocketAddress).
func aioFormatSocketAddress(sa syscall.Sockaddr) string {
	switch a := sa.(type) {
	case *syscall.SockaddrUnix:
		// An unnamed socket, as socketpair() produces, has an empty path; Go
		// reports it as the "@" it uses for abstract socket names.
		if a.Name == "@" {
			return ""
		}
		return a.Name
	case *syscall.SockaddrInet4:
		return fmt.Sprintf("%d.%d.%d.%d:%d", a.Addr[0], a.Addr[1], a.Addr[2], a.Addr[3], a.Port)
	case *syscall.SockaddrInet6:
		return fmt.Sprintf("[%s]:%d", net.IP(a.Addr[:]).String(), a.Port)
	}
	// Otherwise just an empty address
	return ""
}

func aioCmdAccept(i *Interp, af *aioFile, argv []*Obj) int {
	fd, sa, err := syscall.Accept(af.fd)
	if err != nil {
		af.errno = err
		aioSetError(i, af, nil)
		return ERR
	}

	n := 0
	flags := aioNoDelete
	if len(argv) > 0 && argv[0].String() == "-noclose" {
		flags = aioKeepOpen
		n++
	}
	if len(argv) > n {
		if i.SetVariable(argv[n], i.NewStringObj(aioFormatSocketAddress(sa))) != OK {
			syscall.Close(fd)
			return ERR
		}
	}

	filenameObj := i.NewStringObj(aioFormatSocketAddress(sa))
	aioMakeChannel(i, fd, filenameObj, "aio.sockstream%ld", af.addrFam, flags)
	return OK
}

func aioCmdListen(i *Interp, af *aioFile, argv []*Obj) int {
	backlog, rc := i.GetWide(argv[0])
	if rc != OK {
		return ERR
	}
	if err := syscall.Listen(af.fd, int(backlog)); err != nil {
		af.errno = err
		aioSetError(i, af, nil)
		return ERR
	}
	return OK
}

func aioCmdSockname(i *Interp, af *aioFile, argv []*Obj) int {
	sa, err := syscall.Getsockname(af.fd)
	if err != nil {
		af.errno = err
		aioSetError(i, af, nil)
		return ERR
	}
	i.SetResultString(aioFormatSocketAddress(sa))
	return OK
}

func aioCmdPeername(i *Interp, af *aioFile, argv []*Obj) int {
	sa, err := syscall.Getpeername(af.fd)
	if err != nil {
		af.errno = err
		aioSetError(i, af, nil)
		return ERR
	}
	i.SetResultString(aioFormatSocketAddress(sa))
	return OK
}

func aioCmdRecvfrom(i *Interp, af *aioFile, argv []*Obj) int {
	length, rc := i.GetWide(argv[0])
	if rc != OK {
		return ERR
	}
	buf := make([]byte, length+1)
	n, sa, err := syscall.Recvfrom(af.fd, buf[:length], 0)
	if err != nil {
		af.errno = err
		aioSetError(i, af, nil)
		return ERR
	}
	i.SetResult(i.NewStringObjBytes(buf[:n]))
	if len(argv) > 1 {
		return i.SetVariable(argv[1], i.NewStringObj(aioFormatSocketAddress(sa)))
	}
	return OK
}

func aioCmdSendto(i *Interp, af *aioFile, argv []*Obj) int {
	// Note that we don't validate the socket type. Rely on sendto() failing
	// if appropriate.
	sa, rc := aioParseSocketAddress(i, af.addrFam, syscall.SOCK_DGRAM, argv[1].String())
	if rc != OK {
		return ERR
	}
	if err := syscall.Sendto(af.fd, argv[0].Bytes(), 0, sa); err != nil {
		af.errno = err
		aioSetError(i, af, nil)
		return ERR
	}
	i.SetResultInt(int64(argv[0].Len()))
	return OK
}

// aioParseSocketAddress turns an address into a sockaddr for the given
// address family (C: JimParseSocketAddress).
func aioParseSocketAddress(i *Interp, family, socktype int, addr string) (syscall.Sockaddr, int) {
	switch family {
	case syscall.AF_UNIX:
		return aioParseDomainAddress(addr), OK
	case syscall.AF_INET6:
		i.SetResultString("ipv6 not supported")
		return nil, ERR
	case syscall.AF_INET:
		return aioParseIPAddress(i, socktype, addr)
	}
	return nil, ERR
}

// aioParseDomainAddress makes a Unix domain sockaddr from a path
// (C: JimParseDomainAddress).
func aioParseDomainAddress(path string) syscall.Sockaddr {
	return &syscall.SockaddrUnix{Name: path}
}

// aioParseIPAddress parses an IPv4 "addr:port", "addr" or "port" address. A
// missing address means INADDR_ANY and a missing port means 0, which is only
// useful for server sockets (C: JimParseIpAddress).
func aioParseIPAddress(i *Interp, socktype int, hostport string) (syscall.Sockaddr, int) {
	sthost, stport := "0.0.0.0", hostport
	if n := strings.LastIndex(hostport, ":"); n >= 0 {
		sthost, stport = hostport[:n], hostport[n+1:]
	}
	network := "tcp4"
	if socktype == syscall.SOCK_DGRAM {
		network = "udp4"
	}
	// This stands in for getaddrinfo(): it resolves both host names and
	// service names, restricted to the IPv4 family.
	ip, port, ok := aioResolveIPv4(network, sthost, stport)
	if !ok {
		i.Errorf("Not a valid address: %s", hostport)
		return nil, ERR
	}
	sa := &syscall.SockaddrInet4{Port: port}
	copy(sa.Addr[:], ip)
	return sa, OK
}

func aioResolveIPv4(network, host, port string) (ip net.IP, portnum int, ok bool) {
	portnum, err := net.LookupPort(network, port)
	if err != nil {
		return nil, 0, false
	}
	if host == "" {
		// getaddrinfo() rejects an empty node name
		return nil, 0, false
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			return ip4, portnum, true
		}
		return nil, 0, false
	}
	addrs, err := net.LookupIP(host)
	if err != nil {
		return nil, 0, false
	}
	for _, a := range addrs {
		if ip4 := a.To4(); ip4 != nil {
			return ip4, portnum, true
		}
	}
	return nil, 0, false
}

// aioSockopts is the table of settable socket options (C: sockopt_def).
var aioSockopts = []struct {
	name  string
	level int
	opt   int
	bool_ bool
}{
	{"broadcast", syscall.SOL_SOCKET, syscall.SO_BROADCAST, true},
	{"debug", syscall.SOL_SOCKET, syscall.SO_DEBUG, true},
	{"keepalive", syscall.SOL_SOCKET, syscall.SO_KEEPALIVE, true},
	{"oobinline", syscall.SOL_SOCKET, syscall.SO_OOBINLINE, true},
	{"sndbuf", syscall.SOL_SOCKET, syscall.SO_SNDBUF, false},
	{"rcvbuf", syscall.SOL_SOCKET, syscall.SO_RCVBUF, false},
	{"tcp_nodelay", syscall.IPPROTO_TCP, syscall.TCP_NODELAY, false},
}

func aioCmdSockopt(i *Interp, af *aioFile, argv []*Obj) int {
	if len(argv) == 0 {
		dictObj := i.NewListObj(nil)
		for _, so := range aioSockopts {
			value, err := syscall.GetsockoptInt(af.fd, so.level, so.opt)
			if err != nil {
				continue
			}
			if so.bool_ && value != 0 {
				value = 1
			}
			i.ListAppendElement(dictObj, i.NewStringObj(so.name))
			i.ListAppendElement(dictObj, i.NewIntObj(int64(value)))
		}
		i.SetResult(dictObj)
		return OK
	}
	if len(argv) == 1 {
		return subCmdBadArgs
	}
	for _, so := range aioSockopts {
		if argv[0].String() != so.name {
			continue
		}
		var on int
		if so.bool_ {
			b, rc := i.GetBoolean(argv[1])
			if rc != OK {
				return ERR
			}
			if b {
				on = 1
			}
		} else {
			v, rc := i.GetWide(argv[1])
			if rc != OK {
				return ERR
			}
			on = int(v)
		}
		if err := syscall.SetsockoptInt(af.fd, so.level, so.opt, on); err != nil {
			return i.Errorf("Failed to set %#s: %s", argv[0], ioStrerror(err))
		}
		return OK
	}
	return i.Errorf("Unknown sockopt %#s", argv[0])
}

/* ---------------------------------------------------------------------------
 * The subcommand table
 * ------------------------------------------------------------------------ */

// aioSubCmdFn is a channel subcommand; unlike a plain command procedure it
// also receives the channel it was invoked on (C: Jim_CmdPrivData).
type aioSubCmdFn func(i *Interp, af *aioFile, argv []*Obj) int

// aioSubCmdDef is one entry of the channel subcommand table.
type aioSubCmdDef struct {
	cmd, args string
	fn        aioSubCmdFn
	minArgs   int
	maxArgs   int
	flags     int
}

// aioCommandTable is the channel subcommand table (C: aio_command_table). It
// is a function rather than a variable because [accept] creates a channel,
// which needs the table again.
func aioCommandTable() []aioSubCmdDef {
	return []aioSubCmdDef{
		{"read", "?-nonewline|len?", aioCmdRead, 0, 2, 0},
		{"copyto", "handle ?size?", aioCmdCopy, 1, 2, 0},
		{"getfd", "", aioCmdGetfd, 0, 0, ModFlagHidden},
		{"gettaint", "?-source|-sink?", aioCmdGettaint, 1, 1, ModFlagHidden},
		{"gets", "?-eol <str>? ?-keep 0|1? ?var?", aioCmdGets, 0, -1, 0},
		{"puts", "?-nonewline? str", aioCmdPuts, 1, 2, 0},
		{"isatty", "", aioCmdIsatty, 0, 0, 0},
		{"recvfrom", "len ?addrvar?", aioCmdRecvfrom, 1, 2, 0},
		{"sendto", "str address", aioCmdSendto, 2, 2, 0},
		{"accept", "?addrvar?", aioCmdAccept, 0, 1, 0},
		{"listen", "backlog", aioCmdListen, 1, 1, 0},
		{"sockopt", "?opt 0|1?", aioCmdSockopt, 0, 2, 0},
		{"sockname", "", aioCmdSockname, 0, 0, 0},
		{"peername", "", aioCmdPeername, 0, 0, 0},
		{"flush", "", aioCmdFlush, 0, 0, 0},
		{"eof", "", aioCmdEof, 0, 0, 0},
		{"close", "?r(ead)|w(rite)?", aioCmdClose, 0, 1, ModFlagFullArgv},
		{"seek", "offset ?start|current|end", aioCmdSeek, 1, 2, 0},
		{"tell", "", aioCmdTell, 0, 0, 0},
		{"filename", "", aioCmdFilename, 0, 0, 0},
		{"ndelay", "?0|1?", aioCmdNdelay, 0, 1, 0},
		{"sync", "", aioCmdSync, 0, 0, 0},
		{"buffering", "?none|line|full? ?size?", aioCmdBuffering, 0, 2, 0},
		{"translation", "binary|text", aioCmdTranslation, 1, 1, 0},
		{"readsize", "?size?", aioCmdReadsize, 0, 1, 0},
		{"stat", "?var?", aioCmdStat, 0, 1, 0},
		{"lock", "?-wait?", aioCmdLock, 0, 1, 0},
		{"unlock", "", aioCmdUnlock, 0, 0, 0},
		{"tty", "?baud rate? ?data bits? ?stop bits? ?parity even|odd|none? ?handshake xonxoff|rtscts|none? ?input raw|cooked? ?output raw|cooked? ?echo 0|1? ?vmin n? ?vtime n? ?vstart char? ?vstop char?", aioCmdTty, 0, -1, 0},
		{"ttycontrol", "?rts 0|1? ?dtr 0|1? ?break duration?", aioCmdTtycontrol, 0, -1, 0},
	}
}

// aioSubCmds builds the subcommand table of one channel, binding each
// subcommand to that channel.
func aioSubCmds(af *aioFile) []SubCmd {
	defs := aioCommandTable()
	table := make([]SubCmd, len(defs))
	for n := range defs {
		e := &defs[n]
		fn := e.fn
		table[n] = SubCmd{
			Cmd: e.cmd, Args: e.args, MinArgs: e.minArgs, MaxArgs: e.maxArgs, Flags: e.flags,
			Fn: func(i *Interp, argv []*Obj) int { return fn(i, af, argv) },
		}
	}
	return table
}

/* ---------------------------------------------------------------------------
 * Creating channels and the [open] command
 * ------------------------------------------------------------------------ */

// aioParsePosixOpenMode parses a list of POSIX open flag names
// (C: parse_posix_open_mode).
func aioParsePosixOpenMode(i *Interp, modeObj *Obj) (int, int) {
	modetypes := []string{"RDONLY", "WRONLY", "RDWR", "APPEND", "BINARY", "CREAT", "EXCL", "NOCTTY", "TRUNC"}
	modeflags := []int{syscall.O_RDONLY, syscall.O_WRONLY, syscall.O_RDWR, syscall.O_APPEND, 0,
		syscall.O_CREAT, syscall.O_EXCL, syscall.O_NOCTTY, syscall.O_TRUNC}

	flags := 0
	for n, l := 0, i.ListLength(modeObj); n < l; n++ {
		opt, rc := i.GetEnum(i.ListGetIndex(modeObj, n), modetypes, "access mode", ErrMsg)
		if rc != OK {
			return -1, ERR
		}
		flags |= modeflags[opt]
	}
	return flags, OK
}

// aioParseOpenMode turns an [open] mode string into open(2) flags
// (C: parse_open_mode).
func aioParseOpenMode(i *Interp, filenameObj, modeObj *Obj) (int, int) {
	mode := modeObj.String()
	if strings.HasPrefix(mode, "R") || strings.HasPrefix(mode, "W") {
		return aioParsePosixOpenMode(i, modeObj)
	}

	var flags int
	switch {
	case strings.HasPrefix(mode, "r"):
		flags = syscall.O_RDONLY
	case strings.HasPrefix(mode, "w"):
		flags = syscall.O_WRONLY | syscall.O_CREAT | syscall.O_TRUNC
	case strings.HasPrefix(mode, "a"):
		flags = syscall.O_WRONLY | syscall.O_CREAT | syscall.O_APPEND
	default:
		i.Errorf("%s: invalid open mode '%s'", filenameObj.String(), mode)
		return -1, ERR
	}
	mode = mode[1:]

	// The remaining modifiers are optional and order dependent; "b" and "t"
	// have no effect on a POSIX system.
	mode = strings.TrimPrefix(mode, "b")
	mode = strings.TrimPrefix(mode, "t")
	if strings.HasPrefix(mode, "+") {
		mode = mode[1:]
		// read+write so set O_RDWR instead
		flags &= ^(syscall.O_RDONLY | syscall.O_WRONLY)
		flags |= syscall.O_RDWR
	}
	if strings.HasPrefix(mode, "x") {
		mode = mode[1:]
		flags |= syscall.O_EXCL
	}
	// The remaining "F" (O_LARGEFILE) and "e" (close on exec) modifiers are
	// already the default here, and anything else is silently ignored.
	return flags, OK
}

// aioOpenCommand implements [open] (C: JimAioOpenCommand).
func aioOpenCommand(i *Interp, argv []*Obj) int {
	// filehandles created by open are not tainted by default
	flags := aioNoTaint
	n := 0
	if len(argv) > 2 && argv[2].String() == "-noclose" {
		flags = aioKeepOpen
		n++
	}
	if len(argv) > 3+n {
		// As with a subcommand, a negative return asks the caller to report
		// the command's usage (C: JIM_USAGE).
		return subCmdBadArgs
	}

	filename := argv[1].String()
	if strings.HasPrefix(filename, "|") {
		// A leading '|' means a pipeline, which tclcompat.tcl implements
		evalObj := []*Obj{i.NewStringObj("::popen"), i.NewStringObj(filename[1:])}
		if len(argv) == 3+n {
			evalObj = append(evalObj, argv[2+n])
		}
		return i.EvalObjVector(evalObj)
	}

	openflags := syscall.O_RDONLY
	if len(argv) == 3+n {
		var rc int
		if openflags, rc = aioParseOpenMode(i, argv[1], argv[2+n]); rc != OK {
			return ERR
		}
	}

	fd, err := syscall.Open(filename, openflags, 0666)
	if err != nil {
		return i.Errorf("%#s: %s", argv[1], ioStrerror(err))
	}
	aioMakeChannel(i, fd, argv[1], "aio.handle%ld", 0, flags)
	return OK
}

// aioMakeChannel creates a channel command for fd (C: JimMakeChannel).
func aioMakeChannel(i *Interp, fd int, filename *Obj, hdlfmt string, family, flags int) *aioFile {
	i.id++
	cmdName := i.NewStringObj(strings.Replace(hdlfmt, "%ld", formatWide(i.id), 1))
	if filename == nil {
		filename = cmdName
	}

	af := &aioFile{
		filename:  filename,
		cmdName:   cmdName,
		fd:        fd,
		addrFam:   family,
		writebuf:  []byte{},
		wbufLimit: aioDefaultWbufLimit,
		rbufLen:   aioDefaultRbufLen,
	}
	if flags&aioWbufNone != 0 {
		af.wbuft = wbufOptNone
	} else if aioIsatty(fd) {
		af.wbuft = wbufOptLine
	} else {
		af.wbuft = wbufOptFull
	}
	if flags&aioKeepOpen == 0 {
		syscall.CloseOnExec(fd)
	}
	// don't set flags yet so that aioSetNonblocking() works
	aioSetNonblocking(af, flags&aioNonBlock != 0)
	af.flags |= flags

	table := aioSubCmds(af)
	cmd := i.RegisterCmd(cmdName.String(), "", 0, -1,
		func(i *Interp, argv []*Obj) int { return i.CallSubCmd(i.ParseSubCmd(table, argv), argv) },
		CmdIsChannel)
	cmd.subCmds = table
	cmd.privData = af

	ch := i.aioChannels()
	ch.list = append(ch.list, af)

	// Note that the command must use the global namespace, even if the
	// current namespace is something different.
	i.SetResult(i.MakeGlobalNamespaceName(cmdName))
	return af
}

// aioMakeChannelPair creates a pair of channels, as pipe(), socketpair() and
// openpty() produce, and returns them as a two element list
// (C: JimMakeChannelPair).
func aioMakeChannelPair(i *Interp, p [2]int, filename *Obj, hdlfmt string, family, flags int) int {
	aioMakeChannel(i, p[0], filename, hdlfmt, family, flags)
	objPtr := i.NewListObj(nil)
	i.ListAppendElement(objPtr, i.Result())
	aioMakeChannel(i, p[1], filename, hdlfmt, family, flags)
	i.ListAppendElement(objPtr, i.Result())
	i.SetResult(objPtr)
	return OK
}

// aioCreatePipe creates a pipe and returns both ends as channels
// (C: JimCreatePipe).
func aioCreatePipe(i *Interp, filenameObj *Obj, flags int) int {
	var p [2]int
	if err := syscall.Pipe(p[:]); err != nil {
		return aioSetSyscallError(i, err)
	}
	return aioMakeChannelPair(i, p, filenameObj, "aio.pipe%ld", 0, flags)
}

// aioPipeCommand implements [pipe]. Use "socket -noclose pipe" if the ends
// should stay open (C: JimAioPipeCommand).
func aioPipeCommand(i *Interp, argv []*Obj) int {
	return aioCreatePipe(i, argv[0], 0)
}

// aioOpenPtyCommand creates a pseudo terminal pair (C: JimAioOpenPtyCommand).
func aioOpenPtyCommand(i *Interp) int {
	p, path, err := aioOpenpty()
	if err != nil {
		return aioSetSyscallError(i, err)
	}
	// Note: the replica path is used for both handles
	return aioMakeChannelPair(i, p, i.NewStringObj(path), "aio.pty%ld", 0, 0)
}

// aioOpenpty is openpty(3): it opens the pseudo terminal multiplexer, unlocks
// the replica and opens that too. grantpt() is a no-op on a devpts file
// system, so it is not reproduced here.
func aioOpenpty() (p [2]int, path string, err error) {
	primary, err := syscall.Open("/dev/ptmx", syscall.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return p, "", err
	}
	var unlock int32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(primary),
		syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))); errno != 0 {
		syscall.Close(primary)
		return p, "", errno
	}
	var ptn int32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(primary),
		syscall.TIOCGPTN, uintptr(unsafe.Pointer(&ptn))); errno != 0 {
		syscall.Close(primary)
		return p, "", errno
	}
	path = fmt.Sprintf("/dev/pts/%d", ptn)
	replica, err := syscall.Open(path, syscall.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		syscall.Close(primary)
		return p, "", err
	}
	return [2]int{primary, replica}, path, nil
}

// aioSetSyscallError reports a failed system call the way the original does
// when it has no channel to name (C: JimAioSetError with a NULL name).
func aioSetSyscallError(i *Interp, err error) int {
	i.SetResultString(ioStrerror(err))
	return ERR
}

// The socket types [socket] understands (C: socktypes).
var aioSocktypes = []string{
	"unix",
	"unix.server",
	"unix.dgram",
	"unix.dgram.server",
	"dgram",
	"dgram.server",
	"stream",
	"stream.server",
	"pipe",
	"pair",
	"pty",
}

const (
	sockUnix = iota
	sockUnixServer
	sockUnixDgram
	sockUnixDgramServer
	sockDgramClient
	sockDgramServer
	sockStreamClient
	sockStreamServer
	sockStreamPipe
	sockStreamSocketpair
	sockStreamPty
)

// aioCheckShowCommands answers "-commands" with the sorted table
// (C: Jim_CheckShowCommands).
func aioCheckShowCommands(i *Interp, table []string) int {
	sorted := make([]string, len(table))
	copy(sorted, table)
	sort.Strings(sorted)
	listObj := i.NewListObj(nil)
	for _, name := range sorted {
		i.ListAppendElement(listObj, i.NewStringObj(name))
	}
	i.SetResult(listObj)
	return OK
}

// aioSockCommand implements [socket] (C: JimAioSockCommand).
func aioSockCommand(i *Interp, argv []*Obj) int {
	if len(argv) == 2 && argv[1].String() == "-commands" {
		return aioCheckShowCommands(i, aioSocktypes)
	}

	family := syscall.AF_INET
	socketType := syscall.SOCK_STREAM
	ipv6 := false
	flags := 0

	for len(argv) > 1 && strings.HasPrefix(argv[1].String(), "-") {
		options := []string{"-async", "-ipv6", "-noclose"}
		const (
			optAsync = iota
			optIPv6
			optNoclose
		)
		option, rc := i.GetEnum(argv[1], options, "", ErrMsg)
		if rc != OK {
			return ERR
		}
		switch option {
		case optAsync:
			flags |= aioNonBlock
		case optIPv6:
			// This build has no IPv6 support, so the option always fails
			i.SetResultString("ipv6 not supported")
			return ERR
		case optNoclose:
			flags |= aioKeepOpen
		}
		argv = argv[1:]
	}

	if len(argv) < 2 {
		return subCmdBadArgs
	}

	socktype, rc := i.GetEnum(argv[1], aioSocktypes, "socktype", ErrMsg)
	if rc != OK {
		// No need to check for -commands here since we did it above
		return ERR
	}

	i.SetResult(i.emptyObj)

	var filename *Obj
	addr, haveAddr := "", false
	if len(argv) > 2 {
		addr, haveAddr = argv[2].String(), true
		filename = argv[2]
	}

	switch socktype {
	case sockStreamSocketpair:
		if haveAddr || ipv6 {
			return subCmdBadArgs
		}
		p, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
		if err != nil {
			return aioSetSyscallError(i, err)
		}
		return aioMakeChannelPair(i, [2]int{p[0], p[1]}, argv[1], "aio.sockpair%ld", syscall.AF_UNIX, 0)

	case sockStreamPipe:
		if haveAddr || ipv6 {
			return subCmdBadArgs
		}
		return aioCreatePipe(i, argv[1], flags)

	case sockStreamPty:
		if haveAddr || ipv6 {
			return subCmdBadArgs
		}
		return aioOpenPtyCommand(i)
	}

	// Now all the remaining socket types are very similar
	bindAddr, connectAddr := "", ""
	reuse, doListen := false, false

	switch socktype {
	case sockDgramClient:
		connectAddr = addr
		socketType = syscall.SOCK_DGRAM

	case sockStreamClient:
		if !haveAddr {
			return subCmdBadArgs
		}
		connectAddr = addr

	case sockStreamServer:
		if !haveAddr {
			return subCmdBadArgs
		}
		bindAddr = addr
		reuse = true
		doListen = true

	case sockDgramServer:
		if !haveAddr {
			return subCmdBadArgs
		}
		bindAddr = addr
		socketType = syscall.SOCK_DGRAM
		reuse = true

	case sockUnix:
		if !haveAddr {
			return subCmdBadArgs
		}
		connectAddr = addr
		family = syscall.AF_UNIX

	case sockUnixDgram:
		connectAddr = addr
		socketType = syscall.SOCK_DGRAM
		family = syscall.AF_UNIX
		// A dgram unix domain socket client needs to bind to a temporary
		// address to allow the server to send responses
		fd, rc := fileMakeTempFile(i, "")
		if rc != OK {
			return ERR
		}
		syscall.Close(fd)
		syscall.Unlink(i.Result().String())
		bindAddr = i.Result().String()

	case sockUnixServer:
		if !haveAddr {
			return subCmdBadArgs
		}
		bindAddr = addr
		family = syscall.AF_UNIX
		doListen = true

	case sockUnixDgramServer:
		if !haveAddr {
			return subCmdBadArgs
		}
		bindAddr = addr
		socketType = syscall.SOCK_DGRAM
		family = syscall.AF_UNIX

	default:
		i.SetResultString("Unsupported socket type")
		return ERR
	}

	// Now do all the steps necessary for the given socket type
	sock, err := syscall.Socket(family, socketType, 0)
	if err != nil {
		return aioSetSyscallError(i, err)
	}
	if bindAddr != "" {
		sa, rc := aioParseSocketAddress(i, family, socketType, bindAddr)
		if rc != OK {
			syscall.Close(sock)
			return ERR
		}
		if reuse {
			syscall.SetsockoptInt(sock, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
		}
		if err := syscall.Bind(sock, sa); err != nil {
			syscall.Close(sock)
			return i.Errorf("%s: bind: %s", bindAddr, ioStrerror(err))
		}
	}
	if connectAddr != "" {
		sa, rc := aioParseSocketAddress(i, family, socketType, connectAddr)
		if rc != OK {
			syscall.Close(sock)
			return ERR
		}
		// Note that the original never reports an in-progress connect as OK:
		// -async only sets the non-blocking flag, which is applied after the
		// connect, so the connect here is always a blocking one.
		if err := syscall.Connect(sock, sa); err != nil {
			syscall.Close(sock)
			return i.Errorf("%s: connect: %s", connectAddr, ioStrerror(err))
		}
	}
	if doListen {
		if err := syscall.Listen(sock, 5); err != nil {
			syscall.Close(sock)
			return i.Errorf("listen: %s", ioStrerror(err))
		}
	}
	if filename == nil {
		filename = argv[1]
	}

	aioMakeChannel(i, sock, filename, "aio.sock%ld", family, flags)
	return OK
}

// aioMakeStdioChannel creates one of the standard stream channels
// (C: JimMakeStdioChannel).
func aioMakeStdioChannel(i *Interp, fd int, name string, flags int) {
	aioMakeChannel(i, fd, nil, name, 0, aioKeepOpen|flags)
}

// aioPutsCoreCommand is jim.c's debugging [puts], which writes to the C
// library's stdout rather than to the [stdout] channel. jimsh replaces it with
// the Tcl version from tclcompat.tcl as soon as the channels exist, but the
// core command table defines it (C: Jim_PutsCoreCommand).
func aioPutsCoreCommand(i *Interp, argv []*Obj) int {
	if len(argv) == 3 {
		if argv[1].String() != "-nonewline" {
			i.SetResultString("The second argument must be -nonewline")
			return ERR
		}
		cStdout.write(argv[2].Bytes())
	} else {
		cStdout.write(argv[1].Bytes())
		cStdout.write([]byte{'\n'})
	}
	return OK
}

// registerPutsCommand registers [puts], which jim.c has in its core command
// table rather than in the aio extension.
func registerPutsCommand(i *Interp) {
	i.RegisterCmd("puts", "?-nonewline? string", 1, 2, aioPutsCoreCommand, 0)
}

// registerAioCommands registers [open], [socket], [pipe] and the standard
// stream channels (C: Jim_aioInit).
func registerAioCommands(i *Interp) {
	i.PackageProvide("aio", "1.0", ErrMsg)

	i.RegisterCmd("open", "filename ?-noclose? ?mode?", 1, 3, aioOpenCommand, 0)
	i.RegisterCmd("socket", "?-async? ?-ipv6? socktype ?address?", 1, 4, aioSockCommand, 0)
	i.RegisterCmd("pipe", "", 0, 0, aioPipeCommand, 0)

	// Create filehandles for stdin, stdout and stderr. Note that each of them
	// consumes an interpreter id, so the first [open] handle is aio.handle4.
	aioMakeStdioChannel(i, 0, "stdin", 0)
	aioMakeStdioChannel(i, 1, "stdout", 0)
	aioMakeStdioChannel(i, 2, "stderr", aioWbufNone)
}

// FlushChannels flushes the buffered output channels, as the interpreter does
// when it is freed.
func (i *Interp) FlushChannels() {
	ch, _ := i.channels.(*aioChannels)
	if ch == nil {
		return
	}
	for _, af := range ch.list {
		aioFlush(i, af)
	}
}
