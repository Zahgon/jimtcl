/* Jim - A small embeddable Tcl interpreter: the expression engine.
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

import (
	"bytes"
	"math"
	"math/rand"
	"time"
)

// exprNode is one node of a parsed expression: either a term, in which case
// objPtr holds its value, or an operator with up to three operands.
type exprNode struct {
	typ     int /* one of the exprTT* token types */
	objPtr  *Obj
	left    *exprNode
	right   *exprNode
	ternary *exprNode
}

// exprTree is the parsed form of an expression object.
type exprTree struct {
	expr *exprNode /* the first operator or term */
	len  int       /* number of nodes in the tree */
}

// exprBuilder holds the state of the recursive descent tree builder.
type exprBuilder struct {
	parencount  int         /* count of outstanding parentheses */
	level       int         /* recursion depth */
	token       int         /* index of the current token */
	tokens      []exprToken /* the token list */
	stack       []*exprNode /* stack of pending terms */
	exprObjPtr  *Obj        /* the original expression */
	fileNameObj *Obj        /* filename of the original expression */
	nodes       []exprNode  /* storage for all nodes */
	next        int         /* index of the next free node */
}

// exprToken is one token of a parsed expression (C: ParseToken).
type exprToken struct {
	token []byte
	typ   int
	line  int
}

// Flags for exprTreeBuildTree.
const (
	exprUntilClose = 0x0001
	exprFuncArgs   = 0x0002
	exprTernary    = 0x0004
)

func (b *exprBuilder) push(node *exprNode) { b.stack = append(b.stack, node) }

func (b *exprBuilder) pop() *exprNode {
	if len(b.stack) == 0 {
		return nil
	}
	node := b.stack[len(b.stack)-1]
	b.stack = b.stack[:len(b.stack)-1]
	return node
}

// exprTreeBuildTree parses the subexpression at builder.token and leaves the
// node on the stack; builder.token is advanced past the consumed tokens.
//
// precedence is the precedence of the current operator: tokens are consumed
// until an operator of equal or lower precedence is reached (strictly lower if
// right associative).
//
// With exprUntilClose the subexpression extends up to and including the next
// close parenthesis, with exprFuncArgs several terms separated by commas are
// expected and with exprTernary two terms separated by a colon.
//
// expNumterms is how many terms are expected: normally one, but more for
// exprFuncArgs and exprTernary.
func (i *Interp) exprTreeBuildTree(b *exprBuilder, precedence, flags, expNumterms int) int {
	/* Calculate the stack length expected after pushing the expected terms */
	expStacklen := len(b.stack) + expNumterms

	/* The C tests the level before incrementing it */
	level := b.level
	b.level++
	if level > 200 {
		i.SetResultString("Expression too complex")
		return ERR
	}

	for b.tokens[b.token].typ != ttEOL {
		t := &b.tokens[b.token]
		b.token++

		prevtt := ttNone
		if b.token-1 != 0 {
			prevtt = b.tokens[b.token-2].typ
		}

		switch {
		case t.typ == ttSubexprStart:
			if len(b.stack) == expStacklen {
				return i.Errorf("unexpected open parenthesis in expression: \"%#s\"", b.exprObjPtr)
			}
			b.parencount++
			if rc := i.exprTreeBuildTree(b, 0, exprUntilClose, 1); rc != OK {
				return rc
			}
			/* A complete subexpression is on the stack */

		case t.typ == ttSubexprEnd:
			if flags&exprUntilClose == 0 {
				if len(b.stack) == expStacklen && b.level > 1 {
					b.token--
					b.level--
					return OK
				}
				return i.Errorf("unexpected closing parenthesis in expression: \"%#s\"", b.exprObjPtr)
			}
			b.parencount--
			if len(b.stack) == expStacklen {
				/* Return with the expected number of subexpressions on the stack */
				return i.exprTreeFinish(b, flags, expStacklen)
			}

		case t.typ == ttSubexprComma:
			if flags&exprFuncArgs == 0 {
				if len(b.stack) == expStacklen {
					/* handle the comma back at the parent level */
					b.token--
					b.level--
					return OK
				}
				return i.Errorf("unexpected comma in expression: \"%#s\"", b.exprObjPtr)
			}
			/* If we see more terms than expected, it is an error */
			if len(b.stack) > expStacklen {
				return i.Errorf("too many arguments to math function")
			}
			/* just go onto the next arg */

		case t.typ == exprOpColon:
			if flags&exprTernary == 0 {
				if b.level != 1 {
					/* handle the colon back at the parent level */
					b.token--
					b.level--
					return OK
				}
				return i.Errorf(": without ? in expression: \"%#s\"", b.exprObjPtr)
			}
			if len(b.stack) == expStacklen {
				/* handle the colon back at the parent level */
				b.token--
				b.level--
				return OK
			}
			/* just go onto the next term */

		case exprTokenIsOp(t.typ):
			/* Convert -/+ to unary minus or unary plus if necessary */
			if exprTokenIsOp(prevtt) || exprTokenIsStart(prevtt) {
				if t.typ == exprOpSub {
					t.typ = exprOpUnaryMinus
				} else if t.typ == exprOpAdd {
					t.typ = exprOpUnaryPlus
				}
			}

			op := exprOperatorInfoByOpcode(t.typ)

			if op.precedence < precedence ||
				(op.attr&exprOpRightAssoc == 0 && op.precedence == precedence) {
				/* next op is lower precedence, or equal and left associative, so done here */
				b.token--
				return i.exprTreeFinish(b, flags, expStacklen)
			}

			rc := OK
			switch {
			case op.attr&exprOpFunc != 0:
				if b.tokens[b.token].typ != ttSubexprStart {
					i.SetResultString("missing arguments for math function")
					return ERR
				}
				b.token++
				if op.arity == 0 {
					if b.tokens[b.token].typ != ttSubexprEnd {
						i.SetResultString("too many arguments for math function")
						return ERR
					}
					b.token++
					break
				}
				b.parencount++

				/* This will push left and return right */
				rc = i.exprTreeBuildTree(b, 0, exprFuncArgs|exprUntilClose, op.arity)
			case t.typ == exprOpTernary:
				/* Collect the two arguments to the ternary operator */
				rc = i.exprTreeBuildTree(b, op.precedence, exprTernary, 2)
			default:
				/* Recursively handle everything on the right until we see a
				 * precedence <= op->precedence (or == and right associative)
				 * and push that on the term stack.
				 */
				rc = i.exprTreeBuildTree(b, op.precedence, 0, 1)
			}
			if rc != OK {
				return rc
			}

			node := &b.nodes[b.next]
			b.next++
			node.typ = t.typ

			if op.arity >= 3 {
				if node.ternary = b.pop(); node.ternary == nil {
					return i.exprMissingOperand(b, op)
				}
			}
			if op.arity >= 2 {
				if node.right = b.pop(); node.right == nil {
					return i.exprMissingOperand(b, op)
				}
			}
			if op.arity >= 1 {
				if node.left = b.pop(); node.left == nil {
					return i.exprMissingOperand(b, op)
				}
			}

			/* Now push the node */
			b.push(node)

		default:
			var objPtr *Obj

			/* This is a simple non-operator term, so create and push the
			 * appropriate object.
			 */

			/* Two consecutive terms without an operator is invalid */
			if !exprTokenIsStart(prevtt) && !exprTokenIsOp(prevtt) {
				return i.Errorf("missing operator in expression: \"%#s\"", b.exprObjPtr)
			}

			/* Immediately create a double or int object? */
			switch t.typ {
			case ttExprInt:
				if w, n := numStrtoull(string(t.token)); n == len(t.token) {
					objPtr = i.NewIntObj(w)
				}
			case ttExprDouble:
				if d, n := numStrtod(string(t.token)); n == len(t.token) {
					objPtr = i.NewDoubleObj(d)
				}
			}

			if objPtr == nil {
				/* Everything else is stored as a simple string term */
				objPtr = i.NewStringObjBytes(t.token)
				if t.typ == ttCmd {
					/* Only commands need source info */
					objPtr.SetSourceInfo(b.fileNameObj, t.line)
				}
			}

			/* Now push a term node */
			node := &b.nodes[b.next]
			b.next++
			node.objPtr = objPtr
			node.typ = t.typ
			b.push(node)
		}
	}

	return i.exprTreeFinish(b, flags, expStacklen)
}

// exprMissingOperand reports an operator without enough operands.
func (i *Interp) exprMissingOperand(b *exprBuilder, op *exprOperator) int {
	b.next--
	return i.Errorf("missing operand to %s in expression: \"%#s\"", op.name, b.exprObjPtr)
}

// exprTreeFinish checks that the expected number of terms ended up on the
// stack, and reports the appropriate error if not.
func (i *Interp) exprTreeFinish(b *exprBuilder, flags, expStacklen int) int {
	if len(b.stack) == expStacklen {
		b.level--
		return OK
	}

	if flags&exprFuncArgs != 0 {
		which := "many"
		if len(b.stack) < expStacklen {
			which = "few"
		}
		return i.Errorf("too %s arguments for math function", which)
	}
	if len(b.stack) < expStacklen {
		if b.level == 0 {
			return i.Errorf("empty expression")
		}
		return i.Errorf("syntax error in expression \"%#s\": premature end of expression", b.exprObjPtr)
	}
	return i.Errorf("extra terms after expression")
}

// exprTreeCreateTree builds the expression tree from a token list.
func (i *Interp) exprTreeCreateTree(tokens []exprToken, exprObjPtr, fileNameObj *Obj) *exprTree {
	b := &exprBuilder{
		token:       0,
		tokens:      tokens,
		exprObjPtr:  exprObjPtr,
		fileNameObj: fileNameObj,
		/* The tree will never have more nodes than there are tokens - 1 (for EOL) */
		nodes: make([]exprNode, len(tokens)-1),
	}

	rc := i.exprTreeBuildTree(b, 0, 0, 1)

	var top *exprNode
	if rc == OK {
		top = b.pop()

		if b.parencount != 0 {
			i.SetResultString("missing close parenthesis")
			rc = ERR
		}
	}

	if rc != OK {
		return nil
	}
	return &exprTree{expr: top, len: b.next}
}

// setExprFromAny parses the string representation of an expression and caches
// the resulting tree in the object (C: SetExprFromAny). A failed parse still
// installs the (empty) expression representation, exactly as the C does.
func (i *Interp) setExprFromAny(o *Obj) int {
	var tree *exprTree

	/* Try to get information about filename / line number */
	fileNameObj, line := o.SourceInfo()
	if fileNameObj == nil {
		/* Jim_GetSourceInfo() reports line 1 of the empty file name for
		 * objects which carry no source information.
		 */
		fileNameObj, line = i.emptyObj, 1
	}

	exprText := o.Bytes()

	/* Initially tokenise the expression into a token list */
	var tokens []exprToken

	parser := newParser(exprText, line)
	for !parser.eof {
		if parser.parseExpression() != OK {
			i.Errorf("syntax error in expression: \"%#s\"", o)
			if parser.errmsg != "" {
				i.AppendString(i.Result(), ": "+parser.errmsg)
			}
			o.freeIntRep()
			o.kind = kindExpr
			return ERR
		}
		tokens = append(tokens, exprToken{token: parser.token(), typ: parser.tt, line: parser.tline})
	}

	rc := OK
	if len(tokens) <= 1 {
		i.SetResultString("empty expression")
		rc = ERR
	} else {
		rc = i.exprParseCheckMissing(parser.missingCh)
	}
	if rc != OK {
		return rc
	}

	/* Now create the expression tree from the token list */
	tree = i.exprTreeCreateTree(tokens, o, fileNameObj)

	/* Free the old internal rep and set the new one, even on failure */
	o.freeIntRep()
	o.kind = kindExpr
	o.aux = tree

	if tree == nil {
		return ERR
	}
	return OK
}

// getExpression returns the parsed form of an expression object, parsing it if
// necessary (C: JimGetExpression).
func (i *Interp) getExpression(o *Obj) *exprTree {
	if o.kind != kindExpr {
		if i.setExprFromAny(o) != OK {
			return nil
		}
	}
	tree, _ := o.aux.(*exprTree)
	return tree
}

/* ---------------------------------------------------------------------------
 * Expression evaluation
 *
 * Jim uses a recursive evaluation engine for expressions, which takes
 * advantage of the fact that expr's operators can't be redefined.
 * ------------------------------------------------------------------------ */

// exprEvalTermNode evaluates one node of the tree, leaving the value in the
// interpreter result.
func (i *Interp) exprEvalTermNode(node *exprNode) int {
	if exprTokenIsOp(node.typ) {
		return i.exprEvalOperator(node)
	}

	/* A term */
	switch node.typ {
	case ttExprInt, ttExprDouble, ttExprBoolean, ttStr:
		i.SetResult(node.objPtr)
		return OK

	case ttVar:
		if objPtr := i.GetVariable(node.objPtr, ErrMsg); objPtr != nil {
			i.SetResult(objPtr)
			return OK
		}
		return ERR

	case ttDictSugar:
		if objPtr := i.exprExpandDictSugar(node.objPtr); objPtr != nil {
			i.SetResult(objPtr)
			return OK
		}
		return ERR

	case ttEsc:
		if i.safeexpr {
			return ERR
		}
		if objPtr, rc := i.SubstObj(node.objPtr, None); rc == OK {
			i.SetResult(objPtr)
			return OK
		}
		return ERR

	case ttCmd:
		if i.safeexpr {
			return ERR
		}
		return i.EvalObj(node.objPtr)
	}
	/* Should never get here */
	return ERR
}

// exprGetTerm evaluates a term and returns its value object.
func (i *Interp) exprGetTerm(node *exprNode) (*Obj, int) {
	rc := i.exprEvalTermNode(node)
	if rc != OK {
		return nil, rc
	}
	return i.Result(), OK
}

// exprGetTermBoolean evaluates a term as a boolean, returning -1 on error.
func (i *Interp) exprGetTermBoolean(node *exprNode) int {
	if i.exprEvalTermNode(node) == OK {
		return i.exprBool(i.Result())
	}
	return -1
}

// exprExpandDictSugar expands a $var(index) term. The index is substituted and
// used to look up a key of the dictionary held by the variable (C:
// JimExpandDictSugar).
func (i *Interp) exprExpandDictSugar(o *Obj) *Obj {
	if i.safeexpr {
		return o
	}

	varObj, keyObj := i.exprDictSugarParseVarKey(o)

	substKeyObj, rc := i.SubstObj(keyObj, None)
	if rc != OK {
		return nil
	}

	dictObj := i.GetVariable(varObj, ErrMsg)
	if dictObj == nil {
		return nil
	}
	resObj, rc := i.DictKey(dictObj, substKeyObj, None)
	if rc != OK {
		what := "no such element in"
		if rc < 0 {
			what = "variable isn't"
		}
		i.Errorf("can't read \"%#s(%#s)\": %s array", varObj, substKeyObj, what)
		return nil
	}
	return resObj
}

// exprDictSugarParseVarKey splits a VARNAME(INDEX) term into its two parts
// (C: JimDictSugarParseVarKey).
func (i *Interp) exprDictSugarParseVarKey(o *Obj) (varObj, keyObj *Obj) {
	s := o.Bytes()
	p := bytes.IndexByte(s, '(')

	varObj = i.NewStringObjBytes(s[:p])

	key := s[p+1:]
	if len(s) > 0 && s[len(s)-1] == ')' {
		key = key[:len(key)-1]
	}
	return varObj, i.NewStringObjBytes(key)
}

// EvalExpression evaluates an expression object and returns its value.
func (i *Interp) EvalExpression(o *Obj) (*Obj, int) {
	tree := i.getExpression(o)
	if tree == nil {
		return nil, ERR
	}

	/* Check for one of the following common expressions used by while/for:
	 *
	 *   CONST
	 *   $a
	 *   !$a
	 *   $a < CONST, $a < $b  (and <=, >, >=, !=, ==)
	 *
	 * These are evaluated without touching the interpreter result, which also
	 * means that the value of a single term is passed through unchanged,
	 * keeping its string representation.
	 */
	if !i.safeexpr {
		switch tree.len {
		case 1:
			if objPtr := i.exprIntValOrVar(tree.expr); objPtr != nil {
				i.SetResult(objPtr)
				return objPtr, OK
			}

		case 2:
			if tree.expr.typ == exprOpNot {
				objPtr := i.exprIntValOrVar(tree.expr.left)

				if objPtr != nil && objPtr.kind == kindInt {
					res := i.trueObj
					if objPtr.wide != 0 {
						res = i.falseObj
					}
					i.SetResult(res)
					return res, OK
				}
			}

		case 3:
			objPtr := i.exprIntValOrVar(tree.expr.left)
			if objPtr != nil && objPtr.kind == kindInt {
				objPtr2 := i.exprIntValOrVar(tree.expr.right)
				if objPtr2 != nil && objPtr2.kind == kindInt {
					wideValueA := objPtr.wide
					wideValueB := objPtr2.wide
					var cmpRes bool
					switch tree.expr.typ {
					case exprOpLt:
						cmpRes = wideValueA < wideValueB
					case exprOpLte:
						cmpRes = wideValueA <= wideValueB
					case exprOpGt:
						cmpRes = wideValueA > wideValueB
					case exprOpGte:
						cmpRes = wideValueA >= wideValueB
					case exprOpNumEq:
						cmpRes = wideValueA == wideValueB
					case exprOpNumNe:
						cmpRes = wideValueA != wideValueB
					default:
						goto noopt
					}
					res := i.falseObj
					if cmpRes {
						res = i.trueObj
					}
					i.SetResult(res)
					return res, OK
				}
			}
		}
	}
noopt:

	/* Evaluate with the recursive expr engine */
	retcode := i.exprEvalTermNode(tree.expr)

	/* Now transfer ownership of the tree back into the object in case it
	 * shimmered away during the evaluation.
	 */
	o.freeIntRep()
	o.kind = kindExpr
	o.aux = tree

	if retcode != OK {
		return nil, retcode
	}
	return i.Result(), OK
}

// exprIntValOrVar returns the value of a node which is an integer literal or a
// variable reference, or nil for anything else (C: JimExprIntValOrVar).
func (i *Interp) exprIntValOrVar(node *exprNode) *Obj {
	switch node.typ {
	case ttExprInt:
		return node.objPtr
	case ttVar:
		return i.GetVariable(node.objPtr, None)
	case ttDictSugar:
		return i.exprExpandDictSugar(node.objPtr)
	}
	return nil
}

// exprBool converts an expression result to boolean truth, returning -1 when
// the object is not a number or a boolean (C: ExprBool).
func (i *Interp) exprBool(o *Obj) int {
	/* Jim_GetLong() is used by the C; a long is as wide as a jim_wide here */
	if l, rc := i.GetWide(o); rc == OK {
		return exprBoolToInt(l != 0)
	}
	if d, rc := i.GetDouble(o); rc == OK {
		return exprBoolToInt(d != 0)
	}
	if b, rc := i.GetBoolean(o); rc == OK {
		return exprBoolToInt(b)
	}
	return -1
}

func exprBoolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ExprBool evaluates an expression object as a boolean, as [if] and [while] do.
func (i *Interp) ExprBool(o *Obj) (bool, int) {
	if _, rc := i.EvalExpression(o); rc != OK {
		return false, rc
	}
	switch i.exprBool(i.Result()) {
	case 0:
		return false, OK
	case 1:
		return true, OK
	}
	return false, ERR
}

/* ---------------------------------------------------------------------------
 * Pseudo random number generation
 * ------------------------------------------------------------------------ */

// prngInit seeds the generator from the clock, as the C does with
// rand(), time() and clock().
func (i *Interp) prngInit() {
	const prngSeedSize = 256

	i.prng = &prngState{}

	seed := make([]byte, prngSeedSize*4)
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	for n := 0; n < prngSeedSize; n++ {
		v := uint32(r.Uint32())
		for k := 0; k < 4; k++ {
			seed[n*4+k] = byte(v >> (8 * k))
		}
	}
	i.prngSeed(seed)
}

// randomBytes fills dest with pseudo-random data (C: JimRandomBytes).
func (i *Interp) randomBytes(dest []byte) {
	if i.prng == nil {
		i.prngInit()
	}
	prng := i.prng
	for x := range dest {
		prng.i = (prng.i + 1) & 0xff
		si := prng.sbox[prng.i]
		prng.j = (prng.j + si) & 0xff
		sj := prng.sbox[prng.j]
		prng.sbox[prng.i] = sj
		prng.sbox[prng.j] = si
		dest[x] = prng.sbox[(si+sj)&0xff]
	}
}

// prngSeed re-seeds the generator with the given bytes (C: JimPrngSeed).
func (i *Interp) prngSeed(seed []byte) {
	if i.prng == nil {
		i.prngInit()
	}
	prng := i.prng

	/* Set the sbox[i] with i */
	for n := 0; n < 256; n++ {
		prng.sbox[n] = byte(n)
	}
	/* Now use the seed to perform a random permutation of the sbox */
	for n := 0; n < len(seed); n++ {
		t := prng.sbox[n&0xFF]
		prng.sbox[n&0xFF] = prng.sbox[seed[n]]
		prng.sbox[seed[n]] = t
	}
	prng.i, prng.j = 0, 0

	/* Discard at least the first 256 bytes of stream; the C borrows the seed
	 * buffer for this, so the caller's seed is overwritten.
	 */
	for n := 0; n < 256; n += len(seed) {
		i.randomBytes(seed)
	}
}

// randDouble returns a pseudo-random double in the range [0, 1)
// (C: JimRandDouble).
func (i *Interp) randDouble() float64 {
	var buf [8]byte
	i.randomBytes(buf[:])

	var x uint64
	for n := 0; n < 8; n++ {
		x |= uint64(buf[n]) << (8 * n)
	}
	/* The C divides by (double)~0UL, so the result can never reach 1.0 */
	return float64(x) / float64(^uint64(0))
}

/* ---------------------------------------------------------------------------
 * The commands
 * ------------------------------------------------------------------------ */

// exprCoreCommand implements [expr].
func exprCoreCommand(i *Interp, argv []*Obj) int {
	_, rc := i.EvalExpression(argv[1])
	return rc
}

// exprAddMulHelper is the shared implementation of [+] and [*]. The arguments
// are summed (or multiplied) as integers until one of them is not an integer,
// from which point the arithmetic continues in floating point.
func exprAddMulHelper(i *Interp, argv []*Obj, op int) int {
	res := int64(1)
	if op == exprOpAdd {
		res = 0
	}

	n := 0
	for ; n < len(argv); n++ {
		wideValue, rc := i.GetWide(argv[n])
		if rc != OK {
			break
		}
		if op == exprOpAdd {
			res += wideValue
		} else {
			res *= wideValue
		}
	}
	if n == len(argv) {
		i.SetResultInt(res)
		return OK
	}

	doubleRes := float64(res)
	for ; n < len(argv); n++ {
		doubleValue, rc := i.GetDouble(argv[n])
		if rc != OK {
			return ERR
		}
		if op == exprOpAdd {
			doubleRes += doubleValue
		} else {
			doubleRes *= doubleValue
		}
	}
	i.SetResult(i.NewDoubleObj(doubleRes))
	return OK
}

// exprSubDivHelper is the shared implementation of [-] and [/].
func exprSubDivHelper(i *Interp, argv []*Obj, op int) int {
	var (
		res       int64
		doubleRes float64
	)

	if len(argv) == 1 {
		/* The arity = 1 case is different: [- x] returns -x while [/ x]
		 * returns 1/x.
		 */
		wideValue, rc := i.GetWide(argv[0])
		if rc != OK {
			doubleValue, rc := i.GetDouble(argv[0])
			if rc != OK {
				return ERR
			}
			if op == exprOpSub {
				doubleRes = -doubleValue
			} else {
				doubleRes = 1.0 / doubleValue
			}
			i.SetResult(i.NewDoubleObj(doubleRes))
			return OK
		}
		if op == exprOpSub {
			i.SetResultInt(-wideValue)
		} else {
			i.SetResult(i.NewDoubleObj(1.0 / float64(wideValue)))
		}
		return OK
	}

	n := 1
	res, rc := i.GetWide(argv[0])
	if rc != OK {
		if doubleRes, rc = i.GetDouble(argv[0]); rc != OK {
			return ERR
		}
	} else {
		for ; n < len(argv); n++ {
			wideValue, rc := i.GetWide(argv[n])
			if rc != OK {
				doubleRes = float64(res)
				break
			}
			if op == exprOpSub {
				res -= wideValue
			} else {
				if wideValue == 0 {
					i.SetResultString("Division by zero")
					return ERR
				}
				res /= wideValue
			}
		}
		if n == len(argv) {
			i.SetResultInt(res)
			return OK
		}
	}

	for ; n < len(argv); n++ {
		doubleValue, rc := i.GetDouble(argv[n])
		if rc != OK {
			return ERR
		}
		if op == exprOpSub {
			doubleRes -= doubleValue
		} else {
			doubleRes /= doubleValue
		}
	}
	i.SetResult(i.NewDoubleObj(doubleRes))
	return OK
}

// exprAddCoreCommand implements [+].
func exprAddCoreCommand(i *Interp, argv []*Obj) int {
	return exprAddMulHelper(i, argv[1:], exprOpAdd)
}

// exprMulCoreCommand implements [*].
func exprMulCoreCommand(i *Interp, argv []*Obj) int {
	return exprAddMulHelper(i, argv[1:], exprOpMul)
}

// exprSubCoreCommand implements [-].
func exprSubCoreCommand(i *Interp, argv []*Obj) int {
	return exprSubDivHelper(i, argv[1:], exprOpSub)
}

// exprDivCoreCommand implements [/].
func exprDivCoreCommand(i *Interp, argv []*Obj) int {
	return exprSubDivHelper(i, argv[1:], exprOpDiv)
}

// exprRandCoreCommand implements [rand].
func exprRandCoreCommand(i *Interp, argv []*Obj) int {
	var min, max int64

	argv = argv[1:]
	switch len(argv) {
	case 0:
		max = math.MaxInt64
	case 1:
		var rc int
		if max, rc = i.GetWideExpr(argv[0]); rc != OK {
			return ERR
		}
	case 2:
		var rc int
		if min, rc = i.GetWideExpr(argv[0]); rc != OK {
			return ERR
		}
		if max, rc = i.GetWideExpr(argv[1]); rc != OK {
			return ERR
		}
	}
	length := max - min
	if length < 0 {
		i.SetResultString("Invalid arguments (max < min)")
		return ERR
	}
	maxMul := int64(math.MaxInt64)
	if length != 0 {
		maxMul -= math.MaxInt64 % length
	}
	for {
		var buf [8]byte
		i.randomBytes(buf[:])

		var u uint64
		for n := 0; n < 8; n++ {
			u |= uint64(buf[n]) << (8 * n)
		}
		r := int64(u)
		if r < 0 || r >= maxMul {
			continue
		}
		if length == 0 {
			r = 0
		} else {
			r %= length
		}
		i.SetResultInt(min + r)
		return OK
	}
}

// registerExprCommands registers the arithmetic commands, in the order they
// appear in the C core command table.
func registerExprCommands(i *Interp) {
	i.RegisterCmd("*", "?number ...?", 0, -1, exprMulCoreCommand, 0)
	i.RegisterCmd("+", "?number ...?", 0, -1, exprAddCoreCommand, 0)
	i.RegisterCmd("-", "number ?number ...?", 1, -1, exprSubCoreCommand, 0)
	i.RegisterCmd("/", "number ?number ...?", 1, -1, exprDivCoreCommand, 0)
	i.RegisterCmd("expr", "expression", 1, 1, exprCoreCommand, 0)
	i.RegisterCmd("rand", "?min? ?max?", 0, 2, exprRandCoreCommand, 0)
}
