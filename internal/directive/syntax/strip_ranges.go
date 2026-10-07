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

import (
	"slices"
	"strings"
	"unicode"

	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/sqllex"
)

// Range is a half-open range of byte offsets into a text: from the first byte it covers
// to one past the last.
type Range struct {
	// Start is the offset of the first byte.
	Start int

	// End is the offset one past the last byte.
	End int
}

// StripRanges identifies the source ranges kept by StripDirectives.
//
// The ordered, non-overlapping ranges let callers map source positions into the stripped
// SQL.
//
// Takes sql (string) which is the raw SQL text possibly containing directives.
// Takes lexicon (Lexicon) which is how the dialect writes SQL.
// Takes prefixes (namespace.Set) which names the directive prefixes to strip.
//
// Returns []Range which holds the kept ranges, or nil when nothing is kept.
func StripRanges(sql string, lexicon Lexicon, prefixes namespace.Set) []Range {
	return StripTokenRanges(sql, lexicon, prefixes, Tokens(lexicon.Rules, sql))
}

// StripTokenRanges strips directive lines using tokens already scanned under lexicon.
//
// Takes sql (string) which is the original SQL text.
// Takes lexicon (Lexicon) which defines the dialect's comments and lexical rules.
// Takes prefixes (namespace.Set) which identifies the recognised directive namespaces.
// Takes tokens ([]sqllex.Token) which holds tokens already scanned from the supplied SQL.
//
// Returns []Range which holds the retained source ranges with surrounding whitespace
// removed.
func StripTokenRanges(sql string, lexicon Lexicon, prefixes namespace.Set, tokens []sqllex.Token) []Range {
	comments, lines := lexicon.DirectiveCommentsFromTokens(sql, tokens)
	return trimRanges(sql, keptLines(sql, directiveLines(comments, lines, prefixes)))
}

// KeptText writes the ranges of a text one after another.
//
// Takes text (string) which is the text the ranges index.
// Takes ranges ([]Range) which are the ranges to write, in order.
//
// Returns string which is the text the ranges cover.
func KeptText(text string, ranges []Range) string {
	if len(ranges) == 1 {
		return text[ranges[0].Start:ranges[0].End]
	}
	length := 0
	for _, kept := range ranges {
		length += kept.End - kept.Start
	}
	var builder strings.Builder
	builder.Grow(length)
	for _, kept := range ranges {
		builder.WriteString(text[kept.Start:kept.End])
	}
	return builder.String()
}

// keptLines returns the ranges of the lines a text keeps once the stripped lines are left
// out, each kept line joined to the next kept one by the newline that ends it.
//
// Takes text (string) which is the text.
// Takes stripped (map[int]struct{}) which holds the 1-based numbers of the lines left
// out.
//
// Returns []Range which holds the kept ranges, adjacent ones merged.
func keptLines(text string, stripped map[int]struct{}) []Range {
	var ranges []Range
	separator := -1
	lineNumber := 0
	for lineStart := 0; lineStart <= len(text); {
		lineNumber++
		lineEnd := len(text)
		if newline := strings.IndexByte(text[lineStart:], '\n'); newline >= 0 {
			lineEnd = lineStart + newline
		}
		if _, isDirective := stripped[lineNumber]; !isDirective {
			if separator >= 0 {
				ranges = appendRange(ranges, Range{Start: separator, End: separator + 1})
			}
			ranges = appendRange(ranges, Range{Start: lineStart, End: lineEnd})
			separator = -1
			if lineEnd < len(text) {
				separator = lineEnd
			}
		}
		lineStart = lineEnd + 1
	}
	return ranges
}

// trimRanges leaves out the whitespace at both ends of the text the ranges cover, as
// strings.TrimSpace would from the text they write.
//
// Takes text (string) which is the text the ranges index.
// Takes ranges ([]Range) which are the owned line ranges to trim in place.
//
// Returns []Range which holds the trimmed ranges, without empty ones.
func trimRanges(text string, ranges []Range) []Range {
	for index := range ranges {
		kept := &ranges[index]
		trimmed := strings.TrimLeftFunc(text[kept.Start:kept.End], unicode.IsSpace)
		kept.Start = kept.End - len(trimmed)
		if trimmed != "" {
			break
		}
	}
	for index := len(ranges) - 1; index >= 0; index-- {
		kept := &ranges[index]
		trimmed := strings.TrimRightFunc(text[kept.Start:kept.End], unicode.IsSpace)
		kept.End = kept.Start + len(trimmed)
		if trimmed != "" {
			break
		}
	}
	trimmed := dropEmptyRanges(ranges)
	if len(trimmed) == 0 {
		return nil
	}
	return trimmed
}

// appendRange appends a range, merging it into the last one when they touch, and leaving
// out an empty range.
//
// Takes ranges ([]Range) which are the ranges so far.
// Takes next (Range) which is the range to append.
//
// Returns []Range which holds the ranges with next appended.
func appendRange(ranges []Range, next Range) []Range {
	if next.End <= next.Start {
		return ranges
	}
	if last := len(ranges) - 1; last >= 0 && ranges[last].End == next.Start {
		ranges[last].End = next.End
		return ranges
	}
	return append(ranges, next)
}

// dropEmptyRanges leaves out the ranges that cover nothing.
//
// Takes ranges ([]Range) which are the ranges.
//
// Returns []Range which holds the ranges that cover at least one byte.
func dropEmptyRanges(ranges []Range) []Range {
	return slices.DeleteFunc(ranges, func(candidate Range) bool {
		return candidate.End <= candidate.Start
	})
}
