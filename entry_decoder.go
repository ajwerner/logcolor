// Copyright 2013 Google Inc. All Rights Reserved.
// Copyright 2017 The Cockroach Authors.
// Copyright 2018 Andrew Werner, All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// This code is based on code in github.com/cockroachdb/cockroach which
// is based on code which  originated in the github.com/golang/glog package.

package main

import (
	"bufio"
	"io"
	"regexp"
)

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
