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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/diag"
	"seagull.sh/seagull/internal/sqlmodel"
)

func TestValidateDuplicateNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		queries            []*sqlmodel.AnalysedQuery
		expectedCount      int
		expectedDiagnostic func(t *testing.T, diagnostics []diag.SourceError)
	}{
		{
			name:          "no queries returns empty result",
			queries:       nil,
			expectedCount: 0,
			expectedDiagnostic: func(t *testing.T, diagnostics []diag.SourceError) {
				assert.Empty(t, diagnostics)
			},
		},
		{
			name: "unique names returns empty result",
			queries: []*sqlmodel.AnalysedQuery{
				{Name: "GetUser", Filename: "users.sql", Line: 1},
				{Name: "ListUsers", Filename: "users.sql", Line: 10},
				{Name: "CreateUser", Filename: "users.sql", Line: 20},
			},
			expectedCount: 0,
			expectedDiagnostic: func(t *testing.T, diagnostics []diag.SourceError) {
				assert.Empty(t, diagnostics)
			},
		},
		{
			name: "one duplicate returns Q006 with correct filename and line",
			queries: []*sqlmodel.AnalysedQuery{
				{Name: "GetUser", Filename: "users.sql", Line: 1},
				{Name: "GetUser", Filename: "admin.sql", Line: 5},
			},
			expectedCount: 1,
			expectedDiagnostic: func(t *testing.T, diagnostics []diag.SourceError) {
				require.Len(t, diagnostics, 1)

				diagnostic := diagnostics[0]
				assert.Equal(t, "admin.sql", diagnostic.Filename,
					"diagnostic should reference the second (duplicate) file")
				assert.Equal(t, 5, diagnostic.Line,
					"diagnostic should reference the duplicate's line")
				assert.Equal(t, 1, diagnostic.Column)
				assert.Equal(t, "Q006", diagnostic.Code)
				assert.Equal(t, diag.SeverityError, diagnostic.Severity)
				assert.Contains(t, diagnostic.Message, `"GetUser"`,
					"message should include the duplicate query name")
				assert.Contains(t, diagnostic.Message, "users.sql:1",
					"message should reference the first definition's location")
			},
		},
		{
			name: "names differing only in case are duplicates",
			queries: []*sqlmodel.AnalysedQuery{
				{Name: "ZzFoo", Filename: "users.sql", Line: 3},
				{Name: "ZZFOO", Filename: "admin.sql", Line: 7},
			},
			expectedCount: 1,
			expectedDiagnostic: func(t *testing.T, diagnostics []diag.SourceError) {
				require.Len(t, diagnostics, 1)

				diagnostic := diagnostics[0]
				assert.Equal(t, "admin.sql", diagnostic.Filename)
				assert.Equal(t, 7, diagnostic.Line)
				assert.Equal(t, "Q006", diagnostic.Code)
				assert.Contains(t, diagnostic.Message, `"ZZFOO"`,
					"message should name the query exactly as the author wrote it")
				assert.Contains(t, diagnostic.Message, "users.sql:3",
					"message should reference the first definition's location")
			},
		},
		{
			name: "multiple duplicates returns multiple Q006 errors",
			queries: []*sqlmodel.AnalysedQuery{
				{Name: "GetUser", Filename: "users.sql", Line: 1},
				{Name: "ListPosts", Filename: "posts.sql", Line: 1},
				{Name: "GetUser", Filename: "admin.sql", Line: 10},
				{Name: "ListPosts", Filename: "dashboard.sql", Line: 20},
			},
			expectedCount: 2,
			expectedDiagnostic: func(t *testing.T, diagnostics []diag.SourceError) {
				require.Len(t, diagnostics, 2)

				for _, diagnostic := range diagnostics {
					assert.Equal(t, "Q006", diagnostic.Code)
					assert.Equal(t, diag.SeverityError, diagnostic.Severity)
				}
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			diagnostics := ValidateDuplicateNames(testCase.queries)

			assert.Len(t, diagnostics, testCase.expectedCount)
			testCase.expectedDiagnostic(t, diagnostics)
		})
	}
}

func TestValidateDuplicateNamesTreatsASnakeCaseNameAsItsMethod(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		names      []string
		duplicates []string
	}{
		{name: "a snake case name and its pascal case form", names: []string{"GetReportByID", "get_report_by_id"}, duplicates: []string{"get_report_by_id"}},
		{name: "two spellings of one snake case name", names: []string{"list_users", "LIST_USERS"}, duplicates: []string{"LIST_USERS"}},
		{name: "names whose words differ", names: []string{"get_user", "get_users"}, duplicates: nil},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			queries := make([]*sqlmodel.AnalysedQuery, 0, len(testCase.names))
			for index, name := range testCase.names {
				queries = append(queries, &sqlmodel.AnalysedQuery{Name: name, Filename: "reports.sql", Line: index*5 + 1})
			}

			diagnostics := ValidateDuplicateNames(queries)

			var duplicates []string
			for _, diagnostic := range diagnostics {
				require.Equal(t, diag.CodeDuplicateQueryName, diagnostic.Code)
				for _, name := range testCase.names {
					if strings.Contains(diagnostic.Message, "duplicate query name \""+name+"\"") {
						duplicates = append(duplicates, name)
					}
				}
			}
			require.Equal(t, testCase.duplicates, duplicates)
		})
	}
}
