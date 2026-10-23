/* Jim - A small embeddable Tcl interpreter: the expression operators.
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

// Expression opcodes. The token type of an operator is its index in
// exprOperators plus ttExprOp, so this list must stay in step with the
// table below (C: the JIM_EXPROP enum).
const (
	/* Binary operators (numbers) */
	exprOpMul = ttExprOp + iota
	exprOpDiv
	exprOpMod
	exprOpSub
	exprOpAdd
	exprOpLShift
	exprOpRShift
	exprOpRotl
	exprOpRotr
	exprOpLt
	exprOpGt
	exprOpLte
	exprOpGte
	exprOpNumEq
	exprOpNumNe
	exprOpBitAnd
	exprOpBitXor
	exprOpBitOr
	exprOpLogicAnd
	exprOpLogicOr
	exprOpTernary
	exprOpColon
	exprOpPow

	/* Binary operators (strings) */
	exprOpStrEq
	exprOpStrNe
	exprOpStrGlob
	exprOpStrRe
	exprOpStrIn
	exprOpStrNi
	exprOpStrLt
	exprOpStrGt
	exprOpStrLe
	exprOpStrGe

	/* Unary operators (numbers) */
	exprOpNot
	exprOpBitNot
	exprOpUnaryMinus
	exprOpUnaryPlus

	/* Functions */
	exprOpFuncInt
	exprOpFuncWide
	exprOpFuncAbs
	exprOpFuncDouble
	exprOpFuncRound
	exprOpFuncRand
	exprOpFuncSrand

	/* Math functions from libm */
	exprOpFuncSin
	exprOpFuncCos
	exprOpFuncTan
	exprOpFuncAsin
	exprOpFuncAcos
	exprOpFuncAtan
	exprOpFuncAtan2
	exprOpFuncSinh
	exprOpFuncCosh
	exprOpFuncTanh
	exprOpFuncCeil
	exprOpFuncFloor
	exprOpFuncExp
	exprOpFuncLog
	exprOpFuncLog10
	exprOpFuncSqrt
	exprOpFuncPow
	exprOpFuncHypot
	exprOpFuncFmod
)

// Operator attributes.
const (
	exprOpFunc       = 0x0001 /* function syntax */
	exprOpRightAssoc = 0x0002 /* right associative */
)

// The operator implementations. The C table holds a function pointer; naming
// the implementations instead keeps the table free of references to them,
// which Go would reject as an initialisation cycle.
const (
	exprImplNull = iota /* the ":" operator is never evaluated */
	exprImplBin
	exprImplIntBin
	exprImplStrBin
	exprImplNumUnary
	exprImplIntUnary
	exprImplDoubleUnary
	exprImplAnd
	exprImplOr
	exprImplTernary
	exprImplNone
)

// exprOperator describes one operator: its spelling, implementation,
// precedence, number of operands and attributes.
type exprOperator struct {
	name       string
	impl       int
	precedence int
	arity      int
	attr       int
}

// exprEvalOperator evaluates an operator node by calling its implementation
// (C: the funcop member of the operator table).
func (i *Interp) exprEvalOperator(node *exprNode) int {
	switch exprOperatorInfoByOpcode(node.typ).impl {
	case exprImplBin:
		return i.exprOpBin(node)
	case exprImplIntBin:
		return i.exprOpIntBin(node)
	case exprImplStrBin:
		return i.exprOpStrBin(node)
	case exprImplNumUnary:
		return i.exprOpNumUnary(node)
	case exprImplIntUnary:
		return i.exprOpIntUnary(node)
	case exprImplDoubleUnary:
		return i.exprOpDoubleUnary(node)
	case exprImplAnd:
		return i.exprOpAnd(node)
	case exprImplOr:
		return i.exprOpOr(node)
	case exprImplTernary:
		return i.exprOpTernary(node)
	case exprImplNone:
		return i.exprOpNone(node)
	}
	return ERR
}

// exprOperators is the operator table. It *must* be kept in sync with the
// opcode constants above.
var exprOperators = []exprOperator{
	{"*", exprImplBin, 110, 2, 0},
	{"/", exprImplBin, 110, 2, 0},
	{"%", exprImplIntBin, 110, 2, 0},

	{"-", exprImplBin, 100, 2, 0},
	{"+", exprImplBin, 100, 2, 0},

	{"<<", exprImplIntBin, 90, 2, 0},
	{">>", exprImplIntBin, 90, 2, 0},

	{"<<<", exprImplIntBin, 90, 2, 0},
	{">>>", exprImplIntBin, 90, 2, 0},

	{"<", exprImplBin, 80, 2, 0},
	{">", exprImplBin, 80, 2, 0},
	{"<=", exprImplBin, 80, 2, 0},
	{">=", exprImplBin, 80, 2, 0},

	{"==", exprImplBin, 70, 2, 0},
	{"!=", exprImplBin, 70, 2, 0},

	{"&", exprImplIntBin, 50, 2, 0},
	{"^", exprImplIntBin, 49, 2, 0},
	{"|", exprImplIntBin, 48, 2, 0},

	{"&&", exprImplAnd, 10, 2, 0},
	{"||", exprImplOr, 9, 2, 0},
	{"?", exprImplTernary, 5, 3, exprOpRightAssoc},
	{":", exprImplNull, 5, 3, exprOpRightAssoc},

	/* Precedence is higher than * and / but lower than ! and ~, and right-associative */
	{"**", exprImplBin, 120, 2, exprOpRightAssoc},

	{"eq", exprImplStrBin, 60, 2, 0},
	{"ne", exprImplStrBin, 60, 2, 0},
	{"=*", exprImplStrBin, 60, 2, 0},
	{"=~", exprImplStrBin, 60, 2, 0},

	{"in", exprImplStrBin, 55, 2, 0},
	{"ni", exprImplStrBin, 55, 2, 0},

	/* Precedence must be higher than ==, !=, eq, ne but lower than <, >, <=, >= */
	{"lt", exprImplStrBin, 75, 2, 0},
	{"gt", exprImplStrBin, 75, 2, 0},
	{"le", exprImplStrBin, 75, 2, 0},
	{"ge", exprImplStrBin, 75, 2, 0},

	{"!", exprImplNumUnary, 150, 1, exprOpRightAssoc},
	{"~", exprImplIntUnary, 150, 1, exprOpRightAssoc},
	/* The leading space means these can never be matched by the tokenizer;
	 * they are substituted for the binary forms when parsing the tree.
	 */
	{" -", exprImplNumUnary, 150, 1, exprOpRightAssoc},
	{" +", exprImplNumUnary, 150, 1, exprOpRightAssoc},

	{"int", exprImplNumUnary, 200, 1, exprOpFunc},
	{"wide", exprImplNumUnary, 200, 1, exprOpFunc},
	{"abs", exprImplNumUnary, 200, 1, exprOpFunc},
	{"double", exprImplNumUnary, 200, 1, exprOpFunc},
	{"round", exprImplNumUnary, 200, 1, exprOpFunc},
	{"rand", exprImplNone, 200, 0, exprOpFunc},
	{"srand", exprImplIntUnary, 200, 1, exprOpFunc},

	{"sin", exprImplDoubleUnary, 200, 1, exprOpFunc},
	{"cos", exprImplDoubleUnary, 200, 1, exprOpFunc},
	{"tan", exprImplDoubleUnary, 200, 1, exprOpFunc},
	{"asin", exprImplDoubleUnary, 200, 1, exprOpFunc},
	{"acos", exprImplDoubleUnary, 200, 1, exprOpFunc},
	{"atan", exprImplDoubleUnary, 200, 1, exprOpFunc},
	{"atan2", exprImplBin, 200, 2, exprOpFunc},
	{"sinh", exprImplDoubleUnary, 200, 1, exprOpFunc},
	{"cosh", exprImplDoubleUnary, 200, 1, exprOpFunc},
	{"tanh", exprImplDoubleUnary, 200, 1, exprOpFunc},
	{"ceil", exprImplDoubleUnary, 200, 1, exprOpFunc},
	{"floor", exprImplDoubleUnary, 200, 1, exprOpFunc},
	{"exp", exprImplDoubleUnary, 200, 1, exprOpFunc},
	{"log", exprImplDoubleUnary, 200, 1, exprOpFunc},
	{"log10", exprImplDoubleUnary, 200, 1, exprOpFunc},
	{"sqrt", exprImplDoubleUnary, 200, 1, exprOpFunc},
	{"pow", exprImplBin, 200, 2, exprOpFunc},
	{"hypot", exprImplBin, 200, 2, exprOpFunc},
	{"fmod", exprImplBin, 200, 2, exprOpFunc},
}

// exprOperatorInfoByOpcode returns the table entry for an opcode. Term tokens
// have no entry, so a zero operator is returned for them, exactly as the C
// returns a pointer to a zeroed dummy (C: JimExprOperatorInfoByOpcode).
func exprOperatorInfoByOpcode(opcode int) *exprOperator {
	if opcode < ttExprOp {
		return &exprDummyOp
	}
	return &exprOperators[opcode-ttExprOp]
}

var exprDummyOp exprOperator

// exprDoubleToWide converts a double to a wide integer as the C cast does on
// this platform: out-of-range values saturate and NaN becomes zero.
func exprDoubleToWide(d float64) int64 {
	switch {
	case math.IsNaN(d):
		return 0
	case d >= -float64(math.MinInt64):
		return math.MaxInt64
	case d <= float64(math.MinInt64):
		return math.MinInt64
	}
	return int64(d)
}

// exprPowWide raises a wide integer to an integer power (C: JimPowWide).
func exprPowWide(b, e int64) int64 {
	res := int64(1)

	/* Special cases */
	if b == 1 {
		/* 1 ^ any = 1 */
		return 1
	}
	if e < 0 {
		if b != -1 {
			return 0
		}
		/* Only special case is -1 ^ -n: same as +ve n */
		e = -e
	}
	for e != 0 {
		if e&1 != 0 {
			res *= b
		}
		e >>= 1
		b *= b
	}
	return res
}

/* ---------------------------------------------------------------------------
 * The operator implementations
 * ------------------------------------------------------------------------ */

// exprOpNumUnary evaluates a unary operator that takes an integer or a double:
// !, unary - and +, and the int, wide, abs, double and round functions.
func (i *Interp) exprOpNumUnary(node *exprNode) int {
	intresult := true
	var (
		dC float64
		wC int64
	)

	A, rc := i.exprGetTerm(node.left)
	if rc != OK {
		return rc
	}

	/* A double object which has not yet generated its string representation
	 * is never treated as an integer, so that e.g. double(1.0) stays a double.
	 * The conversion must not be attempted in that case, because it would
	 * generate the string representation as a side effect.
	 */
	var wA int64
	haveWide := false
	if A.kind != kindDouble || A.strValid {
		wA, haveWide = i.GetWideNoErr(A)
	}

	if haveWide {
		switch node.typ {
		case exprOpFuncInt, exprOpFuncWide, exprOpFuncRound, exprOpUnaryPlus:
			wC = wA
		case exprOpFuncDouble:
			dC = float64(wA)
			intresult = false
		case exprOpFuncAbs:
			if wA >= 0 {
				wC = wA
			} else {
				wC = -wA
			}
		case exprOpUnaryMinus:
			wC = -wA
		case exprOpNot:
			wC = exprBoolToWide(wA == 0)
		}
	} else if dA, drc := i.GetDouble(A); drc == OK {
		switch node.typ {
		case exprOpFuncInt, exprOpFuncWide:
			wC = exprDoubleToWide(dA)
		case exprOpFuncRound:
			if dA < 0 {
				wC = exprDoubleToWide(dA - 0.5)
			} else {
				wC = exprDoubleToWide(dA + 0.5)
			}
		case exprOpFuncDouble, exprOpUnaryPlus:
			dC = dA
			intresult = false
		case exprOpFuncAbs:
			dC = math.Abs(dA)
			intresult = false
		case exprOpUnaryMinus:
			dC = -dA
			intresult = false
		case exprOpNot:
			wC = exprBoolToWide(dA == 0)
		}
	} else if bA, brc := i.GetBoolean(A); brc == OK {
		switch node.typ {
		case exprOpNot:
			wC = exprBoolToWide(!bA)
		case exprOpUnaryPlus, exprOpUnaryMinus:
			name := "-"
			if node.typ == exprOpUnaryPlus {
				name = "+"
			}
			rc = i.Errorf("can't use non-numeric string as operand of \"%s\"", name)
		}
	} else {
		rc = brc
	}

	if rc == OK {
		if intresult {
			i.SetResultInt(wC)
		} else {
			i.SetResult(i.NewDoubleObj(dC))
		}
	}
	return rc
}

// exprOpIntUnary evaluates a unary operator that requires an integer: ~ and
// the srand function.
func (i *Interp) exprOpIntUnary(node *exprNode) int {
	A, rc := i.exprGetTerm(node.left)
	if rc != OK {
		return rc
	}

	wA, rc := i.GetWide(A)
	if rc == OK {
		switch node.typ {
		case exprOpBitNot:
			i.SetResultInt(^wA)
		case exprOpFuncSrand:
			seed := make([]byte, 8)
			for n := range seed {
				seed[n] = byte(wA >> (8 * n))
			}
			i.prngSeed(seed)
			i.SetResult(i.NewDoubleObj(i.randDouble()))
		}
	}
	return rc
}

// exprOpNone implements rand(), the only operator with no operands.
func (i *Interp) exprOpNone(node *exprNode) int {
	i.SetResult(i.NewDoubleObj(i.randDouble()))
	return OK
}

// exprOpIntBin evaluates a binary operator that requires two integers.
func (i *Interp) exprOpIntBin(node *exprNode) int {
	A, rc := i.exprGetTerm(node.left)
	if rc != OK {
		return rc
	}
	B, rc := i.exprGetTerm(node.right)
	if rc != OK {
		return rc
	}

	wA, rc := i.GetWide(A)
	if rc != OK {
		return rc
	}
	wB, rc := i.GetWide(B)
	if rc != OK {
		return rc
	}
	var wC int64

	switch node.typ {
	case exprOpLShift:
		/* The C shifts by a jim_wide; on this platform only the low 6 bits
		 * of the shift count are used, and a negative count wraps around.
		 */
		wC = wA << (uint64(wB) & 63)
	case exprOpRShift:
		wC = wA >> (uint64(wB) & 63)
	case exprOpBitAnd:
		wC = wA & wB
	case exprOpBitXor:
		wC = wA ^ wB
	case exprOpBitOr:
		wC = wA | wB
	case exprOpMod:
		if wB == 0 {
			wC = 0
			i.SetResultString("Division by zero")
			rc = ERR
		} else {
			/*
			 * From Tcl 8.x
			 *
			 * This code is tricky: C doesn't guarantee much
			 * about the quotient or remainder, but Tcl does.
			 * The remainder always has the same sign as the
			 * divisor and a smaller absolute value.
			 */
			negative := false

			if wB < 0 {
				wB = -wB
				wA = -wA
				negative = true
			}
			wC = wA % wB
			if wC < 0 {
				wC += wB
			}
			if negative {
				wC = -wC
			}
		}
	case exprOpRotl, exprOpRotr:
		uA := uint64(wA)
		uB := uint64(wB)
		const S = 64

		/* Shift left by the word size or more is undefined. */
		uB %= S

		if node.typ == exprOpRotr {
			uB = S - uB
		}
		wC = int64(uA<<(uB&(S-1)) | uA>>((S-uB)&(S-1)))
	}
	/* Note that the C sets the result even when the division by zero error
	 * was reported, so [expr {1%0}] fails with the message "0".
	 */
	i.SetResultInt(wC)

	return rc
}

// exprOpBin evaluates a binary operator on two integers or two doubles, and on
// two strings for the comparison operators.
func (i *Interp) exprOpBin(node *exprNode) int {
	A, rc := i.exprGetTerm(node.left)
	if rc != OK {
		return rc
	}
	B, rc := i.exprGetTerm(node.right)
	if rc != OK {
		return rc
	}

	/* The operands are only treated as integers if neither is a double object
	 * without a string representation; the conversions are attempted in this
	 * order because they generate the string representation as a side effect.
	 */
	var wA, wB int64
	bothWide := false
	if (A.kind != kindDouble || A.strValid) && (B.kind != kindDouble || B.strValid) {
		var okA, okB bool
		if wA, okA = i.GetWideNoErr(A); okA {
			wB, okB = i.GetWideNoErr(B)
		}
		bothWide = okA && okB
	}

	if bothWide {
		var wC int64

		switch node.typ {
		case exprOpPow, exprOpFuncPow:
			if wA == 0 && wB < 0 {
				i.SetResultString("exponentiation of zero by negative power")
				return ERR
			}
			wC = exprPowWide(wA, wB)
		case exprOpAdd:
			wC = wA + wB
		case exprOpSub:
			wC = wA - wB
		case exprOpMul:
			wC = wA * wB
		case exprOpDiv:
			if wB == 0 {
				i.SetResultString("Division by zero")
				return ERR
			}
			/*
			 * From Tcl 8.x
			 *
			 * This code is tricky: C doesn't guarantee much
			 * about the quotient or remainder, but Tcl does.
			 * The remainder always has the same sign as the
			 * divisor and a smaller absolute value.
			 */
			if wB < 0 {
				wB = -wB
				wA = -wA
			}
			wC = wA / wB
			if wA%wB < 0 {
				wC--
			}
		case exprOpLt:
			wC = exprBoolToWide(wA < wB)
		case exprOpGt:
			wC = exprBoolToWide(wA > wB)
		case exprOpLte:
			wC = exprBoolToWide(wA <= wB)
		case exprOpGte:
			wC = exprBoolToWide(wA >= wB)
		case exprOpNumEq:
			wC = exprBoolToWide(wA == wB)
		case exprOpNumNe:
			wC = exprBoolToWide(wA != wB)
		default:
			/* Not an integer operator: fall through to the double case */
			bothWide = false
		}
		if bothWide {
			i.SetResultInt(wC)
			return OK
		}
	}

	/* If either operand is not a number at all, the operands are compared as
	 * strings instead.
	 */
	var dB float64
	bothDouble := false
	dA, rc := i.GetDouble(A)
	if rc == OK {
		var rcB int
		dB, rcB = i.GetDouble(B)
		bothDouble = rcB == OK
	}

	if bothDouble {
		var dC float64
		var wC int64

		switch node.typ {
		case exprOpPow, exprOpFuncPow:
			dC = math.Pow(dA, dB)
		case exprOpFuncAtan2:
			dC = math.Atan2(dA, dB)
		case exprOpFuncHypot:
			dC = math.Hypot(dA, dB)
		case exprOpFuncFmod:
			dC = math.Mod(dA, dB)
		case exprOpAdd:
			dC = dA + dB
		case exprOpSub:
			dC = dA - dB
		case exprOpMul:
			dC = dA * dB
		case exprOpDiv:
			if dB == 0 {
				/* The C returns a signed infinity rather than a NaN
				 * here, even for 0.0/0.0.
				 */
				dC = math.Inf(1)
				if dA < 0 {
					dC = math.Inf(-1)
				}
			} else {
				dC = dA / dB
			}
		case exprOpLt:
			wC = exprBoolToWide(dA < dB)
		case exprOpGt:
			wC = exprBoolToWide(dA > dB)
		case exprOpLte:
			wC = exprBoolToWide(dA <= dB)
		case exprOpGte:
			wC = exprBoolToWide(dA >= dB)
		case exprOpNumEq:
			wC = exprBoolToWide(dA == dB)
		case exprOpNumNe:
			wC = exprBoolToWide(dA != dB)
		default:
			return ERR
		}
		switch node.typ {
		case exprOpLt, exprOpGt, exprOpLte, exprOpGte, exprOpNumEq, exprOpNumNe:
			i.SetResultInt(wC)
		default:
			i.SetResult(i.NewDoubleObj(dC))
		}
		return OK
	}

	/* Handle the string case */
	cmp := i.StringCompareObj(A, B, false)
	var wC int64

	switch node.typ {
	case exprOpLt:
		wC = exprBoolToWide(cmp < 0)
	case exprOpGt:
		wC = exprBoolToWide(cmp > 0)
	case exprOpLte:
		wC = exprBoolToWide(cmp <= 0)
	case exprOpGte:
		wC = exprBoolToWide(cmp >= 0)
	case exprOpNumEq:
		wC = exprBoolToWide(cmp == 0)
	case exprOpNumNe:
		wC = exprBoolToWide(cmp != 0)
	default:
		/* Not a comparison, so the failed numeric conversion is the error */
		return ERR
	}
	i.SetResultInt(wC)
	return OK
}

// exprSearchList reports whether listObj contains an element equal to valObj
// (C: JimSearchList).
func (i *Interp) exprSearchList(listObj, valObj *Obj) bool {
	listlen := i.ListLength(listObj)
	for n := 0; n < listlen; n++ {
		if i.StringEqObj(i.ListGetIndex(listObj, n), valObj) {
			return true
		}
	}
	return false
}

// exprRegexpMatch runs [regexp] to implement the =~ operator. It returns a
// negative value when the match itself failed.
func (i *Interp) exprRegexpMatch(patternObj, obj *Obj) int64 {
	argv := []*Obj{i.NewStringObj("regexp"), patternObj, obj}

	rc := i.EvalObjVector(argv)
	// As in the C, the result is only converted when the command succeeded,
	// so a failing [regexp] keeps its own error message in the result.
	if rc == OK {
		if eq, grc := i.GetWide(i.Result()); grc == OK {
			return eq
		}
	}
	return int64(-rc)
}

// exprOpStrBin evaluates a binary string operator: eq, ne, lt, gt, le, ge, in,
// ni, =* and =~.
func (i *Interp) exprOpStrBin(node *exprNode) int {
	var wC int64

	A, rc := i.exprGetTerm(node.left)
	if rc != OK {
		return rc
	}
	B, rc := i.exprGetTerm(node.right)
	if rc != OK {
		return rc
	}

	switch node.typ {
	case exprOpStrEq, exprOpStrNe:
		wC = exprBoolToWide(i.StringEqObj(A, B))
		if node.typ == exprOpStrNe {
			wC = exprBoolToWide(wC == 0)
		}
	case exprOpStrLt, exprOpStrGt, exprOpStrLe, exprOpStrGe:
		comp := i.StringCompareObj(A, B, false)
		switch node.typ {
		case exprOpStrLt:
			wC = exprBoolToWide(comp == -1)
		case exprOpStrGt:
			wC = exprBoolToWide(comp == 1)
		case exprOpStrLe:
			wC = exprBoolToWide(comp == -1 || comp == 0)
		default: /* exprOpStrGe */
			wC = exprBoolToWide(comp == 0 || comp == 1)
		}
	case exprOpStrIn:
		wC = exprBoolToWide(i.exprSearchList(B, A))
	case exprOpStrNi:
		wC = exprBoolToWide(!i.exprSearchList(B, A))
	case exprOpStrGlob:
		wC = exprBoolToWide(i.StringMatchObj(B, A, false))
	case exprOpStrRe:
		wC = i.exprRegexpMatch(B, A)
		if wC < 0 {
			return ERR
		}
	}
	i.SetResultInt(wC)

	return rc
}

// exprOpAnd evaluates && with short-circuiting: the right operand is not
// evaluated at all when the left one is false.
func (i *Interp) exprOpAnd(node *exprNode) int {
	result := i.exprGetTermBoolean(node.left)

	if result == 1 {
		/* true so evaluate right */
		result = i.exprGetTermBoolean(node.right)
	}
	if result == -1 {
		return ERR
	}
	i.SetResultInt(int64(result))
	return OK
}

// exprOpOr evaluates || with short-circuiting.
func (i *Interp) exprOpOr(node *exprNode) int {
	result := i.exprGetTermBoolean(node.left)

	if result == 0 {
		/* false so evaluate right */
		result = i.exprGetTermBoolean(node.right)
	}
	if result == -1 {
		return ERR
	}
	i.SetResultInt(int64(result))
	return OK
}

// exprOpTernary evaluates ?:, evaluating only the selected branch.
func (i *Interp) exprOpTernary(node *exprNode) int {
	switch i.exprGetTermBoolean(node.left) {
	case 1:
		/* true so select right */
		return i.exprEvalTermNode(node.right)
	case 0:
		/* false so select ternary */
		return i.exprEvalTermNode(node.ternary)
	}
	return ERR
}

// exprBoolToWide is the value of a C comparison: 1 or 0.
func exprBoolToWide(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
