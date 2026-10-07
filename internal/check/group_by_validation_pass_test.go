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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/dialect/ast"
	"seagull.sh/seagull/internal/namespace"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestGroupByValidationPass(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		query         *sqlmodel.AnalysedQuery
		expectedCount int
		checkCodes    []string
	}{
		{
			name: "no group_by directive produces no diagnostic",
			query: &sqlmodel.AnalysedQuery{
				Name:       "TestQuery",
				Command:    sqlmodel.QueryCommandMany,
				GroupByKey: nil,
				Line:       1,
			},
			expectedCount: 0,
		},
		{
			name: "group_by on a command other than many produces Q016",
			query: &sqlmodel.AnalysedQuery{
				Name:       "TestQuery",
				Command:    sqlmodel.QueryCommandOne,
				GroupByKey: []string{"id"},
				OutputColumns: []sqlmodel.OutputColumn{
					{Name: "id", IsEmbedded: false},
					{Name: "name", IsEmbedded: true},
				},
				Line: 1,
			},
			expectedCount: 1,
			checkCodes:    []string{"Q016"},
		},
		{
			name: "valid group_by with many and embed produces no diagnostic",
			query: &sqlmodel.AnalysedQuery{
				Name:       "TestQuery",
				Command:    sqlmodel.QueryCommandMany,
				GroupByKey: []string{"id"},
				OutputColumns: []sqlmodel.OutputColumn{
					{Name: "id", IsEmbedded: false},
					{Name: "title", IsEmbedded: true},
				},
				Line: 1,
			},
			expectedCount: 0,
		},
		{
			name: "group_by without embed produces Q015",
			query: &sqlmodel.AnalysedQuery{
				Name:       "TestQuery",
				Command:    sqlmodel.QueryCommandMany,
				GroupByKey: []string{"id"},
				OutputColumns: []sqlmodel.OutputColumn{
					{Name: "id", IsEmbedded: false},
					{Name: "name", IsEmbedded: false},
				},
				Line: 1,
			},
			expectedCount: 1,
			checkCodes:    []string{"Q015"},
		},
		{
			name: "group_by referencing non-existent column produces Q014",
			query: &sqlmodel.AnalysedQuery{
				Name:       "TestQuery",
				Command:    sqlmodel.QueryCommandMany,
				GroupByKey: []string{"missing_col"},
				OutputColumns: []sqlmodel.OutputColumn{
					{Name: "id", IsEmbedded: false},
					{Name: "items", IsEmbedded: true},
				},
				Line: 1,
			},
			expectedCount: 1,
			checkCodes:    []string{"Q014"},
		},
		{
			name: "a qualified group_by key matches the output column it ends with",
			query: &sqlmodel.AnalysedQuery{
				Name:       "TestQuery",
				Command:    sqlmodel.QueryCommandMany,
				GroupByKey: []string{"users.id", "public.users.ID"},
				OutputColumns: []sqlmodel.OutputColumn{
					{Name: "id", IsEmbedded: false},
					{Name: "items", IsEmbedded: true},
				},
				Line: 1,
			},
			expectedCount: 0,
		},
		{
			name: "a qualified group_by key ending in an unknown column produces Q014",
			query: &sqlmodel.AnalysedQuery{
				Name:       "TestQuery",
				Command:    sqlmodel.QueryCommandMany,
				GroupByKey: []string{"id.missing"},
				OutputColumns: []sqlmodel.OutputColumn{
					{Name: "id", IsEmbedded: false},
					{Name: "items", IsEmbedded: true},
				},
				Line: 1,
			},
			expectedCount: 1,
			checkCodes:    []string{"Q014"},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			pass := &groupByValidationPass{}
			subject := &DiagnosticContext{
				Filename:    "test.sql",
				Query:       testCase.query,
				RawAnalysis: &ast.RawQueryAnalysis{},
			}

			diagnostics := pass.Analyse(t.Context(), subject)

			require.Len(t, diagnostics, testCase.expectedCount)

			for index, code := range testCase.checkCodes {
				if index < len(diagnostics) {
					assert.Equal(t, code, diagnostics[index].Code)
					assert.Equal(t, diag.SeverityWarning, diagnostics[index].Severity)
				}
			}
		})
	}
}

func TestGroupByMissingEmbedIsReportedOnce(t *testing.T) {
	t.Parallel()

	query := &sqlmodel.AnalysedQuery{
		Name:          "ListOrders",
		Command:       sqlmodel.QueryCommandMany,
		GroupByKey:    []string{"orders.id"},
		OutputColumns: []sqlmodel.OutputColumn{{Name: "id"}, {Name: "sku"}},
		Line:          1,
	}
	pass := &groupByValidationPass{}

	alone := pass.Analyse(t.Context(), &DiagnosticContext{Query: query, Filename: "orders.sql"})
	require.Len(t, alone, 1, "a grouped query with no embed at all is reported")
	assert.Equal(t, diag.CodeGroupByMissingEmbed, alone[0].Code)

	analysed := pass.Analyse(t.Context(), &DiagnosticContext{
		Query:               query,
		Filename:            "orders.sql",
		ExistingDiagnostics: []diag.SourceError{{Code: diag.CodeGroupByMissingEmbed, Message: "the columns of order_lines are not embedded"}},
	})
	assert.Empty(t, analysed, "the analyser's report of the unembedded table stands alone")
}

func TestGroupByOnAnotherCommandNamesTheCommand(t *testing.T) {
	t.Parallel()

	pass := &groupByValidationPass{}
	diagnostics := pass.Analyse(t.Context(), &DiagnosticContext{
		Filename: "test.sql",
		Query: &sqlmodel.AnalysedQuery{
			Name:          "GetOrder",
			Command:       sqlmodel.QueryCommandOne,
			GroupByKey:    []string{"id"},
			OutputColumns: []sqlmodel.OutputColumn{{Name: "id"}, {Name: "line", IsEmbedded: true}},
			Line:          1,
		},
		RawAnalysis: &ast.RawQueryAnalysis{},
		Prefixes:    namespace.Default(),
	})

	require.Len(t, diagnostics, 1)
	assert.Equal(t, `sgl.group_by is valid only with the many command; query "GetOrder" uses one`, diagnostics[0].Message)
}
