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

package gogen

import (
	"cmp"
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/emit"
	"seagull.sh/seagull/internal/emit/emitkit"
	"seagull.sh/seagull/internal/gogen/goast"
	"seagull.sh/seagull/internal/sqlmodel"
)

func listQuery(name, filename, sql string) *sqlmodel.AnalysedQuery {
	return &sqlmodel.AnalysedQuery{
		Name:          name,
		Filename:      filename,
		Command:       sqlmodel.QueryCommandMany,
		Parameters:    []sqlmodel.QueryParameter{{Name: "id", Number: 1}},
		OutputColumns: []sqlmodel.OutputColumn{{Name: "id"}},
		Template: sqlmodel.SQLTemplate{Segments: []sqlmodel.SQLSegment{
			textSegment(sql),
			parameterSegment(0),
		}},
	}
}

func TestQueriesWrittenAlikeAreToldApartByACommentLine(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		lookup      bool
		comments    string
		queries     []*sqlmodel.AnalysedQuery
		expectedSQL map[string]string
	}{
		{
			name:   "the later of two queries written alike opens with a line naming it",
			lookup: true,
			queries: []*sqlmodel.AnalysedQuery{
				listQuery("ListB", "a.sql", "SELECT id FROM users WHERE id > "),
				listQuery("ListA", "a.sql", "SELECT id FROM users WHERE id > "),
			},
			expectedSQL: map[string]string{
				"ListA": "SELECT id FROM users WHERE id > $1",
				"ListB": "-- ListB\nSELECT id FROM users WHERE id > $1",
			},
		},
		{
			name:     "the line is written in the dialect's comment syntax",
			lookup:   true,
			comments: "#",
			queries: []*sqlmodel.AnalysedQuery{
				listQuery("First", "a.sql", "SELECT id FROM users WHERE id > "),
				listQuery("Second", "b.sql", "SELECT id FROM users WHERE id > "),
			},
			expectedSQL: map[string]string{
				"First":  "SELECT id FROM users WHERE id > $1",
				"Second": "# Second\nSELECT id FROM users WHERE id > $1",
			},
		},
		{
			name:   "queries written differently keep their SQL",
			lookup: true,
			queries: []*sqlmodel.AnalysedQuery{
				listQuery("Above", "a.sql", "SELECT id FROM users WHERE id > "),
				listQuery("Below", "a.sql", "SELECT id FROM users WHERE id < "),
			},
			expectedSQL: map[string]string{
				"Above": "SELECT id FROM users WHERE id > $1",
				"Below": "SELECT id FROM users WHERE id < $1",
			},
		},
		{
			name:   "without the lookup queries written alike keep their SQL",
			lookup: false,
			queries: []*sqlmodel.AnalysedQuery{
				listQuery("ListA", "a.sql", "SELECT id FROM users WHERE id > "),
				listQuery("ListB", "a.sql", "SELECT id FROM users WHERE id > "),
			},
			expectedSQL: map[string]string{
				"ListA": "SELECT id FROM users WHERE id > $1",
				"ListB": "SELECT id FROM users WHERE id > $1",
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			info := renderingInfo(dollarForm, dollarForm)
			info.CommentPrefix = cmp.Or(testCase.comments, "--")
			emission := renderingEmission(info, emitkit.ListExpand, nil, testCase.queries...)
			emission.Request.Style.QueryNames = emit.QueryNames{Enabled: testCase.lookup, File: "query_names.go", Function: "QueryName"}

			rendered := make(map[string]string, len(testCase.queries))
			for _, query := range testCase.queries {
				generated, err := emission.Query(query, goast.NewImportTracker())
				require.NoError(t, err)
				rendered[query.Name] = generated.Rendered.SQL
			}
			require.Equal(t, testCase.expectedSQL, rendered)
		})
	}
}

func TestACommentLineMovesTheMarksTheGeneratedCodeEditsAt(t *testing.T) {
	t.Parallel()

	first, second := lookupQuery(), lookupQuery()
	second.Name = "ListUsers"
	info := renderingInfo(dollarForm, dollarForm)
	info.CommentPrefix = "--"
	emission := renderingEmission(info, emitkit.ListExpand, nil, first, second)
	emission.Request.Style.QueryNames = emit.QueryNames{Enabled: true, File: "query_names.go", Function: "QueryName"}

	plain, err := emission.Query(first, goast.NewImportTracker())
	require.NoError(t, err)
	labelled, err := emission.Query(second, goast.NewImportTracker())
	require.NoError(t, err)

	line := "-- ListUsers\n"
	require.Equal(t, line+plain.Rendered.SQL, labelled.Rendered.SQL)
	require.Equal(t, plain.Rendered.Marks.Lists[0].Range.Start+len(line), labelled.Rendered.Marks.Lists[0].Range.Start)
	require.Equal(t, "($2)", labelled.Rendered.SQL[labelled.Rendered.Marks.Lists[0].Range.Start:labelled.Rendered.Marks.Lists[0].Range.End])
}
