//go:build ignore

/* Generates the UTF-8 case mapping tables of unicodemap.go.
 *
 * This is the Go form of parse-unidata.tcl from the C distribution and must
 * select exactly the same code points, so that unicodeUpper(), unicodeLower()
 * and unicodeTitle() behave like utf8_upper(), utf8_lower() and utf8_title().
 *
 * Run it from the package directory with:
 *
 *	go generate gen_unicode.go
 *
 * Copyright 2010 Steve Bennett <steveb@workware.net.au>
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

//go:generate go run gen_unicode.go

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
)

const unicodeDataFile = "../../UnicodeData.txt"
const outputFile = "unicodemap.go"

type pair struct{ code, altcode uint16 }

func main() {
	f, err := os.Open(unicodeDataFile)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	var upper, lower, title []pair
	hex := func(s string) uint16 {
		v, err := strconv.ParseUint(s, 16, 32)
		if err != nil {
			log.Fatalf("bad code point %q: %v", s, err)
		}
		return uint16(v)
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)
	for scanner.Scan() {
		fields := strings.Split(strings.TrimSpace(scanner.Text()), ";")
		if len(fields) < 15 {
			continue
		}
		code, class := fields[0], fields[2]
		mapUpper, mapLower, mapTitle := fields[12], fields[13], fields[14]

		// Combining marks are collected but not emitted by parse-unidata.tcl;
		// they are skipped before the code point range check, which is why a
		// mark above U+FFFF does not stop the scan.
		if strings.HasPrefix(class, "M") {
			continue
		}
		cp, err := strconv.ParseUint(code, 16, 32)
		if err != nil {
			continue
		}
		if cp <= 0x7f {
			continue
		}
		if cp > 0xffff {
			break
		}
		if !strings.HasPrefix(class, "L") {
			continue
		}
		if mapUpper != "" {
			upper = append(upper, pair{uint16(cp), hex(mapUpper)})
		}
		if mapLower != "" {
			lower = append(lower, pair{uint16(cp), hex(mapLower)})
		}
		// A title mapping equal to the upper mapping is redundant; one equal to
		// the code point itself is stored as 0, which utf8_title() reads as
		// "title case is the code point itself, do not fall back to upper".
		if mapTitle != "" && mapTitle != mapUpper {
			alt := uint16(0)
			if mapTitle != code {
				alt = hex(mapTitle)
			}
			title = append(title, pair{uint16(cp), alt})
		}
	}
	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}

	var b strings.Builder
	// The licence header of the generated file, which cannot be one raw string
	// because the notice itself contains backticks.
	b.WriteString(`/* Jim Tcl 0.84 ported from C to Go: UTF-8 case mapping tables.
 *
 * Copyright 2010 Steve Bennett <steveb@workware.net.au>
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
 * THIS SOFTWARE IS PROVIDED BY THE JIM TCL PROJECT ` + "``" + `AS IS'' AND ANY
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

// Code generated by gen_unicode.go from UnicodeData.txt. DO NOT EDIT.

package u8

`)
	for _, t := range []struct {
		name  string
		pairs []pair
	}{{"unicodeCaseMappingUpper", upper}, {"unicodeCaseMappingLower", lower}, {"unicodeCaseMappingTitle", title}} {
		fmt.Fprintf(&b, "var %s = []Casemap{\n", t.name)
		for n := 0; n < len(t.pairs); n += 4 {
			var entries []string
			for _, p := range t.pairs[n:min(n+4, len(t.pairs))] {
				entries = append(entries, fmt.Sprintf("{0x%04x, 0x%04x},", p.code, p.altcode))
			}
			fmt.Fprintf(&b, "\t%s\n", strings.Join(entries, " "))
		}
		b.WriteString("}\n\n")
	}

	out := strings.TrimRight(b.String(), "\n") + "\n"
	if err := os.WriteFile(outputFile, []byte(out), 0o644); err != nil {
		log.Fatal(err)
	}
}
