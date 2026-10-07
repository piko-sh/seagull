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

package goldenjson_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/goldenjson"
)

func text(value string) sqlmodel.SQLSegment {
	return sqlmodel.SQLSegment{Kind: sqlmodel.SegmentText, Text: value}
}

func parameter(index int) sqlmodel.SQLSegment {
	return sqlmodel.SQLSegment{Kind: sqlmodel.SegmentParameter, Parameter: index}
}

func TestTemplateWritesTheCompactForm(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		template sqlmodel.SQLTemplate
		expected string
	}{
		{
			name:     "the zero template writes nothing",
			template: sqlmodel.SQLTemplate{},
			expected: "",
		},
		{
			name: "a filtered query with an ORDER BY insertion point and its arguments",
			template: sqlmodel.SQLTemplate{
				Segments: []sqlmodel.SQLSegment{
					text("SELECT id FROM t WHERE "),
					{Kind: sqlmodel.SegmentFilter, Present: true, Children: []sqlmodel.SQLSegment{text("id = "), parameter(0)}},
					{Kind: sqlmodel.SegmentOrderBy},
				},
				Arguments: []int{0},
			},
			expected: "SELECT id FROM t WHERE ⟨where:id = ⟨p0⟩⟩⟨order⟩⟨arguments p0⟩",
		},
		{
			name: "a typed placeholder writes its type and escapes a colon in it",
			template: sqlmodel.SQLTemplate{Segments: []sqlmodel.SQLSegment{
				text("SELECT "),
				{Kind: sqlmodel.SegmentParameter, Parameter: 2, TypeText: "Map(String, UInt64)"},
				text(", "),
				{Kind: sqlmodel.SegmentParameter, Parameter: 3, TypeText: "a:b"},
			}},
			expected: `SELECT ⟨p2 Map(String, UInt64)⟩, ⟨p3 a\:b⟩`,
		},
		{
			name: "lists write their negation, parameter, type and operator text",
			template: sqlmodel.SQLTemplate{Segments: []sqlmodel.SQLSegment{
				text("WHERE id "),
				{Kind: sqlmodel.SegmentList, Parameter: 1, Text: "IN "},
				text(" AND kind "),
				{Kind: sqlmodel.SegmentList, Parameter: 0, Text: "not in ", Negated: true, TypeText: "Array(UInt8)"},
			}},
			expected: "WHERE id ⟨in p1:IN ⟩ AND kind ⟨not in p0 Array(UInt8):not in ⟩",
		},
		{
			name: "columns write their index, alias flag and expression",
			template: sqlmodel.SQLTemplate{Segments: []sqlmodel.SQLSegment{
				text("SELECT "),
				{Kind: sqlmodel.SegmentColumn, Column: 0, Children: []sqlmodel.SQLSegment{text("id")}},
				text(", "),
				{Kind: sqlmodel.SegmentColumn, Column: 1, Aliased: true, Children: []sqlmodel.SQLSegment{text("lower("), parameter(0), text(")")}},
				text(" AS n"),
			}},
			expected: "SELECT ⟨c0:id⟩, ⟨c1 aliased:lower(⟨p0⟩)⟩ AS n",
		},
		{
			name: "a star writes the columns it stands for and escapes their delimiters",
			template: sqlmodel.SQLTemplate{Segments: []sqlmodel.SQLSegment{
				text("SELECT "),
				{Kind: sqlmodel.SegmentStar, Text: "u.*", Star: []sqlmodel.StarColumn{
					{Column: 0, Qualifier: "u", Name: "id"},
					{Column: 1, Name: "full name"},
					{Column: 2, Qualifier: "a.b", Name: "x=y:z"},
				}},
				text(" FROM users u"),
			}},
			expected: `SELECT ⟨star 0=u.id 1=full\ name 2=a\.b.x\=y\:z:u.*⟩ FROM users u`,
		},
		{
			name: "a star written as it is says so",
			template: sqlmodel.SQLTemplate{Segments: []sqlmodel.SQLSegment{
				text("SELECT "),
				{Kind: sqlmodel.SegmentStar, Text: "*", AsWritten: true, Star: []sqlmodel.StarColumn{{Column: 0, Qualifier: "x", Name: "count"}}},
				text(" FROM x"),
			}},
			expected: `SELECT ⟨star written 0=x.count:*⟩ FROM x`,
		},
		{
			name: "clauses write their places, bodies and absence",
			template: sqlmodel.SQLTemplate{Segments: []sqlmodel.SQLSegment{
				{Kind: sqlmodel.SegmentFilter},
				{Kind: sqlmodel.SegmentPagination, Present: true, Children: []sqlmodel.SQLSegment{text("LIMIT "), parameter(1)}},
				{Kind: sqlmodel.SegmentOrderBy, Children: []sqlmodel.SQLSegment{text("ORDER BY id")}},
				{Kind: sqlmodel.SegmentFilter, Present: true},
				{Kind: sqlmodel.SegmentPagination},
			}},
			expected: "⟨where⟩⟨pagination:LIMIT ⟨p1⟩⟩⟨order absent:ORDER BY id⟩⟨where:⟩⟨pagination⟩",
		},
		{
			name: "a VALUES row writes its children whether or not present is set",
			template: sqlmodel.SQLTemplate{Segments: []sqlmodel.SQLSegment{
				text("INSERT INTO t (a, b) VALUES "),
				{Kind: sqlmodel.SegmentValuesRow, Children: []sqlmodel.SQLSegment{text("("), parameter(0), text(", now())")}},
				{Kind: sqlmodel.SegmentValuesRow, Present: true, Children: []sqlmodel.SQLSegment{text("(1)")}},
				{Kind: sqlmodel.SegmentValuesRow},
			}},
			expected: "INSERT INTO t (a, b) VALUES ⟨values:(⟨p0⟩, now())⟩⟨values:(1)⟩⟨values:⟩",
		},
		{
			name:     "text escapes backslashes, brackets, control characters and invalid UTF-8 onto one line",
			template: sqlmodel.SQLTemplate{Segments: []sqlmodel.SQLSegment{text("SELECT '\\n⟨x⟩'\n\tFROM t\r\x00\x7f\xff é")}},
			expected: `SELECT '\\n\⟨x\⟩'\n\tFROM t\r\x00\x7f\xff é`,
		},
		{
			name: "an undeclared kind is written with its name",
			template: sqlmodel.SQLTemplate{
				Segments:  []sqlmodel.SQLSegment{{Kind: sqlmodel.SQLSegmentKind(42), Children: []sqlmodel.SQLSegment{text("x")}}},
				Arguments: []int{3, 1, 3},
			},
			expected: "⟨SQLSegmentKind(42):x⟩⟨arguments p3 p1 p3⟩",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			first := goldenjson.Template(testCase.template)
			second := goldenjson.Template(testCase.template)
			require.Equal(t, testCase.expected, first)
			require.Equal(t, first, second)
			require.NotContains(t, first, "\n")
		})
	}
}

func TestTemplateKeepsDistinctTemplatesApart(t *testing.T) {
	t.Parallel()

	templates := []sqlmodel.SQLTemplate{
		{Segments: []sqlmodel.SQLSegment{text("⟨p0⟩")}},
		{Segments: []sqlmodel.SQLSegment{parameter(0)}},
		{Segments: []sqlmodel.SQLSegment{text("\\n")}},
		{Segments: []sqlmodel.SQLSegment{text("\n")}},
		{Segments: []sqlmodel.SQLSegment{{Kind: sqlmodel.SegmentStar, Star: []sqlmodel.StarColumn{{Column: 0, Qualifier: "a.b", Name: "c"}}}}},
		{Segments: []sqlmodel.SQLSegment{{Kind: sqlmodel.SegmentStar, Star: []sqlmodel.StarColumn{{Column: 0, Qualifier: "a", Name: "b.c"}}}}},
		{Segments: []sqlmodel.SQLSegment{{Kind: sqlmodel.SegmentFilter, Present: true}}},
		{Segments: []sqlmodel.SQLSegment{{Kind: sqlmodel.SegmentFilter}}},
	}

	seen := make(map[string]int, len(templates))
	for index, template := range templates {
		encoded := goldenjson.Template(template)
		previous, duplicate := seen[encoded]
		require.False(t, duplicate, "templates %d and %d both encode as %q", previous, index, encoded)
		seen[encoded] = index
	}
}
