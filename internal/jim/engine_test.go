/* Jim - A small embeddable Tcl interpreter: tests for the engine area.
 *
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

// evalCases are scripts together with the completion code and result that the
// original jimsh 0.84 produces for them.
var evalCases = []struct {
	script string
	rc     int
	result string
}{
	// The parser
	{`set x 42; set x`, OK, "42"},
	{`set a 1; set b 2; list $a$b`, OK, "12"},
	{"set x \"a\nb\"; string length $x", OK, "3"},
	{`set x {a {b c} d}; llength $x`, OK, "3"},
	{`set {a b} 3; set {a b}`, OK, "3"},
	{`set x a; list $x$x`, OK, "aa"},
	{`list a\ b`, OK, `{a b}`},
	{`concat \x41\101B\u{43}`, OK, "AABC"},
	{`concat \q\8\n|`, OK, "q8\n|"},
	{`concat a\`, OK, `a\`},
	{`set x {a}b`, ERR, "extra characters after close-brace"},
	{`if {1} {`, ERR, "missing close-brace"},
	{`puts [`, ERR, `unmatched "["`},
	{`puts "`, ERR, "missing quote"},

	// Variables and the $a(b) sugar
	{`set a(x) 1; set k x; set a($k)`, OK, "1"},
	{`set a(1,2) v; set a(1,2)`, OK, "v"},
	{`set b 1; set b(x) 2`, ERR, `can't set "b(x)": variable isn't array`},
	{`set a(x) 1; unset a(y)`, ERR, `can't unset "a(y)": no such element in array`},
	{`set a 1; set a(x)`, ERR, `can't read "a(x)": variable isn't array`},
	{`set`, ERR, `wrong # args: should be "set varName ?newValue?"`},
	{`set nosuch`, ERR, `can't read "nosuch": no such variable`},
	{`unset nosuch`, ERR, `can't unset "nosuch": no such variable`},
	{`unset -nocomplain nosuch`, OK, ""},
	{`set ::g 1; set g`, OK, "1"},
	{`append x a b c`, OK, "abc"},
	{`set x 1; incr x 5`, OK, "6"},
	{`incr y`, OK, "1"},

	// Procedures
	{`proc p {a {b 2}} {list $a $b}; p 1`, OK, "1 2"},
	{`proc p {a args} {list $a $args}; p 1 2 3`, OK, "1 {2 3}"},
	{`proc p {a} {}; p`, ERR, `wrong # args: should be "p a"`},
	{`proc p {a {b 1} args} {}; p`, ERR, `wrong # args: should be "p a ?b? ?arg ...?"`},
	{`proc p {} {}; p x`, ERR, `wrong # args: should be "p"`},
	{`proc p {{}} {}`, ERR, "argument with no name"},
	{`proc p {} {} {} {}`, ERR, `wrong # args: should be "proc name arglist ?statics? body"`},
	{`proc p {} {{n 0}} {incr n}; p; p`, OK, "2"},
	{`proc p {&v} {set v 2}; set x 1; p x; set x`, OK, "2"},
	{`apply {{a b} {list $b $a}} 1 2`, OK, "2 1"},
	{`apply {x}`, ERR, `can't interpret "x" as a lambda expression`},
	{`proc p {} {error boom}; p`, ERR, "boom"},
	{`proc p {} {p}; p`, ERR, "Too many nested calls. Infinite recursion?"},

	// Control flow
	{`if {1} {set r a} else {set r b}`, OK, "a"},
	{`if {0} {set r a} elseif {1} {set r b}`, OK, "b"},
	{`if {0} {}`, OK, ""},
	{`if {1}`, ERR, `wrong # args: should be "if condition ?then? trueBody ?elseif ...? ?else? ?falseBody?"`},
	{`set i 0; while {$i < 3} {incr i}; set i`, OK, "3"},
	{`for {set i 0} {$i < 3} {incr i} {}; set i`, OK, "3"},
	{`set r {}; foreach {a b} {1 2 3 4} {append r $a$b}; set r`, OK, "1234"},
	{`lmap x {1 2 3} {set x}`, OK, "1 2 3"},
	{`foreach {} {} {}`, ERR, "foreach varlist is empty"},
	{`set r {}; loop i 0 3 {append r $i}; set r`, OK, "012"},
	{`switch 2 1 {set r one} 2 {set r two} default {set r other}`, OK, "two"},
	{`switch -glob ab a* {list yes} default {list no}`, OK, "yes"},
	{`switch x {a - b {list ab} x {list ex}}`, OK, "ex"},
	{`switch x {x -}`, ERR, `no body specified for pattern "x"`},
	{`switch -bad x a b`, ERR, `bad option "-bad": must be -exact, -glob, -regexp, -command procname or --`},
	{`set r {}; foreach a {1 2} {foreach b {1 2} {append r $a$b; break 2}}; set r`, OK, "11"},

	// Results, errors and the stack trace
	{`catch {error x} m; set m`, OK, "x"},
	{`catch {error x}`, OK, "1"},
	{`catch {break}`, OK, "3"},
	{`catch -nobreak {break}`, BREAK, ""},
	{`catch {error x} m o; set o`, OK, "-code 1 -level 0 -errorinfo {{} {} 1 {error x}} -errorcode NONE"},
	{`try {error x} on error {m} {set m}`, OK, "x"},
	{`try {set x 1} finally {set x 2}; set x`, OK, "2"},
	{`return -level 0 -code error boom`, ERR, "boom"},
	{`return -level 0 hi`, OK, "hi"},
	{`return -bad 1 2`, ERR, `wrong # args: should be "return ?-code code? ?-errorinfo stacktrace? ?-level level? ?result?"`},
	{`proc p {} {tailcall set x 1}; p; set x`, OK, "1"},
	{`tailcall`, ERR, "tailcall can only be called from a proc or lambda"},
	{`nosuchcommand`, ERR, `invalid command name "nosuchcommand"`},

	// Scopes
	{`proc p {} {uplevel 1 {set y 9}}; p; set y`, OK, "9"},
	{`proc p {} {upvar 1 x v; set v 2}; set x 1; p; set x`, OK, "2"},
	{`proc p {} {global g; set g 5}; p; set g`, OK, "5"},
	{`proc p {} {set x 1; upvar 1 x x}; set x 2; p`, ERR, `variable "x" already exists`},
	{`proc p {} {upvar 0 z zz; set zz 1; set z}; p`, OK, "1"},
	{`uplevel 2 {}`, ERR, `bad level "2"`},

	// Commands
	{`proc p {} {}; rename p q; q; exists -command p`, OK, "0"},
	{`rename nosuch other`, ERR, `can't rename "nosuch": command doesn't exist`},
	{`proc p {} {}; proc q {} {}; rename p q`, ERR, `can't rename to "q": command already exists`},
	{`proc p {} {}; rename p {}; exists -command p`, OK, "0"},
	{`exists set`, OK, "0"},
	{`set v 1; exists v`, OK, "1"},
	{`exists -command set`, OK, "1"},
	{`alias a set; a x 3; set x`, OK, "3"},
	{`proc p {} {puts base}; proc w {} {local proc p {} {upcall p}}; exists -proc p`, OK, "1"},
	{`upcall set`, ERR, `no previous command: "set"`},

	// [subst] and [lsubst]
	{`set x 1; subst {a$x}`, OK, "a1"},
	{`set x 1; subst -novariables {a$x}`, OK, `a$x`},
	{`subst {[set y 2]}`, OK, "2"},
	{`subst -nocommands {[set y 2]}`, OK, "[set y 2]"},
	{`subst {a\tb}`, OK, "a\tb"},
	{`subst -nobackslashes {a\tb}`, OK, `a\tb`},
	{`lsubst {a b; c d}`, OK, "a b c d"},
	{`lsubst -line {a b; c d}`, OK, "{a b} {c d}"},

	// Argument expansion
	{`list {*}{a b} c`, OK, "a b c"},
	{`list {*}{}`, OK, ""},
	{`list x{*}y`, OK, "x{*}y"},
	{`list {expand}{a b}`, OK, "a b"},
}

func TestEval(t *testing.T) {
	for _, c := range evalCases {
		i := NewInterp()
		rc := i.Eval(c.script)
		if rc != c.rc || i.Result().String() != c.result {
			t.Errorf("%s: got %d %q, want %d %q", c.script, rc, i.Result().String(), c.rc, c.result)
		}
	}
}

func TestParseListObjs(t *testing.T) {
	cases := []struct {
		src  string
		want []string
	}{
		{"", nil},
		{"a b c", []string{"a", "b", "c"}},
		{"  a   b  ", []string{"a", "b"}},
		{"a {b c} d", []string{"a", "b c", "d"}},
		{`a\ b c`, []string{"a b", "c"}},
		{`"a b" c`, []string{"a b", "c"}},
		{"a\nb\tc", []string{"a", "b", "c"}},
		{`a b\`, []string{"a", `b\`}},
	}
	i := NewInterp()
	for _, c := range cases {
		got, _ := i.parseListObjs([]byte(c.src), 1)
		if len(got) != len(c.want) {
			t.Errorf("%q: got %d elements, want %d", c.src, len(got), len(c.want))
			continue
		}
		for n := range got {
			if got[n].String() != c.want[n] {
				t.Errorf("%q: element %d is %q, want %q", c.src, n, got[n].String(), c.want[n])
			}
		}
	}
}

func TestScriptIsComplete(t *testing.T) {
	cases := []struct {
		script   string
		complete bool
		state    byte
	}{
		{"set x 1", true, ' '},
		{"set x {", false, '{'},
		{"set x [", false, '['},
		{`set x "`, false, '"'},
		{`set x \`, false, '\\'},
		{"set }x{", true, ' '},
	}
	i := NewInterp()
	for _, c := range cases {
		complete, state := i.ScriptIsComplete(i.NewStringObj(c.script))
		if complete != c.complete || state != c.state {
			t.Errorf("%q: got %v %q, want %v %q", c.script, complete, state, c.complete, c.state)
		}
	}
}
