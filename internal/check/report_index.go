// Copyright 2026 PolitePixels Limited
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

// This project stands against fascism, authoritarianism, and all forms of
// oppression. We built this to empower people, not to enable those who would
// strip others of their rights and dignity.

package check

import (
	"strconv"
	"strings"

	"seagull.sh/seagull/internal/diag"
)

// reportIndex indexes the diagnostics a query had before a pass ran, so the pass asks in
// constant time whether a problem was reported already.
type reportIndex struct {
	// messages holds each diagnostic's code and message.
	messages map[reportKey]struct{}

	// quoted holds each diagnostic's code with every name its message quotes.
	quoted map[reportKey]struct{}
}

// newReportIndex indexes the codes, messages and quoted names of a query's diagnostics.
//
// Takes diagnostics ([]diag.SourceError) which are the diagnostics.
//
// Returns reportIndex which answers whether a problem was reported already.
func newReportIndex(diagnostics []diag.SourceError) reportIndex {
	index := reportIndex{
		messages: make(map[reportKey]struct{}, len(diagnostics)),
		quoted:   make(map[reportKey]struct{}, len(diagnostics)),
	}
	for position := range diagnostics {
		diagnostic := &diagnostics[position]
		index.messages[reportKey{code: diagnostic.Code, text: diagnostic.Message}] = struct{}{}
		for _, name := range quotedNames(diagnostic.Message) {
			index.quoted[reportKey{code: diagnostic.Code, text: name}] = struct{}{}
		}
	}
	return index
}

// has reports whether a diagnostic with a code and a message was reported.
//
// Takes code (string) which is the diagnostic code.
// Takes message (string) which is the message.
//
// Returns bool which is true when such a diagnostic exists.
func (index reportIndex) has(code, message string) bool {
	_, found := index.messages[reportKey{code: code, text: message}]
	return found
}

// quotes reports whether a diagnostic of a code quotes a name, as a report of an unknown
// column quotes the column.
//
// Takes code (string) which is the diagnostic code.
// Takes name (string) which is the name, unquoted.
//
// Returns bool which is true when such a diagnostic exists.
func (index reportIndex) quotes(code, name string) bool {
	_, found := index.quoted[reportKey{code: code, text: strconv.Quote(name)}]
	return found
}

// reportKey is a diagnostic code with a message, or with a name a message quotes.
type reportKey struct {
	// code is the diagnostic code.
	code string

	// text is the message, or the quoted name with its quotes.
	text string
}

// quotedNames returns every Go-quoted string a message holds, quotes included, as %q
// writes a name into a message.
//
// Takes message (string) which is the message.
//
// Returns []string which holds the quoted strings in order.
func quotedNames(message string) []string {
	var names []string
	for offset := 0; offset < len(message); {
		next := strings.IndexByte(message[offset:], '"')
		if next == -1 {
			break
		}
		offset += next
		quoted, err := strconv.QuotedPrefix(message[offset:])
		if err != nil {
			offset++
			continue
		}
		names = append(names, quoted)
		offset += len(quoted)
	}
	return names
}
