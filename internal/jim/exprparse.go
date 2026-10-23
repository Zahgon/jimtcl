/* Jim - A small embeddable Tcl interpreter: the expression tokenizer.
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
	"github.com/msteveb/jimtcl/internal/u8"
	"strings"
)

// exprTokenIsOp reports whether a token type is an operator (C:
// TOKEN_IS_EXPR_OP).
func exprTokenIsOp(t int) bool { return t >= ttExprOp }

// exprTokenIsStart reports whether a token type may start an expression
// (C: TOKEN_IS_EXPR_START).
func exprTokenIsStart(t int) bool {
	return t == ttNone || t == ttSubexprStart || t == ttSubexprComma
}

/* ---------------------------------------------------------------------------
 * The shared sub-token parsers (C: JimParseSubBrace, JimParseSubQuote,
 * JimParseSubCmd and friends).
 * ------------------------------------------------------------------------ */

/* ---------------------------------------------------------------------------
 * Numeric scanners
 *
 * The tokenizer needs to know how many characters a number occupies, which the
 * C code gets from the endptr of strtoull()/strtod(). These two helpers are the
 * scanning half of that: they return the value and the number of bytes
 * consumed, and consume nothing at all when the text is not a number.
 * ------------------------------------------------------------------------ */

// exprScanFloatDigits returns the offset just past a decimal or hexadecimal
// floating-point mantissa and exponent starting at i, or -1 if there is none.
func exprScanFloatDigits(s string, i int) int {
	hex := false
	if i+1 < len(s) && s[i] == '0' && (s[i+1] == 'x' || s[i+1] == 'X') {
		hex = true
		i += 2
	}
	digits := 0
	isDigit := u8.IsDigit
	if hex {
		isDigit = u8.IsHexDigit
	}
	for i < len(s) && isDigit(s[i]) {
		i++
		digits++
	}
	if i < len(s) && s[i] == '.' {
		i++
		for i < len(s) && isDigit(s[i]) {
			i++
			digits++
		}
	}
	if digits == 0 {
		return -1
	}
	expCh := byte('e')
	if hex {
		expCh = 'p'
	}
	if i < len(s) && (s[i]|0x20) == expCh {
		j := i + 1
		if j < len(s) && (s[j] == '+' || s[j] == '-') {
			j++
		}
		if j < len(s) && u8.IsDigit(s[j]) {
			for j < len(s) && u8.IsDigit(s[j]) {
				j++
			}
			i = j
		}
	}
	return i
}

func exprHasPrefixFold(s, prefix string) bool {
	return len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix)
}

/* ---------------------------------------------------------------------------
 * The expression tokenizer proper
 * ------------------------------------------------------------------------ */

// exprTrueFalseStrings are the literals accepted as booleans; the first four
// are true (C: jim_true_false_strings).
var exprTrueFalseStrings = []string{"1", "true", "yes", "on", "0", "false", "no", "off"}

// parseExpression parses the next token of an expression (C:
// JimParseExpression).
func (pc *parser) parseExpression() int {
	pc.errmsg = ""

	for {
		/* Discard spaces and quoted newline */
		for u8.IsSpace(pc.at(0)) || (pc.at(0) == '\\' && pc.at(1) == '\n') {
			if pc.at(0) == '\n' {
				pc.linenr++
			}
			pc.p++
		}
		/* Discard comments */
		if pc.at(0) == '#' {
			pc.parseComment()
			/* Go back to discarding white space */
			continue
		}
		break
	}

	/* Common case */
	pc.tline = pc.linenr
	pc.tstart = pc.p

	if pc.rem() == 0 {
		pc.tend = pc.p
		pc.tt = ttEOL
		pc.eof = true
		return OK
	}
	switch c := pc.src[pc.p]; c {
	case '(':
		pc.tt = ttSubexprStart
		pc.tend = pc.p
		pc.p++
	case ')':
		pc.tt = ttSubexprEnd
		pc.tend = pc.p
		pc.p++
	case ',':
		pc.tt = ttSubexprComma
		pc.tend = pc.p
		pc.p++
	case '[':
		pc.parseCmd()
	case '$':
		if !pc.parseVar() {
			return pc.parseExprOperator()
		}
		/* Don't allow expr sugar in expressions */
		if pc.tt == ttExprSugar {
			pc.errmsg = "nesting expr in expr is not allowed"
			return ERR
		}
		return OK
	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9', '.':
		return pc.parseExprNumber()
	case '"':
		pc.parseQuote()
	case '{':
		pc.parseBrace()
	case 'N', 'I', 'n', 'i':
		if pc.parseExprIrrational() == ERR {
			if pc.parseExprBoolean() == ERR {
				return pc.parseExprOperator()
			}
		}
	case 't', 'f', 'o', 'y':
		if pc.parseExprBoolean() == ERR {
			return pc.parseExprOperator()
		}
	default:
		return pc.parseExprOperator()
	}
	return OK
}

// parseExprNumber parses an integer or floating-point literal. An integer is
// assumed first; if the text continues with one of ".eENnIi" the number is
// re-scanned as a double and used when it covers more characters.
func (pc *parser) parseExprNumber() int {
	pc.tt = ttExprInt

	_, n := numStrtoull(string(pc.src[pc.p:]))
	pc.p += n

	if strings.IndexByte("eENnIi.", pc.at(0)) >= 0 || pc.p == pc.tstart {
		_, dn := numStrtod(string(pc.src[pc.tstart:]))
		if dn == 0 {
			return ERR
		}
		if end := pc.tstart + dn; end > pc.p {
			/* Yes, double captured more chars */
			pc.tt = ttExprDouble
			pc.p = end
		}
	}
	pc.tend = pc.p - 1
	return OK
}

// parseExprIrrational parses the Inf and NaN literals.
func (pc *parser) parseExprIrrational() int {
	for _, irr := range []string{"NaN", "nan", "NAN", "Inf", "inf", "INF"} {
		if string(pc.src[pc.p:min(pc.p+3, len(pc.src))]) == irr {
			pc.p += 3
			pc.tend = pc.p - 1
			pc.tt = ttExprDouble
			return OK
		}
	}
	return ERR
}

// parseExprBoolean parses a boolean literal.
func (pc *parser) parseExprBoolean() int {
	for _, s := range exprTrueFalseStrings {
		if strings.HasPrefix(string(pc.src[pc.p:]), s) {
			pc.p += len(s)
			pc.tend = pc.p - 1
			pc.tt = ttExprBoolean
			return OK
		}
	}
	return ERR
}

// parseExprOperator parses an operator token, preferring the longest match.
func (pc *parser) parseExprOperator() int {
	bestOp := -1
	bestLen := 0

	for i := range exprOperators {
		op := &exprOperators[i]

		if op.name[0] != pc.at(0) {
			continue
		}
		if len(op.name) > bestLen && strings.HasPrefix(string(pc.src[pc.p:]), op.name) {
			bestOp = i
			bestLen = len(op.name)
		}
	}
	if bestOp < 0 {
		return ERR
	}

	/* Validate parentheses around function arguments */
	if exprOperators[bestOp].attr&exprOpFunc != 0 {
		q := pc.p + bestLen
		for q < len(pc.src) && u8.IsSpace(pc.src[q]) {
			q++
		}
		if q >= len(pc.src) || pc.src[q] != '(' {
			pc.errmsg = "function requires parentheses"
			return ERR
		}
	}
	pc.tend = pc.p + bestLen - 1
	pc.p += bestLen
	pc.tt = bestOp + ttExprOp
	return OK
}

// exprParseCheckMissing reports an unmatched brace, bracket or quote left over
// by the tokenizer (C: JimParseCheckMissing).
func (i *Interp) exprParseCheckMissing(ch byte) int {
	var msg string

	switch ch {
	case '\\', ' ':
		return OK
	case '[':
		msg = "unmatched \"[\""
	case '{':
		msg = "missing close-brace"
	case '}':
		msg = "extra characters after close-brace"
	default: /* including '"' */
		msg = "missing quote"
	}
	i.SetResultString(msg)
	return ERR
}
