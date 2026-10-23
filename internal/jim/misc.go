/* Jim Tcl 0.84 ported from C to Go: the [env], [stacktrace], [time],
 * [timerate] and [xtrace] commands.
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
	"time"
)

// registerMiscCommands registers the remaining core commands of jim.c: the
// usage strings and argument counts are those of Jim_CoreCommandsTable.
func registerMiscCommands(i *Interp) {
	i.RegisterCmd("env", "?varName? ?default?", 0, 2, envCoreCommand, 0)
	i.RegisterCmd("stacktrace", "?firstlevel? ?lastlevel?", 0, 2, stacktraceCoreCommand, 0)
	i.RegisterCmd("time", "script ?count?", 1, 2, timeCoreCommand, 0)
	i.RegisterCmd("timerate", "script ?milliseconds?", 1, 2, timerateCoreCommand, 0)
	i.RegisterCmd("xtrace", "callback", 1, 1, xtraceCoreCommand, 0)
	i.RegisterCmd("collect", "", 0, 0, collectCoreCommand, 0)
	i.RegisterCmd("finalize", "reference ?finalizerProc?", 1, 2, finalizeCoreCommand, 0)
	i.RegisterCmd("getref", "reference", 1, 1, getrefCoreCommand, 0)
	i.RegisterCmd("ref", "string tag ?finalizer?", 2, 3, refCoreCommand, 0)
	i.RegisterCmd("setref", "reference newValue", 2, 2, setrefCoreCommand, 0)
}

// timeUsecBase anchors the monotonic clock that [time] and [timerate] read
// (C: Jim_GetTimeUsec(CLOCK_MONOTONIC_RAW)).
var timeUsecBase = time.Now()

// timeUsec returns the monotonic time in microseconds.
func timeUsec() int64 { return int64(time.Since(timeUsecBase) / time.Microsecond) }

// envCoreCommand implements [env] (C: Jim_EnvCoreCommand).
func envCoreCommand(i *Interp, argv []*Obj) int {
	if len(argv) == 1 {
		listObjPtr := i.NewListObj(nil)
		for _, e := range os.Environ() {
			if equals := strings.IndexByte(e, '='); equals >= 0 {
				i.ListAppendElement(listObjPtr, i.NewStringObj(e[:equals]))
				i.ListAppendElement(listObjPtr, i.NewStringObj(e[equals+1:]))
			}
		}
		i.SetResult(listObjPtr)
		return OK
	}

	val, ok := os.LookupEnv(argv[1].String())
	if !ok {
		if len(argv) < 3 {
			return i.Errorf("environment variable \"%#s\" does not exist", argv[1])
		}
		val = argv[2].String()
	}
	i.SetResult(i.NewStringObj(val))
	return OK
}

// stacktraceCoreCommand implements [stacktrace]
// (C: Jim_StacktraceCoreCommand).
func stacktraceCoreCommand(i *Interp, argv []*Obj) int {
	var skip, last int64

	if len(argv) > 1 {
		var rc int
		if skip, rc = i.GetWideExpr(argv[1]); rc != OK {
			return ERR
		}
	}
	if len(argv) > 2 {
		var rc int
		if last, rc = i.GetWideExpr(argv[2]); rc != OK {
			return ERR
		}
	}

	listObj := i.NewListObj(nil)
	for n := skip; n <= int64(i.procLevel); n++ {
		frame := i.getEvalFrameByProcLevel(int(-n))
		if frame == nil || int64(frame.procLevel) < last {
			break
		}
		i.addStackFrame(frame, listObj)
	}
	i.SetResult(listObj)
	return OK
}

// timeCoreCommand implements [time] (C: Jim_TimeCoreCommand).
func timeCoreCommand(i *Interp, argv []*Obj) int {
	count := int64(1)
	if len(argv) == 3 {
		var rc int
		if count, rc = i.GetWide(argv[2]); rc != OK {
			return ERR
		}
	}
	if count < 0 {
		return OK
	}
	start := timeUsec()
	for n := count; n > 0; n-- {
		if retval := i.EvalObj(argv[1]); retval != OK {
			return retval
		}
	}
	elapsed := timeUsec() - start

	// Short runs are reported as a fractional number of microseconds.
	var result *Obj
	if elapsed < count*10 {
		result = i.NewDoubleObj(float64(elapsed) * 1.0 / float64(count))
	} else if count == 0 {
		result = i.NewIntObj(0)
	} else {
		result = i.NewIntObj(elapsed / count)
	}
	i.SetResult(i.NewStringObj(result.String() + " microseconds per iteration"))
	return OK
}

// timerateCoreCommand implements [timerate] (C: Jim_TimerateCoreCommand).
func timerateCoreCommand(i *Interp, argv []*Obj) int {
	us := int64(0)
	if len(argv) == 3 {
		ms, rc := i.GetWide(argv[2])
		if rc != OK {
			return ERR
		}
		us = ms * 1000
	}
	if us < 1 {
		// Default is 1 second
		us = 1000 * 1000
	}

	// Run until we exceed the time limit
	start := timeUsec()
	count := 0
	var delta int64
	for {
		retval := i.EvalObj(argv[1])
		delta = timeUsec() - start
		if retval != OK {
			return retval
		}
		count++
		if delta >= us {
			break
		}
	}

	// Now try to account for the loop and eval overhead
	start = timeUsec()
	var overhead int64
	for n := 0; n < count; n++ {
		retval := i.EvalObj(i.nullScriptObj)
		overhead = timeUsec() - start
		if retval != OK {
			return retval
		}
	}

	delta -= overhead

	usPerIter := float64(delta) / float64(count)
	objPtr := i.NewListObj(nil)
	i.ListAppendElement(objPtr, i.NewStringObj("us_per_iter"))
	i.ListAppendElement(objPtr, i.NewDoubleObj(usPerIter))
	i.ListAppendElement(objPtr, i.NewStringObj("iters_per_sec"))
	i.ListAppendElement(objPtr, i.NewDoubleObj(1e6/usPerIter))
	i.ListAppendElement(objPtr, i.NewStringObj("count"))
	i.ListAppendElement(objPtr, i.NewIntObj(int64(count)))
	i.ListAppendElement(objPtr, i.NewStringObj("elapsed_us"))
	i.ListAppendElement(objPtr, i.NewIntObj(delta))
	i.SetResult(objPtr)
	return OK
}

// xtraceCoreCommand implements [xtrace] (C: Jim_XtraceCoreCommand).
func xtraceCoreCommand(i *Interp, argv []*Obj) int {
	i.traceCmdObj = nil
	if argv[1].Len() != 0 {
		// Install the new execution trace callback
		i.traceCmdObj = argv[1]
	}
	return OK
}
