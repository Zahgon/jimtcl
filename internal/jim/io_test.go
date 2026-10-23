/* Jim Tcl 0.84 ported from C to Go: tests for the channel, file and clock
 * commands. The expected values are those of the original jimsh.
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
	"syscall"
	"testing"
	"time"
)

func TestIoStrerror(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{nil, "Success"},
		{syscall.Errno(0), "Success"},
		{syscall.ENOENT, "No such file or directory"},
		{syscall.EACCES, "Permission denied"},
		{syscall.EISDIR, "Is a directory"},
		{syscall.ENOTDIR, "Not a directory"},
		{syscall.EEXIST, "File exists"},
		{syscall.EINVAL, "Invalid argument"},
		{syscall.ENOTEMPTY, "Directory not empty"},
		{syscall.ENOTSOCK, "Socket operation on non-socket"},
		{syscall.ENOTTY, "Inappropriate ioctl for device"},
		{syscall.Errno(4095), "Unknown error 4095"},
	}
	for _, tc := range tests {
		if got := ioStrerror(tc.err); got != tc.want {
			t.Errorf("ioStrerror(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

func TestFilePathHelpers(t *testing.T) {
	tests := []struct{ path, want string }{
		{"/", "/"},
		{"///", "/"},
		{"a/b/", "a/b"},
		{"a//", "a"},
		{"", ""},
		{"a", "a"},
	}
	for _, tc := range tests {
		if got := fileStripTrailingSlashes(tc.path); got != tc.want {
			t.Errorf("fileStripTrailingSlashes(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestFileGetFileType(t *testing.T) {
	tests := []struct {
		mode uint32
		want string
	}{
		{syscall.S_IFREG | 0644, "file"},
		{syscall.S_IFDIR | 0755, "directory"},
		{syscall.S_IFCHR | 0666, "characterSpecial"},
		{syscall.S_IFBLK | 0660, "blockSpecial"},
		{syscall.S_IFIFO | 0600, "fifo"},
		{syscall.S_IFLNK | 0777, "link"},
		{syscall.S_IFSOCK | 0755, "socket"},
		{0, "unknown"},
	}
	for _, tc := range tests {
		if got := fileGetFileType(tc.mode); got != tc.want {
			t.Errorf("fileGetFileType(%#o) = %q, want %q", tc.mode, got, tc.want)
		}
	}
}

func TestAioIndexBytes(t *testing.T) {
	tests := []struct {
		haystack, needle string
		want             int
	}{
		{"abc\ndef", "\n", 3},
		{"abcdef", "\n", -1},
		{"a<>b", "<>", 1},
		{"ab", "", 0},
		{"", "x", -1},
	}
	for _, tc := range tests {
		if got := aioIndexBytes([]byte(tc.haystack), []byte(tc.needle)); got != tc.want {
			t.Errorf("aioIndexBytes(%q, %q) = %d, want %d", tc.haystack, tc.needle, got, tc.want)
		}
	}
}

func TestClockStrftime(t *testing.T) {
	// 1234567890 is Fri Feb 13 23:31:30 2009 GMT
	when := time.Unix(1234567890, 0).In(time.UTC)
	tests := []struct{ format, want string }{
		{"%Y-%m-%d", "2009-02-13"},
		{"%H:%M:%S", "23:31:30"},
		{"%a|%A|%b|%B", "Fri|Friday|Feb|February"},
		{"%j|%e|%p|%Z|%z", "044|13|PM|GMT|+0000"},
		{"%s", "1234567890"},
		{"%C|%y|%G|%g", "20|09|2009|09"},
		{"%D|%F|%T|%R|%r", "02/13/09|2009-02-13|23:31:30|23:31|11:31:30 PM"},
		{"%U|%V|%W|%u|%w", "06|07|06|5|5"},
		{"%-d|%_d|%0e|%03d|%10Y|%-10Y", "13|13|13|013|0000002009|      2009"},
		{"%%|%i|%5N", "%|%i|  %5N"},
		{"%c|%x|%X", "Fri Feb 13 23:31:30 2009|02/13/09|23:31:30"},
		{"%^a|%#p|%P|%n|%t", "FRI|pm|pm|\n|\t"},
	}
	for _, tc := range tests {
		if got := clockStrftime(tc.format, when, true); got != tc.want {
			t.Errorf("clockStrftime(%q) = %q, want %q", tc.format, got, tc.want)
		}
	}
	// A small year is not padded, while an explicit width is
	small := time.Unix(-62003991233, 0).In(time.UTC)
	for _, tc := range []struct{ format, want string }{
		{"%Y|%C|%y|%G|%g|%j|%d", "5|0|05|5|05|063|04"},
		{"%10Y|%-10Y", "0000000005|         5"},
	} {
		if got := clockStrftime(tc.format, small, true); got != tc.want {
			t.Errorf("clockStrftime(%q) = %q, want %q", tc.format, got, tc.want)
		}
	}
}

func TestClockStrptime(t *testing.T) {
	tests := []struct {
		value, format string
		want          int64
		ok            bool
	}{
		{"2020-01-02 03:04:05", "%Y-%m-%d %H:%M:%S", 1577934245, true},
		{"2009-02-13T23:31:30", "%FT%T", 1234567890, true},
		{"Fri Feb 13 23:31:30 2009", "%a %b %e %H:%M:%S %Y", 1234567890, true},
		{"February 13 2009 23:31:30", "%B %d %Y %H:%M:%S", 1234567890, true},
		{"11:31:30 PM 2009-02-13", "%r %F", 1234567890, true},
		{"044 2009 23:31:30", "%j %Y %H:%M:%S", 1234567890, true},
		{"1234567890", "%s", 1234567890, true},
		{"  2020-1-2 03:04:05 ", " %Y-%m-%d %H:%M:%S ", 1577934245, true},
		{"x", "%Y", 0, false},
		{"", "%Y", 0, false},
		{"2020-01-02 ", "%Y-%m-%d", 0, false},
		{"2020-13-01", "%Y-%m-%d", 0, false},
		{"-100", "%s", 0, false},
	}
	for _, tc := range tests {
		tm := clockTm{}
		ok := clockStrptime(tc.value, tc.format, &tm)
		if ok != tc.ok {
			t.Errorf("clockStrptime(%q, %q) ok = %v, want %v", tc.value, tc.format, ok, tc.ok)
			continue
		}
		if ok {
			if got := clockTimegm(&tm); got != tc.want {
				t.Errorf("clockStrptime(%q, %q) = %d, want %d", tc.value, tc.format, got, tc.want)
			}
		}
	}
}

func TestStaticExtSource(t *testing.T) {
	// make-c-ext.tcl blanks comment lines so that the line numbers of the
	// remaining lines do not change.
	src := "# a comment\nset x 1\n\t# indented comment\nset y 2 ;# trailing\n"
	want := "\nset x 1\n\nset y 2 ;# trailing\n"
	got := StaticExtSource("stdlib.tcl", src)
	if got.Source != want {
		t.Errorf("Source = %q, want %q", got.Source, want)
	}
	if got.Package != "stdlib" || got.FileName != "stdlib.tcl" {
		t.Errorf("Package/FileName = %q/%q", got.Package, got.FileName)
	}
	if got := StaticExtSource("x.tcl", "a\r\nb").Source; got != "a\nb\n" {
		t.Errorf("carriage returns not removed: %q", got)
	}
}
