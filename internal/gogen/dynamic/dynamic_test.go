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

package dynamic

import (
	"testing"

	"github.com/stretchr/testify/require"

	"seagull.sh/seagull/internal/sqlmodel"
)

func TestFindSortableParameterReturnsFirstSortable(t *testing.T) {
	t.Parallel()

	found := findSortableParameter(sortableQuery())
	require.NotNil(t, found)
	require.Equal(t, "sort", found.Name)

	withoutSortable := &sqlmodel.AnalysedQuery{Parameters: []sqlmodel.QueryParameter{textParam("id", 1)}}
	require.Nil(t, findSortableParameter(withoutSortable))
}

func sortableQuery() *sqlmodel.AnalysedQuery {
	return &sqlmodel.AnalysedQuery{
		Name:      "SearchPosts",
		Filename:  "posts.sql",
		Command:   sqlmodel.QueryCommandMany,
		IsDynamic: true,
		ReadOnly:  true,
		Parameters: []sqlmodel.QueryParameter{
			textParam("author", 1),
			{
				Name:         "page_size",
				Number:       2,
				SQLType:      sqlmodel.SQLType{Category: sqlmodel.TypeCategoryInteger},
				Context:      sqlmodel.ParameterContextLimit,
				DefaultLimit: new(20),
				MaxLimit:     new(100),
			},
			{
				Name:            "sort",
				SQLType:         sqlmodel.SQLType{Category: sqlmodel.TypeCategoryText},
				Kind:            sqlmodel.ParameterDirectiveSortable,
				SortableColumns: []string{"created_at", "title"},
			},
		},
		OutputColumns: []sqlmodel.OutputColumn{textColumn("id", false), textColumn("title", false)},
		Template: template(
			text("SELECT id, title FROM posts WHERE "),
			clause(sqlmodel.SegmentFilter, text("author = "), parameter(0)),
			text(" "),
			clause(sqlmodel.SegmentOrderBy, text("ORDER BY created_at")),
			text(" "),
			clause(sqlmodel.SegmentPagination, text("LIMIT "), parameter(1)),
			text(";"),
		),
	}
}

func TestSortablePreludeWritesTheOrderByAtItsPlace(t *testing.T) {
	t.Parallel()

	emission := runtimeEmission()
	query := queryFor(t, emission, sortableQuery())

	prelude, err := Prelude(query)

	require.NoError(t, err)
	require.Equal(t, "SELECT id, title FROM posts WHERE author = $1  LIMIT $2;", query.Rendered.SQL)
	require.Equal(t, `if params.PageSize == 0 {
	params.PageSize = 20
}
if params.PageSize < 0 {
	params.PageSize = 0
}
if params.PageSize > 100 {
	params.PageSize = 100
}
query := searchPostsSQL[:46]
switch string(params.Sort) {
case "created_at", "title":
	query = query + (" ORDER BY " + string(params.Sort))
	switch params.SortDirection {
	case OrderAsc, OrderDesc:
		query = query + (" " + string(params.SortDirection))
	}
}
query = query + searchPostsSQL[46:]
`, renderStatements(t, prelude.Statements))
	require.Equal(t, `type SearchPostsParams struct {
	Author		string
	PageSize	int
	Sort		SearchPostsOrderBy
	SortDirection	OrderDirection
}
type SearchPostsOrderBy string
const (
	SearchPostsOrderByCreatedAt	SearchPostsOrderBy	= "created_at"
	SearchPostsOrderByTitle		SearchPostsOrderBy	= "title"
)
`, renderDecls(t, BuildDynamicDeclarations(query)))
}
