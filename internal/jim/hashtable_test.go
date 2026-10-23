/* Jim Tcl 0.84 ported from C to Go: hash table order tests.
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

import (
	"strings"
	"testing"
)

// The expected orders are those the original jimsh 0.84 prints for
// [info locals] and [info vars] after the same insertions.
func TestHashTableOrder(t *testing.T) {
	tests := []struct {
		insert string
		want   string
	}{
		{"zz aa mm b qqq", "b mm zz aa qqq"},
		{"a1 b2 c3 d4 e5 f6", "e5 b2 d4 a1 f6 c3"},
	}
	for _, tc := range tests {
		var h htable[int]
		for i, k := range strings.Fields(tc.insert) {
			h.set(k, i)
		}
		if got := strings.Join(h.keys(), " "); got != tc.want {
			t.Errorf("insert %q: order %q, want %q", tc.insert, got, tc.want)
		}
	}
}

func TestHashTableGrowth(t *testing.T) {
	var h htable[int]
	for i := 0; i < 40; i++ {
		h.set(string(rune('a'+i%26))+string(rune('0'+i/26)), i)
	}
	if h.len() != 40 || len(h.keys()) != 40 {
		t.Fatalf("len %d, keys %d, want 40", h.len(), len(h.keys()))
	}
	if !h.delete("a0") || h.delete("a0") {
		t.Error("delete should succeed once")
	}
	if _, ok := h.get("b0"); !ok {
		t.Error("b0 should still be present")
	}
}
