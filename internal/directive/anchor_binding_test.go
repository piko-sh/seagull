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

package directive

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestNamedAnchorNamesTheParameter(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		binding      string
		wantName     string
		wantAnchor   string
		wantOptional bool
	}{
		{name: "bare", binding: "-- :title as sgl.param", wantName: "title", wantAnchor: "title"},
		{name: "keyword arguments only", binding: "-- :title as sgl.param(optional: true)", wantName: "title", wantAnchor: "title", wantOptional: true},
		{name: "empty argument list", binding: "-- :title as sgl.param()", wantName: "title", wantAnchor: "title"},
		{name: "renamed positionally", binding: "-- :customer as sgl.param(buyer)", wantName: "buyer", wantAnchor: "customer"},
		{name: "renamed by keyword", binding: "-- :customer as sgl.param(name: buyer, optional: true)", wantName: "buyer", wantAnchor: "customer", wantOptional: true},
		{name: "at sigil", binding: "-- @title as sgl.param(optional: true)", wantName: "title", wantAnchor: "title", wantOptional: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			block, diagnostics := parseSQL(t, newTestParserWithNamedPrefix(),
				"-- sgl.query(name: Find, command: many)\n"+testCase.binding+"\nSELECT 1;")

			require.Empty(t, diagnostics)
			require.Len(t, block.Parameters, 1)
			parameter := block.Parameters[0]
			require.True(t, parameter.IsNamed)
			require.Equal(t, testCase.wantName, parameter.Name)
			require.Equal(t, testCase.wantAnchor, parameter.Anchor)
			require.Equal(t, testCase.wantOptional, parameter.IsOptional)
		})
	}
}

func TestPositionalAnchorStillRequiresAName(t *testing.T) {
	t.Parallel()

	block, diagnostics := parseSQL(t, newTestParserWithNamedPrefix(),
		"-- sgl.query(name: Find, command: many)\n-- $1 as sgl.param(optional: true)\nSELECT 1;")

	require.Len(t, diagnostics, 1)
	require.Equal(t, "missing required name on sgl.param", diagnostics[0].Message)
	require.Empty(t, block.Parameters)
}

func TestBindNamedAnchors(t *testing.T) {
	t.Parallel()

	named := func(anchor string, number int) *ParameterDirective {
		return &ParameterDirective{Name: anchor + "_name", Anchor: anchor, Number: number, IsNamed: true}
	}
	positional := func(number int) *ParameterDirective {
		return &ParameterDirective{Name: "positional", Number: number}
	}
	sortable := func() *ParameterDirective {
		return &ParameterDirective{Name: "order_by", Kind: sqlmodel.ParameterDirectiveSortable}
	}
	reference := func(name string, number int) ast.RawParameterReference {
		return ast.RawParameterReference{Name: name, Number: number}
	}

	testCases := []struct {
		name        string
		parameters  []*ParameterDirective
		references  []ast.RawParameterReference
		wantNumbers []int
	}{
		{
			name:        "declared in statement order",
			parameters:  []*ParameterDirective{named("customer", 1), named("title", 2)},
			references:  []ast.RawParameterReference{reference("customer", 1), reference("title", 2)},
			wantNumbers: []int{1, 2},
		},
		{
			name:        "declared in reverse order",
			parameters:  []*ParameterDirective{named("title", 1), named("customer", 2)},
			references:  []ast.RawParameterReference{reference("customer", 1), reference("title", 2)},
			wantNumbers: []int{2, 1},
		},
		{
			name:        "a reused placeholder keeps its first number",
			parameters:  []*ParameterDirective{named("title", 1), named("customer", 2)},
			references:  []ast.RawParameterReference{reference("customer", 1), reference("title", 2), reference("customer", 1)},
			wantNumbers: []int{2, 1},
		},
		{
			name:        "beside positional placeholders",
			parameters:  []*ParameterDirective{named("title", 1), positional(1)},
			references:  []ast.RawParameterReference{reference("", 1), reference("title", 2)},
			wantNumbers: []int{2, 1},
		},
		{
			name:        "an unused anchor takes a number past every other",
			parameters:  []*ParameterDirective{named("missing", 1), named("title", 2), positional(4)},
			references:  []ast.RawParameterReference{reference("title", 1)},
			wantNumbers: []int{5, 1, 4},
		},
		{
			name:        "a sortable directive is left for the resolver",
			parameters:  []*ParameterDirective{named("title", 1), sortable()},
			references:  []ast.RawParameterReference{reference("title", 1)},
			wantNumbers: []int{1, 0},
		},
		{
			name:        "no references",
			parameters:  []*ParameterDirective{named("title", 1)},
			references:  nil,
			wantNumbers: []int{1},
		},
		{
			name:        "a dialect that names no placeholder",
			parameters:  []*ParameterDirective{named("p2", 1), named("p1", 2)},
			references:  []ast.RawParameterReference{reference("", 2), reference("", 1)},
			wantNumbers: []int{1, 2},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			BindNamedAnchors(testCase.parameters, testCase.references)

			numbers := make([]int, 0, len(testCase.parameters))
			for _, parameter := range testCase.parameters {
				numbers = append(numbers, parameter.Number)
			}
			require.Equal(t, testCase.wantNumbers, numbers)
		})
	}
}
