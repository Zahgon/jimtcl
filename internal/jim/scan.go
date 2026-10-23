/* Jim Tcl 0.84 ported from C to Go: the scan format object and the scan
 * command.
 *
 * Copyright 2005 Salvatore Sanfilippo <antirez@invece.org>
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
	"strconv"

	"github.com/msteveb/jimtcl/internal/u8"
)

// scanFmtPart holds one part of a scan format string: the literal text to be
// matched before the conversion, and the conversion itself
// (C: ScanFmtPartDescr).
type scanFmtPart struct {
	charSet   string // the character set of a %[...] conversion
	prefix    string // literal prefix to be scanned before the conversion
	hasPrefix bool
	// width is a size_t in the C, so a negative width given in the format
	// string wraps round to a huge value rather than disabling the width.
	width    uint64
	pos      int // -1 no assign, 0 natural position, >0 XPG3 position
	typ      byte
	modifier byte
}

// scanFmt is the parsed representation of a scan format string
// (C: ScanFmtStringObj). err is the message for a malformed format, which is
// reported when the format is used rather than when it is parsed.
type scanFmt struct {
	parts     []scanFmtPart
	convCount int
	maxPos    int
	err       string
}

// Outcome of a scan: a result list, end of input, or a bad format.
const (
	scanDone = iota
	scanEOF
	scanFailed
)

// numScanInt reads a decimal number at pos the way the C's
// sscanf("%d%n", ...) does, including skipping leading white space.
func numScanInt(s string, pos int) (val, end int, ok bool) {
	i := pos
	for i < len(s) && u8.IsSpace(s[i]) {
		i++
	}
	j := i
	if j < len(s) && (s[j] == '+' || s[j] == '-') {
		j++
	}
	d := j
	for j < len(s) && u8.IsDigit(s[j]) {
		j++
	}
	if j == d {
		return 0, pos, false
	}
	v, err := strconv.Atoi(s[i:j])
	if err != nil {
		return 0, pos, false
	}
	return v, j, true
}

// setScanFmtFromAny parses a format string and caches its parsed
// representation on the object (C: SetScanFmtFromAny).
func (i *Interp) setScanFmtFromAny(o *Obj) {
	f := &scanFmt{}
	str := o.String()
	o.freeIntRep()
	o.kind = kindFormat
	o.aux = f

	lastPos := -1
	p := 0
	for p < len(str) {
		var part scanFmtPart

		// Overread and store any "literal" prefix.
		if str[p] != '%' || p+1 < len(str) && str[p+1] == '%' {
			part.hasPrefix = true
			var b []byte
			for p < len(str) {
				if str[p] == '%' {
					if !(p+1 < len(str) && str[p+1] == '%') {
						break
					}
					p++
				}
				b = append(b, str[p])
				p++
			}
			part.prefix = string(b)
		}
		// Skip the conversion introducing '%' sign. The format may have ended
		// with nothing but the literal prefix, in which case that prefix is
		// dropped, exactly as the C's "goto done" does.
		p++
		if p >= len(str) {
			return
		}

		if str[p] == '*' {
			part.pos = -1 // this conversion will not be assigned
			p++
		} else {
			f.convCount++
		}

		// The next token may be either a width or an XPG3 position.
		if width, end, ok := numScanInt(str, p); ok {
			p = end
			if part.pos != -1 && p < len(str) && str[p] == '$' {
				p++
				part.pos = width
				// Look if "natural" positioning and XPG3 were mixed.
				if lastPos == 0 && part.pos > 0 || lastPos > 0 && part.pos == 0 {
					f.err = "cannot mix \"%\" and \"%n$\" conversion specifiers"
					return
				}
				for _, prev := range f.parts {
					if prev.pos != -1 && prev.pos == part.pos {
						f.err = "variable is assigned by multiple \"%n$\" conversion specifiers"
						return
					}
				}
				if part.pos < 0 {
					f.err = "\"%n$\" conversion specifier is negative"
					return
				}
				// Try to find a width after the XPG3 specifier.
				if w, end, ok := numScanInt(str, p); ok {
					part.width = uint64(w)
					p = end
				}
				if part.pos > f.maxPos {
					f.maxPos = part.pos
				}
			} else {
				part.width = uint64(width)
			}
		}
		// If the positioning mode was undetermined yet, fix this.
		if lastPos == -1 {
			lastPos = part.pos
		}

		if p < len(str) && str[p] == '[' {
			part.typ = '['
			p++
			var set []byte
			// A leading '^' negates, and a ']' right after it is part of the set.
			if p < len(str) && str[p] == '^' {
				set = append(set, str[p])
				p++
			}
			if p < len(str) && str[p] == ']' {
				set = append(set, str[p])
				p++
			}
			for p < len(str) && str[p] != ']' {
				set = append(set, str[p])
				p++
			}
			if p >= len(str) || str[p] != ']' {
				f.err = "unmatched [ in format string"
				return
			}
			// In case a range fence was given "backwards", swap it.
			for swapped := true; swapped; {
				swapped = false
				for j := 1; j < len(set)-1; j++ {
					if set[j] == '-' && set[j-1] > set[j+1] {
						set[j-1], set[j+1] = set[j+1], set[j-1]
						swapped = true
					}
				}
			}
			part.charSet = string(set)
		} else {
			// Remember any valid modifier if given.
			if p < len(str) && (str[p] == 'h' || str[p] == 'l' || str[p] == 'L') {
				part.modifier = str[p] | 0x20
				p++
			}
			if p >= len(str) {
				f.err = "missing scan conversion character"
				return
			}
			part.typ = str[p]
			switch {
			case !numIsScanConv(part.typ):
				f.err = "bad scan conversion character"
				return
			case part.typ == 'c' && part.width != 0:
				f.err = "field width may not be specified in %c conversion"
				return
			case part.typ == 'u' && part.modifier == 'l':
				f.err = "unsigned wide not supported"
				return
			}
		}
		p++
		f.parts = append(f.parts, part)
	}
}

func numIsScanConv(c byte) bool {
	switch c {
	case 'e', 'f', 'g', 'c', 's', 'n', 'd', 'o', 'x', 'u', 'i':
		return true
	}
	return false
}

// scanAString scans an unspecified string that ends at the next white space,
// or a string described by a character set (C: JimScanAString).
func (i *Interp) scanAString(part *scanFmtPart, str string) *Obj {
	n := 0
	for n < len(str) {
		if part.typ != '[' && u8.IsSpace(str[n]) {
			break // end of string via white space if unspecified
		}
		c, step := u8.ToUnicode([]byte(str[n:]))
		// The set is matched by the shared [string match] charset matcher in
		// its [scan] flavour (C: JimCharsetMatch with JIM_CHARSET_SCAN).
		set := []byte(part.charSet)
		if _, in := charsetMatch(set, 0, len(set), c, charsetScan); part.typ == '[' && !in {
			break
		}
		n += step
	}
	return i.NewStringObj(str[:n])
}

// scanOneEntry scans one entry out of str starting at pos. It returns the
// value scanned, if any, and the number of bytes consumed, or -1 if the input
// was already exhausted.
func (i *Interp) scanOneEntry(str string, pos int, f *scanFmt, idx int) (*Obj, int) {
	part := &f.parts[idx]
	anchor := pos

	if part.hasPrefix {
		// Skip the literal prefix and adjust the string to be parsed.
		n := 0
		for pos < len(str) && n < len(part.prefix) {
			if u8.IsSpace(part.prefix[n]) {
				// White space in the prefix skips white space in the input.
				for pos < len(str) && u8.IsSpace(str[pos]) {
					pos++
				}
			} else if part.prefix[n] != str[pos] {
				break // the prefix does not match here
			} else {
				pos++
			}
			n++
		}
		if pos >= len(str) {
			return nil, -1 // all of str consumed: EOF condition
		}
		if n < len(part.prefix) {
			return nil, 0 // not the whole prefix consumed, no conversion
		}
	}

	// For all but the following conversions, skip leading white space.
	if part.typ != 'c' && part.typ != '[' && part.typ != 'n' {
		for pos < len(str) && u8.IsSpace(str[pos]) {
			pos++
		}
	}
	scanned := pos - anchor

	switch {
	case part.typ == 'n':
		// A pseudo conversion: how much has been scanned so far?
		return i.NewIntObj(int64(anchor + scanned)), scanned
	case pos >= len(str):
		return nil, -1
	case part.typ == 'c':
		// %c is a special, simple case: no width.
		c, step := u8.ToUnicode([]byte(str[pos:]))
		return i.NewIntObj(int64(c)), scanned + step
	}

	tok := str[pos:]
	if part.width > 0 {
		// Do not scan as far as possible but only the given width.
		sLen := u8.Strlen([]byte(tok))
		tLen := int(part.width)
		if part.width > uint64(sLen) {
			tLen = sLen
		}
		tok = tok[:numUtf8Index([]byte(tok), tLen)]
	}

	switch part.typ {
	case 'd', 'o', 'x', 'u', 'i':
		base := 10
		switch part.typ {
		case 'o':
			base = 8
		case 'x':
			base = 16
		case 'i':
			base = 0
		}
		var w int64
		var end int
		if base == 0 {
			w, end = numStrtoull(tok)
		} else {
			var u uint64
			u, end = cStrtoull(tok, base)
			w = int64(u)
		}
		if end != 0 {
			return i.NewIntObj(w), scanned + end
		}
		return nil, numScanNothing(tok)

	case 's', '[':
		value := i.scanAString(part, tok)
		return value, scanned + value.Len()

	case 'e', 'f', 'g':
		d, end := numStrtod(tok)
		if end != 0 {
			return i.NewDoubleObj(d), scanned + end
		}
		return nil, numScanNothing(tok)
	}
	return nil, scanned
}

// numScanNothing distinguishes a conversion that failed (for instance on a
// prefix mismatch) from one that found the input exhausted.
func numScanNothing(tok string) int {
	if len(tok) > 0 {
		return 0
	}
	return -1
}

// scanValues scans strObj according to the parsed format f and returns all
// converted (and not ignored) values (C: Jim_ScanString, which wraps them in a
// list). The second result says whether the input was exhausted before
// anything could be scanned, or whether the format turned out to be unusable.
func (i *Interp) scanValues(strObj *Obj, f *scanFmt) ([]*Obj, int) {
	str := strObj.String()
	// The values of XPG3 conversions go into fixed slots, which start out
	// holding a shared empty string so that a slot assigned twice can be
	// recognised. Values of "natural" conversions are appended after them.
	empty := i.NewStringObj("")
	values := make([]*Obj, f.maxPos)
	for n := range values {
		values[n] = empty
	}

	scanned, pos := 1, 0
	for n := range f.parts {
		part := &f.parts[n]
		// Only the last part may be "literal" without a conversion: skip it.
		if part.typ == 0 {
			continue
		}
		var value *Obj
		// As long as any conversion could be done, we will proceed.
		if scanned > 0 {
			value, scanned = i.scanOneEntry(str, pos, f, n)
		}
		// In case our first try results in EOF, we will leave.
		if scanned == -1 && n == 0 {
			return nil, scanEOF
		}
		pos += scanned

		// A nil value means no conversion took place, so take the empty string.
		if value == nil {
			value = i.NewStringObj("")
		}
		switch {
		case part.pos == -1: // a conversion that is not assigned
		case part.pos == 0:
			values = append(values, value)
		case values[part.pos-1] == empty:
			values[part.pos-1] = value
		default:
			// The slot was already used; the format parser rejects this, so
			// this should not be reachable.
			return nil, scanFailed
		}
	}
	return values, scanDone
}

// scanFmtOf returns the parsed scan format of an object, parsing it if needed.
func (i *Interp) scanFmtOf(o *Obj) *scanFmt {
	if f, ok := o.aux.(*scanFmt); ok && o.kind == kindFormat {
		return f
	}
	i.setScanFmtFromAny(o)
	return o.aux.(*scanFmt)
}

// scanCoreCommand implements [scan] (C: Jim_ScanCoreCommand).
func scanCoreCommand(i *Interp, argv []*Obj) int {
	f := i.scanFmtOf(argv[2])
	if f.err != "" {
		i.SetResultString(f.err)
		return ERR
	}
	if len(argv) > 3 {
		switch {
		case f.maxPos > len(argv)-3:
			i.SetResultString("\"%n$\" argument index out of range")
			return ERR
		case f.convCount > len(argv)-3:
			i.SetResultString("different numbers of variable names and field specifiers")
			return ERR
		case f.convCount < len(argv)-3:
			i.SetResultString("variable is not assigned by any conversion specifiers")
			return ERR
		}
	}

	values, status := i.scanValues(argv[1], f)
	if status == scanFailed {
		return ERR
	}
	if len(argv) > 3 {
		// With variable names given, [scan] assigns the non-empty values and
		// returns how many it assigned, or -1 at end of input.
		count := -1
		rc := OK
		if status == scanDone {
			count = 0
			for n, o := range values {
				// The C indexes argv by the value number, so a format with
				// more values than variables would read past argv.
				if n+3 >= len(argv) {
					break
				}
				if o.Len() > 0 {
					count++
					// A variable that cannot be set makes [scan] fail, but
					// the C keeps assigning the remaining values first.
					if i.SetVariable(argv[3+n], o) != OK {
						rc = ERR
					}
				}
			}
		}
		if rc == OK {
			i.SetResultInt(int64(count))
		}
		return rc
	}
	if status == scanEOF {
		i.SetResult(i.NewListObj(nil))
		return OK
	}
	i.SetResult(i.NewListObj(values))
	return OK
}
