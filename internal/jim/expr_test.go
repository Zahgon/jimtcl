/* Jim - A small embeddable Tcl interpreter: tests for the expression engine.
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

import "testing"

// exprTests are expressions and the exact result (or error message) produced by
// the original jimsh 0.84.
var exprTests = []struct {
	expr    string
	want    string
	wantErr bool
}{
	{"1+2", "3", false},
	{"7/-2", "-4", false},
	{"-7%2", "1", false},
	{"7%-2", "-1", false},
	{"2**62*4", "0", false},
	{"0x7fffffffffffffff+1", "-9223372036854775808", false},
	{"0xffffffffffffffff", "-1", false},
	{"2**-1", "0", false},
	{"(-2)**-3", "0", false},
	{"-9223372036854775808/-1", "-9223372036854775808", false},
	{"-9223372036854775808%-1", "0", false},
	{"1<<70", "64", false},
	{"1<<-1", "-9223372036854775808", false},
	{"-1>>70", "-1", false},
	{"1<<<1", "2", false},
	{"1>>>1", "-9223372036854775808", false},
	{"2<<<63", "1", false},
	{"4&3|2^1", "3", false},
	{"~5", "-6", false},
	{"!0.0", "1", false},
	{"!!5", "1", false},
	{"5.0/2", "2.5", false},
	{"1.0/0", "Inf", false},
	{"-1.0/0", "-Inf", false},
	{"0.0/0", "Inf", false},
	{"1e300*1e300", "Inf", false},
	{"sqrt(-1)", "NaN", false},
	{"abs(-0.0)", "0.0", false},
	{"abs(-9223372036854775808)", "-9223372036854775808", false},
	{"int(-3.7)", "-3", false},
	{"int(1e300)", "9223372036854775807", false},
	{"int(-1e300)", "-9223372036854775808", false},
	{"int(NaN)", "0", false},
	{"round(2.5)", "3", false},
	{"round(-2.5)", "-3", false},
	{"double(1)", "1.0", false},
	{"wide(1.9)", "1", false},
	{"2.0", "2.0", false},
	{"10 < 9.5", "0", false},
	{`"10" < "9"`, "0", false},
	{`1 < "a"`, "1", false},
	{`1 == "a"`, "0", false},
	{"1.0 == 1", "1", false},
	{"1 eq 1.0", "0", false},
	{`"abc" eq "abc"`, "1", false},
	{`"abc" lt "abd"`, "1", false},
	{`"" eq ""`, "1", false},
	{"1 in {1 2}", "1", false},
	{"1 ni {1 2}", "0", false},
	{`"abc" =* "a*"`, "1", false},
	{"true", "true", false},
	{"yes", "yes", false},
	{"on && off", "0", false},
	{"-2**2", "4", false},
	{"2 ** 3 ** 2", "512", false},
	{"1 ? 2 : 3", "2", false},
	{"0?1:0?2:3", "3", false},
	{"1,2", "1", false},
	{"((1))", "1", false},
	{".5", "0.5", false},
	{"5.", "5.0", false},
	{"0b101", "5", false},
	{"0o17", "15", false},
	{"0d19", "19", false},
	{"08", "8", false},
	{"inf+1", "Inf", false},
	{"nan", "NaN", false},
	{`"0x10" + 0`, "16", false},
	{`" 5 " + 1`, "6", false},
	/* The right operand of a short-circuited && or || is not evaluated */
	{"0 && [error foo]", "0", false},
	{"1 || [error foo]", "1", false},
	{"0 ? [error foo] : 5", "5", false},

	/* Errors */
	{"1/0", "Division by zero", true},
	/* [expr {1%0}] reports its error through the result of the division,
	 * which the C overwrites with the (zero) value.
	 */
	{"1%0", "0", true},
	{"0**-1", "exponentiation of zero by negative power", true},
	{"", "empty expression", true},
	{"   ", "empty expression", true},
	{"foo", `syntax error in expression: "foo"`, true},
	{"0x", `syntax error in expression: "0x"`, true},
	{"1e", `syntax error in expression: "1e"`, true},
	{"int", `syntax error in expression: "int": function requires parentheses`, true},
	{"$(1+2)", `syntax error in expression: "$(1+2)": nesting expr in expr is not allowed`, true},
	{"(1", "missing close parenthesis", true},
	{"1)", `unexpected closing parenthesis in expression: "1)"`, true},
	{"1 (2)", `unexpected open parenthesis in expression: "1 (2)"`, true},
	{"1 2", `missing operator in expression: "1 2"`, true},
	{"1+", `syntax error in expression "1+": premature end of expression`, true},
	{"*1", `missing operand to * in expression: "*1"`, true},
	{"1:2", `: without ? in expression: "1:2"`, true},
	{"int()", "too few arguments for math function", true},
	{"int(1,2)", "too many arguments for math function", true},
	{"int(1,2,3)", "too many arguments to math function", true},
	{"rand(1)", "too many arguments for math function", true},
	{"pow(1)", "too few arguments for math function", true},
	{`sin("a")`, `expected floating-point number but got "a"`, true},
	{`"a"+1`, `expected floating-point number but got "a"`, true},
	{`-"a"`, `expected boolean but got "a"`, true},
	{"-true", `can't use non-numeric string as operand of "-"`, true},
	{"+true", `can't use non-numeric string as operand of "+"`, true},
	{`~"a"`, `expected integer but got "a"`, true},
	{"1.5 % 1", `expected integer but got "1.5"`, true},
	{"{", "missing close-brace", true},
	{`"`, "missing quote", true},
}

func TestEvalExpression(t *testing.T) {
	i := NewInterp()
	for _, tc := range exprTests {
		res, rc := i.EvalExpression(i.NewStringObj(tc.expr))
		if (rc != OK) != tc.wantErr {
			t.Errorf("expr {%s}: rc=%d, want error %v (%s)", tc.expr, rc, tc.wantErr, i.Result())
			continue
		}
		got := i.Result().String()
		if rc == OK {
			got = res.String()
		}
		if got != tc.want {
			t.Errorf("expr {%s} = %q, want %q", tc.expr, got, tc.want)
		}
	}
}

func TestExprBool(t *testing.T) {
	tests := []struct {
		expr string
		want bool
	}{
		{"1", true},
		{"0", false},
		{"0.0", false},
		{"1.5", true},
		{"true", true},
		{"off", false},
		{"1 < 2", true},
	}
	i := NewInterp()
	for _, tc := range tests {
		got, rc := i.ExprBool(i.NewStringObj(tc.expr))
		if rc != OK {
			t.Errorf("ExprBool(%s): rc=%d (%s)", tc.expr, rc, i.Result())
			continue
		}
		if got != tc.want {
			t.Errorf("ExprBool(%s) = %v, want %v", tc.expr, got, tc.want)
		}
	}
	if _, rc := i.ExprBool(i.NewStringObj(`"a"`)); rc != ERR {
		t.Errorf(`ExprBool("a"): rc=%d, want ERR`, rc)
	}
}

// TestExprOperatorTable checks that the operator table and the opcodes it is
// indexed by are still in step.
func TestExprOperatorTable(t *testing.T) {
	if n := exprOpFuncFmod - ttExprOp + 1; n != len(exprOperators) {
		t.Fatalf("operator table has %d entries, opcodes cover %d", len(exprOperators), n)
	}
	for i, op := range exprOperators {
		if got := exprOperatorInfoByOpcode(i + ttExprOp); got != &exprOperators[i] {
			t.Errorf("opcode %d does not map to %q", i+ttExprOp, op.name)
		}
	}
}

// TestExprSafe checks that a safely evaluated expression refuses to run a
// command or a substitution, which is what [incr] and friends rely on.
func TestExprSafe(t *testing.T) {
	i := NewInterp()
	for _, e := range []string{"[error foo]", `"[error foo]"`} {
		i.safeexpr = true
		_, rc := i.EvalExpression(i.NewStringObj(e))
		i.safeexpr = false
		if rc != ERR {
			t.Errorf("safe expr {%s}: rc=%d, want ERR", e, rc)
		}
	}
}

// TestExprNumberScanners checks the tokenizer's number scanners against the
// behaviour of the C strtoull()/strtod() the original uses, including the
// quirks: "0x" scans as a plain 0, and an out-of-range integer wraps around.
func TestExprNumberScanners(t *testing.T) {
	ints := []struct {
		s string
		v int64
		n int
	}{
		{"0", 0, 1}, {"1", 1, 1}, {"-1", -1, 2}, {"+5", 5, 2},
		{"0x10", 16, 4}, {"0X1f", 31, 4}, {"0o17", 15, 4}, {"0b101", 5, 5},
		{"0d19", 19, 4}, {"08", 8, 2}, {"007", 7, 3},
		{"0x", 0, 1}, {"0xg", 0, 1}, {"1e", 1, 1},
		{"0xffffffffffffffff", -1, 18},
		{"9223372036854775807", 9223372036854775807, 19},
		{"9223372036854775808", -9223372036854775808, 19},
		{"-9223372036854775808", -9223372036854775808, 20},
		{"18446744073709551616", -1, 20},
		{"abc", 0, 0}, {"", 0, 0}, {" 5x", 5, 2}, {"-0x10", -16, 5},
		{"0x-5", 0, 1},
	}
	for _, tc := range ints {
		v, n := numStrtoull(tc.s)
		if v != tc.v || n != tc.n {
			t.Errorf("numStrtoull(%q) = %d,%d want %d,%d", tc.s, v, n, tc.v, tc.n)
		}
	}
	dbls := []struct {
		s string
		v float64
		n int
	}{
		{"0.5", 0.5, 3}, {".5", 0.5, 2}, {"5.", 5, 2}, {"1e5", 100000, 3},
		{"1e", 1, 1}, {"1e+", 1, 1}, {"1e+5", 100000, 4}, {"1.5e-3", 0.0015, 6},
		{"inf", 0, 3}, {"nan", 0, 3}, {"abc", 0, 0}, {"", 0, 0},
		{"0x1.8p3", 12, 7}, {"0x1.", 1, 4}, {"1.0e", 1, 3},
	}
	for _, tc := range dbls {
		v, n := numStrtod(tc.s)
		if n != tc.n {
			t.Errorf("numStrtod(%q) len = %d want %d", tc.s, n, tc.n)
			continue
		}
		if tc.s == "inf" || tc.s == "nan" {
			continue
		}
		if v != tc.v {
			t.Errorf("numStrtod(%q) = %v want %v", tc.s, v, tc.v)
		}
	}
}
