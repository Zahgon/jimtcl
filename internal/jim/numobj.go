/* Jim Tcl 0.84 ported from C to Go: the numeric, boolean, index and
 * return-code object representations.
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
	"math"
	"strconv"
	"strings"
)

// maxIndex is C's INT_MAX, the saturation value of an index.
const maxIndex = math.MaxInt32

// minIntInDouble and maxIntInDouble bound the integers that can be used as a
// double without loss, assuming a 53 bit mantissa.
const (
	minIntInDouble = -(int64(1) << 53)
	maxIntInDouble = -(minIntInDouble + 1)
)

// formatWide renders an integer object (C: UpdateStringOfInt).
func formatWide(w int64) string { return strconv.FormatInt(w, 10) }

// formatDouble renders a double object (C: UpdateStringOfDouble). The C uses
// "%.12g" and then appends ".0" when the result has neither a '.' nor an 'e',
// so that an integral double still looks like a double.
func formatDouble(d float64) string {
	switch {
	case math.IsNaN(d):
		return "NaN"
	case math.IsInf(d, -1):
		return "-Inf"
	case math.IsInf(d, 1):
		return "Inf"
	}
	s := strconv.FormatFloat(d, 'g', 12, 64)
	if !strings.ContainsAny(s, ".e") {
		s += ".0"
	}
	return s
}

// setIntFromAny converts an object to the integer representation
// (C: SetIntFromAny). As in the C, the string representation is kept, so
// "set x 1.00; expr {$x+0}" still reports $x as 1.00.
func (i *Interp) setIntFromAny(o *Obj, flags int) int {
	if o.kind == kindCoercedDouble {
		// A coerced double already holds an integer value: simple switch.
		o.kind = kindInt
		return OK
	}

	w, ok, rangeErr := stringToWide(o.String(), 0)
	if !ok {
		if flags&ErrMsg != 0 {
			return i.Errorf("expected integer but got \"%#s\"", o)
		}
		return ERR
	}
	if (w == math.MinInt64 || w == math.MaxInt64) && rangeErr {
		i.SetResultString("Integer value too big to be represented")
		return ERR
	}
	o.freeIntRep()
	o.kind = kindInt
	o.wide = w
	return OK
}

// GetWide returns the integer value of an object (C: Jim_GetWide).
func (i *Interp) GetWide(o *Obj) (int64, int) {
	if o.kind != kindInt {
		if i.setIntFromAny(o, ErrMsg) == ERR {
			return 0, ERR
		}
	}
	return o.wide, OK
}

// GetWideNoErr returns the integer value of an object without setting an error
// result when the format is bad (C: JimGetWideNoErr).
func (i *Interp) GetWideNoErr(o *Obj) (int64, bool) {
	if o.kind != kindInt {
		if i.setIntFromAny(o, None) == ERR {
			return 0, false
		}
	}
	return o.wide, true
}

// GetWideExpr returns the integer value of an object, evaluating it as an
// expression if it is not already a plain integer (C: Jim_GetWideExpr).
func (i *Interp) GetWideExpr(o *Obj) (int64, int) {
	// As an optimisation, try to convert to int first.
	if o.kind == kindSource || o.kind == kindNone {
		i.setIntFromAny(o, None)
	}
	if o.kind == kindInt {
		return o.wide, OK
	}

	// safeexpr can never be set here, because evaluating an expression
	// safely can never cause a script to be run.
	i.safeexpr = true
	res, ret := i.EvalExpression(o)
	i.safeexpr = false

	var w int64
	if ret == OK {
		w, ret = i.GetWide(res)
	}
	if ret != OK {
		// By doing this we throw away any more detailed message, but typical
		// integer expressions won't be very complex.
		return 0, i.Errorf("expected integer expression but got \"%#s\"", o)
	}
	return w, OK
}

// setDoubleFromAny converts an object to the double representation
// (C: SetDoubleFromAny). An integer that a double can hold exactly becomes a
// "coerced double": an int masquerading as a double, which keeps the integer
// string representation so that converting back loses nothing.
func (i *Interp) setDoubleFromAny(o *Obj) int {
	if o.kind == kindInt && o.wide >= minIntInDouble && o.wide <= maxIntInDouble {
		o.kind = kindCoercedDouble
		return OK
	}

	// Preserve the string representation, needed so we can convert back to
	// int without loss.
	str := o.String()

	if w, ok, _ := stringToWide(str, 10); ok {
		o.freeIntRep()
		o.kind = kindCoercedDouble
		o.wide = w
		return OK
	}
	d, ok := stringToDouble(str)
	if !ok {
		return i.Errorf("expected floating-point number but got \"%#s\"", o)
	}
	o.freeIntRep()
	o.kind = kindDouble
	o.dbl = d
	return OK
}

// GetDouble returns the floating-point value of an object (C: Jim_GetDouble).
func (i *Interp) GetDouble(o *Obj) (float64, int) {
	if o.kind == kindCoercedDouble {
		return float64(o.wide), OK
	}
	if o.kind != kindDouble && i.setDoubleFromAny(o) == ERR {
		return 0, ERR
	}
	if o.kind == kindCoercedDouble {
		return float64(o.wide), OK
	}
	return o.dbl, OK
}

// trueFalseStrings are the words accepted as booleans; the first four are true
// (C: jim_true_false_strings).
var trueFalseStrings = []string{
	"1", "true", "yes", "on",
	"0", "false", "no", "off",
}

// setBooleanFromAny converts an object to an internal boolean value, which the
// C stores as an ordinary integer (C: SetBooleanFromAny).
func (i *Interp) setBooleanFromAny(o *Obj, flags int) int {
	index := -1
	for n, s := range trueFalseStrings {
		if s == o.String() {
			index = n
			break
		}
	}
	if index < 0 {
		if flags&ErrMsg != 0 {
			return i.Errorf("expected boolean but got \"%#s\"", o)
		}
		return ERR
	}
	o.freeIntRep()
	o.kind = kindInt
	// There are 4 true values in trueFalseStrings.
	if index < 4 {
		o.wide = 1
	} else {
		o.wide = 0
	}
	return OK
}

// GetBoolean returns the boolean value of an object (C: Jim_GetBoolean).
func (i *Interp) GetBoolean(o *Obj) (bool, int) {
	if o.kind != kindInt {
		if i.setBooleanFromAny(o, ErrMsg) == ERR {
			return false, ERR
		}
	}
	return o.wide != 0, OK
}

// indexStringRep renders an index object (C: UpdateStringOfIndex). "end" is
// stored as -1 and "end-N" as -(N+1); -INT_MAX is a plain negative index.
func indexStringRep(o *Obj) string {
	idx := int(o.wide)
	switch {
	case idx == -1:
		return "end"
	case idx >= 0 || idx == -maxIndex:
		return strconv.Itoa(idx)
	default:
		return "end" + strconv.Itoa(idx+1)
	}
}

// NewIndexObj returns an index object for the given value.
func (i *Interp) NewIndexObj(idx int) *Obj {
	return &Obj{kind: kindIndex, wide: int64(idx), charLen: -1}
}

// setIndexFromAny converts an object to the cached index representation
// (C: SetIndexFromAny). An index is an integer expression, optionally relative
// to "end".
func (i *Interp) setIndexFromAny(o *Obj) int {
	badIndex := func() int {
		return i.Errorf("bad index \"%#s\": must be intexpr or end?[+-]intexpr?", o)
	}

	str := o.String()
	end := false
	var idx int64
	exprObj := o

	if strings.HasPrefix(str, "end") {
		end = true
		str = str[3:]
		idx = 0
		switch {
		case str == "":
			exprObj = nil
		case str[0] == '-' || str[0] == '+':
			// A temporary object to evaluate the offset; this only happens
			// once unless the index object shimmers, since the result is kept.
			exprObj = i.NewStringObj(str)
		default:
			return badIndex()
		}
	}
	if exprObj != nil {
		w, ret := i.GetWideExpr(exprObj)
		if ret != OK {
			return badIndex()
		}
		idx = w
	}

	switch {
	case end:
		if idx > 0 {
			idx = maxIndex
		} else {
			idx-- // end-1 is represented as -2
		}
	case idx < 0:
		idx = -maxIndex
	case idx > maxIndex:
		return badIndex()
	}

	o.freeIntRep()
	o.kind = kindIndex
	o.wide = idx
	return OK
}

// GetIndex returns the index value of an object (C: Jim_GetIndex). Out of
// range indexes saturate at ±INT_MAX.
func (i *Interp) GetIndex(o *Obj) (int, int) {
	// Avoid shimmering if the object is an integer.
	if o.kind == kindInt {
		switch {
		case o.wide < 0:
			return -maxIndex, OK
		case o.wide > maxIndex:
			return maxIndex, OK
		}
		return int(o.wide), OK
	}
	if o.kind != kindIndex && i.setIndexFromAny(o) == ERR {
		return 0, ERR
	}
	return int(o.wide), OK
}

// returnCodes are the names of the standard return codes, in the same order as
// OK, ERR, ... (C: jimReturnCodes).
var returnCodes = []string{"ok", "error", "return", "break", "continue", "signal", "exit", "eval"}

// ReturnCodeName converts a return code to a string, or "?" for a
// non-standard code (C: Jim_ReturnCode).
func ReturnCodeName(code int) string {
	if code < 0 || code >= len(returnCodes) {
		return "?"
	}
	return returnCodes[code]
}

// setReturnCodeFromAny converts an object to a cached return-code
// representation (C: SetReturnCodeFromAny).
func (i *Interp) setReturnCodeFromAny(o *Obj) int {
	code, ok := i.GetWideNoErr(o)
	if ok {
		// The C stores the code in an int, so a wide value is truncated.
		code = int64(int32(code))
	} else {
		n, ret := i.GetEnum(o, returnCodes, "", None)
		if ret != OK {
			return i.Errorf("expected return code but got \"%#s\"", o)
		}
		code = int64(n)
	}
	o.freeIntRep()
	o.kind = kindReturnCode
	o.wide = code
	return OK
}

// GetReturnCode returns the return code an object stands for
// (C: Jim_GetReturnCode).
func (i *Interp) GetReturnCode(o *Obj) (int, int) {
	if o.kind != kindReturnCode && i.setReturnCodeFromAny(o) == ERR {
		return 0, ERR
	}
	return int(o.wide), OK
}
