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

package goast

import (
	"bytes"
	"fmt"
	"go/format"
	"strings"
)

const (
	// typeKeyword opens a type declaration in formatted source.
	typeKeyword = "type "

	// structOpening ends the first line of a struct type declaration in formatted source.
	structOpening = " struct {"

	// structClosing is the line closing a top-level struct type in formatted source.
	structClosing = "}"

	// fieldIndent starts a field of a top-level struct in formatted source.
	fieldIndent = "\t"
)

// DocumentFields puts doc comments above fields of the struct types in formatted Go
// source, then formats it again so the fields keep gofmt's alignment. go/printer places a
// field's doc comment from its position, which the generated AST does not carry, so the
// comments are written into the printed source instead.
//
// Takes source ([]byte) which is formatted Go source.
// Takes docs (map[string]map[string][]string) which maps a struct type's name to its
// fields' names and each field's doc comment lines, each with its marker.
//
// Returns []byte which is the source with the comments, or the source as it is when docs
// holds none.
// Returns error when the commented source does not format.
func DocumentFields(source []byte, docs map[string]map[string][]string) ([]byte, error) {
	if len(docs) == 0 {
		return source, nil
	}
	var commented bytes.Buffer
	commented.Grow(len(source))
	first := true
	var fields map[string][]string
	inside := false
	for line := range strings.SplitSeq(string(source), "\n") {
		if !first {
			_ = commented.WriteByte('\n')
		}
		first = false
		switch {
		case !inside:
			if name, opens := openedStruct(line); opens {
				inside, fields = true, docs[name]
			}
		case line == structClosing:
			inside, fields = false, nil
		case strings.HasPrefix(line, fieldIndent) && !strings.HasPrefix(line, fieldIndent+fieldIndent):
			for _, comment := range fields[firstWord(line)] {
				_, _ = commented.WriteString(fieldIndent)
				_, _ = commented.WriteString(comment)
				_ = commented.WriteByte('\n')
			}
		}
		_, _ = commented.WriteString(line)
	}
	formatted, err := format.Source(commented.Bytes())
	if err != nil {
		return nil, fmt.Errorf("formatting documented fields: %w", err)
	}
	return formatted, nil
}

// openedStruct reports whether a line of formatted source opens a top-level struct type.
//
// Takes line (string) which is the line.
//
// Returns string which is the struct type's name.
// Returns bool which is true when the line opens one.
func openedStruct(line string) (string, bool) {
	if !strings.HasPrefix(line, typeKeyword) || !strings.HasSuffix(line, structOpening) {
		return "", false
	}
	return strings.TrimSuffix(strings.TrimPrefix(line, typeKeyword), structOpening), true
}

// firstWord returns the first word of a line, such as a field's name.
//
// Takes line (string) which is the line.
//
// Returns string which is the word, or empty for a blank line.
func firstWord(line string) string {
	for word := range strings.FieldsSeq(line) {
		return word
	}
	return ""
}
