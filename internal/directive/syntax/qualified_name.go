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

package syntax

import "strings"

const (
	// maxQualifiedNameParts is the most dot-separated parts SplitQualifiedName reads: a
	// column qualified by its table and schema.
	maxQualifiedNameParts = 3

	// identifierQuote opens and closes a quoted part of a qualified name.
	identifierQuote = '"'
)

// NamePart is one dot-separated part of a qualified name as a directive writes it.
type NamePart struct {
	// Name is the part without its quotes, a doubled quote inside a quoted part read as one.
	Name string

	// Written is the part as the directive writes it, quotes included.
	Written string

	// Quoted reports whether the part is written in double quotes, so its letter case is
	// kept as written rather than folded as the dialect folds an unquoted identifier.
	Quoted bool
}

// SplitQualifiedName splits a dotted name, such as events."UserID" or
// public.events.user_id, into its parts. A part written in double quotes may hold dots
// and a doubled quote, and keeps its letter case; an unquoted part is read as written.
//
// Takes name (string) which is the name as the directive writes it.
//
// Returns []NamePart which holds the parts in order.
// Returns bool which is false when a part is empty, a quote is left open, a quoted part
// is followed by anything but a dot, or the name has more than maxQualifiedNameParts
// parts.
func SplitQualifiedName(name string) ([]NamePart, bool) {
	var parts []NamePart
	rest := name
	for {
		if len(parts) == maxQualifiedNameParts {
			return nil, false
		}
		part, remainder, ok := readNamePart(rest)
		if !ok {
			return nil, false
		}
		parts = append(parts, part)
		if remainder == "" {
			return parts, true
		}
		rest = remainder[1:]
	}
}

// readNamePart reads the first part of a dotted name.
//
// Takes text (string) which opens with the part.
//
// Returns NamePart which is the part.
// Returns string which is the text after the part: empty, or opening with the dot before
// the next part.
// Returns bool which is false when the part is empty, its quote is left open, or a quoted
// part is followed by anything but a dot.
func readNamePart(text string) (NamePart, string, bool) {
	if text == "" || text[0] != identifierQuote {
		end := strings.IndexByte(text, '.')
		if end < 0 {
			end = len(text)
		}
		written := text[:end]
		return NamePart{Name: written, Written: written, Quoted: false}, text[end:], written != ""
	}
	var name strings.Builder
	for position := 1; position < len(text); position++ {
		if text[position] != identifierQuote {
			name.WriteByte(text[position])
			continue
		}
		if position+1 < len(text) && text[position+1] == identifierQuote {
			name.WriteByte(identifierQuote)
			position++
			continue
		}
		remainder := text[position+1:]
		if (remainder != "" && remainder[0] != '.') || name.Len() == 0 {
			return NamePart{}, "", false
		}
		return NamePart{Name: name.String(), Written: text[:position+1], Quoted: true}, remainder, true
	}
	return NamePart{}, "", false
}
