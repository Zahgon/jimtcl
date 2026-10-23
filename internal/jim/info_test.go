/* Jim Tcl 0.84 ported from C to Go: tests for the introspection area.
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

// newInfoTestInterp returns an interpreter with just enough state for the parts
// of the area that do not need the evaluation engine.
func newInfoTestInterp() *Interp {
	i := &Interp{references: map[string]*Reference{}}
	i.emptyObj = i.NewStringObj("")
	i.result = i.emptyObj
	return i
}

func TestNamespaceQualifiersAndTail(t *testing.T) {
	i := newInfoTestInterp()
	tests := []struct{ in, qualifiers, tail string }{
		{"::bob::tom", "::bob", "tom"},
		{"bob::tom", "bob", "tom"},
		{"a::b::c", "a::b", "c"},
		{"::bob", "", "bob"},
		{"bob", "", "bob"},
		{"::", "", ""},
		{"", "", ""},
		{":single", "", ":single"},
	}
	for _, tc := range tests {
		if got := i.NamespaceQualifiers(i.NewStringObj(tc.in)).String(); got != tc.qualifiers {
			t.Errorf("qualifiers %q = %q, want %q", tc.in, got, tc.qualifiers)
		}
		if got := i.NamespaceTail(i.NewStringObj(tc.in)).String(); got != tc.tail {
			t.Errorf("tail %q = %q, want %q", tc.in, got, tc.tail)
		}
	}
}

func TestCanonicalNamespace(t *testing.T) {
	i := newInfoTestInterp()
	tests := []struct{ ns, name, want string }{
		{"", "abc", "abc"},
		{"", "::abc", "abc"},
		{"", "abc::def", "abc::def"},
		{"abc", "def", "abc::def"},
		{"abc", "::def", "def"},
		{"", "::::a", "a"},
		{"x", "y", "x::y"},
	}
	for _, tc := range tests {
		got := i.canonicalNamespace(i.NewStringObj(tc.ns), i.NewStringObj(tc.name)).String()
		if got != tc.want {
			t.Errorf("canonical %q %q = %q, want %q", tc.ns, tc.name, got, tc.want)
		}
	}
}

func TestPatternHelpers(t *testing.T) {
	trivial := []struct {
		pattern string
		want    bool
	}{{"abc", true}, {"", true}, {"a*", false}, {"a?", false}, {"a[b]", false}, {`a\b`, false}}
	for _, tc := range trivial {
		if got := infoTrivialMatch(tc.pattern); got != tc.want {
			t.Errorf("infoTrivialMatch(%q) = %v, want %v", tc.pattern, got, tc.want)
		}
	}
	qualified := []struct{ in, want string }{
		{"::set", "set"}, {"::::set", "set"}, {"a::b", "a::b"}, {":x", ":x"}, {"set", "set"},
	}
	for _, tc := range qualified {
		if got := infoNoQualifier(tc.in); got != tc.want {
			t.Errorf("infoNoQualifier(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if !infoIsGlobalNamespace(newInfoTestInterp().NewStringObj("::a")) {
		t.Error("::a should name the global namespace")
	}
	if infoIsGlobalNamespace(newInfoTestInterp().NewStringObj(":a")) {
		t.Error(":a should not name the global namespace")
	}
}

func TestNewReferenceStringRep(t *testing.T) {
	i := newInfoTestInterp()
	tests := []struct{ tag, want string }{
		{"tag", "<reference.<tag____>.00000000000000000000>"},
		{"t", "<reference.<t______>.00000000000000000002>"},
		{"tag with spaces!!!!!!!", "<reference.<tag_wit>.00000000000000000004>"},
		{"", "<reference.<_______>.00000000000000000006>"},
	}
	for _, tc := range tests {
		ref := i.NewReference(i.NewStringObj("value"), i.NewStringObj(tc.tag), nil)
		if got := ref.String(); got != tc.want {
			t.Errorf("ref tag %q = %q, want %q", tc.tag, got, tc.want)
		}
		// The reference must be resolvable from its string representation.
		if refPtr := i.GetReference(i.NewStringObj(tc.want)); refPtr == nil {
			t.Errorf("reference %q not found: %s", tc.want, i.Result())
		}
	}
}

func TestGetReferenceErrors(t *testing.T) {
	i := newInfoTestInterp()
	tests := []struct{ in, want string }{
		{"foo", `expected reference but got "foo"`},
		{"<reference.<tag____>.00000000000000000099>", `invalid reference id "<reference.<tag____>.00000000000000000099>"`},
		{"<reference.<tag!___>.00000000000000000000>", `expected reference but got "<reference.<tag!___>.00000000000000000000>"`},
		{"<reference.<tag____>.0000000000000000000x>", `expected reference but got "<reference.<tag____>.0000000000000000000x>"`},
	}
	for _, tc := range tests {
		if refPtr := i.GetReference(i.NewStringObj(tc.in)); refPtr != nil {
			t.Errorf("GetReference(%q) unexpectedly succeeded", tc.in)
			continue
		}
		if got := i.Result().String(); got != tc.want {
			t.Errorf("GetReference(%q) error = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRefMarkString(t *testing.T) {
	const ref = "<reference.<tag____>.00000000000000000042>"
	tests := []struct {
		in   string
		want []uint64
	}{
		{"", nil},
		{"short", nil},
		{ref, []uint64{42}},
		{"::" + ref, []uint64{42}},
		{"prefix " + ref + " suffix", []uint64{42}},
		{"<reference.<tag____>.000000000000000000x2>", nil},
	}
	for _, tc := range tests {
		marks := map[uint64]bool{}
		refMarkString(tc.in, marks)
		if len(marks) != len(tc.want) {
			t.Errorf("refMarkString(%q) marked %d ids, want %d", tc.in, len(marks), len(tc.want))
			continue
		}
		for _, id := range tc.want {
			if !marks[id] {
				t.Errorf("refMarkString(%q) did not mark %d", tc.in, id)
			}
		}
	}
}

func TestPackageProvide(t *testing.T) {
	i := newInfoTestInterp()
	if rc := i.PackageProvide("foo", "1.0", ErrMsg); rc != OK {
		t.Fatalf("first provide failed: %s", i.Result())
	}
	if rc := i.PackageProvide("foo", "1.0", ErrMsg); rc != ERR {
		t.Fatal("second provide should fail")
	}
	if got := i.Result().String(); got != `package "foo" was already provided` {
		t.Errorf("error = %q", got)
	}
	// An automatic entry has an empty version and may be replaced.
	i.PackageProvide("bar", "", 0)
	if rc := i.PackageProvide("bar", "1.0", ErrMsg); rc != OK {
		t.Errorf("replacing the automatic entry failed: %s", i.Result())
	}
	if got := pkgVersion(i, "bar"); got != "1.0" {
		t.Errorf("version = %q, want 1.0", got)
	}
}

func TestIsDictSugar(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{{"a(1)", true}, {"a", false}, {"a)", false}, {"(a)", true}, {"a(", false}}
	for _, tc := range tests {
		if got := nsIsDictSugar(tc.in); got != tc.want {
			t.Errorf("nsIsDictSugar(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// pkgVersion returns the recorded version of a package.
func pkgVersion(i *Interp, name string) string {
	ver, _ := i.packages.get(name)
	if ver == nil {
		return ""
	}
	return ver.String()
}
