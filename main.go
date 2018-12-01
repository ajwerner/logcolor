package main

import (
	"bufio"
	"crypto/md5"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"runtime"
	"runtime/pprof"
	"text/template"

	"github.com/lucasb-eyer/go-colorful"
	"github.com/wayneashleyberry/truecolor/pkg/color"
)

type colorMap map[string]*color.Message

func (m *colorMap) getColor(s string) *color.Message {
	if col, ok := (*m)[s]; ok {
		return col
	}
	sum := md5.Sum([]byte(s))
	f1 := float64(binary.BigEndian.Uint64(sum[8:])) / math.MaxUint64
	f2 := float64(binary.BigEndian.Uint64(sum[:8])) / math.MaxUint64
	f3 := float64(binary.LittleEndian.Uint64(sum[4:])) / math.MaxUint64
	h := 360 * f1
	c := .2 + .3*f2
	l := .6 + .3*f3
	col := color.Color(colorful.Hcl(h, c, l).Clamped().RGB255())
	(*m)[s] = col
	return col
}

func main() {
	// we want to get a pattern for the log header
	// we want to get a template for the replacement
	headerPattern := flag.String("log-header-pattern", `(?m)^(?P<prefix>^[\w_\-.]+> )(?P<header>([IWEF])(\d{6} \d{2}:\d{2}:\d{2}.\d{6}) (?:(\d+) )?([^:]+):(\d+))`, "Capture group for log header")
	outTemplate := flag.String("output-template",
		`{{ with $p := .Match "prefix" }}{{ with $c := color $p }}{{ $.Match "header" | printf "%s%s" $p | $c.Sprint  }}{{ end }}{{ end }}{{.Message}}`, "Golang text template for outputting the body., object will be "+
			`
type Entry struct {
    Pattern *regexp.Regexp
    Match   [][]string
    Header  string
    Message string
}`)
	runtime.Gosched()
	f, _ := os.Create("profile")
	pprof.StartCPUProfile(f)
	defer f.Close()
	defer pprof.StopCPUProfile()
	flag.Parse()
	pattern, err := regexp.Compile(*headerPattern)
	dieIf(err)
	// so we want to parse the template
	cm := colorMap{}
	tmpl, err := template.New("logs").Funcs(template.FuncMap{
		"color": cm.getColor,
	}).Parse(*outTemplate)

	dieIf(err)
	// then we want to open the out file,
	r := os.Stdin
	d := NewEntryDecoder(pattern, r)
	le := LogEntry{
		Pattern:     pattern,
		subexpNames: map[string]int{},
	}
	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()
	for {
		switch err := d.Decode(&le.Entry); err {
		case nil:
			err := tmpl.Execute(os.Stdout, &le)
			dieIf(err)
		case io.EOF:
			return
		default:
			dieIf(err)
		}
	}
}

func (le *LogEntry) Match(capture string) (string, error) {
	idx, ok := le.findSubexp(capture)
	if !ok {
		return "", fmt.Errorf("no capture group %v does not exist", capture)
	}

	return le.Header[le.matches[2*idx]:le.matches[(2*idx)+1]], nil
}

func dieIf(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func (le *LogEntry) findSubexp(capture string) (int, bool) {
	if idx, ok := le.subexpNames[capture]; ok {
		return idx, ok
	}
	for i, n := range le.Pattern.SubexpNames() {
		if n == capture {
			le.subexpNames[n] = i
			return i, true
		}
	}
	return -1, false
}

type LogEntry struct {
	Entry
	subexpNames map[string]int

	Pattern *regexp.Regexp
}

type Entry struct {
	Header  string
	Message string
	matches []int
}

type EntryDecoder struct {
	re                 *regexp.Regexp
	scanner            *bufio.Scanner
	truncatedLastEntry bool
}

func NewEntryDecoder(re *regexp.Regexp, r io.Reader) *EntryDecoder {
	d := &EntryDecoder{re: re, scanner: bufio.NewScanner(r)}
	d.scanner.Split(d.split)
	return d
}

func (d *EntryDecoder) Decode(e *Entry) error {
	for {
		if !d.scanner.Scan() {
			if err := d.scanner.Err(); err != nil {
				return err
			}
			return io.EOF
		}
		b := d.scanner.Bytes()
		m := d.re.FindSubmatchIndex(b)
		if m == nil {
			continue
		}
		e.Header = string(b[m[0]:m[1]])
		e.Message = string(b[m[1]:])
		e.matches = m

		return nil
	}
}

func (d *EntryDecoder) split(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if d.truncatedLastEntry {
		i := d.re.FindIndex(data)
		if i == nil {
			// If there's no entry that starts in this chunk, advance past it, since
			// we've truncated the entry it was originally part of.
			return len(data), nil, nil
		}
		d.truncatedLastEntry = false
		if i[0] > 0 {
			// If an entry starts anywhere other than the first index, advance to it
			// to maintain the invariant that entries start at the beginning of data.
			// This isn't necessary, but simplifies the code below.
			return i[0], nil, nil
		}
		// If i[0] == 0, then a new entry starts at the beginning of data, so fall
		// through to the normal logic.
	}
	// From this point on, we assume we're currently positioned at a log entry.
	onNoMatch := func() (int, []byte, error) {
		if atEOF {
			return len(data), data, nil
		}
		if len(data) >= bufio.MaxScanTokenSize {
			// If there's no room left in the buffer, return the current truncated
			// entry.
			d.truncatedLastEntry = true
			return len(data), data, nil
		}
		// If there is still room to read more, ask for more before deciding whether
		// to truncate the entry.
		return 0, nil, nil
	}
	i := d.re.FindIndex(data)
	if i == nil {
		return onNoMatch()
	}
	j := d.re.FindIndex(data[i[1]:])
	if j == nil {
		return onNoMatch()
	}
	// i[1]+j[0] is the start of the next log entry, but we need to adjust the value
	return i[1] + j[0], data[:i[1]+j[0]], nil
}
