/* Jim Tcl 0.84 ported from C to Go: the [clock] ensemble (C: jim-clock.c).
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
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// clockOptions holds the ?-format string? ?-gmt boolean? options
// (C: struct clock_options).
type clockOptions struct {
	gmt    bool
	format string
}

// clockBufLen is the size of the buffer [clock format] formats into. strftime
// returns 0 - which the C reports as an error - when the result does not fit.
const clockBufLen = 100

// clockParseOptions parses the ?-format string? ?-gmt boolean? options; argc
// must be even (C: parse_clock_options).
func clockParseOptions(i *Interp, argv []*Obj, opts *clockOptions) int {
	options := []string{"-gmt", "-format"}
	const (
		optGmt = iota
		optFormat
	)

	for n := 0; n < len(argv); n += 2 {
		option, rc := i.GetEnum(argv[n], options, "", ErrMsg|EnumAbbrev)
		if rc != OK {
			return ERR
		}
		switch option {
		case optGmt:
			gmt, rc := i.GetBoolean(argv[n+1])
			if rc != OK {
				return ERR
			}
			opts.gmt = gmt
		case optFormat:
			opts.format = argv[n+1].String()
		}
	}
	return OK
}

// clockLocation is the zone [clock] works in: UTC for -gmt, otherwise the
// zone named by $TZ, which is what localtime() uses.
func clockLocation(gmt bool) *time.Location {
	if gmt {
		return time.UTC
	}
	return time.Local
}

// clockTimeRange is the range of time_t values glibc's gmtime() accepts; it
// returns NULL outside it, which the C reports as an invalid time.
const (
	clockMinTime = -67768040609740800
	clockMaxTime = 67768036191676799
)

func clockCmdFormat(i *Interp, argv []*Obj) int {
	options := clockOptions{format: "%a %b %d %H:%M:%S %Z %Y"}

	seconds, rc := i.GetWide(argv[0])
	if rc != OK {
		return ERR
	}
	if len(argv)%2 == 0 {
		return subCmdBadArgs
	}
	if clockParseOptions(i, argv[1:], &options) != OK {
		return ERR
	}

	if seconds < clockMinTime || seconds > clockMaxTime {
		i.SetResultString("format string too long or invalid time")
		return ERR
	}
	buf := clockStrftime(options.format, time.Unix(seconds, 0).In(clockLocation(options.gmt)), options.gmt)
	if len(buf) == 0 || len(buf) >= clockBufLen {
		i.SetResultString("format string too long or invalid time")
		return ERR
	}
	i.SetResultString(buf)
	return OK
}

func clockCmdScan(i *Interp, argv []*Obj) int {
	// No default format
	options := clockOptions{}

	if len(argv)%2 == 0 {
		return subCmdBadArgs
	}
	if clockParseOptions(i, argv[1:], &options) != OK {
		return ERR
	}
	if options.format == "" {
		return subCmdBadArgs
	}

	// The fields the format does not mention keep the value they have in the
	// current local time, which is what the C fills the struct tm with.
	tm := clockBrokenDown(time.Now().In(time.Local))

	if !clockStrptime(argv[0].String(), options.format, &tm) {
		i.SetResultString("Failed to parse time according to format")
		return ERR
	}

	if options.gmt {
		tm.isdst = 0
		i.SetResultInt(clockTimegm(&tm))
	} else {
		tm.isdst = -1
		i.SetResultInt(clockMktime(&tm))
	}
	return OK
}

func clockCmdSeconds(i *Interp, argv []*Obj) int {
	i.SetResultInt(clockTimeUsec(clockRealtime) / 1000000)
	return OK
}

func clockCmdClicks(i *Interp, argv []*Obj) int {
	i.SetResultInt(clockTimeUsec(clockMonotonicRaw))
	return OK
}

func clockCmdMicros(i *Interp, argv []*Obj) int {
	i.SetResultInt(clockTimeUsec(clockRealtime))
	return OK
}

func clockCmdMillis(i *Interp, argv []*Obj) int {
	i.SetResultInt(clockTimeUsec(clockRealtime) / 1000)
	return OK
}

// The POSIX clock identifiers [clock] reads.
const (
	clockRealtime     = 0
	clockMonotonicRaw = 4
)

// clockTimeUsec reads a clock in microseconds (C: Jim_GetTimeUsec), falling
// back to the wall clock if the clock identifier is not supported.
func clockTimeUsec(which uintptr) int64 {
	var ts syscall.Timespec
	_, _, errno := syscall.Syscall(syscall.SYS_CLOCK_GETTIME, which,
		uintptr(unsafe.Pointer(&ts)), 0)
	if errno != 0 {
		return time.Now().UnixMicro()
	}
	return int64(ts.Sec)*1000000 + int64(ts.Nsec)/1000
}

// clockCommandTable is the [clock] ensemble (C: clock_command_table).
var clockCommandTable = []SubCmd{
	{Cmd: "clicks", Fn: clockCmdClicks, MinArgs: 0, MaxArgs: 0},
	{Cmd: "format", Args: "seconds ?-format string? ?-gmt boolean?", Fn: clockCmdFormat, MinArgs: 1, MaxArgs: 5},
	{Cmd: "microseconds", Fn: clockCmdMicros, MinArgs: 0, MaxArgs: 0},
	{Cmd: "milliseconds", Fn: clockCmdMillis, MinArgs: 0, MaxArgs: 0},
	{Cmd: "scan", Args: "str -format format ?-gmt boolean?", Fn: clockCmdScan, MinArgs: 3, MaxArgs: 5},
	{Cmd: "seconds", Fn: clockCmdSeconds, MinArgs: 0, MaxArgs: 0},
}

// registerClockCommands registers [clock] (C: Jim_clockInit).
func registerClockCommands(i *Interp) {
	i.PackageProvide("clock", "1.0", ErrMsg)
	i.RegisterSubCmd("clock", clockCommandTable)
}

/* ---------------------------------------------------------------------------
 * The broken-down time, strftime and strptime
 * ------------------------------------------------------------------------ */

// clockTm is a broken-down time (C: struct tm). The field names and ranges
// follow the C structure because strftime and strptime work on them directly.
type clockTm struct {
	sec    int // 0-60
	min    int // 0-59
	hour   int // 0-23
	mday   int // 1-31
	mon    int // 0-11
	year   int // years since 1900
	wday   int // 0-6, Sunday is 0
	yday   int // 0-365
	isdst  int // >0 DST, 0 no DST, <0 unknown
	zone   string
	gmtoff int
}

func clockBrokenDown(t time.Time) clockTm {
	zone, offset := t.Zone()
	isdst := 0
	if t.IsDST() {
		isdst = 1
	}
	return clockTm{
		sec: t.Second(), min: t.Minute(), hour: t.Hour(),
		mday: t.Day(), mon: int(t.Month()) - 1, year: t.Year() - 1900,
		wday: int(t.Weekday()), yday: t.YearDay() - 1,
		isdst: isdst, zone: zone, gmtoff: offset,
	}
}

// clockTimegm converts a broken-down time to seconds since the epoch without
// consulting the timezone (C: jim_timegm).
func clockTimegm(tm *clockTm) int64 {
	m := tm.mon + 1
	y := 1900 + tm.year
	if m <= 2 {
		y--
	}
	era := y
	if y < 0 {
		era = y - 399
	}
	era /= 400
	yoe := y - era*400
	mp := m - 3
	if m <= 2 {
		mp = m + 9
	}
	doy := (153*mp+2)/5 + tm.mday - 1
	doe := yoe*365 + yoe/4 - yoe/100 + doy
	days := int64(era)*146097 + int64(doe) - 719468
	secs := int64(tm.hour)*3600 + int64(tm.min)*60 + int64(tm.sec)

	return days*24*60*60 + secs
}

// clockMktime converts a broken-down time in the local timezone to seconds
// since the epoch (C: mktime). tm.isdst selects standard or summer time where
// the wall clock time is ambiguous; a negative value lets the zone decide.
func clockMktime(tm *clockTm) int64 {
	t := time.Date(1900+tm.year, time.Month(tm.mon+1), tm.mday, tm.hour, tm.min, tm.sec, 0, time.Local)
	wantDST := tm.isdst > 0
	if tm.isdst < 0 || t.IsDST() == wantDST {
		return t.Unix()
	}
	// The requested setting is the other one, so look half a year either way
	// for an offset that has it and use that instead.
	for _, probe := range []time.Time{t.AddDate(0, -6, 0), t.AddDate(0, 6, 0)} {
		if probe.IsDST() == wantDST {
			_, offset := probe.Zone()
			return clockTimegm(tm) - int64(offset)
		}
	}
	return t.Unix()
}

// clockMonthNames and friends are the C locale's names, which is the locale
// jimsh runs in.
var (
	clockDayNames   = []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
	clockMonthNames = []string{"January", "February", "March", "April", "May", "June",
		"July", "August", "September", "October", "November", "December"}
)

// clockStrftime is strftime(3) in the C locale. gmt selects the "GMT" zone
// name that gmtime() reports, as opposed to the one localtime() reports.
func clockStrftime(format string, t time.Time, gmt bool) string {
	tm := clockBrokenDown(t)
	if gmt {
		tm.zone, tm.gmtoff, tm.isdst = "GMT", 0, 0
	}

	var b strings.Builder
	for n := 0; n < len(format); n++ {
		if format[n] != '%' {
			b.WriteByte(format[n])
			continue
		}
		spec, pad, width, k := clockParseConversion(format, n)
		if spec == 0 {
			// Not a conversion glibc knows: it copies the specification out
			// unchanged, but still honours the field width.
			text := format[n:k]
			for len(text) < width {
				text = " " + text
			}
			b.WriteString(text)
			n = k - 1
			continue
		}
		b.WriteString(clockConversion(spec, pad, width, &tm, t))
		n = k - 1
	}
	return b.String()
}

// clockParseConversion splits a conversion specification into its padding
// flag, field width and conversion character, and returns the index just past
// it. The conversion character is 0 when the specification is not one glibc
// recognises, in which case it is copied out literally.
func clockParseConversion(format string, n int) (spec byte, pad byte, width int, end int) {
	k := n + 1
	for k < len(format) && strings.IndexByte("-_0^#", format[k]) >= 0 {
		pad = format[k]
		k++
	}
	for k < len(format) && format[k] >= '0' && format[k] <= '9' {
		width = width*10 + int(format[k]-'0')
		k++
	}
	// The E and O modifiers select an alternative representation, which the C
	// locale does not have, so they are ignored.
	if k < len(format) && (format[k] == 'E' || format[k] == 'O') {
		k++
	}
	if k >= len(format) {
		// A specification cut short by the end of the format, "%" included:
		// glibc copies out what is there and stops.
		return 0, pad, width, k
	}
	if strings.IndexByte("aAbBcCdDeFgGhHIjklmMnpPrRsStTuUVwWxXyYzZ%", format[k]) < 0 {
		return 0, pad, width, k + 1
	}
	return format[k], pad, width, k + 1
}

// clockConversion renders one strftime conversion. The default padding of each
// numeric conversion is the one glibc uses.
func clockConversion(spec, pad byte, width int, tm *clockTm, t time.Time) string {
	num := func(digits int, padChar byte, v int64) string {
		// A '-' flag drops the default minimum width and pads with spaces, a
		// '_' pads with spaces and a '0' with zeroes.
		switch pad {
		case '-':
			digits, padChar = 0, ' '
		case '_':
			padChar = ' '
		case '0':
			padChar = '0'
		}
		if width > digits {
			digits = width
		}
		s := strconv.FormatInt(v, 10)
		neg := strings.HasPrefix(s, "-")
		if neg {
			s = s[1:]
		}
		for len(s) < digits {
			s = string(padChar) + s
		}
		if neg {
			s = "-" + s
		}
		return s
	}
	text := func(s string) string {
		switch pad {
		case '^':
			s = strings.ToUpper(s)
		case '#':
			// '#' asks for the opposite case of the usual representation
			s = strings.ToLower(s)
		}
		for len(s) < width {
			s = " " + s
		}
		return s
	}

	switch spec {
	case 'a':
		return text(clockDayNames[tm.wday][:3])
	case 'A':
		return text(clockDayNames[tm.wday])
	case 'b', 'h':
		return text(clockMonthNames[tm.mon][:3])
	case 'B':
		return text(clockMonthNames[tm.mon])
	case 'c':
		return clockConversions("%a %b %e %H:%M:%S %Y", tm, t)
	case 'C':
		return num(1, '0', int64(clockDiv(1900+tm.year, 100)))
	case 'd':
		return num(2, '0', int64(tm.mday))
	case 'D':
		return clockConversions("%m/%d/%y", tm, t)
	case 'e':
		return num(2, ' ', int64(tm.mday))
	case 'F':
		return clockConversions("%Y-%m-%d", tm, t)
	case 'g':
		isoYear, _ := t.ISOWeek()
		return num(2, '0', int64(clockMod(isoYear, 100)))
	case 'G':
		isoYear, _ := t.ISOWeek()
		return num(1, '0', int64(isoYear))
	case 'H':
		return num(2, '0', int64(tm.hour))
	case 'I':
		return num(2, '0', int64(clockHour12(tm.hour)))
	case 'j':
		return num(3, '0', int64(tm.yday+1))
	case 'k':
		return num(2, ' ', int64(tm.hour))
	case 'l':
		return num(2, ' ', int64(clockHour12(tm.hour)))
	case 'm':
		return num(2, '0', int64(tm.mon+1))
	case 'M':
		return num(2, '0', int64(tm.min))
	case 'n':
		return "\n"
	case 'p':
		if tm.hour < 12 {
			return text("AM")
		}
		return text("PM")
	case 'P':
		if tm.hour < 12 {
			return "am"
		}
		return "pm"
	case 'r':
		return clockConversions("%I:%M:%S %p", tm, t)
	case 'R':
		return clockConversions("%H:%M", tm, t)
	case 's':
		// glibc interprets the broken-down time in the local zone here, even
		// when it came from gmtime().
		return num(1, '0', clockMktime(tm))
	case 'S':
		return num(2, '0', int64(tm.sec))
	case 't':
		return "\t"
	case 'T':
		return clockConversions("%H:%M:%S", tm, t)
	case 'u':
		if tm.wday == 0 {
			return num(1, '0', 7)
		}
		return num(1, '0', int64(tm.wday))
	case 'U':
		return num(2, '0', int64((tm.yday+7-tm.wday)/7))
	case 'V':
		_, week := t.ISOWeek()
		return num(2, '0', int64(week))
	case 'w':
		return num(1, '0', int64(tm.wday))
	case 'W':
		return num(2, '0', int64((tm.yday+7-(tm.wday+6)%7)/7))
	case 'x':
		return clockConversions("%m/%d/%y", tm, t)
	case 'X':
		return clockConversions("%H:%M:%S", tm, t)
	case 'y':
		return num(2, '0', int64(clockMod(1900+tm.year, 100)))
	case 'Y':
		return num(1, '0', int64(1900+tm.year))
	case 'z':
		sign, off := "+", tm.gmtoff
		if off < 0 {
			sign, off = "-", -off
		}
		return sign + num(2, '0', int64(off/3600)) + num(2, '0', int64(off%3600/60))
	case 'Z':
		return text(tm.zone)
	case '%':
		return "%"
	}
	return ""
}

// clockConversions expands the fixed sub-formats of %c, %D, %F, %r, %R, %T,
// %x and %X, which carry no padding flag of their own.
func clockConversions(format string, tm *clockTm, t time.Time) string {
	var b strings.Builder
	for n := 0; n < len(format); n++ {
		if format[n] != '%' {
			b.WriteByte(format[n])
			continue
		}
		spec, pad, width, k := clockParseConversion(format, n)
		if spec == 0 {
			b.WriteString(format[n:k])
			n = k - 1
			continue
		}
		b.WriteString(clockConversion(spec, pad, width, tm, t))
		n = k - 1
	}
	return b.String()
}

func clockHour12(hour int) int {
	h := hour % 12
	if h == 0 {
		h = 12
	}
	return h
}

// clockDiv and clockMod round towards minus infinity, as the year arithmetic
// in glibc's strftime does.
func clockDiv(a, b int) int {
	if a < 0 {
		return -((-a + b - 1) / b)
	}
	return a / b
}

func clockMod(a, b int) int {
	m := a % b
	if m < 0 {
		m += b
	}
	return m
}

/* ---------------------------------------------------------------------------
 * strptime
 * ------------------------------------------------------------------------ */

// clockStrptime is strptime(3) in the C locale: it fills in the fields of tm
// that the format mentions and reports whether the whole of s was consumed,
// which is what the C requires (pt != 0 && *pt == 0).
func clockStrptime(s, format string, tm *clockTm) bool {
	p := &clockParser{s: s}
	for n := 0; n < len(format); n++ {
		c := format[n]
		if c == '%' {
			n++
			// The E and O modifiers select an alternative representation,
			// which the C locale does not have.
			for n < len(format) && (format[n] == 'E' || format[n] == 'O') {
				n++
			}
			if n >= len(format) || !p.conversion(format[n], tm) {
				return false
			}
			continue
		}
		if clockIsSpace(c) {
			// Whitespace in the format matches any amount of whitespace
			p.skipSpace()
			continue
		}
		if !p.literal(c) {
			return false
		}
	}
	p.deriveDate(tm)
	return p.atEnd()
}

// deriveDate completes the date the way the tail of glibc's strptime does:
// the month and day of the month can come from the day of the year, and the
// day of the week and the day of the year are recomputed from the rest.
func (p *clockParser) deriveDate(tm *clockTm) {
	if !p.wantXday {
		return
	}
	if !p.haveWday {
		if !(p.haveMon && p.haveMday) && p.haveYday {
			mon := 0
			for clockMonthYday(1900+tm.year, mon) <= tm.yday {
				mon++
			}
			if !p.haveMon {
				tm.mon = mon - 1
			}
			if !p.haveMday {
				tm.mday = tm.yday - clockMonthYday(1900+tm.year, mon-1) + 1
			}
			p.haveMon, p.haveMday = true, true
		}
		if p.haveMon || uint(tm.mon) <= 11 {
			tm.wday = clockDayOfWeek(tm)
		}
	}
	if !p.haveYday && (p.haveMon || uint(tm.mon) <= 11) {
		tm.yday = clockMonthYday(1900+tm.year, tm.mon) + tm.mday - 1
	}
}

// clockMonthYday is the number of days in the year before month mon (0 based)
// (C: __mon_yday).
func clockMonthYday(year, mon int) int {
	days := []int{0, 31, 59, 90, 120, 151, 181, 212, 243, 273, 304, 334, 365}
	if mon < 0 {
		return 0
	}
	if mon > 12 {
		mon = 12
	}
	d := days[mon]
	if mon >= 2 && clockIsLeap(year) {
		d++
	}
	return d
}

func clockIsLeap(year int) bool {
	return year%4 == 0 && (year%100 != 0 || year%400 == 0)
}

func clockDayOfWeek(tm *clockTm) int {
	t := time.Date(1900+tm.year, time.Month(tm.mon+1), tm.mday, 0, 0, 0, 0, time.UTC)
	return int(t.Weekday())
}

func clockIsSpace(c byte) bool { return strings.IndexByte(" \t\n\r\f\v", c) >= 0 }

type clockParser struct {
	s   string
	pos int
	// The flags glibc's strptime tracks to decide what it can derive.
	haveMon, haveMday, haveYday, haveWday, wantXday bool
}

func (p *clockParser) atEnd() bool { return p.pos == len(p.s) }

func (p *clockParser) skipSpace() {
	for p.pos < len(p.s) && clockIsSpace(p.s[p.pos]) {
		p.pos++
	}
}

func (p *clockParser) literal(c byte) bool {
	if p.pos < len(p.s) && p.s[p.pos] == c {
		p.pos++
		return true
	}
	return false
}

// number reads up to n digits, as strptime's get_number() does: leading
// spaces are skipped and the value must lie between from and to.
func (p *clockParser) number(from, to, n int) (int, bool) {
	for p.pos < len(p.s) && p.s[p.pos] == ' ' {
		p.pos++
	}
	if p.pos >= len(p.s) || p.s[p.pos] < '0' || p.s[p.pos] > '9' {
		return 0, false
	}
	val := 0
	for n > 0 && p.pos < len(p.s) && p.s[p.pos] >= '0' && p.s[p.pos] <= '9' {
		if val != 0 && val*10 > to {
			break
		}
		val = val*10 + int(p.s[p.pos]-'0')
		p.pos++
		n--
	}
	if val < from || val > to {
		return 0, false
	}
	return val, true
}

// epochSeconds reads the digits of %s. glibc reads them one by one rather
// than through get_number, so there is no sign and no upper limit.
func (p *clockParser) epochSeconds() (int64, bool) {
	start := p.pos
	for p.pos < len(p.s) && p.s[p.pos] >= '0' && p.s[p.pos] <= '9' {
		p.pos++
	}
	if p.pos == start {
		return 0, false
	}
	var secs int64
	for _, c := range []byte(p.s[start:p.pos]) {
		secs = secs*10 + int64(c-'0')
	}
	return secs, true
}

// matchName matches one of the given names, the full name first and then its
// first three characters, case insensitively.
func (p *clockParser) matchName(names []string) (int, bool) {
	rest := p.s[p.pos:]
	for n, name := range names {
		if len(rest) >= len(name) && strings.EqualFold(rest[:len(name)], name) {
			p.pos += len(name)
			return n, true
		}
	}
	for n, name := range names {
		if len(rest) >= 3 && strings.EqualFold(rest[:3], name[:3]) {
			p.pos += 3
			return n, true
		}
	}
	return 0, false
}

func (p *clockParser) conversion(spec byte, tm *clockTm) bool {
	switch spec {
	case '%':
		return p.literal('%')
	case 'n', 't':
		p.skipSpace()
		return true
	case 'a', 'A':
		n, ok := p.matchName(clockDayNames)
		if ok {
			tm.wday = n
			p.haveWday = true
		}
		return ok
	case 'b', 'B', 'h':
		n, ok := p.matchName(clockMonthNames)
		if ok {
			tm.mon = n
			p.haveMon = true
			p.wantXday = true
		}
		return ok
	case 'c':
		return p.format("%a %b %e %H:%M:%S %Y", tm)
	case 'C':
		v, ok := p.number(0, 99, 2)
		if ok {
			tm.year = v*100 - 1900 + clockMod(1900+tm.year, 100)
			p.wantXday = true
		}
		return ok
	case 'd', 'e':
		v, ok := p.number(1, 31, 2)
		if ok {
			tm.mday = v
			p.haveMday = true
			p.wantXday = true
		}
		return ok
	case 'D':
		return p.format("%m/%d/%y", tm)
	case 'F':
		return p.format("%Y-%m-%d", tm)
	case 'H', 'k':
		v, ok := p.number(0, 23, 2)
		if ok {
			tm.hour = v
		}
		return ok
	case 'I', 'l':
		v, ok := p.number(1, 12, 2)
		if ok {
			tm.hour = v % 12
		}
		return ok
	case 'j':
		v, ok := p.number(1, 366, 3)
		if ok {
			tm.yday = v - 1
			p.haveYday = true
			p.wantXday = true
		}
		return ok
	case 'm':
		v, ok := p.number(1, 12, 2)
		if ok {
			tm.mon = v - 1
			p.haveMon = true
			p.wantXday = true
		}
		return ok
	case 'M':
		v, ok := p.number(0, 59, 2)
		if ok {
			tm.min = v
		}
		return ok
	case 'p':
		n, ok := p.matchName([]string{"AM", "PM"})
		if ok && n == 1 {
			tm.hour = tm.hour%12 + 12
		} else if ok {
			tm.hour = tm.hour % 12
		}
		return ok
	case 'r':
		return p.format("%I:%M:%S %p", tm)
	case 'R':
		return p.format("%H:%M", tm)
	case 's':
		v, ok := p.epochSeconds()
		if ok {
			*tm = clockBrokenDown(time.Unix(v, 0).In(time.Local))
		}
		return ok
	case 'S':
		v, ok := p.number(0, 61, 2)
		if ok {
			tm.sec = v
		}
		return ok
	case 'T':
		return p.format("%H:%M:%S", tm)
	case 'u':
		v, ok := p.number(1, 7, 1)
		if ok {
			tm.wday = v % 7
			p.haveWday = true
		}
		return ok
	case 'U', 'V', 'W':
		_, ok := p.number(0, 53, 2)
		return ok
	case 'w':
		v, ok := p.number(0, 6, 1)
		if ok {
			tm.wday = v
			p.haveWday = true
		}
		return ok
	case 'x':
		return p.format("%m/%d/%y", tm)
	case 'X':
		return p.format("%H:%M:%S", tm)
	case 'y':
		v, ok := p.number(0, 99, 2)
		if ok {
			// The two digit year is 1969-2068, as in glibc
			if v < 69 {
				v += 100
			}
			tm.year = v
			p.wantXday = true
		}
		return ok
	case 'Y':
		v, ok := p.number(0, 9999, 4)
		if ok {
			tm.year = v - 1900
			p.wantXday = true
		}
		return ok
	case 'Z':
		// Accept and ignore a timezone name
		for p.pos < len(p.s) && (p.s[p.pos] >= 'A' && p.s[p.pos] <= 'Z' || p.s[p.pos] >= 'a' && p.s[p.pos] <= 'z') {
			p.pos++
		}
		return true
	case 'z':
		return p.timezoneOffset(tm)
	}
	return false
}

// format applies a sub-format, for the compound conversions.
func (p *clockParser) format(format string, tm *clockTm) bool {
	for n := 0; n < len(format); n++ {
		if format[n] == '%' {
			n++
			if !p.conversion(format[n], tm) {
				return false
			}
			continue
		}
		if format[n] == ' ' {
			p.skipSpace()
			continue
		}
		if !p.literal(format[n]) {
			return false
		}
	}
	return true
}

// timezoneOffset parses an RFC 822 style offset such as "+1000" or "Z".
func (p *clockParser) timezoneOffset(tm *clockTm) bool {
	if p.pos < len(p.s) && (p.s[p.pos] == 'Z' || p.s[p.pos] == 'z') {
		p.pos++
		tm.gmtoff = 0
		return true
	}
	if p.pos >= len(p.s) || (p.s[p.pos] != '+' && p.s[p.pos] != '-') {
		return false
	}
	neg := p.s[p.pos] == '-'
	p.pos++
	hours, ok := p.number(0, 99, 2)
	if !ok {
		return false
	}
	p.literal(':')
	mins := 0
	if p.pos < len(p.s) && p.s[p.pos] >= '0' && p.s[p.pos] <= '9' {
		if mins, ok = p.number(0, 59, 2); !ok {
			return false
		}
	}
	tm.gmtoff = hours*3600 + mins*60
	if neg {
		tm.gmtoff = -tm.gmtoff
	}
	return true
}
