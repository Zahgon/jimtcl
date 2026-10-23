/*
 * jimsh behaviour tests.
 *
 * Copyright 2005 Salvatore Sanfilippo <antirez@invece.org>
 * Copyright 2009 Steve Bennett <steveb@workware.net.au>
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

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// jimshPath builds the interpreter once for the tests in this package.
var jimshPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "jimsh-test")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)
	jimshPath = filepath.Join(dir, "jimsh")
	build := exec.Command("go", "build", "-buildvcs=false", "-o", jimshPath, ".")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

// run evaluates a script with jimsh -e in a fixed environment.
func run(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(jimshPath, args...)
	cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "LANG=C.UTF-8", "TZ=UTC", "HOME=" + t.TempDir()}
	var out, err bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &err
	_ = cmd.Run()
	return out.String(), err.String(), cmd.ProcessState.ExitCode()
}

// The expected values of this table were recorded from the C implementation of
// Jim Tcl 0.84, which this port reproduces byte for byte.
var behaviour = []struct {
	script, out, err string
	code             int
}{
	{"expr {1+2}", "3\n", "", 0},
	{"expr {7/-2}", "-4\n", "", 0},
	{"expr {-7%2}", "1\n", "", 0},
	{"expr {2**62*4}", "0\n", "", 0},
	{"expr {1.0/3}", "0.333333333333\n", "", 0},
	{"expr {1e22}", "1e+22\n", "", 0},
	{"expr {10 < 9}", "0\n", "", 0},
	{"expr {\"10\" < \"9\"}", "0\n", "", 0},
	{"format %.12g [expr {1.0/7}]", "0.142857142857\n", "", 0},
	{"format %c 955", "λ\n", "", 0},
	{"format {%2$s-%1$s} a b", "b-a\n", "", 0},
	{"scan \"12 3.5\" \"%d %f\"", "12 3.5\n", "", 0},
	{"list a \"b c\" {} \\{ #", "a {b c} {} \\{ #\n", "", 0},
	{"list [list a b] c", "{a b} c\n", "", 0},
	{"lsort -unique {b a b 10 9}", "10 9 a b\n", "", 0},
	{"lsort -integer {10 9 100}", "9 10 100\n", "", 0},
	{"lsort -real {1 1.0 2}", "1 1.0 2\n", "", 0},
	{"string length éé", "2\n", "", 0},
	{"string bytelength éé", "4\n", "", 0},
	{"string totitle \"éab\"", "Éab\n", "", 0},
	{"string toupper ß", "ß\n", "", 0},
	{"string index abcdé 4", "é\n", "", 0},
	{"string range abcdé 1 end-1", "bcd\n", "", 0},
	{"string map {a 1 aa 2} aaa", "111\n", "", 0},
	{"string match {[a-c]*} b", "1\n", "", 0},
	{"string is double \" 1e3 \"", "1\n", "", 0},
	{"string is integer 0x10", "1\n", "", 0},
	{"string trim xxaxx x", "a\n", "", 0},
	{"string first \"\" abc", "-1\n", "", 0},
	{"string reverse \"éa\"", "aé\n", "", 0},
	{"dict keys {z 1 y 2 a 3}", "z y a\n", "", 0},
	{"dict get {a 1 b 2} b", "2\n", "", 0},
	{"regexp -inline {(a+)(b*)} aaab", "aaab aaa b\n", "", 0},
	{"regsub -all {a*} bab X", "XbXXbX\n", "", 0},
	{"regsub {(a)(b)} ab {\\2\\1}", "ba\n", "", 0},
	{"join {a {b c} {}} -", "a-b c-\n", "", 0},
	{"split \"a,,b\" ,", "a {} b\n", "", 0},
	{"split abc \"\"", "a b c\n", "", 0},
	{"concat \" a \" {b c} \"\"", "a b c \n", "", 0},
	{"lrange {a b c d} 1 end-1", "b c\n", "", 0},
	{"llength {a {b c} {}}", "3\n", "", 0},
	{"info version", "0.84\n", "", 0},
	{"info returncodes", "0 ok 1 error 2 return 3 break 4 continue 5 signal 6 exit 7 eval\n", "", 0},
	{"clock format 0 -format {%Y-%m-%dT%H:%M:%S} -gmt 1", "1970-01-01T00:00:00\n", "", 0},
	{"apply {{a b} {expr {$a+$b}}} 2 3", "5\n", "", 0},
	{"proc p {a {b 7}} { list $a $b }; list [p 1] [p 1 2]", "{1 7} {1 2}\n", "", 0},
	{"set x 1.50; list [expr {$x}] $x [expr {$x+0}]", "1.50 1.50 1.5\n", "", 0},
	{"catch {expr 1/0} m; set m", "Division by zero\n", "", 0},
	{"catch {lindex} m; set m", "wrong # args: should be \"lindex list ?index ...?\"\n", "", 0},
	{"catch {string index abc} m; set m", "wrong # args: should be \"string index string index\"\n", "", 0},
	{"catch {dict get {a 1} b} m; set m", "key \"b\" not known in dictionary\n", "", 0},
	{"catch {unknowncmd} m; set m", "invalid command name \"unknowncmd\"\n", "", 0},
	{"catch {lsort -bad {a}} m; set m", "bad option \"-bad\": must be -ascii, -command, -decreasing, -dictionary, -increasing, -index, -integer, -nocase, -real, -stride, or -unique\n", "", 0},
	{"catch {format %d x} m; set m", "expected integer but got \"x\"\n", "", 0},
	{"proc hp {} {set zz 1; set aa 2; set mm 3; set b 4; set qqq 5; info locals}; hp", "b mm zz aa qqq\n", "", 0},
	{"clock scan {2020-01-02 11:22:33} -format {%Y-%m-%d %H:%M:%S} -gmt 1", "1577964153\n", "", 0},
}

func TestBehaviour(t *testing.T) {
	for _, tc := range behaviour {
		gotOut, gotErr, code := run(t, "-e", tc.script)
		if gotOut != tc.out || gotErr != tc.err || code != tc.code {
			t.Errorf("jimsh -e %q\n  stdout %q, want %q\n  stderr %q, want %q\n  exit %d, want %d",
				tc.script, gotOut, tc.out, gotErr, tc.err, code, tc.code)
		}
	}
}

// TestSuite runs the Tcl test suite in tests/, which the original passes
// completely in this configuration.
func TestSuite(t *testing.T) {
	names, err := os.ReadFile(filepath.Join("tests", "go-suite.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range strings.Fields(string(names)) {
		t.Run(name, func(t *testing.T) {
			cmd := exec.Command(jimshPath, name)
			cmd.Dir = "tests"
			cmd.Env = []string{"PATH=/usr/local/bin:/usr/bin:/bin", "LANG=C.UTF-8", "TZ=UTC",
				"HOME=" + t.TempDir()}
			out, _ := cmd.CombinedOutput()
			last := ""
			for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
				if strings.Contains(line, "Total") {
					last = line
				}
			}
			if !strings.Contains(last, "Failed     0") || last == "" {
				t.Errorf("%s: %s", name, lastLines(string(out), 12))
			}
		})
	}
}

// lastLines keeps a failing suite's report short.
func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
