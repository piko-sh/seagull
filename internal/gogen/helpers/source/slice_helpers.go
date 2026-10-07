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

package source

import (
	"strconv"
	"strings"
)

const (
	// sglListPrefix is the text before each list placeholder number.
	//
	//sgl:inline prefix
	sglListPrefix = "#"

	// sglListSuffix is the text after each list placeholder number.
	//
	//sgl:inline suffix
	sglListSuffix = ""

	// sglListSuffixed enables suffixes in generated list placeholders.
	//
	//sgl:inline suffixed
	sglListSuffixed = false

	// sglListNumbered enables numbering of generated list placeholders.
	//
	//sgl:inline numbered
	sglListNumbered = true
)

// sglListMark locates a slice parameter's list in a query: the operator before it from
// Operator, and the parenthesised placeholder from Start to End, with the test that
// replaces both when the slice is empty.
type sglListMark struct {
	// Empty is the dialect's replacement predicate for an empty list.
	Empty string

	// Operator is the offset at which the list operator begins.
	Operator int

	// Start is the offset at which the placeholder list begins.
	Start int

	// End is the exclusive offset of the placeholder list.
	End int
}

// sglList replaces a slice parameter with placeholders or an empty-list test.
//
// It returns the edit that writes a slice parameter's list: count placeholders in
// parentheses, numbered from first, in place of the one the query holds, or, for an empty
// slice, the dialect's test no row passes, or every row passes for NOT IN, in place of
// the operator and the list.
//
// Takes mark (sglListMark) which locates the original list and its operator.
// Takes first (int) which is the first placeholder number.
// Takes count (int) which is the number of list elements to bind.
//
// Returns []sglEdit which replaces the placeholder list or its empty-list predicate.
func sglList(mark sglListMark, first, count int) []sglEdit {
	const decimalBase = 10
	if count == 0 {
		return []sglEdit{{Text: mark.Empty, Start: mark.Operator, End: mark.End}}
	}
	var builder strings.Builder
	width := len(sglListPrefix) + len(sglListSuffix) + 1
	if sglListNumbered {
		var digits [20]byte
		width += len(strconv.AppendInt(digits[:0], int64(first+count-1), decimalBase))
	}
	builder.Grow(2 + count*width)
	builder.WriteByte('(')
	for index := range count {
		if index > 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(sglListPrefix)
		if sglListNumbered {
			var digits [20]byte
			builder.Write(strconv.AppendInt(digits[:0], int64(first+index), decimalBase))
		}
		if sglListSuffixed {
			builder.WriteString(sglListSuffix)
		}
	}
	builder.WriteByte(')')
	return []sglEdit{{Text: builder.String(), Start: mark.Start, End: mark.End}}
}
