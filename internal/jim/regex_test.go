/* Jim Tcl 0.84 ported from C to Go: tests for the regexp and regsub commands.
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
	"testing"
)

// TestRegsubReplace covers the subSpec substitutions of [regsub]. The expected
// values come from the original jimsh: regsub {(a)(b)} ab <subSpec>
func TestRegsubReplace(t *testing.T) {
	// The match of {(a)(b)} against "ab"
	pmatch := []int{0, 2, 0, 1, 1, 2}

	tests := []struct{ spec, want string }{
		{`\2\1`, "ba"},
		{`&-&`, "ab-ab"},
		{`\0`, "ab"},
		{`\1\1`, "aa"},
		{`x\\y`, `x\y`},
		{`x\&y`, "x&y"},
		{`x\9y`, "xy"},
		{`x\qy`, `x\qy`},
		{`a\`, `a\`},
		{"", ""},
	}
	for _, tc := range tests {
		var b strings.Builder
		regsubReplace(&b, tc.spec, "ab", pmatch)
		if got := b.String(); got != tc.want {
			t.Errorf("regsubReplace(%q) = %q, want %q", tc.spec, got, tc.want)
		}
	}

	// An unmatched group expands to nothing: regsub {b} ab {[\1]} is "a[]"
	var b strings.Builder
	regsubReplace(&b, `[\1]`, "ab", []int{1, 2})
	if got := b.String(); got != "[]" {
		t.Errorf(`regsubReplace("[\\1]") = %q, want "[]"`, got)
	}
}

func TestRegexpUtf8(t *testing.T) {
	strlen := []struct {
		s       string
		bytelen int
		want    int
	}{
		{"abc", 3, 3},
		{"abc", 0, 0},
		{"ébc", 4, 3},
		{"ébc", 2, 1},
		{"éé", 3, 2},   // a sequence straddling bytelen counts as one char
		{"\xc3", 1, 1}, // an invalid sequence counts as one char per byte
		{"\xff\xfe", 2, 2},
	}
	for _, tc := range strlen {
		if got := regexpUtf8Strlen(tc.s, tc.bytelen); got != tc.want {
			t.Errorf("regexpUtf8Strlen(%q, %d) = %d, want %d", tc.s, tc.bytelen, got, tc.want)
		}
	}

	index := []struct {
		s     string
		chars int
		want  int
	}{
		{"abc", 0, 0},
		{"abc", 2, 2},
		{"ébc", 1, 2},
		{"ébc", 3, 4},
		{"ébc", 9, 4}, // never past the end of the string
		{"\xff", 1, 1},
	}
	for _, tc := range index {
		if got := u8.ByteIndex(tc.s, tc.chars); got != tc.want {
			t.Errorf("u8.ByteIndex(%q, %d) = %d, want %d", tc.s, tc.chars, got, tc.want)
		}
	}

	if got := regexpCString("ab\x00cd"); got != "ab" {
		t.Errorf("regexpCString: %q, want %q", got, "ab")
	}
}
