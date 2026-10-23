/* Jim - A small embeddable Tcl interpreter: the expression math functions.
 *
 * Copyright 2005 Salvatore Sanfilippo <antirez@invece.org>
 * Copyright 2005 Clemens Hintze <c.hintze@gmx.net>
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

import "math"

// exprOpDoubleUnary evaluates one of the libm functions of a single argument.
// The C calls the C library directly; Go's math package is used here, which
// can differ in the last bit for some of these functions.
func (i *Interp) exprOpDoubleUnary(node *exprNode) int {
	A, rc := i.exprGetTerm(node.left)
	if rc != OK {
		return rc
	}

	dA, rc := i.GetDouble(A)
	if rc != OK {
		return rc
	}

	var dC float64
	switch node.typ {
	case exprOpFuncSin:
		dC = math.Sin(dA)
	case exprOpFuncCos:
		dC = math.Cos(dA)
	case exprOpFuncTan:
		dC = math.Tan(dA)
	case exprOpFuncAsin:
		dC = math.Asin(dA)
	case exprOpFuncAcos:
		dC = math.Acos(dA)
	case exprOpFuncAtan:
		dC = math.Atan(dA)
	case exprOpFuncSinh:
		dC = math.Sinh(dA)
	case exprOpFuncCosh:
		dC = math.Cosh(dA)
	case exprOpFuncTanh:
		dC = math.Tanh(dA)
	case exprOpFuncCeil:
		dC = math.Ceil(dA)
	case exprOpFuncFloor:
		dC = math.Floor(dA)
	case exprOpFuncExp:
		dC = math.Exp(dA)
	case exprOpFuncLog:
		dC = math.Log(dA)
	case exprOpFuncLog10:
		dC = math.Log10(dA)
	case exprOpFuncSqrt:
		dC = math.Sqrt(dA)
	}
	i.SetResult(i.NewDoubleObj(dC))

	return rc
}
