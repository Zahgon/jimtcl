/* Jim - A small embeddable Tcl interpreter: tests for lists and dictionaries.
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

// newListTestInterp returns the minimal interpreter the list and dictionary
// code needs, without registering any command.
func newListTestInterp() *Interp {
	i := &Interp{}
	i.emptyObj = i.NewStringObj("")
	i.result = i.emptyObj
	return i
}

func (i *Interp) listTestObjs(elems ...string) []*Obj {
	objs := make([]*Obj, len(elems))
	for n, e := range elems {
		objs[n] = i.NewStringObj(e)
	}
	return objs
}

// TestMakeListStringRep checks the quoting of the list string representation
// against the values the C implementation produces.
func TestMakeListStringRep(t *testing.T) {
	i := newListTestInterp()
	tests := []struct {
		elems []string
		want  string
	}{
		{nil, ""},
		{[]string{""}, "{}"},
		{[]string{"a", "b c", ""}, "a {b c} {}"},
		{[]string{"#"}, "{#}"},
		{[]string{"#", "a b"}, "{#} {a b}"},
		{[]string{"#a", "b"}, "{#a} b"},
		{[]string{"a", "#b"}, "a #b"},
		{[]string{"#{"}, `\#\{`},
		{[]string{"{a"}, `\{a`},
		{[]string{"a{"}, `a\{`},
		{[]string{"}"}, `\}`},
		{[]string{"a}b"}, `a\}b`},
		{[]string{"{}"}, "{{}}"},
		{[]string{"{a}"}, "{{a}}"},
		{[]string{"a{}b"}, "a{}b"},
		{[]string{"a "}, "{a }"},
		{[]string{"a\nb"}, "{a\nb}"},
		{[]string{"a\tb"}, "{a\tb}"},
		{[]string{`\`}, `\\`},
		{[]string{`\x`}, `{\x}`},
		{[]string{`x\`}, `x\\`},
		{[]string{"a\\\nb"}, `a\\\nb`},
		{[]string{`a\}b`}, `{a\}b}`},
		{[]string{"]a["}, "{]a[}"},
		{[]string{"a$b"}, "{a$b}"},
		{[]string{"a;b"}, "{a;b}"},
	}
	for _, tt := range tests {
		got := makeListStringRep(i.listTestObjs(tt.elems...))
		if got != tt.want {
			t.Errorf("makeListStringRep(%q) = %q, want %q", tt.elems, got, tt.want)
		}
	}
}

func TestListElementQuotingType(t *testing.T) {
	tests := []struct {
		s    string
		want int
	}{
		{"", eleStrBrace},
		{"abc", eleStrSimple},
		{"a{}b", eleStrSimple},
		{"a b", eleStrBrace},
		{"{a}", eleStrBrace},
		{`"a"`, eleStrBrace},
		{"{a", eleStrQuote},
		{"}", eleStrQuote},
		{`a\`, eleStrQuote},
		{"a[b", eleStrBrace},
		{"]a", eleStrQuote},
	}
	for _, tt := range tests {
		if got := listElementQuotingType([]byte(tt.s)); got != tt.want {
			t.Errorf("listElementQuotingType(%q) = %d, want %d", tt.s, got, tt.want)
		}
	}
}

// TestConcatObj checks the whitespace trimming of [concat].
func TestConcatObj(t *testing.T) {
	i := newListTestInterp()
	tests := []struct {
		elems []string
		want  string
	}{
		{nil, ""},
		{[]string{" a ", "b"}, "a b"},
		{[]string{"", "a", ""}, "a "},
		{[]string{"a", "", "b"}, "a b"},
		{[]string{`a\ `, "b"}, `a\  b`},
		{[]string{"  ", "b"}, "b"},
	}
	for _, tt := range tests {
		if got := i.ConcatObj(i.listTestObjs(tt.elems...)).String(); got != tt.want {
			t.Errorf("ConcatObj(%q) = %q, want %q", tt.elems, got, tt.want)
		}
	}
}

func TestListStrToUll(t *testing.T) {
	tests := []struct {
		s    string
		want int64
		end  int
	}{
		{"0", 0, 1},
		{"123abc", 123, 3},
		{"0x10", 16, 4},
		{"0b11", 3, 4},
		{"0o17", 15, 4},
		{"0xg", 0, 1},
		{"007", 7, 3},
		{"0x", 0, 1},
	}
	for _, tt := range tests {
		got, end := numStrtoull(tt.s)
		if got != tt.want || end != tt.end {
			t.Errorf("numStrtoull(%q) = %d, %d, want %d, %d", tt.s, got, end, tt.want, tt.end)
		}
	}
}

// TestListMergeSortOrder checks that the elements are compared in the same
// order as the merge sort of the C library, which [lsort -command] exposes.
func TestListMergeSortOrder(t *testing.T) {
	i := newListTestInterp()
	elems := i.listTestObjs("h", "g", "f", "e", "d", "c", "b", "a")
	var log string
	listMergeSort(elems, make([]*Obj, len(elems)), func(a, b *Obj) int {
		log += a.String() + b.String() + " "
		return stringsCompare(a.String(), b.String())
	})
	const want = "hg fe ge gf dc ba ca cb ea eb ec ed "
	if log != want {
		t.Errorf("comparison order = %q, want %q", log, want)
	}
	var sorted string
	for _, e := range elems {
		sorted += e.String()
	}
	if sorted != "abcdefgh" {
		t.Errorf("sorted = %q, want %q", sorted, "abcdefgh")
	}
}

// TestDictInsertionOrder checks that removing a key swaps the last pair into
// the hole it leaves, which is what the string representation shows.
func TestDictInsertionOrder(t *testing.T) {
	i := newListTestInterp()
	tests := []struct {
		keys  []string // alternating key, value
		unset []string
		add   []string
		want  string
	}{
		{[]string{"a", "1", "b", "2", "c", "3"}, []string{"b"}, []string{"d", "4"}, "a 1 c 3 d 4"},
		{[]string{"a", "1", "b", "2", "c", "3"}, []string{"c"}, nil, "a 1 b 2"},
		{[]string{"a", "1", "b", "2", "c", "3"}, []string{"a"}, nil, "c 3 b 2"},
		{[]string{"a", "1", "b", "2", "c", "3"}, []string{"a", "c"}, nil, "b 2"},
		{[]string{"a", "1", "b", "2"}, nil, []string{"a", "9"}, "a 9 b 2"},
		{[]string{"a", "1", "b", "2"}, []string{"zz"}, nil, "a 1 b 2"},
	}
	for _, tt := range tests {
		d := i.NewDictObj(i.listTestObjs(tt.keys...))
		for _, k := range tt.unset {
			i.DictAddElement(d, i.NewStringObj(k), nil)
		}
		for n := 0; n+1 < len(tt.add); n += 2 {
			i.DictAddElement(d, i.NewStringObj(tt.add[n]), i.NewStringObj(tt.add[n+1]))
		}
		if got := d.String(); got != tt.want {
			t.Errorf("dict %v unset %v add %v = %q, want %q", tt.keys, tt.unset, tt.add, got, tt.want)
		}
	}
}

// TestDictLookupAfterRemove checks that the keys of a dictionary can still be
// found after entries have been removed from the hash table.
func TestDictLookupAfterRemove(t *testing.T) {
	i := newListTestInterp()
	var elems []string
	keys := []string{"one", "two", "three", "four", "five", "six", "seven", "eight"}
	for n, k := range keys {
		elems = append(elems, k, string(rune('0'+n)))
	}
	d := i.NewDictObj(i.listTestObjs(elems...))
	for _, k := range []string{"one", "four", "seven"} {
		if i.DictAddElement(d, i.NewStringObj(k), nil) != OK {
			t.Fatalf("removing %q failed", k)
		}
	}
	for _, k := range []string{"two", "three", "five", "six", "eight"} {
		if _, rc := i.DictKey(d, i.NewStringObj(k), None); rc != OK {
			t.Errorf("key %q not found after removals", k)
		}
	}
	for _, k := range []string{"one", "four", "seven"} {
		if _, rc := i.DictKey(d, i.NewStringObj(k), None); rc == OK {
			t.Errorf("removed key %q still found", k)
		}
	}
	if size, rc := i.DictSize(d); rc != OK || size != 5 {
		t.Errorf("DictSize = %d, %d, want 5, OK", size, rc)
	}
}

func TestNewDictObjOddLength(t *testing.T) {
	i := newListTestInterp()
	if d := i.NewDictObj(i.listTestObjs("a", "1", "b")); d != nil {
		t.Errorf("NewDictObj with an odd length returned %q", d.String())
	}
	if got, want := i.Result().String(), "missing value to go with key"; got != want {
		t.Errorf("result = %q, want %q", got, want)
	}
}

// TestObjectHashOfInteger checks that the numeric shortcut of the object hash
// gives the same value as hashing the string representation, which the
// dictionary relies on when an object acquires a string representation.
func TestObjectHashOfInteger(t *testing.T) {
	i := newListTestInterp()
	for _, w := range []int64{0, 1, 9, 10, -1, -1234, 2147483646, -2147483646} {
		n := i.NewIntObj(w)
		s := i.NewStringObj(formatWide(w))
		if got, want := objectHTHashFunction(n), objectHTHashFunction(s); got != want {
			t.Errorf("hash(%d) = %d, hash(%q) = %d", w, got, s.String(), want)
		}
	}
}

func stringsCompare(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
