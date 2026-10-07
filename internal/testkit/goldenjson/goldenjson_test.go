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
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/goldenjson"
)

type projectionSample struct {
	Pointer   *int
	Interface any
	Values    map[string]int
	Name      string
	hidden    string
	Elements  []sqlmodel.SQLType
	Count     int
	Category  sqlmodel.SQLTypeCategory
	Enabled   bool
}

func TestProject(t *testing.T) {
	t.Parallel()

	zero := 0
	testCases := []struct {
		value    any
		name     string
		expected string
	}{
		{
			name:     "empty fields are left out and a zero enum is still named",
			value:    projectionSample{},
			expected: `{"Category":"integer"}`,
		},
		{
			name:     "a pointer to zero is kept",
			value:    projectionSample{Pointer: &zero, Category: sqlmodel.TypeCategoryText},
			expected: `{"Category":"text","Pointer":0}`,
		},
		{
			name: "set scalars, maps and slices are written and empty elements keep their place",
			value: projectionSample{
				Name:     "items",
				Count:    2,
				Enabled:  true,
				Values:   map[string]int{"b": 0, "a": 1},
				Elements: []sqlmodel.SQLType{{}, {EngineName: "WHOLE32"}},
				hidden:   "never written",
			},
			expected: `{"Category":"integer","Count":2,"Elements":[{"Category":"integer"},` +
				`{"Category":"integer","EngineName":"WHOLE32"}],"Enabled":true,"Name":"items","Values":{"a":1,"b":0}}`,
		},
		{
			name:     "a kind that reports itself absent is left out and a set kind is named",
			value:    []sqlmodel.SQLType{{Category: sqlmodel.TypeCategoryText}, sqlmodel.NewTemporalType("MOMENT", sqlmodel.TemporalKindTimestamp, true)},
			expected: `[{"Category":"text"},{"Category":"temporal","EngineName":"MOMENT","Temporal":"timestamp","TimeZone":true}]`,
		},
		{
			name:     "an interface holding an enum is named",
			value:    projectionSample{Interface: sqlmodel.QueryCommandExecRows},
			expected: `{"Category":"integer","Interface":"execrows"}`,
		},
		{
			name:     "a diagnostic severity is named",
			value:    []diag.SourceError{{Code: "Q001", Line: 1, Severity: diag.SeverityWarning}},
			expected: `[{"Code":"Q001","Line":1,"Severity":"warning"}]`,
		},
		{
			name: "a template is written as its text and an empty one is left out",
			value: sqlmodel.AnalysedQuery{
				Name: "GetItem",
				Template: sqlmodel.SQLTemplate{
					Segments:  []sqlmodel.SQLSegment{{Kind: sqlmodel.SegmentText, Text: "SELECT id FROM items WHERE id = "}, {Kind: sqlmodel.SegmentParameter}},
					Arguments: []int{0},
				},
				CountTemplate: nil,
			},
			expected: `{"Command":"one","Name":"GetItem","Template":"SELECT id FROM items WHERE id = ⟨p0⟩⟨arguments p0⟩"}`,
		},
		{
			name:     "a template behind a pointer is written as its text",
			value:    &sqlmodel.SQLTemplate{Segments: []sqlmodel.SQLSegment{{Kind: sqlmodel.SegmentText, Text: "SELECT 1"}}},
			expected: `"SELECT 1"`,
		},
		{
			name:     "a nil value projects to null",
			value:    nil,
			expected: `null`,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			encoded, err := json.Marshal(goldenjson.Project(testCase.value))
			require.NoError(t, err)
			require.JSONEq(t, testCase.expected, string(encoded))
		})
	}
}
