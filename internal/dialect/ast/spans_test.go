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

package ast_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"seagull.sh/seagull/internal/dialect/ast"
)

func TestSpanIsKnownWhenItCoversAByte(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		span ast.Span
		want bool
	}{
		{name: "the zero span", span: ast.Span{}, want: false},
		{name: "an empty span away from the start", span: ast.Span{Start: 7, End: 7}, want: false},
		{name: "a span ending before it starts", span: ast.Span{Start: 9, End: 3}, want: false},
		{name: "one byte at the start", span: ast.Span{Start: 0, End: 1}, want: true},
		{name: "several bytes", span: ast.Span{Start: 4, End: 12}, want: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, testCase.span.Known())
		})
	}
}

func TestSpanContainsASpanLyingWithinIt(t *testing.T) {
	t.Parallel()

	outer := ast.Span{Start: 10, End: 20}
	testCases := []struct {
		name  string
		span  ast.Span
		other ast.Span
		want  bool
	}{
		{name: "a span strictly inside", span: outer, other: ast.Span{Start: 12, End: 18}, want: true},
		{name: "the same span", span: outer, other: outer, want: true},
		{name: "a span sharing the start", span: outer, other: ast.Span{Start: 10, End: 11}, want: true},
		{name: "a span sharing the end", span: outer, other: ast.Span{Start: 19, End: 20}, want: true},
		{name: "a span starting before", span: outer, other: ast.Span{Start: 9, End: 15}, want: false},
		{name: "a span ending after", span: outer, other: ast.Span{Start: 15, End: 21}, want: false},
		{name: "a span wholly outside", span: outer, other: ast.Span{Start: 30, End: 40}, want: false},
		{name: "a span covering it", span: outer, other: ast.Span{Start: 0, End: 30}, want: false},
		{name: "an unknown span inside", span: outer, other: ast.Span{Start: 15, End: 15}, want: false},
		{name: "a span in an unknown span", span: ast.Span{}, other: ast.Span{Start: 0, End: 0}, want: false},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, testCase.span.Contains(testCase.other))
		})
	}
}
