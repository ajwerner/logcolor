// Copyright 2018 Andrew Werner, All Rights Reserved.

// Command logcolor is used to rewrite log messages with a stable terminal color
// palette, specifically for colorizing merged cockroachdb logs.
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
	"text/template"

	"github.com/lucasb-eyer/go-colorful"
	"github.com/wayneashleyberry/truecolor/pkg/color"
)

//go:generate go doc '"github.com/ajwerner/logcolor".LogEntry

func main() {
	headerPattern := flag.String("log-header-pattern", `(?m)^(?P<prefix>^[\w_\-.]+> )(?P<header>([IWEF])(\d{6} \d{2}:\d{2}:\d{2}.\d{6}) (?:(\d+) )?([^:]+):(\d+))`, "Capture group for log header")
	outTemplate := flag.String("output-template", `
{{- with $p := .Match "prefix" -}}
{{- with $c := color $p -}}
{{ $.Match "header" | printf "%s%s" $p | $c.Sprint  }}
{{- end -}}
{{- end -}}
{{- .Message -}}`,
		"Golang text template for outputting the body.")
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

func dieIf(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// LogEntry is the root element passed to the output template
type LogEntry struct {
	Entry
	// Pattern is the Regexp which captured the header.
	Pattern *regexp.Regexp

	subexpNames map[string]int
}

func (le *LogEntry) Match(capture string) (string, error) {
	idx, ok := le.findSubexp(capture)
	if !ok {
		return "", fmt.Errorf("no capture group %v does not exist", capture)
	}

	return le.Header[le.matches[2*idx]:le.matches[(2*idx)+1]], nil
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
	c := .33 + .2*f2
	l := .6 + .30*f3
	col := color.Color(colorful.Hcl(h, c, l).Clamped().RGB255())
	(*m)[s] = col
	return col
}
