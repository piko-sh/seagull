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

package dialecttest

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/app"
	"seagull.sh/seagull/internal/sqlmodel"
	"seagull.sh/seagull/internal/testkit/fuzzcase"
)

func TestPositiveModelOracleRejectsMissingAnalysisAndOutput(t *testing.T) {
	query := fuzzcase.Query{Columns: 1, Values: []fuzzcase.Value{{Integer: 7}}}
	result := &app.DetailedResult{
		Report: &app.Report{Queries: []*sqlmodel.AnalysedQuery{{
			OutputColumns: []sqlmodel.OutputColumn{{Name: "id"}},
			Parameters:    []sqlmodel.QueryParameter{{Number: 1}},
		}}},
		Result: &app.Result{Files: []sqlmodel.GeneratedFile{{Name: "query.go", Content: []byte("package query")}}},
	}
	require.Empty(t, structuredResultProblems(result, query))
	require.NotEmpty(t, structuredResultProblems(nil, query))
	require.NotEmpty(t, structuredResultProblems(&app.DetailedResult{}, query))
	result.Report.Queries[0].Parameters = nil
	require.NotEmpty(t, structuredResultProblems(result, query))
	result.Report.Queries = nil
	require.NotEmpty(t, structuredResultProblems(result, query))
	result.Report.Queries = []*sqlmodel.AnalysedQuery{{OutputColumns: []sqlmodel.OutputColumn{{Name: "id"}}, Parameters: []sqlmodel.QueryParameter{{Number: 1}}}}
	result.Result.Files = nil
	require.NotEmpty(t, structuredResultProblems(result, query))
}

func TestAliasOracleNormalisesOnlyTheChosenAliases(t *testing.T) {
	original := &sqlmodel.AnalysedQuery{Name: "Q", OutputColumns: []sqlmodel.OutputColumn{{
		Name: "id", SourceTable: "fuzz_items", SourceColumn: "id", SourceQualifier: "i",
	}}}
	changed := *original
	changed.OutputColumns = append([]sqlmodel.OutputColumn(nil), original.OutputColumns...)
	changed.OutputColumns[0].SourceQualifier = "renamed_items"
	require.True(t, sameTransformedQueryMeaning(original, &changed))
	changed.OutputColumns[0].SourceTable = "fuzz_peers"
	require.False(t, sameTransformedQueryMeaning(original, &changed))
	changed.OutputColumns[0].SourceTable = "fuzz_items"
	changed.OutputColumns[0].Nullable = true
	require.False(t, sameTransformedQueryMeaning(original, &changed))
	changed.OutputColumns[0].Nullable = false
	changed.OutputColumns[0].Name = "different"
	require.False(t, sameTransformedQueryMeaning(original, &changed))
	require.Equal(t, "renamed_items", changed.OutputColumns[0].SourceQualifier)
}
