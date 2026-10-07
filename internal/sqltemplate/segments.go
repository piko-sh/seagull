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

package sqltemplate

import (
	"strings"
	"unicode"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

// treatment says how segmentsWithin writes a node.
type treatment uint8

const (
	// keep writes the node as its segment.
	keep treatment = iota

	// unwrap writes the node's children in its place, without a segment of its own.
	unwrap

	// leaveOut writes nothing for the node, its text included.
	leaveOut
)

// segmentsWithin writes the part of a node's children a window covers as segments: the
// text the query keeps between them, and each child within the window as treat says. A
// child the window cuts through cannot be written.
//
// Takes container (*node) which holds the children.
// Takes window (ast.Span) which is the range of Input.SQL to write.
// Takes treat (func(*node) treatment) which says how to write each child, or is nil to
// keep every child.
//
// Returns []sqlmodel.SQLSegment which holds the segments.
// Returns bool which is false when the window cuts through a child.
func (b *builder) segmentsWithin(container *node, window ast.Span, treat func(current *node) treatment) ([]sqlmodel.SQLSegment, bool) {
	var segments []sqlmodel.SQLSegment
	cursor := window.Start
	for _, child := range container.children {
		switch {
		case outside(child.span, window):
			continue
		case child.span.Start < window.Start || child.span.End > window.End:
			return nil, false
		}
		segments = appendText(segments, b.kept(cursor, child.span.Start))
		cursor = child.span.End
		written, ok := b.treated(child, treat)
		if !ok {
			return nil, false
		}
		segments = appendSegments(segments, written)
	}
	return appendText(segments, b.kept(cursor, window.End)), true
}

// treated writes one child as treat says.
//
// Takes child (*node) which is the child.
// Takes treat (func(*node) treatment) which says how, or is nil to keep it.
//
// Returns []sqlmodel.SQLSegment which holds what the child writes.
// Returns bool which is false when the child cannot be written.
func (b *builder) treated(child *node, treat func(current *node) treatment) ([]sqlmodel.SQLSegment, bool) {
	how := keep
	if treat != nil {
		how = treat(child)
	}
	switch how {
	case leaveOut:
		return nil, true
	case unwrap:
		if child.leaf {
			return appendText(nil, b.kept(child.span.Start, child.span.End)), true
		}
		return b.segmentsWithin(child, child.span, treat)
	default:
		segment, ok := b.segment(child, treat)
		return []sqlmodel.SQLSegment{segment}, ok
	}
}

// segment writes one node as its segment, its children written inside it unless it is a
// leaf.
//
// Takes current (*node) which is the node.
// Takes treat (func(*node) treatment) which says how to write its children, or is nil.
//
// Returns sqlmodel.SQLSegment which is the segment.
// Returns bool which is false when a child cannot be written.
func (b *builder) segment(current *node, treat func(current *node) treatment) (sqlmodel.SQLSegment, bool) {
	segment := current.segment
	if current.leaf {
		return segment, true
	}
	children, ok := b.segmentsWithin(current, current.span, treat)
	segment.Children = children
	return segment, ok
}

// appendSegments appends segments, joining a text segment that starts them to a text
// segment that ends the ones before.
//
// Takes segments ([]sqlmodel.SQLSegment) which are the segments so far.
// Takes more ([]sqlmodel.SQLSegment) which are the segments to append.
//
// Returns []sqlmodel.SQLSegment which holds them all.
func appendSegments(segments, more []sqlmodel.SQLSegment) []sqlmodel.SQLSegment {
	for index := range more {
		if more[index].Kind == sqlmodel.SegmentText {
			segments = appendText(segments, more[index].Text)
			continue
		}
		segments = append(segments, more[index])
	}
	return segments
}

// outside reports whether a node lies wholly outside a window: a place before or after
// it, or a span that ends before it starts or starts once it has ended.
//
// Takes span (ast.Span) which is the node's span.
// Takes window (ast.Span) which is the window.
//
// Returns bool which is true when the node lies outside.
func outside(span, window ast.Span) bool {
	if !span.Known() {
		return span.Start < window.Start || span.Start > window.End
	}
	return span.End <= window.Start || span.Start >= window.End
}

// appendText appends nonempty text; withArguments coalesces adjacent pieces once.
//
// Takes segments ([]sqlmodel.SQLSegment) which are the segments so far.
// Takes text (string) which is the text.
//
// Returns []sqlmodel.SQLSegment which holds the segments with the text appended.
func appendText(segments []sqlmodel.SQLSegment, text string) []sqlmodel.SQLSegment {
	if text == "" {
		return segments
	}
	return append(segments, textSegment(text))
}

// textSegment returns a text segment.
//
// Takes text (string) which is the text.
//
// Returns sqlmodel.SQLSegment which is the segment.
func textSegment(text string) sqlmodel.SQLSegment {
	segment := sqlmodel.SQLSegment{}
	segment.Kind = sqlmodel.SegmentText
	segment.Text = text
	return segment
}

// trimTrailingSpace leaves out the whitespace that ends the segments, when they end in
// text.
//
// Takes segments ([]sqlmodel.SQLSegment) which are the segments; the last may be changed
// in place.
//
// Returns []sqlmodel.SQLSegment which holds the trimmed segments.
func trimTrailingSpace(segments []sqlmodel.SQLSegment) []sqlmodel.SQLSegment {
	for len(segments) > 0 {
		last := len(segments) - 1
		if segments[last].Kind != sqlmodel.SegmentText {
			break
		}
		segments[last].Text = strings.TrimRightFunc(segments[last].Text, unicode.IsSpace)
		if segments[last].Text != "" {
			break
		}
		segments = segments[:last]
	}
	return segments
}

// coalesceText joins each run of adjacent text once and normalises nested segments.
//
// Takes segments ([]sqlmodel.SQLSegment) which holds the SQL segments to combine.
//
// Returns []sqlmodel.SQLSegment which shares the input slice with adjacent text and
// nested runs combined.
func coalesceText(segments []sqlmodel.SQLSegment) []sqlmodel.SQLSegment {
	result := segments[:0]
	for index := 0; index < len(segments); {
		segment := segments[index]
		index++
		if segment.Kind == sqlmodel.SegmentText {
			end, size := index, len(segment.Text)
			for end < len(segments) && segments[end].Kind == sqlmodel.SegmentText {
				size += len(segments[end].Text)
				end++
			}
			segment.Text = joinText(segments[index-1:end], size)
			index = end
		} else {
			segment.Children = coalesceText(segment.Children)
		}
		result = append(result, segment)
	}
	clear(segments[len(result):])
	return result
}

// joinText copies a run once, returning a single segment's text without allocation.
//
// Takes segments ([]sqlmodel.SQLSegment) which holds the SQL segments to combine.
// Takes size (int) which is the total byte length of the text segments.
//
// Returns string which is the joined text, sharing a single segment's string when
// possible.
func joinText(segments []sqlmodel.SQLSegment, size int) string {
	if len(segments) == 1 {
		return segments[0].Text
	}
	var text strings.Builder
	text.Grow(size)
	for _, segment := range segments {
		text.WriteString(segment.Text)
	}
	return text.String()
}
